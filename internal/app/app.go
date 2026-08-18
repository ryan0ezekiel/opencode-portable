// Package app orchestrates the launcher: locate USB, detect host, load
// manifest, select runtime, construct the portable environment and launch
// OpenCode with forwarded arguments.
package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"opencode-portable/internal/acquire"
	"opencode-portable/internal/detect"
	"opencode-portable/internal/diag"
	"opencode-portable/internal/environ"
	"opencode-portable/internal/launch"
	"opencode-portable/internal/layout"
	"opencode-portable/internal/logx"
	"opencode-portable/internal/manifest"
	"opencode-portable/internal/probe"
	"opencode-portable/internal/selector"
	"opencode-portable/internal/update"
	"opencode-portable/internal/usb"
	"opencode-portable/internal/version"
)

//go:embed default.json
var defaultManifestJSON []byte

// Options are the launcher's own command-line options.
type Options struct {
	Diagnose  bool
	DryRun    bool
	Update    bool
	UpdateAll bool
	Verbose   bool
	// Forward holds all remaining arguments to be passed to OpenCode.
	Forward []string
}

// ParseArgs extracts launcher options from argv. Only the documented
// launcher flags are intercepted; everything else (including --help) is
// forwarded to OpenCode unchanged.
func ParseArgs(args []string) Options {
	var o Options
	rest := make([]string, 0, len(args))
	seenDashDash := false
	for _, a := range args {
		if seenDashDash {
			rest = append(rest, a)
			continue
		}
		switch a {
		case "--":
			seenDashDash = true
		case "--diagnose":
			o.Diagnose = true
		case "--dry-run":
			o.DryRun = true
		case "--update":
			o.Update = true
		case "--all-platforms":
			o.UpdateAll = true
		case "--verbose":
			o.Verbose = true
		default:
			rest = append(rest, a)
		}
	}
	o.Forward = rest
	return o
}

// portableConfig is the launcher's own configuration (config/portable.json).
type portableConfig struct {
	ToolPolicy string `json:"tool_policy"`
	LogLevel   string `json:"log_level"`
}

func defaultPortableConfig() portableConfig {
	return portableConfig{ToolPolicy: "prefer_host_fallback_usb", LogLevel: "info"}
}

func loadPortableConfig(root string) portableConfig {
	cfg := defaultPortableConfig()
	b, err := os.ReadFile(layout.PortableConfigPath(root))
	if err != nil {
		return cfg
	}
	var raw portableConfig
	if json.Unmarshal(b, &raw) != nil {
		return cfg
	}
	if raw.ToolPolicy != "" {
		cfg.ToolPolicy = raw.ToolPolicy
	}
	if raw.LogLevel != "" {
		cfg.LogLevel = raw.LogLevel
	}
	return cfg
}

func (c portableConfig) validate() error {
	switch c.ToolPolicy {
	case "prefer_host_fallback_usb", "usb_only", "host_only":
	default:
		return fmt.Errorf("unknown tool_policy %q (expected prefer_host_fallback_usb, usb_only or host_only)", c.ToolPolicy)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unknown log_level %q (expected debug, info, warn or error)", c.LogLevel)
	}
	return nil
}

func parseLogLevel(s string) logx.Level {
	switch s {
	case "debug":
		return logx.LevelDebug
	case "warn":
		return logx.LevelWarn
	case "error":
		return logx.LevelError
	}
	return logx.LevelInfo
}

