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
	"sync"
	"time"
	"unicode"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/netutil"
	"tvremote/internal/player"
	"tvremote/internal/provider"
	"tvremote/internal/server"
)

// version is set at build time via -ldflags "-X main.version=1.2.3" (see
// build-app.sh / release.yml); it defaults to "dev" for local `go run`/`go build`.
var version = "dev"

// bindHost is the interface the HTTP server listens on. It is deliberately all
// interfaces (0.0.0.0) so phones on the LAN can reach the remote; see the LAN
// security model (command allow-list + Origin guard) in internal/server.
const bindHost = "0.0.0.0"

func main() {
	logPath := setupLogging()
	guardSingleInstance() // exits a duplicate Windows launch before doing any work
	cfg := config.Load()
	if lang := os.Getenv("TVREMOTE_LANGUAGE"); lang != "" {
		config.SetLanguage(lang)
		cfg = config.Load()
	}
	i18n.SetPreferred(cfg.Language)
	// Logs are diagnostic artifacts that may be shared across locales, so keep
	// their text stable and searchable in English regardless of UI language.
	log.Printf("TinyPlay starting; log directory: %s", filepath.Dir(logPath))
	p := player.New()
	wireReporters(p)

	// Bind before doing anything else so we know the real port: if the
	// configured one is taken (e.g. the user already runs another build on
	// the same port), fall back to a free port instead of crashing. The actual
	// port is then advertised everywhere (QR, intro page, handshake file).
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
		log.Printf("TinyPlay ready; phone URL: %s", localURL)
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP service failed: %v", err)
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
	archiveNonEnglishLog(path)
	f, err := newRotatingLogFile(path, 5<<20, 3)
	if err != nil {
		return ""
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return path
}

// rotatingLogFile bounds the active core log while the desktop app stays open
// for a long time. log.Logger may write from several goroutines, so rotation
// and writes share one lock.
type rotatingLogFile struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	maxBytes int64
	backups  int
}

func newRotatingLogFile(path string, maxBytes int64, backups int) (*rotatingLogFile, error) {
	rotateLog(path, maxBytes, backups)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &rotatingLogFile{file: f, path: path, maxBytes: maxBytes, backups: backups}, nil
}

func (f *rotatingLogFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	info, err := f.file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() > 0 && info.Size()+int64(len(p)) > f.maxBytes {
		if err := f.file.Close(); err != nil {
			return 0, err
		}
		rotateLogNow(f.path, f.backups)
		f.file, err = os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, err
		}
	}
	return f.file.Write(p)
}

// archiveNonEnglishLog keeps the active diagnostic file consistently English
// after upgrading from releases that localised log lines. The old file is kept
// intact beside it for support history rather than being discarded.
func archiveNonEnglishLog(path string) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, r := range string(contents) {
		if unicode.Is(unicode.Han, r) {
			archive := fmt.Sprintf("%s.pre-english-%s", path, time.Now().UTC().Format("20060102T150405Z"))
			_ = os.Rename(path, archive)
			return
		}
	}
}

// rotateLog bounds the persistent core log. The long-lived core log keeps at
// most three 5 MiB history files plus the active file; mpv.log has its own hard
// ceiling in internal/player.
func rotateLog(path string, maxBytes int64, backups int) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes || backups < 1 {
		return
	}
	rotateLogNow(path, backups)
}

func rotateLogNow(path string, backups int) {
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for i := backups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, fmt.Sprintf("%s.%d", path, i+1))
		}
	}
	_ = os.Rename(path, path+".1")
}

// listen binds 0.0.0.0:want, then a few ports after it, then an OS-assigned free
// port, returning the listener and the port it actually bound to.
func listen(want int) (net.Listener, int) {
	for _, port := range candidatePorts(want) {
		if ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost, port)); err == nil {
			return ln, port
		}
	}
	// Last resort: let the OS pick any free port.
	ln, err := net.Listen("tcp", bindHost+":0")
	if err != nil {
		log.Fatalf("Could not bind any port: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

func candidatePorts(want int) []int {
	if want <= 0 {
		want = 1980
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
	p.StopReporter = func(serverID, itemID, sessionID string, posTicks int64, duration float64, mediaSourceID string) {
		if srv := config.GetServer(serverID); srv != nil && config.IsFileServerType(srv.Type) {
			config.RecordLocalPlayback(serverID, itemID, float64(posTicks)/1e7, duration)
			return
		}
		if c, err := provider.FromServer(serverID); err == nil {
			c.ReportStopped(itemID, sessionID, posTicks, mediaSourceID)
		}
	}
	p.ProgressReporter = func(serverID, itemID, sessionID string, posTicks int64, duration float64, isPaused bool, mediaSourceID string) {
		if srv := config.GetServer(serverID); srv != nil && config.IsFileServerType(srv.Type) {
			config.RecordLocalPlayback(serverID, itemID, float64(posTicks)/1e7, duration)
			return
		}
		if c, err := provider.FromServer(serverID); err == nil {
			c.ReportProgress(itemID, sessionID, posTicks, isPaused, mediaSourceID)
		}
	}
	p.ScreensaverImageProvider = func(serverID, itemID, posterItemID string, index int) []byte {
		targetID := posterItemID
		if targetID == "" {
			targetID = itemID
		}
		if targetID == "" {
			return nil
		}
		c, err := provider.FromServer(serverID)
		if err != nil {
			return nil
		}
		data, _ := c.BackdropBytes(targetID, 1080, index)
		return data
	}
}
