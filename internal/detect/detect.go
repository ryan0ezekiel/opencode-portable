// Package detect gathers information about the host machine that is relevant
// to runtime compatibility and tool selection. Detection is intentionally
// focused: only information that influences decisions is collected, and no
// detection step is allowed to block startup (failures degrade to "unknown").
package detect

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"opencode-portable/internal/usb"
)

// HostCapabilities describes the host environment as observed by the
// launcher. Every field is informational; the runtime selector decides what
// the values mean.
type HostCapabilities struct {
	OS          string // windows | linux | darwin
	Arch        string // amd64 | arm64
	OSVersion   string
	Kernel      string // linux kernel release
	Libc        string // glibc | musl | unknown ("" on windows/darwin)
	LibcVersion string
	RAMBytes    uint64

	CPUModel string
	HasAVX2  bool
	GPU      string

	HasGit         bool
	GitVersion     string
	HasRipgrep     bool
	RipgrepVersion string
	HasNode        bool
	NodeVersion    string
	HasBun         bool
	BunVersion     string

	USB usb.Info
}

// SupportedOS reports whether the given OS is a supported target.
func SupportedOS(os string) bool {
	switch os {
	case "windows", "linux", "darwin":
		return true
	}
	return false
}

// SupportedArch reports whether the given architecture is a supported target.
func SupportedArch(arch string) bool {
	switch arch {
	case "amd64", "arm64":
		return true
	}
	return false
}

var (
	gitVerRe  = regexp.MustCompile(`git version (\S+)`)
	rgVerRe   = regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	nodeVerRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)
	bunVerRe  = regexp.MustCompile(`(\d+\.\d+\.\d+)`)
)

// Detect gathers all host capabilities. It never returns an error for
// informational failures; only structural failures (e.g. cannot determine
// the launcher's own location) are errors.
func Detect(usbInfo usb.Info) (HostCapabilities, error) {
	c := HostCapabilities{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
		USB:  usbInfo,
	}

	// Platform-specific: OS version, kernel, libc, RAM, CPU, GPU.
	c.detectPlatform()

	// Host tool detection: versions only; presence is not required.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	detectTool(ctx, &c.HasGit, &c.GitVersion, gitVerRe, "git", "--version")
	detectTool(ctx, &c.HasRipgrep, &c.RipgrepVersion, rgVerRe, "rg", "--version")
	detectTool(ctx, &c.HasNode, &c.NodeVersion, nodeVerRe, "node", "--version")
	detectTool(ctx, &c.HasBun, &c.BunVersion, bunVerRe, "bun", "--version")

	return c, nil
}

// detectTool runs a version probe for an optional host tool. Failures simply
// leave the booleans false.
func detectTool(ctx context.Context, has *bool, ver *string, re *regexp.Regexp, name string, args ...string) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	s := strings.TrimSpace(string(out))
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		*has = true
		*ver = m[1]
	}
}
