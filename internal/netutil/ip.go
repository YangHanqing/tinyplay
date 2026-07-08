// Package netutil ports app/core/network.py: detecting the LAN IP so the phone
// can reach the server.
package netutil

import "net"

// LocalIP returns the primary LAN IPv4 address, or 127.0.0.1 on failure. It
// uses the same trick as the Python branch: open a UDP socket toward a public
// address (no packets are actually sent) and read back the chosen local addr.
func LocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return "127.0.0.1"
}
