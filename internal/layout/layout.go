// Package layout defines the on-USB directory structure. All paths derive
// from the USB root discovered at runtime; no drive letter or mount path is
// ever assumed.
package layout

import (
	"os"
	"path/filepath"
)

const (
	// RuntimesDir holds platform runtimes: runtimes/<os>/<arch>/<variant>/.
	RuntimesDir = "runtimes"
	// ToolsDir holds USB-provided helper tools: tools/<os>/<arch>/<tool>/.
	ToolsDir = "tools"
	// ConfigDir holds portable configuration.
	ConfigDir = "config"
	// DataDir holds portable application data (sessions, auth, storage).
	DataDir = "data"
	// CacheDir holds portable cache data.
	CacheDir = "cache"
	// LogsDir holds launcher logs.
	LogsDir = "logs"
	// DownloadsDir holds downloaded artifacts awaiting install.
	DownloadsDir = "downloads"
	// ManifestFile is the USB-local manifest name.
	ManifestFile = "manifest.json"
	// PortableConfigFile is the launcher's own configuration file.
	PortableConfigFile = "portable.json"
)

// RuntimeDir returns the install directory for a runtime variant.
func RuntimeDir(root, os, arch, variant string) string {
	return filepath.Join(root, RuntimesDir, os, arch, variant)
}

// RuntimeBinary returns the path to a runtime's binary inside the variant
// directory.
func RuntimeBinary(root, os, arch, variant, binary string) string {
	return filepath.Join(RuntimeDir(root, os, arch, variant), binary)
}

// ToolDir returns the install directory for a USB tool.
func ToolDir(root, os, arch, tool string) string {
	return filepath.Join(root, ToolsDir, os, arch, tool)
}

// ToolBinary returns the path to a tool's primary executable.
func ToolBinary(root, os, arch, tool, binary string) string {
	return filepath.Join(ToolDir(root, os, arch, tool), binary)
}

// ManifestPath returns the USB-local manifest path.
func ManifestPath(root string) string { return filepath.Join(root, ManifestFile) }

// PortableConfigPath returns the launcher's own config path.
func PortableConfigPath(root string) string {
	return filepath.Join(root, ConfigDir, PortableConfigFile)
}

// EnsureDirs creates the standard USB directories (best effort).
func EnsureDirs(root string) {
	for _, d := range []string{
		RuntimesDir, ToolsDir, ConfigDir, DataDir, CacheDir, LogsDir, DownloadsDir,
	} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
}
