// Package update implements verified OpenCode updates for the USB volume.
//
// Update flow:
//
//	check official release
//	    ↓
//	determine required artifacts
//	    ↓
//	download
//	    ↓
//	verify (sha256 from the official release)
//	    ↓
//	install to USB (atomic staging + swap)
//	    ↓
//	probe the new runtime
//	    ↓
//	update manifest (atomic)
//
// If anything fails, the previously working runtime and manifest are kept
// untouched. Config, data and cache directories are never modified.
package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"opencode-portable/internal/acquire"
	"opencode-portable/internal/detect"
	"opencode-portable/internal/layout"
	"opencode-portable/internal/manifest"
	"opencode-portable/internal/probe"
	"opencode-portable/internal/release"
	"opencode-portable/internal/selector"
)

// Result summarizes an update run.
type Result struct {
	OldVersion     string
	NewVersion     string
	Updated        []string // platform keys updated
	Skipped        []string // platform keys skipped (missing artifacts)
	AlreadyCurrent bool
}

// Options controls an update run.
type Options struct {
	// AllPlatforms updates every platform in the manifest; otherwise only
	// the host platform's runtime is updated.
	AllPlatforms bool
	// Quiet suppresses progress output.
	Quiet bool
	// Logf receives progress messages ("" disables).
	Logf func(format string, args ...any)
}

// Run performs the update. root is the USB root; m is the current manifest
// (which is also updated in place on success). caps describes the host and
// determines which installed variants must be verified by execution probe:
// a variant that cannot run on the current host (e.g. a musl build on a
// glibc-only system) is still installed as a fallback for other machines,
// but is not probed.
func Run(ctx context.Context, root string, m *manifest.Manifest, caps detect.HostCapabilities, opts Options) (*Result, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	rel, err := release.FetchLatest(m.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("cannot check for updates: %w", err)
	}

	res := &Result{OldVersion: m.OpenCodeVersion, NewVersion: rel.Tag}
	if selector.CompareVersions(rel.Tag, m.OpenCodeVersion) == 0 {
		res.AlreadyCurrent = true
		return res, nil
	}

	// Build the updated manifest: same artifact names, new URLs and
	// digests from the official release. Start from a full copy of the
	// current runtimes: a host-only update must never drop the entries of
	// the other platforms (a manifest missing platforms is invalid).
	updated := *m
	updated.OpenCodeVersion = rel.Tag
	updated.Runtimes = make(map[string]manifest.PlatformRuntime, len(m.Runtimes))
	for k, v := range m.Runtimes {
		updated.Runtimes[k] = v
	}

	platforms := manifest.SupportedPlatforms()
	if !opts.AllPlatforms {
		hostKey := manifest.PlatformKey(caps.OS, caps.Arch)
		if _, ok := m.Runtimes[hostKey]; !ok {
			return nil, fmt.Errorf("cannot update: host platform %s is not present in the manifest", hostKey)
		}
		platforms = []string{hostKey}
	}

	for _, pk := range platforms {
		pr, ok := m.Runtimes[pk]
		if !ok {
			continue
		}
		newPR := manifest.PlatformRuntime{Variants: make([]manifest.Variant, 0, len(pr.Variants))}
		available := true
		for _, v := range pr.Variants {
			asset := rel.Asset(v.Artifact)
			if asset == nil || asset.SHA256 == "" {
				logf("artifact %s not published for %s (or no checksum); keeping %s", v.Artifact, pk, v.Name)
				available = false
				break
			}
			nv := v
			nv.URL = release.URL(m.SourceRepo, rel.Tag, v.Artifact)
			nv.SHA256 = asset.SHA256
			nv.Size = asset.Size
			newPR.Variants = append(newPR.Variants, nv)
		}
		if !available {
			res.Skipped = append(res.Skipped, pk)
			updated.Runtimes[pk] = pr // keep old entry for this platform
			continue
		}
		updated.Runtimes[pk] = newPR
	}

	// Download + install only the platforms requested.
	targets := platforms
	if !opts.AllPlatforms {
		targets = []string{manifest.PlatformKey(caps.OS, caps.Arch)}
	}
	dl := acquire.NewDownloader()
	dl.Quiet = opts.Quiet
	dl.Logf = logf

	for _, pk := range targets {
		// Skipped platforms keep their old entries, which are already
		// installed; downloading them again would be pointless.
		if contains(res.Skipped, pk) {
			continue
		}
		osName, arch := splitKey(pk)
		pr := updated.Runtimes[pk]
		// Only the variant this host would actually use must pass an
		// execution probe. The others are fallbacks for other machines:
		// their integrity is guaranteed by digest verification alone.
		probeName := ""
		if pk == manifest.PlatformKey(caps.OS, caps.Arch) {
			for _, v := range pr.Variants {
				if ok, _ := selector.StaticCompatibility(v, caps); ok {
					probeName = v.Name
					break
				}
			}
			// Defensive: if static analysis rules out every variant (e.g.
			// an unusual host), probe the first one anyway rather than
			// installing an unverified runtime.
			if probeName == "" && len(pr.Variants) > 0 {
				probeName = pr.Variants[0].Name
			}
		}
		for _, v := range pr.Variants {
			if err := installVariant(ctx, dl, root, osName, arch, v, v.Name == probeName, logf); err != nil {
				return nil, fmt.Errorf("update failed for %s/%s (%s); previous version kept: %w", osName, arch, v.Name, err)
			}
		}
		res.Updated = append(res.Updated, pk)
	}

	// If nothing was actually updated (all requested artifacts unavailable
	// in the new release), leave the manifest untouched: bumping the
	// version while keeping old URLs would claim an update that did not
	// happen.
	if len(res.Updated) == 0 {
		res.NewVersion = res.OldVersion
		return res, nil
	}

	// Manifest swap is the commit point: only after all installs succeeded.
	if err := updated.Save(layout.ManifestPath(root)); err != nil {
		return nil, fmt.Errorf("cannot save updated manifest: %w", err)
	}
	return res, nil
}

