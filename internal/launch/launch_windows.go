//go:build windows

package launch

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	createNewConsole = 0x00000010
)

// Exec runs the target binary as a child process with inherited standard
// streams, waits for it, and returns its exit code. When the launcher was
// started without a console (e.g. double-clicked in Explorer), the child is
// given a fresh console so the TUI has somewhere to render.
func Exec(binary, argv0 string, args []string, env []string) (int, error) {
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if !hasConsole() {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	}

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

func hasConsole() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetConsoleWindow")
	r1, _, _ := proc.Call()
	return r1 != 0
}
