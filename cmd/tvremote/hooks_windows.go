//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"tvremote/internal/player"
)

// wireMonitorHint lets a fresh mpv spawn open on whichever monitor currently
// hosts TinyPlay's own window (see openWindow/mainWindowHWND), mirroring the
// MonitorFromWindow lookup winFullscreen.enter already does for that same
// window's own borderless fullscreen. Queried fresh on every Play rather than
// cached, since the window can be dragged to a different monitor between
// plays; mainWindowHWND is 0 whenever no TinyPlay window is open, in which
// case Play falls back to mpv's default placement unchanged.
//
// Positions by pixel geometry rather than a --screen index: Win32's
// GetMonitorInfo and mpv's Windows backend both use the same top-left-origin
// coordinate space, so this sidesteps having to match Windows' monitor
// enumeration order to mpv's.
func wireMonitorHint(p *player.Player) {
	p.MonitorHint = func() (string, bool) {
		hwnd := mainWindowHWND
		if hwnd == 0 {
			return "", false
		}
		mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
		if mon == 0 {
			return "", false
		}
		var mi winMonitorInfo
		mi.Size = uint32(unsafe.Sizeof(mi))
		ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
		if ok == 0 {
			return "", false
		}
		return fmt.Sprintf("--geometry=+%d+%d", mi.Monitor.Left, mi.Monitor.Top), true
	}
}
