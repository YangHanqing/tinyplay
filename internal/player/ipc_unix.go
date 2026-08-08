//go:build !windows

package player

import (
	"net"
	"os"
	"path/filepath"
)

// platformDefaultSocket: on macOS/Linux mpv uses a Unix socket.
func platformDefaultSocket() string {
	return filepath.Join(os.TempDir(), "tvremote-mpvsocket")
}

func dialMPV(addr string) (net.Conn, error) {
	return net.Dial("unix", addr)
}

// allowForegroundActivation is a Windows-only concern (see the counterpart in
// ipc_windows.go): macOS activates a newly launched GUI process by default.
func allowForegroundActivation(pid int) {}
