//go:build !windows

package probe

import (
	"os/exec"
	"syscall"
)

// classifyExit classifies a probe failure on unix-like systems.
func classifyExit(res *Result, err error) {
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
		ws := ee.Sys().(syscall.WaitStatus)
		switch {
		case ws.Signaled():
			res.Error = "binary terminated by signal " + ws.Signal().String()
		case res.ExitCode == 126:
			res.Error = "binary exists but cannot be executed (permissions or exec disabled on the volume)"
		case res.ExitCode == 127:
			res.Error = "binary or its interpreter cannot be found"
		default:
			res.Error = "binary exited with code " + itoa(res.ExitCode)
		}
		return
	}
	res.Error = "cannot execute binary: " + err.Error()
}
