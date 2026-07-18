package main

import (
	"log"
	"net"
	"os"
	"runtime"
)

// singleInstanceAddr is a loopback-only, arbitrary high port claimed purely as
// a process-wide mutex (see guardSingleInstance).
const singleInstanceAddr = "127.0.0.1:47611"

// guardLn holds the single-instance lock for the whole process lifetime; it is
// never closed on purpose so no second instance can grab the port.
var guardLn net.Listener

// guardSingleInstance stops a second copy of the app from starting. macOS
// already single-instances .app launches through LaunchServices, so this only
// matters on Windows, where double-clicking TinyPlay.exe again would otherwise
// spawn another tray + server. We claim a fixed loopback port as a mutex: the
// second process fails to bind and exits immediately. The bind is released
// automatically when the first process ends — even on a crash — so there is no
// stale lock file to clean up (which fits the portable, delete-to-uninstall
// model).
func guardSingleInstance() {
	if runtime.GOOS != "windows" {
		return
	}
	ln, err := net.Listen("tcp", singleInstanceAddr)
	if err != nil {
		log.Print("TinyPlay is already running; exiting this duplicate launch.")
		os.Exit(0)
	}
	guardLn = ln
}
