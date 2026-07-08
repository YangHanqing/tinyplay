//go:build darwin

package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// runShell on macOS: the native SwiftUI app owns the tray (NSStatusItem) and
// the intro/QR window, and runs this binary as a sidecar. So here we just block
// until the parent terminates us. The SwiftUI app reads the QR target from the
// /desktop page (or renders it natively).
func runShell(localURL string, httpSrv *http.Server) {
	_ = localURL
	_ = httpSrv
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
