//go:build windows

package detect

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type osVersionInfoEx struct {
	OSVersionInfoSize uint32
	MajorVersion      uint32
	MinorVersion      uint32
	BuildNumber       uint32
	PlatformID        uint32
	CSDVersion        [128]uint16
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	ntdll             = syscall.NewLazyDLL("ntdll.dll")
	procRtlGetVersion = ntdll.NewProc("RtlGetVersion")
	procGlobalMemory  = kernel32.NewProc("GlobalMemoryStatusEx")
	procProcessorFeat = kernel32.NewProc("IsProcessorFeaturePresent")
)

// detectPlatform fills OS version, RAM, CPU and GPU fields on Windows.
func (c *HostCapabilities) detectPlatform() {
	c.detectOSVersion()
	c.detectKernel()
	c.detectRAM()
	c.detectCPU()
	c.detectGPU()
}

func (c *HostCapabilities) detectOSVersion() {
	var vi osVersionInfoEx
	vi.OSVersionInfoSize = uint32(unsafe.Sizeof(vi))
	if r1, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi))); r1 == 0 {
		c.OSVersion = "Windows " + strconv.FormatUint(uint64(vi.MajorVersion), 10) +
			"." + strconv.FormatUint(uint64(vi.MinorVersion), 10) +
			" (build " + strconv.FormatUint(uint64(vi.BuildNumber), 10) + ")"
	}
}

func (c *HostCapabilities) detectKernel() {
	// Windows reports its kernel as the NT version via RtlGetVersion; reuse.
	if c.OSVersion != "" {
		c.Kernel = "NT " + c.OSVersion
	}
}

func (c *HostCapabilities) detectRAM() {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	if r1, _, _ := procGlobalMemory.Call(uintptr(unsafe.Pointer(&ms))); r1 != 0 {
		c.RAMBytes = ms.TotalPhys
	}
}

const (
	pfAVX2InstructionsAvailable = 40
)

func (c *HostCapabilities) detectCPU() {
	// Processor name from the registry via environment is not available;
	// read it directly from the hardware description key through the
	// registry API is complex, so fall back to PROCESSOR_IDENTIFIER.
	c.CPUModel = os.Getenv("PROCESSOR_IDENTIFIER")
	// AVX2 availability: Windows 10+ exposes this feature probe.
	if r1, _, _ := procProcessorFeat.Call(uintptr(pfAVX2InstructionsAvailable)); r1 != 0 {
		c.HasAVX2 = true
	}
}

func (c *HostCapabilities) detectGPU() {
	// Best-effort via PowerShell; absence never blocks startup.
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return
	}
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_VideoController | Select-Object -First 1).Name").Output()
	if err != nil {
		return
	}
	c.GPU = strings.TrimSpace(string(out))
}
