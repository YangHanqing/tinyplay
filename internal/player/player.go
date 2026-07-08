// Package player is the Go port of app/services/player.py: it controls mpv over
// its JSON IPC channel and stores the play context that survives browser
// reconnects.
//
// It is decoupled from emby: the stop/progress reporters are injected as
// callbacks (the HTTP layer wires them to the Emby client), mirroring how
// api.py sets player._stop_reporter / _progress_reporter.
package player

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"tvremote/internal/config"
)

var observeProps = []string{
	"time-pos", "duration", "percent-pos",
	"pause", "core-idle", "paused-for-cache", "volume", "mute", "speed",
	"sub-delay", "audio-delay", "avsync",
	"video-codec", "width", "height", "container-fps",
	"dwidth", "dheight",
	"audio-codec", "audio-samplerate", "audio-channels",
	"hwdec", "hwdec-current", "file-format",
	"video-format", "video-params", "video-target-params",
	"video-bitrate", "audio-bitrate", "demuxer-cache-state", "cache-speed",
	"frame-drop-count", "vo-drop-frame-count",
	"decoder-frame-drop-count", "mistimed-frame-count",
	"track-list", "current-tracks",
	"scale", "audio-device", "audio-out-params",
	"osd-width", "osd-height",
}

// PlayContext survives browser disconnects so GET /api/player/state can rebuild
// the episode list. JSON keys match the Python branch / app.js expectations.
type PlayContext struct {
	ServerID     string `json:"server_id"`
	ItemID       string `json:"item_id"`
	SeriesID     string `json:"series_id"`
	SeasonID     string `json:"season_id"`
	Title        string `json:"title"`
	SeriesTitle  string `json:"series_title"`
	EpisodeLabel string `json:"episode_label"`
	PosterItemID string `json:"poster_item_id"`
	// IsLive marks an IPTV channel play: the frontend hides the seek bar and
	// duration for these instead of inferring it from the active source type
	// (a source switch could happen mid-session).
	IsLive bool `json:"is_live"`
	// ChannelID is deliberately separate from ItemID: ItemID doubles as the
	// gate for this package's own background Emby progress/stop reporting
	// (see fireStopReport/progressRun below), which must stay suppressed for
	// IPTV (a channel is not an Emby item). ChannelID exists purely so
	// state() can still tell the frontend which channel survived a browser
	// reconnect, without re-enabling that reporting.
	ChannelID string `json:"channel_id"`
}

// PlayOptions are the arguments to Play (mirrors player.play kwargs).
type PlayOptions struct {
	ServerID      string
	Title         string
	ItemID        string
	SeriesID      string
	SeasonID      string
	SeriesTitle   string
	EpisodeLabel  string
	PosterItemID  string
	ChannelID     string
	StartSeconds  float64
	MediaSourceID string
	IsLive        bool
}

// Player owns the mpv process and its IPC connection.
type Player struct {
	socket string

	mu      sync.Mutex
	proc    *exec.Cmd
	running bool
	ctx     PlayContext

	propsMu   sync.Mutex
	liveProps map[string]any

	connMu sync.Mutex
	conn   net.Conn

	playSessionID string
	mediaSourceID string
	lastPosTicks  int64 // atomic

	// Reporters are wired by the server layer; both may be nil.
	StopReporter     func(serverID, itemID, sessionID string, posTicks int64, durationSeconds float64, mediaSourceID string)
	ProgressReporter func(serverID, itemID, sessionID string, posTicks int64, durationSeconds float64, isPaused bool, mediaSourceID string)

	// ScreensaverImageProvider is injected by the server layer so player stays
	// decoupled from Emby. It should return backdrop bytes for the requested
	// image index, or nil when no backdrop is available.
	ScreensaverImageProvider func(serverID, itemID, posterItemID string, index int) []byte

	screensaverMu sync.Mutex
	screensaver   screensaverState
}

// New creates the player and starts its background goroutines.
func New() *Player {
	socket := os.Getenv("TVREMOTE_MPV_SOCKET")
	if socket == "" {
		socket = platformDefaultSocket()
	}
	p := &Player{socket: socket, liveProps: map[string]any{}}
	info := DetectMPV()
	if info.Available {
		log.Printf("mpv detected: source=%s path=%s", info.Source, info.Path)
	} else {
		log.Printf("mpv not found; playback will be unavailable until the bundled runtime is restored or mpv is installed in PATH")
	}
	go p.propReaderRun()
	go p.progressRun()
	go p.screensaverRun()
	return p
}

func (p *Player) mpvExe() string {
	if info := DetectMPV(); info.Available {
		return info.Path
	}
	return "mpv"
}

// MPVInfo describes the usable mpv selected for playback. Source is one of
// custom (TVREMOTE_MPV_EXE), bundled, system, or missing.
type MPVInfo struct {
	Path      string
	Source    string
	Available bool
}

