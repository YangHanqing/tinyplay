//go:build windows

package player

import (
	"net"
	"syscall"

	"github.com/Microsoft/go-winio"
)

// platformDefaultSocket: on Windows mpv exposes a named pipe.
func platformDefaultSocket() string {
	return `\\.\pipe\mpvsocket`
}

func dialMPV(addr string) (net.Conn, error) {
	return winio.DialPipe(addr, nil)
}

var procAllowSetForegroundWindow = syscall.NewLazyDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowForegroundActivation grants the freshly spawned mpv process the right
// to bring its own window to the front. Play() is invoked from an HTTP
// handler goroutine — a background thread with no window and no local input
// behind it — which Windows' foreground-lock rules treat as unprivileged, so
// without this call mpv's window is silently opened *behind* whatever
// currently holds the foreground (typically the QR/intro window). This
// mirrors bringWindowToFront in cmd/tvremote/shell_windows.go, which grants
// the same right to the shell's own windows for the identical reason.
func allowForegroundActivation(pid int) {
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(pid))
}
