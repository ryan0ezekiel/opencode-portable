//go:build windows

package probe

import "os/exec"

// Windows NTSTATUS codes for execution failures.
const (
	statusIllegalInstruction    = 0xC000001D
	statusIllegalInstructionAlt = 0xC0000096
	statusBadImageFormat        = 0xC000007B
	statusDllNotFound           = 0xC0000135
)

// classifyExit classifies a probe failure on Windows. Abnormal Windows
// termination codes (NTSTATUS) surface as positive exit codes.
func classifyExit(res *Result, err error) {
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		res.ExitCode = code
		switch code {
		case statusBadImageFormat:
			res.Error = "binary is not a valid executable for this system"
		case statusDllNotFound:
			res.Error = "binary is missing a required DLL"
		case statusIllegalInstruction, statusIllegalInstructionAlt:
			res.Error = "binary requires CPU instructions not available on this processor (a baseline build may be needed)"
		case -1:
			res.Error = "process terminated abnormally"
		default:
			res.Error = "binary exited with code " + itoa(code)
		}
		return
	}
	res.Error = "cannot execute binary: " + err.Error()
}
