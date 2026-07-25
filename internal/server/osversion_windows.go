//go:build windows

package server

import (
	"fmt"
	"syscall"
	"unsafe"
)

// rtlOSVersionInfoW mirrors RTL_OSVERSIONINFOW.
type rtlOSVersionInfoW struct {
	dwOSVersionInfoSize uint32
	dwMajorVersion      uint32
	dwMinorVersion      uint32
	dwBuildNumber       uint32
	dwPlatformID        uint32
	szCSDVersion        [128]uint16
}

// osVersion reports the Windows version as "major.minor.build" (e.g.
// "10.0.22631"). It goes through ntdll's RtlGetVersion rather than
// GetVersionEx because GetVersionEx reports 6.2 for every release newer than
// Windows 8 unless the executable ships a compatibility manifest naming each
// supported OS — a bug report claiming "Windows 8" on a Windows 11 machine is
// worse than no version at all. The build number is the part that matters:
// it's what separates Windows 10 from 11 and one feature update from the next.
func osVersion() string {
	info := rtlOSVersionInfoW{}
	info.dwOSVersionInfoSize = uint32(unsafe.Sizeof(info))
	proc := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if err := proc.Find(); err != nil {
		return ""
	}
	// RtlGetVersion returns STATUS_SUCCESS (0) and never actually fails, but
	// check anyway rather than reporting a zeroed struct as "0.0.0".
	if status, _, _ := proc.Call(uintptr(unsafe.Pointer(&info))); status != 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", info.dwMajorVersion, info.dwMinorVersion, info.dwBuildNumber)
}
