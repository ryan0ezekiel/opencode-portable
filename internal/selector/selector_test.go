package selector

import (
	"testing"

	"opencode-portable/internal/detect"
	"opencode-portable/internal/manifest"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.18.18", "1.18.18", 0},
		{"1.18.18", "1.18.17", 1},
		{"1.18.17", "1.18.18", -1},
		{"1.18.18", "1.18.2", 1}, // numeric, not lexicographic
		{"1.18.18", "1.19.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"v1.18.18", "1.18.18", 0}, // optional v prefix
		{"1.18.18", "1.18.18.0", 0},
		{"0.9.0", "0.10.0", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVariantWeightOrder(t *testing.T) {
	// native must always beat baseline, which beats musl, which beats
	// baseline-musl (higher weight is better).
	order := []string{"native", "baseline", "musl", "baseline-musl"}
	for i := 0; i+1 < len(order); i++ {
		if variantWeight(order[i]) <= variantWeight(order[i+1]) {
			t.Errorf("variantWeight(%q)=%d should be > variantWeight(%q)=%d",
				order[i], variantWeight(order[i]), order[i+1], variantWeight(order[i+1]))
		}
	}
}

func TestStaticCompatibility(t *testing.T) {
	base := detect.HostCapabilities{
		OS:          "linux",
		Arch:        "amd64",
		Libc:        "glibc",
		LibcVersion: "2.44",
		HasAVX2:     true,
	}
	mk := func(libc, minLibc string, cpu []string) manifest.Variant {
		return manifest.Variant{
			Name: "x",
			Requires: manifest.Requires{
				Libc:    libc,
				MinLibc: minLibc,
				CPU:     cpu,
			},
		}
	}

	cases := []struct {
		name string
		host detect.HostCapabilities
		v    manifest.Variant
		ok   bool
	}{
		{"glibc native ok", base, mk("glibc", "2.17", []string{"avx2"}), true},
		{"older glibc than required", detect.HostCapabilities{OS: "linux", Arch: "amd64", Libc: "glibc", LibcVersion: "2.16", HasAVX2: true}, mk("glibc", "2.17", nil), false},
		{"musl host needs musl build", detect.HostCapabilities{OS: "linux", Arch: "amd64", Libc: "musl", LibcVersion: "1.2", HasAVX2: true}, mk("glibc", "2.17", nil), false},
		{"musl build on glibc host ok", base, mk("musl", "", nil), true},
		{"avx2 missing", detect.HostCapabilities{OS: "linux", Arch: "amd64", Libc: "glibc", LibcVersion: "2.44", HasAVX2: false}, mk("glibc", "2.17", []string{"avx2"}), false},
		{"no cpu requirement on old cpu", detect.HostCapabilities{OS: "linux", Arch: "amd64", Libc: "glibc", LibcVersion: "2.44", HasAVX2: false}, mk("glibc", "2.17", nil), true},
		{"macos has no libc requirement", detect.HostCapabilities{OS: "darwin", Arch: "arm64"}, mk("", "", nil), true},
	}
	for _, c := range cases {
		ok, _ := StaticCompatibility(c.v, c.host)
		if ok != c.ok {
			t.Errorf("%s: StaticCompatibility = %v, want %v", c.name, ok, c.ok)
		}
	}
}