// DetectMPV selects the first usable runtime. Invalid overrides deliberately
// fall through so a stale developer setting cannot disable a bundled build.
func DetectMPV() MPVInfo {
	if exe := resolveMPV(os.Getenv("TVREMOTE_MPV_EXE")); exe != "" {
		return MPVInfo{Path: exe, Source: "custom", Available: true}
	}
	if exe := bundledMPV(); exe != "" {
		return MPVInfo{Path: exe, Source: "bundled", Available: true}
	}
	if exe, err := exec.LookPath("mpv"); err == nil {
		return MPVInfo{Path: exe, Source: "system", Available: true}
	}
	return MPVInfo{Source: "missing"}
}

func resolveMPV(candidate string) string {
	if candidate == "" {
		return ""
	}
	if filepath.IsAbs(candidate) || filepath.Dir(candidate) != "." {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		return ""
	}
	exe, err := exec.LookPath(candidate)
	if err != nil {
		return ""
	}
	return exe
}

func cacheConfig() (int, int, int) {
	secs := config.NormalizeMpvCacheSecs(config.Load().MpvCacheSecs)
	const (
		bytesPerCacheSecond = 2 * 1024 * 1024
		minMaxBytes         = 256 * 1024 * 1024
		maxMaxBytes         = 1536 * 1024 * 1024
		minBackBytes        = 64 * 1024 * 1024
		maxBackBytes        = 256 * 1024 * 1024
	)
	maxBytes := secs * bytesPerCacheSecond
	if maxBytes < minMaxBytes {
		maxBytes = minMaxBytes
	}
	if maxBytes > maxMaxBytes {
		maxBytes = maxMaxBytes
	}
	backBytes := maxBytes / 8
	if backBytes < minBackBytes {
		backBytes = minBackBytes
	}
	if backBytes > maxBackBytes {
		backBytes = maxBackBytes
	}
	return secs, maxBytes, backBytes
}

func cacheArgs() []string {
	secs, maxBytes, backBytes := cacheConfig()
	return []string{
		"--cache=yes",
		"--cache-secs=" + strconv.Itoa(secs),
		"--demuxer-max-bytes=" + strconv.Itoa(maxBytes),
		"--demuxer-max-back-bytes=" + strconv.Itoa(backBytes),
	}
}

func (p *Player) applyCacheOptions() {
	secs, maxBytes, backBytes := cacheConfig()
	p.send([]any{"set_property", "cache", "yes"})
	p.send([]any{"set_property", "cache-secs", secs})
	p.send([]any{"set_property", "demuxer-max-bytes", maxBytes})
	p.send([]any{"set_property", "demuxer-max-back-bytes", backBytes})
}

func aspectArgs() []string {
	return []string{
		"--video-aspect-override=no",
		"--video-unscaled=no",
		"--panscan=0",
		"--keepaspect=yes",
	}
}

func (p *Player) resetAspectOptions() {
	p.send([]any{"set_property", "video-aspect-override", "no"})
	p.send([]any{"set_property", "video-unscaled", "no"})
	p.send([]any{"set_property", "panscan", 0})
	p.send([]any{"set_property", "keepaspect", true})
}