// Run executes the launcher and returns the process exit code.
func Run(args []string) int {
	opts := ParseArgs(args)

	// 1. Locate the USB root from the launcher's own position.
	info, err := usb.Locate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "OpenCode Portable: "+err.Error())
		return 1
	}
	layout.EnsureDirs(info.Root)

	// 2. Launcher config (config/portable.json). Loaded before logging so
	// that a configured log_level takes effect.
	cfg := loadPortableConfig(info.Root)

	// 3. Logging.
	verbose := opts.Verbose || os.Getenv("OPENCODE_PORTABLE_VERBOSE") == "1"
	logger := logx.New(filepath.Join(info.Root, layout.LogsDir), parseLogLevel(cfg.LogLevel), verbose)
	defer logger.Close()
	logger.Info("OpenCode Portable %s starting (os=%s arch=%s root=%s)", version.String(), runtime.GOOS, runtime.GOARCH, info.Root)
	if verr := cfg.validate(); verr != nil {
		logger.Warn("config problem: %v", verr)
	}

	// 4. Manifest: USB-local wins; embedded defaults as fallback.
	m, manifestSource := loadManifest(info.Root, logger)
	logger.Info("manifest loaded from %s (opencode %s)", manifestSource, m.OpenCodeVersion)

	// 5. Host detection.
	caps, err := detect.Detect(info)
	if err != nil {
		userError(logger, opts.Verbose, "OpenCode Portable: host detection failed: "+err.Error())
		return 1
	}
	logger.Info("host: %s/%s libc=%s %s kernel=%s", caps.OS, caps.Arch, caps.Libc, caps.LibcVersion, caps.Kernel)

	// 6. --diagnose: report and exit before any selection.
	if opts.Diagnose {
		return runDiagnose(caps, m, info, cfg)
	}

	// 7. --update.
	if opts.Update {
		return runUpdate(info, m, caps, opts, logger)
	}

	// 8. Unsupported platform: clean error, never a stack trace.
	if !detect.SupportedOS(caps.OS) || !detect.SupportedArch(caps.Arch) {
		userError(logger, opts.Verbose, unsupportedMessage(caps.OS, caps.Arch))
		return 1
	}

	// 9. noexec USB: explain, do not circumvent.
	if err := info.CheckExecutable(); err != nil {
		userError(logger, opts.Verbose, noexecMessage(err))
		return 1
	}

	// 10. Runtime selection with fallback chain.
	engine := &selector.Engine{
		Caps:     caps,
		Manifest: m,
		Logf:     func(f string, a ...any) { logger.Info(f, a...) },
	}
	cands := engine.Candidates()
	engine.ProbeInstalled(cands)

	var chosen *selector.Candidate
	if opts.DryRun {
		// --dry-run must not modify the USB: no downloads, no installs,
		// no manifest persistence. Report the plan only.
		chosen = pickCandidate(cands)
	} else {
		var perr error
		chosen, perr = ensureRuntime(caps, cands, logger)
		if perr != nil {
			fmt.Fprintln(os.Stderr, perr.Error())
			logger.Error("runtime selection failed: %v", perr)
			return 1
		}
	}
	logger.Info("selected runtime: %s/%s %s (opencode %s)", chosen.OS, chosen.Arch, chosen.Variant.Name, m.OpenCodeVersion)

	// Persist the manifest to the USB after a successful first run so
	// subsequent launches (and updates) use the USB-local copy. Never in
	// dry-run mode.
	if manifestSource == "embedded default" && info.Writable && !opts.DryRun {
		if err := m.Save(layout.ManifestPath(info.Root)); err != nil {
			logger.Warn("could not persist manifest to USB: %v", err)
		} else {
			logger.Info("manifest persisted to %s", layout.ManifestPath(info.Root))
		}
	}

	// 11. Read-only USB handling: run but explain what cannot persist.
	if !info.Writable {
		msg := "OpenCode Portable: the USB volume is read-only. OpenCode will run, but config, data and cache cannot be persisted to this volume and will be lost when it exits."
		logger.Warn("%s", msg)
		if !opts.Verbose {
			fmt.Fprintln(os.Stderr, msg)
		}
	}

	// 12. Tool selection (host-first, USB fallback, never install).
	toolDirs := selectTools(caps, m, info.Root, cfg, logger)
	if len(toolDirs) > 0 {
		logger.Info("USB tools added to PATH: %s", strings.Join(toolDirs, string(os.PathListSeparator)))
	}

	// 13. Portable environment.
	pe := environ.New(info.Root)
	env := pe.Build(os.Environ(), toolDirs)

	// 14. --dry-run: report the plan, do not launch.
	if opts.DryRun {
		return runDryRun(caps, m, chosen, pe, toolDirs, env)
	}

	// 15. Launch OpenCode with forwarded arguments.
	logger.Info("launching %s with args %v", chosen.InstalledPath, opts.Forward)
	code, err := launch.Exec(chosen.InstalledPath, chosen.Variant.Binary, opts.Forward, env)
	if err != nil {
		userError(logger, opts.Verbose, "OpenCode Portable: failed to launch OpenCode: "+err.Error())
		return 1
	}
	return code
}

