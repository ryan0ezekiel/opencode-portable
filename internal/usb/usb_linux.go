//go:build linux

package usb

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// probeMount inspects /proc/self/mounts to determine the mount covering the
// USB root, its filesystem type, and its options (noexec, ro, ...).
func (i *Info) probeMount() {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		i.Executable = true
		return
	}
	defer f.Close()

	root := filepath.Clean(i.Root)
	var bestMount, bestFS string
	var bestOpts []string
	bestLen := -1

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		mountPoint := unescapeMount(fields[1])
		mountPoint = filepath.Clean(mountPoint)
		if mountPoint == "/" || root == mountPoint || strings.HasPrefix(root, mountPoint+string(filepath.Separator)) {
			if len(mountPoint) > bestLen {
				bestLen = len(mountPoint)
				bestMount = mountPoint
				bestFS = unescapeMount(fields[2])
				bestOpts = strings.Split(unescapeMount(fields[3]), ",")
			}
		}
	}

	i.MountOptions = bestOpts
	i.Filesystem = bestFS
	_ = bestMount

	// Executable defaults to true; only the noexec mount option disables it.
	i.Executable = true
	for _, opt := range bestOpts {
		switch {
		case opt == "noexec":
			i.Executable = false
		case opt == "ro":
			i.ReadOnly = true
		}
	}
	if bestOpts == nil {
		// No matching mount found; assume execution is permitted. The
		// runtime probe remains the definitive check.
		i.Executable = true
	}
}

// unescapeMount decodes the octal escapes (\040 etc.) used in mount tables.
func unescapeMount(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for j := 0; j < len(s); j++ {
		if s[j] == '\\' && j+3 < len(s) && isOctal(s[j+1]) && isOctal(s[j+2]) && isOctal(s[j+3]) {
			b.WriteByte((s[j+1]-'0')<<6 | (s[j+2]-'0')<<3 | (s[j+3] - '0'))
			j += 3
			continue
		}
		b.WriteByte(s[j])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }
