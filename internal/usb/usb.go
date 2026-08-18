// Package usb locates the launcher on the USB drive and reports the
// filesystem capabilities of the USB volume (writability, executability,
// read-only state). The launcher never assumes a fixed drive letter or mount
// path: the USB root is always derived from the launcher's own location.
package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Info describes the USB root and its capabilities.
type Info struct {
	// Root is the USB root directory (the directory containing the launcher).
	Root string
	// Launcher is the path to the currently running launcher executable.
	Launcher string
	// Writable reports whether files can be created in the USB root.
	Writable bool
	// Executable reports whether binaries on this volume can be executed.
	// False when, e.g., a Linux mount carries the noexec option.
	Executable bool
	// ReadOnly reports whether the volume is mounted read-only.
	ReadOnly bool
	// Filesystem is the filesystem type when detectable ("" otherwise).
	Filesystem string
	// MountOptions are the mount options for the volume when detectable.
	MountOptions []string
}

// ErrNoExec reports a mount with execution disabled.
type ErrNoExec struct{ Path string }

func (e *ErrNoExec) Error() string {
	return "the USB filesystem at " + e.Path + " is mounted with execution disabled (noexec); OpenCode Portable cannot execute binaries directly from this mount"
}

// ErrReadOnly reports a volume that cannot be written.
type ErrReadOnly struct{ Path string }

func (e *ErrReadOnly) Error() string {
	return "the USB filesystem at " + e.Path + " is read-only"
}

// Locate resolves the USB root from the launcher executable path.
//
// Layout contract: the launcher sits directly in the USB root, e.g.
//
//	<usb>/OpenCodePortable/OpenCodePortable(.exe)
//
// On macOS, when launched from an application bundle, the executable lives at
//
//	<usb>/OpenCodePortable/OpenCodePortable.app/Contents/MacOS/OpenCodePortable
//
// and the root is derived by walking up three levels.
func Locate() (Info, error) {
	exe, err := os.Executable()
	if err != nil {
		return Info{}, fmt.Errorf("cannot determine launcher location: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		// Symlink resolution is best-effort; keep the raw path.
		exe, _ = filepath.Abs(exe)
	}
	exe = filepath.Clean(exe)

	dir := filepath.Dir(exe)

	// macOS app bundle: executable sits at <root>/OpenCodePortable.app/Contents/MacOS/.
	if strings.Contains(dir, string(filepath.Separator)+"Contents"+string(filepath.Separator)+"MacOS") {
		dir = filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		return Info{}, fmt.Errorf("cannot resolve USB root: %w", err)
	}

	info := Info{Root: dir, Launcher: exe}
	info.probeCapabilities()
	return info, nil
}

// probeCapabilities fills Writable, Executable, ReadOnly, Filesystem and
// MountOptions. Platform files provide mount-level details where available.
func (i *Info) probeCapabilities() {
	i.probeWritable()
	i.probeMount()
}

// probeWritable attempts to create and remove a temporary file in the root.
func (i *Info) probeWritable() {
	tmp := filepath.Join(i.Root, ".portable-write-test")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		i.Writable = false
		i.ReadOnly = true
		return
	}
	_ = f.Close()
	_ = os.Remove(tmp)
	i.Writable = true
}

// CheckExecutable returns an error when execution from the volume is not
// possible. On Linux this consults the mount options; other platforms treat
// execution as available.
func (i *Info) CheckExecutable() error {
	if !i.Executable {
		return &ErrNoExec{Path: i.Root}
	}
	return nil
}

// CheckWritable returns an error when the volume cannot be written.
func (i *Info) CheckWritable() error {
	if !i.Writable {
		return &ErrReadOnly{Path: i.Root}
	}
	return nil
}
