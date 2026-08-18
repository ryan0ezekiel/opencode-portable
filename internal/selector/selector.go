// Package selector implements the adaptive runtime selection engine.
//
// The engine does not map OS→binary. Instead it:
//
//	detect host
//	    ↓
//	list candidate runtimes
//	    ↓
//	remove statically incompatible candidates
//	    ↓
//	probe installed candidates (can this actually start?)
//	    ↓
//	score candidates
//	    ↓
//	select best candidate
//
// If the best candidate is not installed, the caller downloads it and probes
// it; on probe failure the next candidate is tried (portable fallback).
package selector

import (
	"os"
	"sort"
	"strings"

	"opencode-portable/internal/detect"
	"opencode-portable/internal/layout"
	"opencode-portable/internal/manifest"
	"opencode-portable/internal/probe"
	"opencode-portable/internal/version"
)

// Candidate is one runtime option for the host platform.
type Candidate struct {
	OS      string
	Arch    string
	Variant manifest.Variant

	// InstalledPath is the on-USB binary path when installed ("" otherwise).
	InstalledPath string
	Installed     bool

	// StaticOK reports whether static compatibility analysis passed.
	StaticOK   bool
	StaticNote string

	// Probe is the execution probe result (nil until probed).
	Probe *probe.Result

	// Score ranks candidates; higher is better.
	Score int
}

// Variant preference weight: native builds are preferred for performance;
// baseline builds target older CPUs; musl builds trade performance for
// libc independence.
func variantWeight(name string) int {
	switch name {
	case "native":
		return 0
	case "baseline":
		return -5
	case "musl":
		return -10
	case "baseline-musl":
		return -15
	}
	return -20
}

// Engine evaluates candidates using host capabilities and the manifest.
type Engine struct {
	Caps     detect.HostCapabilities
	Manifest *manifest.Manifest
	Logf     func(format string, args ...any)
}

func (e *Engine) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

// CandidateError explains that no runtime could be selected.
type CandidateError struct {
	OS   string
	Arch string
}

func (e *CandidateError) Error() string {
	return "no compatible OpenCode runtime could be selected for " + e.OS + "/" + e.Arch
}

// StaticCompatibility evaluates a variant against host capabilities using
// only static information (libc, CPU features). The execution probe remains
// the final authority.
func StaticCompatibility(v manifest.Variant, c detect.HostCapabilities) (bool, string) {
	req := v.Requires
	switch req.Libc {
	case "glibc":
		if c.Libc == "musl" {
			return false, "host libc is musl; glibc build is not compatible"
		}
		if c.Libc == "glibc" && req.MinLibc != "" && CompareVersions(c.LibcVersion, req.MinLibc) < 0 {
			return false, "host glibc " + c.LibcVersion + " is older than required " + req.MinLibc
		}
	case "musl":
		// musl builds are self-contained and run on glibc hosts too.
		if c.Libc == "" {
			// Unknown libc; let the probe decide.
		}
	case "any":
		// Always statically compatible.
	}
	for _, feat := range req.CPU {
		if feat == "avx2" && !c.HasAVX2 && c.Arch == "amd64" {
			return false, "CPU lacks AVX2 required by this build"
		}
	}
	return true, ""
}

// Candidates builds and statically filters the candidate list for the host.
func (e *Engine) Candidates() []*Candidate {
	c := e.Caps
	variants := e.Manifest.RuntimesFor(c.OS, c.Arch)
	out := make([]*Candidate, 0, len(variants))
	for i := range variants {
		v := variants[i]
		ok, note := StaticCompatibility(v, c)
		installed := layout.RuntimeBinary(e.Caps.USB.Root, c.OS, c.Arch, v.Name, v.Binary)
		exists := false
		if _, err := os.Stat(installed); err == nil {
			exists = true
		}
		score := 0
		if ok {
			score += 50
		}
		score += variantWeight(v.Name)
		if exists {
			score += 5
		}
		out = append(out, &Candidate{
			OS:            c.OS,
			Arch:          c.Arch,
			Variant:       v,
			InstalledPath: installed,
			Installed:     exists,
			StaticOK:      ok,
			StaticNote:    note,
			Score:         score,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return variantWeight(out[i].Variant.Name) > variantWeight(out[j].Variant.Name)
	})
	return out
}

// ProbeInstalled probes every installed candidate and updates scores.
// Candidates whose probe succeeds get the definitive compatibility signal.
func (e *Engine) ProbeInstalled(cands []*Candidate) {
	for _, cd := range cands {
		if !cd.Installed {
			continue
		}
		e.logf("probing runtime %s (installed)", cd.Variant.Name)
		res := probe.Probe(cd.InstalledPath)
		cd.Probe = &res
		if res.OK {
			cd.Score += 100
			cd.StaticNote = "execution probe succeeded (version " + res.Version + ")"
			e.logf("runtime %s probe OK (version %s)", cd.Variant.Name, res.Version)
		} else {
			cd.Score -= 200
			cd.StaticNote = "execution probe failed: " + res.Error
			e.logf("runtime %s probe FAILED: %s", cd.Variant.Name, res.Error)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return variantWeight(cands[i].Variant.Name) > variantWeight(cands[j].Variant.Name)
	})
}

// Select returns the best candidate, or a CandidateError when the platform
// itself is unsupported or no candidate exists.
func (e *Engine) Select() (*Candidate, error) {
	c := e.Caps
	if !detect.SupportedOS(c.OS) {
		return nil, &CandidateError{OS: c.OS, Arch: c.Arch}
	}
	if !detect.SupportedArch(c.Arch) {
		return nil, &CandidateError{OS: c.OS, Arch: c.Arch}
	}

	cands := e.Candidates()
	if len(cands) == 0 {
		return nil, &CandidateError{OS: c.OS, Arch: c.Arch}
	}
	e.ProbeInstalled(cands)
	if cands[0].Probe != nil && cands[0].Probe.OK {
		return cands[0], nil
	}
	// No installed candidate works (or none installed). Choose the best
	// statically-compatible candidate; the caller downloads and probes it,
	// falling back to the next if needed.
	for _, cd := range cands {
		if cd.StaticOK && (cd.Probe == nil || !cd.Probe.OK) {
			return cd, nil
		}
	}
	// Everything statically failed; return the best-scoring candidate anyway
	// so the caller can attempt a probe-based fallback chain.
	return cands[0], nil
}

// Selection describes a final selection for diagnostics.
type Selection struct {
	OS      string
	Arch    string
	Version string // OpenCode version from manifest
	Variant string
	Binary  string
	Source  string // "usb" | "to-be-downloaded"
	Reason  string
}

// Describe renders a human-readable selection reason.
func (s *Selection) Describe() string {
	return s.Reason
}

// LauncherVersion returns the bootstrapper version string for diagnostics.
func LauncherVersion() string { return version.String() }

// CompareVersions compares dotted numeric versions; returns -1, 0 or 1.
// Non-numeric suffixes (e.g. "2.31-13") are handled by numeric prefix
// comparison of each dot-separated component. Optional leading "v"
// (GitHub release tags use v1.2.3) is ignored.
func CompareVersions(a, b string) int {
	// Optional leading "v" (GitHub release tags use v1.2.3) is ignored.
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int64
		if i < len(pa) {
			x = parseLeadingInt(pa[i])
		}
		if i < len(pb) {
			y = parseLeadingInt(pb[i])
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func parseLeadingInt(s string) int64 {
	var v int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		v = v*10 + int64(ch-'0')
	}
	return v
}
