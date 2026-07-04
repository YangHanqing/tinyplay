//go:build windows

package player

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// platformDefaultSocket: on Windows mpv exposes a named pipe.
func platformDefaultSocket() string {
	return `\\.\pipe\mpvsocket`
}

func dialMPV(addr string) (net.Conn, error) {
	return winio.DialPipe(addr, nil)
}
