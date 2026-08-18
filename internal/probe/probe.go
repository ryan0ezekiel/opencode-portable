// Package probe performs safe execution probes of runtime binaries: it runs
// a candidate with --version under a timeout and classifies the outcome.
// This is the authoritative compatibility test: static analysis (libc,
// CPU features) can only pre-filter; only actually starting the binary
// proves it will run.
package probe

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Result classifies one probe run.
type Result struct {
	OK       bool
	Version  string
	Output   string
	Error    string // human-readable failure classification
	ExitCode int
	TimedOut bool
}

// Timeout bounds a single probe.
const Timeout = 15 * time.Second

// VersionFlag is the argument used to make the binary exit quickly.
const VersionFlag = "--version"

// Probe runs binary with --version and classifies the outcome.
func Probe(binary string) Result {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, VersionFlag)
	// The child gets a detached stdin so it never blocks waiting for input.
	cmd.Stdin = nil
	out, err := cmd.Output()

	res := Result{Output: strings.TrimSpace(string(out))}
	if err == nil {
		res.OK = true
		res.Version = firstLine(res.Output)
		return res
	}

	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Error = "probe timed out (binary did not respond within " + Timeout.String() + ")"
		return res
	}

	classifyExit(&res, err)
	return res
}

// ClassifySignal maps probe failure text to a short compatibility label used
// in diagnostics.
func (r *Result) ClassifySignal() string {
	if r.TimedOut {
		return "timeout"
	}
	if strings.Contains(r.Error, "signal") {
		return "signal"
	}
	if strings.Contains(r.Error, "cannot be executed") {
		return "not-executable"
	}
	if strings.Contains(r.Error, "cannot be found") {
		return "missing-interpreter"
	}
	return "exit"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