// pickCandidate selects the runtime a real run would use without acquiring
// anything: a verified installed runtime first, then the best statically
// compatible candidate. Used by --dry-run so the report is truthful and the
// USB is left untouched.
func pickCandidate(cands []*selector.Candidate) *selector.Candidate {
	for _, cd := range cands {
		if cd.Probe != nil && cd.Probe.OK {
			return cd
		}
	}
	for _, cd := range cands {
		if cd.StaticOK {
			return cd
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return nil
}

// userError logs an error to the log file and prints it to stderr exactly
// once (the verbose logger already mirrors to stderr).
func userError(logger *logx.Logger, verbose bool, msg string) {
	logger.Error("%s", msg)
	if !verbose {
		fmt.Fprintln(os.Stderr, msg)
	}
}

// loadManifest loads the USB-local manifest or falls back to the embedded
// default generated from the official release.
func loadManifest(root string, logger *logx.Logger) (*manifest.Manifest, string) {
	path := layout.ManifestPath(root)
	m, err := manifest.Load(path)
	if err == nil {
		return m, path
	}
	if !os.IsNotExist(err) {
		logger.Warn("USB manifest unusable (%v); using embedded default manifest", err)
	}
	m, err = manifest.Parse(defaultManifestJSON)
	if err != nil {
		// Cannot happen for a build-time generated default; treat as fatal.
		fmt.Fprintln(os.Stderr, "OpenCode Portable: embedded manifest is invalid: "+err.Error())
		os.Exit(1)
	}
	return m, "embedded default"
}

func unsupportedMessage(osName, arch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "OpenCode Portable does not support this platform.\n\n")
	fmt.Fprintf(&b, "Detected platform: %s/%s\n", osName, arch)
	fmt.Fprintln(&b, "\nSupported platforms:")
	for _, pk := range manifest.SupportedPlatforms() {
		fmt.Fprintf(&b, "  %s\n", pk)
	}
	fmt.Fprintln(&b, "\nThis launcher is compatible with the supported platforms listed above; please use the matching launcher build.")
	return b.String()
}

func noexecMessage(err error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", err)
	fmt.Fprintln(&b, "OpenCode Portable cannot execute binaries directly from this mount, and it will not attempt to circumvent this security restriction.")
	fmt.Fprintln(&b, "\nLegitimate alternatives:")
	fmt.Fprintln(&b, "  1. Remount the volume with execution enabled (mount -o remount,exec <device> <mountpoint>)")
	fmt.Fprintln(&b, "  2. Move the OpenCodePortable folder onto a volume that permits execution")
	return b.String()
}

// ensureRuntime walks the candidate list in priority order, acquiring and
// probing runtimes until one works. This implements the portable fallback:
// a native runtime that fails to execute falls back to the next candidate.
func ensureRuntime(caps detect.HostCapabilities, cands []*selector.Candidate, logger *logx.Logger) (*selector.Candidate, error) {
	var problems []string
	firstRun := true
	for _, cd := range cands {
		if cd.Probe != nil && cd.Probe.OK {
			return cd, nil
		}
		if firstRun && !cd.Installed && caps.USB.Writable {
			fmt.Fprintln(os.Stderr, "OpenCode Portable")
			fmt.Fprintln(os.Stderr, "Preparing first run...")
			firstRun = false
		}
		acquireNow(caps, cd, logger)
		if cd.Probe != nil && cd.Probe.OK {
			return cd, nil
		}
		if cd.Probe != nil {
			problems = append(problems, fmt.Sprintf("%s: probe failed (%s)", cd.Variant.Name, cd.Probe.Error))
		} else {
			problems = append(problems, fmt.Sprintf("%s: not available", cd.Variant.Name))
		}
	}

	// Nothing worked. Explain, including a hint about the USB state.
	reason := "no compatible OpenCode runtime is available"
	if len(problems) > 0 {
		reason = strings.Join(problems, "; ")
	}
	if !caps.USB.Writable {
		reason += ". The USB volume is read-only, so the runtime could not be prepared. Run the launcher from a writable USB volume, or prepare the USB on another machine first (see README.txt)."
	}
	return nil, fmt.Errorf("OpenCode Portable: %s", reason)
}

func acquireNow(caps detect.HostCapabilities, cd *selector.Candidate, logger *logx.Logger) {
	if cd.Probe != nil && cd.Probe.OK {
		return
	}
	if !caps.USB.Writable {
		logger.Warn("cannot acquire %s: USB volume is read-only", cd.Variant.Name)
		return
	}
	logger.Info("acquiring runtime %s from %s", cd.Variant.Name, cd.Variant.URL)

	dl := acquire.NewDownloader()
	dl.Logf = func(f string, a ...any) { logger.Info(f, a...) }
	artifactPath := filepath.Join(caps.USB.Root, layout.DownloadsDir, cd.Variant.Artifact)

	if err := dl.Download(context.Background(), cd.Variant.URL, artifactPath, cd.Variant.Size, cd.Variant.SHA256); err != nil {
		logger.Error("download failed for %s: %v", cd.Variant.Artifact, err)
		cd.StaticNote = "download failed: " + err.Error()
		return
	}
	if err := acquire.VerifySHA256(artifactPath, cd.Variant.SHA256); err != nil {
		logger.Error("verification failed for %s: %v", cd.Variant.Artifact, err)
		cd.StaticNote = "verification failed: " + err.Error()
		return
	}

	// Install into a staging directory, then move into place atomically.
	target := layout.RuntimeDir(caps.USB.Root, cd.OS, cd.Arch, cd.Variant.Name)
	staging := target + ".staging"
	_ = os.RemoveAll(staging)
	if err := acquire.Extract(artifactPath, cd.Variant.Archive, staging); err != nil {
		logger.Error("extraction failed for %s: %v", cd.Variant.Artifact, err)
		cd.StaticNote = "extraction failed: " + err.Error()
		return
	}
	bin := filepath.Join(staging, cd.Variant.Binary)
	if _, err := os.Stat(bin); err != nil {
		logger.Error("archive %s missing binary %s", cd.Variant.Artifact, cd.Variant.Binary)
		cd.StaticNote = "archive missing binary " + cd.Variant.Binary
		_ = os.RemoveAll(staging)
		return
	}
	_ = os.Chmod(bin, 0o755)

	// Swap.
	backup := target + ".backup"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		_ = os.Rename(target, backup)
	}
	if err := os.Rename(staging, target); err != nil {
		if _, berr := os.Stat(backup); berr == nil {
			_ = os.Rename(backup, target)
		}
		cd.StaticNote = "install failed: " + err.Error()
		return
	}
	_ = os.RemoveAll(backup)
	cd.Installed = true
	cd.InstalledPath = layout.RuntimeBinary(caps.USB.Root, cd.OS, cd.Arch, cd.Variant.Name, cd.Variant.Binary)

	// Probe the freshly installed binary at its final location.
	res := probe.Probe(cd.InstalledPath)
	cd.Probe = &res
	if res.OK {
		logger.Info("runtime %s installed and verified (version %s)", cd.Variant.Name, res.Version)
	} else {
		logger.Warn("runtime %s installed but probe failed: %s", cd.Variant.Name, res.Error)
		cd.StaticNote = "probe failed: " + res.Error
	}
}

// selectTools chooses USB tool directories to prepend to PATH per policy:
// prefer a usable host tool, fall back to the USB copy, never install.
func selectTools(caps detect.HostCapabilities, m *manifest.Manifest, root string, cfg portableConfig, logger *logx.Logger) []string {
	var dirs []string
	tools := m.ToolsFor(caps.OS, caps.Arch)
	hostHas := map[string]bool{
		"git":     caps.HasGit,
		"ripgrep": caps.HasRipgrep,
	}
	names := []string{"git", "ripgrep"}
	for _, name := range names {
		useUSB := cfg.ToolPolicy == "usb_only" || (cfg.ToolPolicy == "prefer_host_fallback_usb" && !hostHas[name])
		if !useUSB {
			if hostHas[name] {
				logger.Debug("tool %s: using host version", name)
			} else {
				logger.Debug("tool %s: host version unavailable", name)
			}
			continue
		}
		t, ok := tools[name]
		if !ok {
			logger.Warn("tool %s: no USB manifest entry for %s/%s", name, caps.OS, caps.Arch)
			continue
		}
		binPath := filepath.Join(layout.ToolDir(root, caps.OS, caps.Arch, name), t.RootDir, t.BinDir, t.Binary)
		if _, err := os.Stat(binPath); err != nil {
			logger.Warn("tool %s: USB copy missing at %s", name, binPath)
			continue
		}
		dir := filepath.Join(layout.ToolDir(root, caps.OS, caps.Arch, name), t.RootDir, t.BinDir)
		dirs = append(dirs, dir)
		logger.Info("tool %s: using USB copy (%s)", name, dir)
	}
	return dirs
}

