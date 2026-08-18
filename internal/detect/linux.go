//go:build linux

package detect

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	glibcLddRe     = regexp.MustCompile(`(?i)glibc\s+(\d+\.\d+(?:\.\d+)?)`)
	glibcGetconfRe = regexp.MustCompile(`(?i)glibc\s+(\d+\.\d+(?:\.\d+)?)`)
	muslLddRe      = regexp.MustCompile(`(?i)musl libc.*?(\d+\.\d+(?:\.\d+)?)?`)
	libcSoRe       = regexp.MustCompile(`GNU C Library.*?version\s+(\d+\.\d+(?:\.\d+)?)`)
	memTotalRe     = regexp.MustCompile(`MemTotal:\s+(\d+)\s+kB`)
)

// detectPlatform fills OS version, kernel, libc, RAM, CPU and GPU fields on
// Linux. All detection is best-effort.
func (c *HostCapabilities) detectPlatform() {
	c.detectKernel()
	c.detectLibc()
	c.detectCPU()
	c.detectRAM()
	c.detectGPU()
}

func (c *HostCapabilities) detectKernel() {
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		c.Kernel = strings.TrimSpace(string(b))
	}
	// OS version from os-release when available.
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				c.OSVersion = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				break
			}
		}
	}
}

func (c *HostCapabilities) detectLibc() {
	// 1) ldd --version reports the libc and its version on both glibc and
	//    musl systems.
	if out, err := exec.Command("ldd", "--version").Output(); err == nil {
		s := string(out)
		if m := glibcLddRe.FindStringSubmatch(s); len(m) > 1 {
			c.Libc = "glibc"
			c.LibcVersion = m[1]
			return
		}
		if strings.Contains(s, "musl") {
			c.Libc = "musl"
			if m := muslLddRe.FindStringSubmatch(s); len(m) > 1 {
				c.LibcVersion = m[1]
			}
			return
		}
	}
	// 2) getconf GNU_LIBC_VERSION on glibc systems.
	if out, err := exec.Command("getconf", "GNU_LIBC_VERSION").Output(); err == nil {
		if m := glibcGetconfRe.FindStringSubmatch(string(out)); len(m) > 1 {
			c.Libc = "glibc"
			c.LibcVersion = m[1]
			return
		}
	}
	// 3) The glibc shared object is an executable; running it prints its
	//    version banner.
	for _, p := range []string{
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/lib64/libc.so.6",
		"/lib/aarch64-linux-gnu/libc.so.6",
		"/usr/lib/libc.so.6",
	} {
		if _, err := os.Stat(p); err == nil {
			cmd := exec.Command(p)
			done := make(chan []byte, 1)
			cmd.Stdout = chanWriter(done)
			_ = cmd.Start()
			select {
			case b := <-done:
				if m := libcSoRe.FindStringSubmatch(string(b)); len(m) > 1 {
					c.Libc = "glibc"
					c.LibcVersion = m[1]
					return
				}
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		}
	}
	// 4) musl loader presence.
	matches, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	if len(matches) > 0 {
		c.Libc = "musl"
	}
}

type chanWriter chan []byte

func (w chanWriter) Write(b []byte) (int, error) {
	select {
	case w <- append([]byte(nil), b...):
	default:
	}
	return len(b), nil
}

func (c *HostCapabilities) detectCPU() {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	var flags []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "model name"):
			c.CPUModel = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		case strings.HasPrefix(line, "flags"):
			flags = append(flags, strings.Fields(strings.SplitN(line, ":", 2)[1])...)
		}
	}
	for _, fl := range flags {
		if fl == "avx2" {
			c.HasAVX2 = true
		}
	}
}

func (c *HostCapabilities) detectRAM() {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	if m := memTotalRe.FindStringSubmatch(string(b)); len(m) > 1 {
		if kb, err := strconv.ParseUint(m[1], 10, 64); err == nil {
			c.RAMBytes = kb * 1024
		}
	}
}

func (c *HostCapabilities) detectGPU() {
	// Best effort; lspci may be absent. Never blocks startup: lspci is
	// bound with a short deadline like every other detection step.
	if _, err := exec.LookPath("lspci"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "lspci")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "vga") || strings.Contains(low, "3d") || strings.Contains(low, "display") {
			// Strip the "00:02.0 " bus address prefix and the trailing
			// "(rev NN)" revision tag.
			line = strings.TrimSpace(line)
			if i := strings.Index(line, " "); i >= 0 && strings.HasPrefix(line, "0") {
				line = strings.TrimSpace(line[i:])
			}
			line = strings.TrimSpace(line)
			if i := strings.Index(line, ":"); i >= 0 {
				line = strings.TrimSpace(line[i+1:])
			}
			if i := strings.Index(line, "(rev"); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			c.GPU = line
			return
		}
	}
}
