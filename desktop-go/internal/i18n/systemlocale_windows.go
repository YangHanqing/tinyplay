//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

var getUserDefaultLocaleName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")

func systemLocale() string {
	buf := make([]uint16, 85)
	n, _, _ := getUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
