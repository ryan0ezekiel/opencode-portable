// Package manifest defines the OpenCode Portable manifest: the description of
// which official OpenCode artifacts exist for each supported platform, where
// they come from, and how they are verified.
//
// The manifest is never hand-maintained against a specific OpenCode version.
// A default manifest is generated from the official release (scripts/
// generate-manifest.sh) and embedded at build time; the USB-local
// manifest.json (also generated) takes precedence at runtime. `--update`
// refreshes the USB manifest from the official release API.
package manifest

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// SchemaVersion is the current manifest schema.
const SchemaVersion = 1

// Manifest describes all platform runtimes and tools carried by the USB.
type Manifest struct {
	SchemaVersion   int                        `json:"schema_version"`
	SourceRepo      string                     `json:"source"` // official release source, e.g. anomalyco/opencode
	OpenCodeVersion string                     `json:"opencode_version"`
	Runtimes        map[string]PlatformRuntime `json:"runtimes"`
	Tools           map[string]PlatformTools   `json:"tools,omitempty"`
}

// PlatformRuntime lists the runtime variants for one OS/arch key.
type PlatformRuntime struct {
	Variants []Variant `json:"variants"`
}

// Variant is one OpenCode runtime build for a platform.
type Variant struct {
	Name     string   `json:"name"` // native | baseline | musl | baseline-musl
	Artifact string   `json:"artifact"`
	URL      string   `json:"url"`
	SHA256   string   `json:"sha256"`
	Size     int64    `json:"size,omitempty"`
	Archive  string   `json:"archive"`            // zip | tar.gz
	Binary   string   `json:"binary"`             // executable name inside the archive
	Requires Requires `json:"requires,omitempty"` // static compatibility hints
}

// Requires lists static compatibility requirements for a variant. These are
// hints used for pre-selection; the execution probe is the final authority.
type Requires struct {
	Libc    string   `json:"libc,omitempty"`     // "glibc" | "musl" | "any"
	MinLibc string   `json:"min_libc,omitempty"` // e.g. "2.17"
	CPU     []string `json:"cpu,omitempty"`      // e.g. ["avx2"]
	Note    string   `json:"note,omitempty"`     // human-readable note
}

// PlatformTools lists the USB-provided helper tools for one OS/arch key.
type PlatformTools struct {
	Tools map[string]Tool `json:"tools"`
}

// Tool describes a USB-bundled helper (git, ripgrep, ...).
type Tool struct {
	Name        string   `json:"name"`
	Artifact    string   `json:"artifact"`
	URL         string   `json:"url"`
	SHA256      string   `json:"sha256"`
	Size        int64    `json:"size,omitempty"`
	Archive     string   `json:"archive"`               // zip | tar.gz | raw
	RootDir     string   `json:"root_dir,omitempty"`    // directory inside the archive containing the payload
	BinDir      string   `json:"bin_dir,omitempty"`     // relative path inside RootDir holding executables
	Binary      string   `json:"binary"`                // primary executable (goes on PATH)
	Executables []string `json:"executables,omitempty"` // extra files needing the exec bit on unix
}

// PlatformKey builds the manifest map key for an OS/arch pair.
func PlatformKey(os, arch string) string { return os + "/" + arch }

// SupportedPlatforms lists every supported platform key.
func SupportedPlatforms() []string {
	return []string{
		"windows/amd64", "windows/arm64",
		"linux/amd64", "linux/arm64",
		"darwin/amd64", "darwin/arm64",
	}
}

// ErrCorrupt is returned for structurally invalid manifests.
var ErrCorrupt = errors.New("corrupted manifest")

// Load reads and validates a manifest from disk.
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse validates and parses manifest bytes.
func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks structural integrity of the manifest.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %d (expected %d)", ErrCorrupt, m.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(m.SourceRepo) == "" {
		return fmt.Errorf("%w: missing source", ErrCorrupt)
	}
	if strings.TrimSpace(m.OpenCodeVersion) == "" {
		return fmt.Errorf("%w: missing opencode_version", ErrCorrupt)
	}
	if len(m.Runtimes) == 0 {
		return fmt.Errorf("%w: no runtimes", ErrCorrupt)
	}
	for _, pk := range SupportedPlatforms() {
		pr, ok := m.Runtimes[pk]
		if !ok || len(pr.Variants) == 0 {
			return fmt.Errorf("%w: missing runtimes for %s", ErrCorrupt, pk)
		}
		for _, v := range pr.Variants {
			if err := v.validate(); err != nil {
				return fmt.Errorf("%w: %s: %v", ErrCorrupt, pk, err)
			}
		}
	}
	for pk, pt := range m.Tools {
		for name, t := range pt.Tools {
			if err := t.validate(); err != nil {
				return fmt.Errorf("%w: tool %s/%s: %v", ErrCorrupt, pk, name, err)
			}
		}
	}
	return nil
}

func (v *Variant) validate() error {
	if v.Name == "" {
		return errors.New("variant missing name")
	}
	if v.Artifact == "" || v.URL == "" || v.SHA256 == "" {
		return errors.New("variant missing artifact/url/sha256")
	}
	if v.Archive != "zip" && v.Archive != "tar.gz" {
		return errors.New("variant has unknown archive type " + v.Archive)
	}
	if v.Binary == "" {
		return errors.New("variant missing binary name")
	}
	if !validSHA256(v.SHA256) {
		return errors.New("variant sha256 must be 64 hex chars")
	}
	if v.Size < 0 {
		return errors.New("variant has negative size")
	}
	return nil
}

// validSHA256 reports whether s is a well-formed lowercase or uppercase hex
// SHA-256 digest. A malformed digest would pass a length check alone, then
// fail confusingly at every download verification.
func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (t *Tool) validate() error {
	if t.Name == "" || t.Artifact == "" || t.URL == "" || t.SHA256 == "" {
		return errors.New("tool missing name/artifact/url/sha256")
	}
	if t.Archive != "zip" && t.Archive != "tar.gz" && t.Archive != "raw" {
		return errors.New("tool has unknown archive type " + t.Archive)
	}
	if t.Binary == "" {
		return errors.New("tool missing binary name")
	}
	if !validSHA256(t.SHA256) {
		return errors.New("tool sha256 must be 64 hex chars")
	}
	if t.Size < 0 {
		return errors.New("tool has negative size")
	}
	return nil
}

// RuntimesFor returns the variants for a platform, or nil.
func (m *Manifest) RuntimesFor(os, arch string) []Variant {
	pr, ok := m.Runtimes[PlatformKey(os, arch)]
	if !ok {
		return nil
	}
	return pr.Variants
}

// ToolsFor returns the tools for a platform, or nil.
func (m *Manifest) ToolsFor(os, arch string) map[string]Tool {
	pt, ok := m.Tools[PlatformKey(os, arch)]
	if !ok {
		return nil
	}
	return pt.Tools
}

// Save writes the manifest atomically.
func (m *Manifest) Save(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
