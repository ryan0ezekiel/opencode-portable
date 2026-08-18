//go:build !linux

package usb

// probeMount is a no-op outside Linux: mount-option parsing is specific to
// the Linux proc filesystem. Execution is treated as available.
func (i *Info) probeMount() {
	i.Executable = true
}