func runDiagnose(caps detect.HostCapabilities, m *manifest.Manifest, info usb.Info, cfg portableConfig) int {
	out := os.Stdout
	fmt.Fprintf(out, "OpenCode Portable %s\n", version.String())
	fmt.Fprintln(out, "────────────────────────")
	diag.Host(out, caps)
	diag.Hardware(out, caps)
	diag.Tools(out, caps)
	diag.USB(out, caps)

	engine := &selector.Engine{Caps: caps, Manifest: m}
	cands := engine.Candidates()
	engine.ProbeInstalled(cands)
	diag.Runtimes(out, m, cands)

	var sels []string
	for _, cd := range cands {
		if cd.Probe != nil && cd.Probe.OK {
			sels = append(sels, fmt.Sprintf("  %s/%s %s (OpenCode %s)\n  Reason: execution probe succeeded (version %s)", cd.OS, cd.Arch, cd.Variant.Name, m.OpenCodeVersion, cd.Probe.Version))
			break
		}
	}
	if len(sels) == 0 {
		sels = append(sels, "  none compatible yet")
	}
	fmt.Fprintln(out, "\nSelected runtime:")
	fmt.Fprintln(out, sels[0])

	fmt.Fprintln(out, "\nPortable environment:")
	pe := environ.New(caps.USB.Root)
	for _, l := range pe.Describe() {
		fmt.Fprintln(out, l)
	}

	var errs []string
	if !info.Executable {
		errs = append(errs, "USB mounted with execution disabled (noexec)")
	}
	if !info.Writable {
		errs = append(errs, "USB volume is read-only: config/data/cache cannot persist")
	}
	if verr := cfg.validate(); verr != nil {
		errs = append(errs, "config/portable.json: "+verr.Error())
	}
	diag.Errors(out, errs)
	return 0
}

