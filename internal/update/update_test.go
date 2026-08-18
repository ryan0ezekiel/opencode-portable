package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-portable/internal/detect"
	"opencode-portable/internal/layout"
	"opencode-portable/internal/manifest"
)

const (
	testOldSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testNewSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// testArtifact builds a tar.gz containing an executable "opencode" script
// that answers --version, so the variant passes an execution probe.
func testArtifact(t *testing.T, version string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := "#!/bin/sh\nprintf 'opencode " + version + "\\n'\n"
	if err := tw.WriteHeader(&tar.Header{Name: "opencode", Mode: 0o755, Size: int64(len(script))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func mkVariant(name string) manifest.Variant {
	return manifest.Variant{
		Name:     name,
		Artifact: "opencode-linux-x64" + variantSuffix(name) + ".tar.gz",
		URL:      "https://github.com/test/repo/releases/download/v1.17.0/opencode-linux-x64" + variantSuffix(name) + ".tar.gz",
		SHA256:   testOldSHA,
		Size:     42,
		Archive:  "tar.gz",
		Binary:   "opencode",
		Requires: manifest.Requires{Libc: "glibc", MinLibc: "2.17"},
	}
}

func variantSuffix(name string) string {
	switch name {
	case "baseline":
		return "-baseline"
	case "musl":
		return "-musl"
	case "baseline-musl":
		return "-baseline-musl"
	}
	return ""
}

// mkManifest builds a full 6-platform manifest at the given version.
func mkManifest(version string) *manifest.Manifest {
	m := &manifest.Manifest{
		SchemaVersion:   manifest.SchemaVersion,
		SourceRepo:      "test/repo",
		OpenCodeVersion: version,
		Runtimes:        map[string]manifest.PlatformRuntime{},
	}
	for _, pk := range manifest.SupportedPlatforms() {
		osName, arch := splitKey(pk)
		var variants []manifest.Variant
		if osName == "linux" && arch == "amd64" {
			for _, n := range []string{"native", "baseline", "musl", "baseline-musl"} {
				variants = append(variants, mkVariant(n))
			}
		} else {
			variants = append(variants, mkVariant("native"))
		}
		m.Runtimes[pk] = manifest.PlatformRuntime{Variants: variants}
	}
	return m
}

// newUpdateServer serves a fake GitHub-shaped release and the artifacts it
// publishes, returning the server and the artifact bytes per name.
func newUpdateServer(t *testing.T, tag string, artifactNames []string) (*httptest.Server, map[string][]byte) {
	t.Helper()
	artifacts := map[string][]byte{}
	for _, name := range artifactNames {
		b, _ := testArtifact(t, strings.TrimPrefix(tag, "v"))
		artifacts[name] = b
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var assets []map[string]any
		for name, b := range artifacts {
			sum := sha256.Sum256(b)
			assets = append(assets, map[string]any{
				"name":                 name,
				"size":                 len(b),
				"digest":               "sha256:" + hex.EncodeToString(sum[:]),
				"browser_download_url": "https://example.invalid/" + name,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": assets})
	})
	mux.HandleFunc("/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		b, ok := artifacts[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	t.Setenv("OPENCODE_PORTABLE_RELEASE_URL", srv.URL+"/releases/latest")
	return srv, artifacts
}

func linuxAMD64Caps() detect.HostCapabilities {
	return detect.HostCapabilities{
		OS:          "linux",
		Arch:        "amd64",
		Libc:        "glibc",
		LibcVersion: "2.44",
		HasAVX2:     true,
	}
}

func TestUpdateHostOnlyPreservesOtherPlatforms(t *testing.T) {
	root := t.TempDir()
	m := mkManifest("1.17.0")
	names := []string{
		"opencode-linux-x64.tar.gz",
		"opencode-linux-x64-baseline.tar.gz",
		"opencode-linux-x64-musl.tar.gz",
		"opencode-linux-x64-baseline-musl.tar.gz",
	}
	newUpdateServer(t, "v1.18.18", names)

	res, err := Run(context.Background(), root, m, linuxAMD64Caps(), Options{})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.AlreadyCurrent {
		t.Fatal("update should not be already-current")
	}
	if len(res.Updated) != 1 || res.Updated[0] != "linux/amd64" {
		t.Fatalf("updated = %v, want [linux/amd64]", res.Updated)
	}
	if res.NewVersion != "v1.18.18" {
		t.Fatalf("new version = %q, want v1.18.18", res.NewVersion)
	}

	// The saved manifest must still validate (all 6 platforms present)
	// and only the host platform must point at the new version.
	saved, err := manifest.Load(layout.ManifestPath(root))
	if err != nil {
		t.Fatalf("saved manifest invalid: %v", err)
	}
	if saved.OpenCodeVersion != "v1.18.18" {
		t.Fatalf("saved version = %q", saved.OpenCodeVersion)
	}
	host := saved.Runtimes["linux/amd64"]
	if len(host.Variants) != 4 {
		t.Fatalf("host variants = %d, want 4", len(host.Variants))
	}
	for _, v := range host.Variants {
		if v.SHA256 == testOldSHA || v.SHA256 == "" {
			t.Errorf("host variant %s not updated (sha %s)", v.Name, v.SHA256)
		}
		if !strings.Contains(v.URL, "v1.18.18") {
			t.Errorf("host variant %s URL not updated: %s", v.Name, v.URL)
		}
	}
	other := saved.Runtimes["windows/amd64"]
	if len(other.Variants) == 0 {
		t.Fatal("windows/amd64 entry lost by host-only update")
	}
	for _, v := range other.Variants {
		if v.SHA256 != testOldSHA {
			t.Errorf("non-host variant %s must keep its old entry (sha %s)", v.Name, v.SHA256)
		}
		if !strings.Contains(v.URL, "v1.17.0") {
			t.Errorf("non-host variant %s must keep old URL, got %s", v.Name, v.URL)
		}
	}

	// The host runtime must be installed and executable.
	bin := layout.RuntimeBinary(root, "linux", "amd64", "native", "opencode")
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("installed native runtime missing: %v", err)
	}
	if st.Mode()&0o111 == 0 {
		t.Fatal("installed native runtime not executable")
	}
}

func TestUpdateAlreadyCurrentIgnoresVPrefix(t *testing.T) {
	root := t.TempDir()
	m := mkManifest("v1.18.18")                  // manifest with v-prefix...
	srv, _ := newUpdateServer(t, "1.18.18", nil) // ...release without

	res, err := Run(context.Background(), root, m, linuxAMD64Caps(), Options{})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.AlreadyCurrent {
		t.Fatalf("AlreadyCurrent = false, want true (v-prefix must be ignored)")
	}
	if res.NewVersion != "1.18.18" {
		t.Fatalf("NewVersion = %q", res.NewVersion)
	}
	// No downloads must have happened; the manifest must not be written.
	if _, err := os.Stat(layout.ManifestPath(root)); !os.IsNotExist(err) {
		t.Fatal("already-current update must not touch the manifest")
	}
	_ = srv
}

func TestUpdateNothingPublishedLeavesManifestUntouched(t *testing.T) {
	root := t.TempDir()
	m := mkManifest("1.17.0")
	// New release with no published assets at all.
	newUpdateServer(t, "v2.0.0", nil)

	res, err := Run(context.Background(), root, m, linuxAMD64Caps(), Options{})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(res.Updated) != 0 {
		t.Fatalf("updated = %v, want none", res.Updated)
	}
	if res.NewVersion != res.OldVersion {
		t.Fatalf("version must not advance when nothing was updated: %q → %q", res.OldVersion, res.NewVersion)
	}
	if _, err := os.Stat(layout.ManifestPath(root)); !os.IsNotExist(err) {
		t.Fatal("no-op update must not write a manifest")
	}
}

func TestUpdateMissingHostPlatformFails(t *testing.T) {
	root := t.TempDir()
	m := mkManifest("1.17.0")
	delete(m.Runtimes, "linux/amd64")
	newUpdateServer(t, "v1.18.18", nil)

	_, err := Run(context.Background(), root, m, linuxAMD64Caps(), Options{})
	if err == nil {
		t.Fatal("expected error for missing host platform")
	}
	if !strings.Contains(err.Error(), "not present in the manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}
