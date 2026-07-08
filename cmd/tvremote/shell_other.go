//go:build !darwin && !windows

package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// runShell on Linux (dev only — not a shipping target): just block. Open
// localURL in a browser yourself, or hit /desktop for the QR page.
func runShell(localURL string, httpSrv *http.Server) {
	_ = localURL
	_ = httpSrv
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