// bundledMPV looks for an mpv shipped alongside our binary. The CI build places
// it there: `mpv/mpv.exe` next to TinyPlay.exe on Windows, and inside the .app
// bundle on macOS (where the Swift shell normally passes TVREMOTE_MPV_EXE, so
// this is just a fallback). Returns "" if no bundled mpv is found.
func bundledMPV() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(self)
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(dir, "mpv", "mpv.exe"),
			filepath.Join(dir, "mpv.exe"),
		}
	} else {
		// Go core lives at Contents/Resources/tvremote-core; the CI build
		// places mpv at Contents/Resources/mpv/bin/mpv (dylibbundler layout).
		// The ../Resources paths are kept as fallback for shells that run the
		// core binary from Contents/MacOS instead.
		candidates = []string{
			filepath.Join(dir, "mpv", "bin", "mpv"),                    // bundled (.app): Resources/mpv/bin/mpv
			filepath.Join(dir, "mpv"),                                  // flat layout next to binary
			filepath.Join(dir, "..", "Resources", "mpv", "bin", "mpv"), // MacOS → Resources fallback
			filepath.Join(dir, "..", "Resources", "mpv", "mpv"),
			filepath.Join(dir, "..", "Resources", "mpv"),
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func (p *Player) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// ── IPC connection management ────────────────────────────────────────────────

func (p *Player) setConn(c net.Conn) {
	p.connMu.Lock()
	p.conn = c
	p.connMu.Unlock()
}

func (p *Player) clearConn() {
	p.connMu.Lock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	p.connMu.Unlock()
}

// send writes a command frame to mpv. Returns {ok, error} like Python's _send.
func (p *Player) send(command []any) map[string]any {
	return p.sendIPC(command)
}

func (p *Player) sendNative(command map[string]any) map[string]any {
	return p.sendIPC(command)
}

func (p *Player) sendIPC(command any) map[string]any {
	frame, _ := json.Marshal(map[string]any{"command": command})
	frame = append(frame, '\n')
	p.connMu.Lock()
	defer p.connMu.Unlock()
	if p.conn == nil {
		return map[string]any{"ok": false, "error": "mpv is not connected"}
	}
	if _, err := p.conn.Write(frame); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// propReaderRun maintains a persistent IPC connection and caches property
// values pushed by mpv's observe_property events.
func (p *Player) propReaderRun() {
	for {
		if !p.isRunning() {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		conn, err := dialMPV(p.socket)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		p.setConn(conn)
		for i, prop := range observeProps {
			frame, _ := json.Marshal(map[string]any{"command": []any{"observe_property", i + 1, prop}})
			conn.Write(append(frame, '\n'))
		}
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				p.handleEvent(line)
			}
			if err != nil {
				break
			}
		}
		// mpv pipe closed — report unexpected exit to Emby and reset.
		p.clearConn()
		p.mu.Lock()
		itemID := p.ctx.ItemID
		p.mu.Unlock()
		if itemID != "" {
			p.fireStopReport()
			p.mu.Lock()
			p.ctx = PlayContext{}
			p.mu.Unlock()
		}
		p.propsMu.Lock()
		p.liveProps = map[string]any{}
		p.propsMu.Unlock()
		time.Sleep(time.Second)
	}
}

func (p *Player) handleEvent(line []byte) {
	var msg struct {
		Event string          `json:"event"`
		Name  string          `json:"name"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(line, &msg) != nil || msg.Event != "property-change" {
		return
	}
	var data any
	_ = json.Unmarshal(msg.Data, &data)
	p.propsMu.Lock()
	p.liveProps[msg.Name] = data
	p.propsMu.Unlock()
	if msg.Name == "time-pos" {
		if f, ok := data.(float64); ok {
			atomic.StoreInt64(&p.lastPosTicks, int64(f*1e7))
		}
	}
}

// progressRun periodically reports live position to Emby so resume stays fresh.
func (p *Player) progressRun() {
	for {
		time.Sleep(10 * time.Second)
		reporter := p.ProgressReporter
		p.mu.Lock()
		itemID := p.ctx.ItemID
		p.mu.Unlock()
		if reporter == nil || itemID == "" || !p.isRunning() {
			continue
		}
		pos := atomic.LoadInt64(&p.lastPosTicks)
		if pos <= 0 {
			continue
		}
		p.propsMu.Lock()
		isPaused, _ := p.liveProps["pause"].(bool)
		duration := numeric(p.liveProps["duration"])
		p.propsMu.Unlock()
		p.mu.Lock()
		serverID := p.ctx.ServerID
		p.mu.Unlock()
		reporter(serverID, itemID, p.playSessionID, pos, duration, isPaused, p.mediaSourceID)
	}
}

func (p *Player) fireStopReport() {
	reporter := p.StopReporter
	p.mu.Lock()
	itemID := p.ctx.ItemID
	serverID := p.ctx.ServerID
	p.mu.Unlock()
	if reporter == nil || itemID == "" {
		return
	}
	p.propsMu.Lock()
	duration := numeric(p.liveProps["duration"])
	p.propsMu.Unlock()
	go reporter(serverID, itemID, p.playSessionID, atomic.LoadInt64(&p.lastPosTicks), duration, p.mediaSourceID)
}

func numeric(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

// ── Public API ───────────────────────────────────────────────────────────────

func (p *Player) Props() map[string]any {
	p.propsMu.Lock()
	defer p.propsMu.Unlock()
	out := make(map[string]any, len(p.liveProps))
	for k, v := range p.liveProps {
		out[k] = v
	}
	return out
}

func (p *Player) State() map[string]any {
	p.mu.Lock()
	ctx := p.ctx
	running := p.running
	p.mu.Unlock()
	return map[string]any{
		"running":        running,
		"item_id":        ctx.ItemID,
		"series_id":      ctx.SeriesID,
		"season_id":      ctx.SeasonID,
		"title":          ctx.Title,
		"series_title":   ctx.SeriesTitle,
		"episode_label":  ctx.EpisodeLabel,
		"poster_item_id": ctx.PosterItemID,
		"is_live":        ctx.IsLive,
		"channel_id":     ctx.ChannelID,
	}
}

func (p *Player) Command(cmd []any) map[string]any {
	p.dismissScreensaver()
	return p.send(cmd)
}

// PlaySessionID exposes the current session id (used right after Play to report
// playback start, as api.py does).
func (p *Player) PlaySessionID() string { return p.playSessionID }

func (p *Player) Play(url string, opt PlayOptions) map[string]any {
	p.hideScreensaver()

	// Switching to a different item while playing: stop-report the old one.
	p.mu.Lock()
	prev := p.ctx.ItemID
	p.mu.Unlock()
	if prev != "" && prev != opt.ItemID {
		p.fireStopReport()
	}

	p.playSessionID = randHex()
	p.mediaSourceID = opt.MediaSourceID
	if p.mediaSourceID == "" {
		p.mediaSourceID = opt.ItemID
	}
	atomic.StoreInt64(&p.lastPosTicks, 0)

	startValue := "none"
	if opt.StartSeconds > 0 {
		startValue = formatFloat(opt.StartSeconds)
	}

	var result map[string]any
	if p.isRunning() {
		p.applyCacheOptions()
		p.send([]any{"set_property", "start", startValue})
		result = p.send([]any{"loadfile", url, "replace"})
		if opt.Title != "" {
			p.send([]any{"set_property", "title", opt.Title})
		}
		p.send([]any{"set_property", "fullscreen", true})
		p.send([]any{"set_property", "hwdec", "auto-safe"})
		// The remote's volume slider controls the OS output level (see
		// internal/sysvolume), so mpv itself is pinned at unity gain —
		// otherwise the two controls would stack and fight each other.
		p.send([]any{"set_property", "volume", 100})
		p.resetAspectOptions()
	} else {
		playerLog, mpvLog := openPlayerLog()
		exe := p.mpvExe()
		args := []string{
			url,
			"--input-ipc-server=" + p.socket,
			"--terminal=yes",
			"--fullscreen",
			// Without this, mpv can go fullscreen against the wrong
			// display's dimensions on multi-monitor setups (e.g. an
			// ultrawide secondary monitor), leaving black bars on all
			// sides instead of filling the screen it's actually on.
			"--fs-screen=current",
			"--hwdec=auto-safe",
			// See the set_property call above: the phone's slider drives
			// the OS volume, so mpv itself always stays at unity gain.
			"--volume=100",
		}
		args = append(args, cacheArgs()...)
		args = append(args, aspectArgs()...)
		if playerLog != nil {
			// Verbose output is captured by Go's bounded writer below, so a
			// long-lived mpv process cannot grow the log without limit.
			args = append(args, "--msg-level=all=v")
		}
		if opt.StartSeconds > 0 {
			args = append(args, "--start="+startValue)
		}
		if opt.Title != "" {
			args = append(args, "--title="+opt.Title)
		}
		cmd := exec.Command(exe, args...)
		// Also capture anything mpv writes to stderr/stdout (panics, loader
		// errors) into the same logs dir.
		if playerLog != nil {
			cmd.Stdout = playerLog
			cmd.Stderr = playerLog
		}
		if err := cmd.Start(); err != nil {
			if playerLog != nil {
				playerLog.Close()
			}
			return map[string]any{"ok": false, "error": "Could not start the player: " + exe}
		}
		log.Printf("Started player %s (pid=%d), log: %s", exe, cmd.Process.Pid, mpvLog)
		p.mu.Lock()
		p.proc = cmd
		p.running = true
		p.mu.Unlock()
		go func() {
			err := cmd.Wait()
			if playerLog != nil {
				playerLog.Close()
			}
			if err != nil {
				// A non-zero exit or signal is the process crash the user observes.
				// Surfacing it here (with the log path) gives us something to act on.
				log.Printf("mpv exited unexpectedly: %v; see %s", err, mpvLog)
			}
			p.mu.Lock()
			p.running = false
			p.mu.Unlock()
		}()
		result = map[string]any{"ok": true}
	}

	p.mu.Lock()
	p.ctx = PlayContext{
		ServerID:     opt.ServerID,
		ItemID:       opt.ItemID,
		SeriesID:     opt.SeriesID,
		SeasonID:     opt.SeasonID,
		Title:        opt.Title,
		SeriesTitle:  opt.SeriesTitle,
		EpisodeLabel: opt.EpisodeLabel,
		PosterItemID: opt.PosterItemID,
		IsLive:       opt.IsLive,
		ChannelID:    opt.ChannelID,
	}
	p.mu.Unlock()
	return result
}

func (p *Player) Stop() map[string]any {
	p.hideScreensaver()
	p.fireStopReport()
	result := p.send([]any{"quit"})
	p.mu.Lock()
	p.ctx = PlayContext{}
	p.mu.Unlock()
	p.propsMu.Lock()
	p.liveProps = map[string]any{}
	p.propsMu.Unlock()
	return result
}
