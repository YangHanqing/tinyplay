// Command tvremote is the headless core: it runs the HTTP server (REST API +
// embedded phone frontend + mpv control) and then hands off to the
// platform-specific desktop shell.
//
//   - macOS: the SwiftUI app (see ../../macos) launches this binary as a
//     sidecar; runShell just blocks until the parent kills it.
//   - Windows: runShell brings up the systray + a WebView2 window showing the
//     intro/QR page.
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"tvremote/internal/config"
	"tvremote/internal/emby"
	"tvremote/internal/i18n"
	"tvremote/internal/netutil"
	"tvremote/internal/player"
	"tvremote/internal/server"
)

func main() {
	logPath := setupLogging()
	guardSingleInstance() // exits a duplicate Windows launch before doing any work
	cfg := config.Load()
	log.Printf(i18n.System("log_start"), filepath.Dir(logPath))
	p := player.New()
	wireReporters(p)

	// Bind before doing anything else so we know the real port: if the
	// configured one is taken (e.g. the user already runs the Python build on
	// 8080), fall back to a free port instead of crashing. The actual port is
	// then advertised everywhere (QR, intro page, handshake file).
	ln, port := listen(cfg.ListenPort)

	srv := server.New(p)
	srv.SetPort(port)
	httpSrv := &http.Server{Handler: srv.Handler()}

	localURL := fmt.Sprintf("http://%s:%d", netutil.LocalIP(), port)

	// Handshake for the native macOS shell: it sets TVREMOTE_URL_FILE, launches
	// this core as a sidecar, then reads the URL back to point its QR window at
	// the right LAN address/port. The Windows shell builds localURL in-process
	// and ignores this.
	if f := os.Getenv("TVREMOTE_URL_FILE"); f != "" {
		_ = os.WriteFile(f, []byte(localURL), 0o644)
	}

	go func() {
		log.Printf(i18n.System("log_ready"), localURL)
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf(i18n.System("log_http_failed"), err)
		}
	}()

	runShell(localURL, httpSrv)
}

// setupLogging tees the Go core's log output to a file under config.LogDir() so
// that, in a windowless double-click app, there is still a place to look when
// something goes wrong (e.g. mpv crashing on a specific video). Returns the log
// file path. On any failure it leaves logging on stderr and returns "".
func setupLogging() string {
	dir := config.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "tvremote.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return ""
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return path
}

// listen binds 0.0.0.0:want, then a few ports after it, then an OS-assigned free
// port, returning the listener and the port it actually bound to.
func listen(want int) (net.Listener, int) {
	for _, port := range candidatePorts(want) {
		if ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port)); err == nil {
			return ln, port
		}
	}
	// Last resort: let the OS pick any free port.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		log.Fatalf(i18n.System("log_bind_failed"), err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

func candidatePorts(want int) []int {
	if want <= 0 {
		want = 8080
	}
	ports := make([]int, 0, 6)
	for i := 0; i < 6; i++ {
		ports = append(ports, want+i)
	}
	return ports
}

// wireReporters connects the player's playback-event callbacks to the active
// Emby server (mirrors api.py setting player._stop_reporter / _progress_reporter).
func wireReporters(p *player.Player) {
	p.StopReporter = func(itemID, sessionID string, posTicks int64, mediaSourceID string) {
		if c, err := emby.FromActive(); err == nil {
			c.ReportStopped(itemID, sessionID, posTicks, mediaSourceID)
		}
	}
	p.ProgressReporter = func(itemID, sessionID string, posTicks int64, isPaused bool, mediaSourceID string) {
		if c, err := emby.FromActive(); err == nil {
			c.ReportProgress(itemID, sessionID, posTicks, isPaused, mediaSourceID)
		}
	}
	p.ScreensaverImageProvider = func(itemID, posterItemID string, index int) []byte {
		targetID := posterItemID
		if targetID == "" {
			targetID = itemID
		}
		if targetID == "" {
			return nil
		}
		c, err := emby.FromActive()
		if err != nil {
			return nil
		}
		data, _ := c.BackdropBytes(targetID, 1080, index)
		return data
	}
}
