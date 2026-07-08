package main

import (
	"log"
	"net"
	"os"
	"runtime"

	"tvremote/internal/i18n"
)

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
	// Loopback-only, arbitrary high port used purely as a lock.
	ln, err := net.Listen("tcp", "127.0.0.1:47611")
	if err != nil {
		log.Print(i18n.System("log_already_running"))
		os.Exit(0)
	}
	guardLn = ln
}