func runDryRun(caps detect.HostCapabilities, m *manifest.Manifest, chosen *selector.Candidate, pe environ.Portable, toolDirs []string, env []string) int {
	out := os.Stdout
	fmt.Fprintln(out, "OpenCode Portable — dry run (nothing was launched)")
	fmt.Fprintln(out, "────────────────────────────────────────────")
	fmt.Fprintf(out, "Host:          %s/%s\n", caps.OS, caps.Arch)
	if caps.Libc != "" {
		fmt.Fprintf(out, "libc:          %s %s\n", caps.Libc, caps.LibcVersion)
	}
	fmt.Fprintf(out, "USB root:      %s\n", caps.USB.Root)
	if chosen == nil {
		fmt.Fprintln(out, "OpenCode:      no compatible runtime available for this platform")
		fmt.Fprintln(out, "\nWould launch: nothing")
		return 0
	}
	fmt.Fprintf(out, "OpenCode:      %s (%s, %s)\n", m.OpenCodeVersion, chosen.Variant.Name, chosen.Variant.Artifact)
	fmt.Fprintf(out, "Runtime path:  %s\n", chosen.InstalledPath)
	if !chosen.Installed {
		fmt.Fprintf(out, "Runtime state: would be downloaded from %s\n", chosen.Variant.URL)
	} else {
		fmt.Fprintln(out, "Runtime state: already installed")
	}
	if len(toolDirs) > 0 {
		fmt.Fprintf(out, "USB tools:     %s\n", strings.Join(toolDirs, ", "))
	} else {
		fmt.Fprintln(out, "USB tools:     none required (host tools used)")
	}
	fmt.Fprintln(out, "\nPortable environment (applied to the OpenCode process only):")
	for _, l := range pe.Describe() {
		fmt.Fprintln(out, l)
	}
	fmt.Fprintln(out, "\nEnvironment variables (selected):")
	interesting := []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "OPENCODE_CONFIG", "OPENCODE_DISABLE_AUTOUPDATE", "TMPDIR", "APPDATA", "LOCALAPPDATA", "PATH"}
	for _, kv := range env {
		for _, k := range interesting {
			if strings.HasPrefix(kv, k+"=") {
				fmt.Fprintf(out, "  %s\n", kv)
			}
		}
	}
	fmt.Fprintln(out, "\nWould launch: "+chosen.InstalledPath)
	return 0
}

func runUpdate(info usb.Info, m *manifest.Manifest, caps detect.HostCapabilities, opts Options, logger *logx.Logger) int {
	if !info.Writable {
		fmt.Fprintln(os.Stderr, "OpenCode Portable: cannot update: the USB volume is read-only.")
		return 1
	}
	res, err := update.Run(context.Background(), info.Root, m, caps, update.Options{
		AllPlatforms: opts.UpdateAll,
		Quiet:        false,
		Logf:         func(f string, a ...any) { logger.Info(f, a...) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "OpenCode Portable: "+err.Error())
		logger.Error("update failed: %v", err)
		return 1
	}
	if res.AlreadyCurrent {
		fmt.Printf("OpenCode Portable: already up to date (OpenCode %s)\n", m.OpenCodeVersion)
		return 0
	}
	if len(res.Updated) == 0 {
		fmt.Printf("OpenCode Portable: no updated artifacts were published for the requested platforms; the USB manifest is unchanged (OpenCode %s)\n", m.OpenCodeVersion)
		return 0
	}
	fmt.Printf("OpenCode Portable: updated %s → %s\n", res.OldVersion, res.NewVersion)
	if len(res.Updated) > 0 {
		fmt.Printf("  updated platforms: %s\n", strings.Join(res.Updated, ", "))
	}
	if len(res.Skipped) > 0 {
		fmt.Printf("  skipped (artifacts not published): %s\n", strings.Join(res.Skipped, ", "))
	}
	fmt.Println("  config, data and cache preserved")
	return 0
}
