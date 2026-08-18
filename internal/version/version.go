// Package version holds build-time version information for the
// OpenCode Portable bootstrapper.
package version

// These values are overridden at build time via -ldflags.
var (
	// Version is the bootstrapper version (not the OpenCode version).
	Version = "dev"

	// Commit is the git commit the binary was built from, when known.
	Commit = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return Version
}
