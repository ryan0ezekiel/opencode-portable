//go:build darwin

package detect

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// detectPlatform fills OS version, kernel, RAM, CPU and GPU fields on macOS.
func (c *HostCapabilities) detectPlatform() {
	c.detectOSVersion()
	c.detectKernel()
	c.detectRAM()
	c.detectCPU()
	c.detectGPU()
}

func (c *HostCapabilities) detectOSVersion() {
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		c.OSVersion = strings.TrimSpace(string(out))
	}
}

func (c *HostCapabilities) detectKernel() {
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		c.Kernel = strings.TrimSpace(string(out))
	}
}

// sysctlString runs sysctl -n <name>.
func sysctlString(name string) string {
	out, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *HostCapabilities) detectRAM() {
	if s := sysctlString("hw.memsize"); s != "" {
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			c.RAMBytes = n
		}
	}
}

func (c *HostCapabilities) detectCPU() {
	c.CPUModel = sysctlString("machdep.cpu.brand_string")
	avx := sysctlString("hw.optional.avx2_0")
	c.HasAVX2 = avx == "1" || c.Arch == "arm64"
}

func (c *HostCapabilities) detectGPU() {
	// system_profiler can be slow; bound it tightly. Failure is fine.
	cmd := exec.Command("system_profiler", "SPDisplaysDataType")
	done := make(chan string, 1)
	go func() {
		out, err := cmd.Output()
		if err != nil {
			done <- ""
			return
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Chipset Model:") {
				done <- strings.TrimSpace(strings.TrimPrefix(line, "Chipset Model:"))
				return
			}
		}
		done <- ""
	}()
	select {
	case g := <-done:
		c.GPU = g
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
	}
}
