// Package diag renders the --diagnose and --dry-run reports.
//
// The reports contain only paths, versions and detection results — never
// credentials, tokens or configuration values.
package diag

import (
	"fmt"
	"io"
	"strings"

	"opencode-portable/internal/detect"
	"opencode-portable/internal/manifest"
	"opencode-portable/internal/selector"
)

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Host renders the Host section of a diagnose report.
func Host(w io.Writer, c detect.HostCapabilities) {
	fmt.Fprintln(w, "Host:")
	fmt.Fprintf(w, "  OS:          %s\n", c.OS)
	fmt.Fprintf(w, "  Architecture: %s\n", c.Arch)
	if c.OSVersion != "" {
		fmt.Fprintf(w, "  OS version:  %s\n", c.OSVersion)
	}
	if c.Kernel != "" {
		fmt.Fprintf(w, "  Kernel:      %s\n", c.Kernel)
	}
	if c.Libc != "" {
		fmt.Fprintf(w, "  libc:        %s %s\n", c.Libc, c.LibcVersion)
	} else {
		fmt.Fprintf(w, "  libc:        n/a\n")
	}
}

// Hardware renders the Hardware section.
func Hardware(w io.Writer, c detect.HostCapabilities) {
	fmt.Fprintln(w, "\nHardware:")
	fmt.Fprintf(w, "  RAM:         %s\n", humanBytes(c.RAMBytes))
	fmt.Fprintf(w, "  CPU:         %s\n", orUnknown(c.CPUModel))
	if c.HasAVX2 || c.Arch != "amd64" {
		fmt.Fprintf(w, "  AVX2:        %s\n", yesNo(c.HasAVX2))
	}
	fmt.Fprintf(w, "  GPU:         %s\n", orUnknown(c.GPU))
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

// Tools renders the host tools section.
func Tools(w io.Writer, c detect.HostCapabilities) {
	fmt.Fprintln(w, "\nTools:")
	row := func(name, ver string, has bool) {
		if has {
			fmt.Fprintf(w, "  %-10s %s (host)\n", name+":", ver)
		} else {
			fmt.Fprintf(w, "  %-10s not found on host\n", name+":")
		}
	}
	row("Git", c.GitVersion, c.HasGit)
	row("ripgrep", c.RipgrepVersion, c.HasRipgrep)
	row("Node", c.NodeVersion, c.HasNode)
	row("Bun", c.BunVersion, c.HasBun)
}

// USB renders the USB section.
func USB(w io.Writer, c detect.HostCapabilities) {
	fmt.Fprintln(w, "\nUSB:")
	fmt.Fprintf(w, "  Path:        %s\n", c.USB.Root)
	fmt.Fprintf(w, "  Filesystem:  %s\n", orUnknown(c.USB.Filesystem))
	fmt.Fprintf(w, "  Writable:    %s\n", yesNo(c.USB.Writable))
	fmt.Fprintf(w, "  Executable:  %s\n", yesNo(c.USB.Executable))
	if len(c.USB.MountOptions) > 0 {
		fmt.Fprintf(w, "  Mount opts:  %s\n", strings.Join(c.USB.MountOptions, ","))
	}
}

// Runtimes renders the runtime candidate section.
func Runtimes(w io.Writer, m *manifest.Manifest, cands []*selector.Candidate) {
	fmt.Fprintln(w, "\nRuntimes:")
	fmt.Fprintf(w, "  OpenCode version: %s (source: %s)\n", m.OpenCodeVersion, m.SourceRepo)
	if len(cands) == 0 {
		fmt.Fprintln(w, "  (no candidates for this platform)")
		return
	}
	for _, cd := range cands {
		state := "not installed"
		if cd.Installed {
			state = "installed"
			if cd.Probe != nil {
				if cd.Probe.OK {
					state = "installed, probe OK (" + cd.Probe.Version + ")"
				} else {
					state = "installed, probe FAILED"
				}
			}
		}
		line := fmt.Sprintf("  %-8s %-14s %s", cd.OS+"/"+cd.Arch, cd.Variant.Name, state)
		if !cd.StaticOK {
			line += " [static: " + cd.StaticNote + "]"
		}
		fmt.Fprintln(w, line)
	}
}

// Selection renders the selected runtime section.
func Selection(w io.Writer, s *selector.Selection, reason string) {
	fmt.Fprintln(w, "\nSelected runtime:")
	if s == nil {
		fmt.Fprintln(w, "  none")
		return
	}
	fmt.Fprintf(w, "  %s/%s %s (OpenCode %s)\n", s.OS, s.Arch, s.Variant, s.Version)
	fmt.Fprintf(w, "  Source: %s\n", s.Source)
	fmt.Fprintf(w, "  Reason: %s\n", reason)
}

// Environment renders the portable environment section.
func Environment(w io.Writer, lines []string) {
	fmt.Fprintln(w, "\nEnvironment:")
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// Errors renders failure diagnostics.
func Errors(w io.Writer, errs []string) {
	if len(errs) == 0 {
		return
	}
	fmt.Fprintln(w, "\nProblems:")
	for _, e := range errs {
		fmt.Fprintf(w, "  ! %s\n", e)
	}
}