// installVariant downloads, verifies, extracts and swaps a runtime variant
// atomically. On probe failure of the new binary the previous one is
// restored. probeThis indicates the variant that will actually be used on
// the current host; only it is verified by execution probe. Other variants
// are installed digest-verified as fallbacks for other machines.
func installVariant(ctx context.Context, dl *acquire.Downloader, root, osName, arch string, v manifest.Variant, probeThis bool, logf func(string, ...any)) error {
	artifactPath := filepath.Join(root, layout.DownloadsDir, v.Artifact)
	if err := dl.Download(ctx, v.URL, artifactPath, v.Size, v.SHA256); err != nil {
		return err
	}

	target := layout.RuntimeDir(root, osName, arch, v.Name)
	staging := target + ".staging"
	backup := target + ".backup"

	// Stage the new variant in a fresh directory.
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := acquire.Extract(artifactPath, v.Archive, staging); err != nil {
		return err
	}
	bin := filepath.Join(staging, v.Binary)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("extracted archive missing binary %s", v.Binary)
	}
	_ = os.Chmod(bin, 0o755)

	// Probe before swapping — only the variant this host would use.
	if probeThis {
		logf("verifying new runtime %s (%s)", v.Name, v.Artifact)
		res := probe.Probe(bin)
		if !res.OK {
			_ = os.RemoveAll(staging)
			return fmt.Errorf("new runtime %s failed verification probe: %s", v.Name, res.Error)
		}
	} else {
		logf("installing %s (%s) as fallback without probe", v.Name, v.Artifact)
	}

	// Atomic swap.
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		// Restore the previous version.
		if _, berr := os.Stat(backup); berr == nil {
			_ = os.Rename(backup, target)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func splitKey(pk string) (string, string) {
	for i := 0; i < len(pk); i++ {
		if pk[i] == '/' {
			return pk[:i], pk[i+1:]
		}
	}
	return pk, ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
