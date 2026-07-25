//go:build darwin

package server

import "syscall"

// osVersion reports the macOS release ("15.5"). kern.osproductversion is the
// marketing version and exists on macOS 11+; kern.osrelease is the Darwin
// kernel version, kept as a fallback so an older host still says something.
func osVersion() string {
	if v, err := syscall.Sysctl("kern.osproductversion"); err == nil && v != "" {
		return v
	}
	if v, err := syscall.Sysctl("kern.osrelease"); err == nil && v != "" {
		return "darwin " + v
	}
	return ""
}
