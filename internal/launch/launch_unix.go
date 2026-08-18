//go:build !windows

package launch

import "syscall"

// Exec replaces the current process with the target binary. This gives the
// child perfect signal and exit-code semantics: the launcher becomes
// OpenCode, and the terminal sees OpenCode itself as the foreground
// process. On success the launcher never returns; the exit code returned is
// always 0 alongside a nil error.
func Exec(binary, argv0 string, args []string, env []string) (int, error) {
	err := syscall.Exec(binary, append([]string{argv0}, args...), env)
	return 0, err
}
