// Package environ constructs the process environment handed to OpenCode.
//
// The environment redirects OpenCode's configuration, data, cache and
// temporary storage onto the USB volume via the standard mechanisms
// OpenCode already honors (XDG base directories on Linux/macOS, APPDATA/
// LOCALAPPDATA on Windows, and the OPENCODE_* variables). The host
// environment is never modified — only the child process sees these values.
package environ

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"opencode-portable/internal/layout"
)

// Portable describes the USB-local state directories.
type Portable struct {
	Root       string
	ConfigDir  string
	DataDir    string
	CacheDir   string
	LogsDir    string
	TmpDir     string
	ConfigFile string // OPENCODE_CONFIG value, set only when the file exists
}

// New builds a Portable from the USB root.
func New(root string) Portable {
	p := Portable{
		Root:      root,
		ConfigDir: filepath.Join(root, layout.ConfigDir),
		DataDir:   filepath.Join(root, layout.DataDir),
		CacheDir:  filepath.Join(root, layout.CacheDir),
		LogsDir:   filepath.Join(root, layout.LogsDir),
		TmpDir:    filepath.Join(root, layout.CacheDir, "tmp"),
	}
	// If the user keeps a portable global config, point OpenCode at it.
	oc := filepath.Join(p.ConfigDir, "opencode", "opencode.json")
	if _, err := os.Stat(oc); err == nil {
		p.ConfigFile = oc
	}
	return p
}

// Build returns the full environment for the OpenCode process. base is the
// current environment; toolDirs are USB tool directories (each already
// pointing at the directory holding executables) to prepend to PATH.
func (p *Portable) Build(base []string, toolDirs []string) []string {
	env := map[string]string{}
	for _, kv := range base {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			env[k] = v
		}
	}

	pathVar := "PATH"
	pathVal := env[pathVar]

	switch runtime.GOOS {
	case "windows":
		// OpenCode on Windows stores config/data under APPDATA and
		// LOCALAPPDATA; scope both to the USB for the child process.
		env["APPDATA"] = filepath.Join(p.ConfigDir)
		env["LOCALAPPDATA"] = filepath.Join(p.DataDir)
		env["TMP"] = p.TmpDir
		env["TEMP"] = p.TmpDir
		env["XDG_CONFIG_HOME"] = p.ConfigDir
		env["XDG_DATA_HOME"] = p.DataDir
		env["XDG_CACHE_HOME"] = p.CacheDir
	default:
		// Linux/macOS: XDG base directory redirection keeps all portable
		// state on the USB. HOME is intentionally left untouched so the
		// user's project context (and any read-only dotfiles OpenCode
		// consults) remain meaningful.
		env["XDG_CONFIG_HOME"] = p.ConfigDir
		env["XDG_DATA_HOME"] = p.DataDir
		env["XDG_CACHE_HOME"] = p.CacheDir
		env["TMPDIR"] = p.TmpDir
		env["TMP"] = p.TmpDir
	}

	// We manage OpenCode updates ourselves; disable its self-update checks.
	env["OPENCODE_DISABLE_AUTOUPDATE"] = "1"

	if p.ConfigFile != "" {
		env["OPENCODE_CONFIG"] = p.ConfigFile
	}

	// Prepend only the specific tool directories selected for this run.
	// Never the whole USB volume.
	if len(toolDirs) > 0 {
		parts := make([]string, 0, len(toolDirs)+1)
		for _, d := range toolDirs {
			if d != "" {
				parts = append(parts, d)
			}
		}
		if pathVal != "" {
			parts = append(parts, pathVal)
		}
		env[pathVar] = strings.Join(parts, string(os.PathListSeparator))
	}

	// Deduplicate PATH entries (host environments can contain many
	// duplicates; the child deserves a sane lookup path).
	if v, ok := env[pathVar]; ok {
		env[pathVar] = dedupPath(v, string(os.PathListSeparator))
	}

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// dedupPath removes duplicate entries preserving first-occurrence order.
// On Windows the comparison is case-insensitive: the filesystem and the
// PATH lookup are case-insensitive, so entries differing only in case are
// duplicates there.
func dedupPath(p, sep string) string {
	ci := runtime.GOOS == "windows"
	seen := map[string]bool{}
	parts := strings.Split(p, sep)
	out := parts[:0]
	for _, part := range parts {
		if part == "" {
			continue
		}
		key := part
		if ci {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, part)
	}
	return strings.Join(out, sep)
}

// Describe returns the portable redirections as human-readable lines for
// diagnostics (paths only; no values that could contain secrets).
func (p *Portable) Describe() []string {
	lines := []string{
		"  Config: " + p.ConfigDir,
		"  Data:   " + p.DataDir,
		"  Cache:  " + p.CacheDir,
		"  Logs:   " + p.LogsDir,
		"  Tmp:    " + p.TmpDir,
	}
	if p.ConfigFile != "" {
		lines = append(lines, "  OPENCODE_CONFIG: "+p.ConfigFile)
	}
	return lines
}
