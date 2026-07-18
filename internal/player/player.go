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
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
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
	// VariantIndex is the zero-based IPTV stream variant currently loaded. It is
	// part of the playback context so every phone remote can highlight the
	// actual stream after a reconnect.
	VariantIndex int `json:"variant_index"`
	// SourceType is "dlna" for receiver playback. It tells the phone UI to
	// remove library-specific controls and prevents media-server reporting.
	SourceType string `json:"source_type"`
	// PlaybackCompleted is set only when mpv exited on a natural EOF (which
	// also covers a seek past the end — mpv reports both the same way) for a
	// series episode. It survives the ctx-clearing that otherwise happens on
	// process exit so the host can coordinate next-episode autoplay and so a
	// connected UI can still reconstruct what just finished; a fresh Play()
	// or an explicit Stop() both reset it to false.
	PlaybackCompleted bool `json:"playback_completed"`
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
	VariantIndex  int
	StartSeconds  float64
	MediaSourceID string
	IsLive        bool
	SourceType    string
	// HTTPHeaders is populated only from the IPTV parser's allow-list. It is
	// never returned through the LAN API or diagnostic report.
	HTTPHeaders map[string]string
}

// Player owns the mpv process and its IPC connection.
type Player struct {
	socket string

	mu      sync.Mutex
	proc    *exec.Cmd
	running bool
	ctx     PlayContext
	// playbackRevision changes whenever the semantic playback context changes.
	// Remote pages use it to distinguish A→B (both may be Emby) from an ordinary
	// progress tick. Waiters blocked in WaitPlaybackRevision are woken via
	// playbackRevWait whenever the revision is bumped.
	playbackRevision uint64
	// playbackRevWait is closed (and replaced) on every revision bump. New
	// initializes it; WaitPlaybackRevision also initializes it lazily for tests
	// that construct Player values directly.
	playbackRevWait chan struct{}

	propsMu   sync.Mutex
	liveProps map[string]any

	connMu sync.Mutex
	conn   net.Conn

	playSessionID string
	mediaSourceID string
	lastPosTicks  int64 // atomic
	// liveRecoveryNotified de-duplicates mpv's end-file/property event burst.
	// The server owns policy and URL re-resolution; Player only reports the
	// engine fact after it has seen a live input terminate unexpectedly.
	liveRecoveryNotified bool

	diagMu            sync.Mutex
	currentDiagnostic *playbackAttempt
	lastDiagnostic    map[string]any

	// Reporters are wired by the server layer; all may be nil.
	StopReporter             func(serverID, itemID, sessionID string, posTicks int64, durationSeconds float64, mediaSourceID string)
	ProgressReporter         func(serverID, itemID, sessionID string, posTicks int64, durationSeconds float64, isPaused bool, mediaSourceID string)
	PlaybackStartedReporter  func(context PlayContext)
	LiveInterruptionReporter func(context PlayContext, reason string)
	// BeforePlay runs immediately when Play starts (e.g. close website window).
	// Optional; must be quick and non-blocking for the caller path.
	BeforePlay func()
	// NaturalEOFReporter fires after a natural end-file (including seek past
	// EOF) for a series episode. The server owns autoplay policy, resolution,
	// and the grace-period timer so a disconnected browser cannot block the
	// transition. Non-series, live, error, and explicit-stop exits never fire.
	NaturalEOFReporter func(context PlayContext, revision uint64)

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
	p := &Player{
		socket:          socket,
		liveProps:       map[string]any{},
		playbackRevWait: make(chan struct{}),
	}
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
	if exe := systemMPV(); exe != "" {
		return MPVInfo{Path: exe, Source: "system", Available: true}
	}
	return MPVInfo{Source: "missing"}
}

// systemMPV finds an mpv supplied by the operating system. Finder-launched
// macOS apps do not inherit a shell's Homebrew PATH, so check the two standard
// Homebrew locations after PATH. This remains a development fallback only:
// released TinyPlay apps are required to carry their own bundled runtime.
func systemMPV() string {
	if exe, err := exec.LookPath("mpv"); err == nil {
		return exe
	}
	if runtime.GOOS == "darwin" {
		for _, candidate := range []string{"/opt/homebrew/bin/mpv", "/usr/local/bin/mpv"} {
			if exe := resolveMPV(candidate); exe != "" {
				return exe
			}
		}
	}
	return ""
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
		bytesPerCacheSecond = 512 * 1024
		minMaxBytes         = 128 * 1024 * 1024
		// Keep the normal cache safe on 2 GB Windows PCs. This is packet
		// cache only; mpv, the OS, decoder and GPU still need memory too.
		maxMaxBytes  = 512 * 1024 * 1024
		minBackBytes = 64 * 1024 * 1024
		maxBackBytes = 64 * 1024 * 1024
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

const (
	liveCacheSecs     = 10
	liveCacheMaxBytes = 32 * 1024 * 1024
)

func liveCacheArgs() []string {
	return []string{
		"--cache=yes",
		"--cache-secs=" + strconv.Itoa(liveCacheSecs),
		"--demuxer-max-bytes=" + strconv.Itoa(liveCacheMaxBytes),
		// Live streams have no useful backwards seek range, so don't reserve
		// memory for one.
		"--demuxer-max-back-bytes=0",
	}
}

func (p *Player) applyCacheOptions() {
	secs, maxBytes, backBytes := cacheConfig()
	p.send([]any{"set_property", "cache", "yes"})
	p.send([]any{"set_property", "cache-secs", secs})
	p.send([]any{"set_property", "demuxer-max-bytes", maxBytes})
	p.send([]any{"set_property", "demuxer-max-back-bytes", backBytes})
}

func (p *Player) applyLiveCacheOptions() {
	p.send([]any{"set_property", "cache", "yes"})
	p.send([]any{"set_property", "cache-secs", liveCacheSecs})
	p.send([]any{"set_property", "demuxer-max-bytes", liveCacheMaxBytes})
	p.send([]any{"set_property", "demuxer-max-back-bytes", 0})
}

func (p *Player) disableCacheOptions() {
	// Explicitly override a user's mpv.conf too. Live inputs and local media
	// should not accumulate a large packet buffer just because mpv's defaults
	// (or an old process's settings) allow one.
	p.send([]any{"set_property", "cache", "no"})
}

type playbackCacheMode uint8

const (
	cacheDisabled playbackCacheMode = iota
	cacheLive
	cacheOnDemand
)

// playbackCacheModeFor keeps the rule based on the kind of media rather than
// its transport: IPTV gets a short, bounded live buffer; all remote on-demand
// playback (including DLNA URLs and LAN NAS sources) gets the user's selected
// buffer; only a direct local folder is left to the OS filesystem cache.
func playbackCacheModeFor(sourceType string) playbackCacheMode {
	switch sourceType {
	case "local":
		return cacheDisabled
	case "iptv":
		return cacheLive
	default:
		return cacheOnDemand
	}
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

func mpvHTTPHeaderFields(headers map[string]string) (userAgent, fields string) {
	if len(headers) == 0 {
		return "", ""
	}
	keys := make([]string, 0, len(headers))
	for name := range headers {
		if name != "User-Agent" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		value := strings.TrimSpace(headers[name])
		if value == "" || strings.ContainsAny(value, "\r\n") {
			continue
		}
		parts = append(parts, name+": "+value)
	}
	return strings.TrimSpace(headers["User-Agent"]), strings.Join(parts, ",")
}

// applyHTTPHeaders runs before loadfile so a reused mpv process cannot carry a
// previous commercial playlist's Cookie/Referer into another source.
func (p *Player) applyHTTPHeaders(headers map[string]string) {
	userAgent, fields := mpvHTTPHeaderFields(headers)
	if userAgent == "" {
		userAgent = "TinyPlay/1.0"
	}
	p.send([]any{"set_property", "user-agent", userAgent})
	p.send([]any{"set_property", "http-header-fields", fields})
}

func mpvHTTPHeaderArgs(headers map[string]string) []string {
	userAgent, fields := mpvHTTPHeaderFields(headers)
	args := []string{"--user-agent=" + firstNonEmptyString(userAgent, "TinyPlay/1.0")}
	if fields != "" {
		args = append(args, "--http-header-fields="+fields)
	}
	return args
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

// send writes a command frame to mpv and returns {ok, error} like Python's
// _send. command is either an []any (positional command, e.g. ["seek", 5]) or a
// map[string]any (named command); mpv's JSON IPC accepts both shapes.
func (p *Player) send(command any) map[string]any {
	frame, _ := json.Marshal(map[string]any{"command": command})
	frame = append(frame, '\n')
	p.connMu.Lock()
	defer p.connMu.Unlock()
	if p.conn == nil {
		return map[string]any{"ok": false, "error": "mpv is not connected"}
	}
	// Bound the write so a wedged mpv can't hold connMu forever and stall every
	// subsequent command; a timed-out write drops the connection and propReader
	// reconnects.
	_ = p.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
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
		// The IPC pipe can disappear briefly while mpv is still alive. Do not
		// erase the attempt here: the process waiter is the authoritative owner
		// of terminal cleanup and can preserve the actual exit/end-file evidence.
		p.clearConn()
		p.appendDiagnosticEvent("ipc_disconnected", map[string]any{})
		time.Sleep(time.Second)
	}
}

func (p *Player) handleEvent(line []byte) {
	var msg struct {
		Event string          `json:"event"`
		Name  string          `json:"name"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return
	}
	if msg.Event != "property-change" {
		// mpv flattens event-specific fields onto the event object itself;
		// "data" is property-change's own payload field, not a wrapper, so
		// end-file's reason/error only exist at the top level. recordMPVEvent
		// whitelists the keys it keeps, so parsing the whole line cannot leak
		// a URL or path into the diagnostic log.
		var fields map[string]any
		_ = json.Unmarshal(line, &fields)
		p.recordMPVEvent(msg.Event, fields)
		switch msg.Event {
		case "file-loaded":
			p.reportPlaybackStarted()
		case "end-file":
			reason, _ := fields["reason"].(string)
			if reason == "error" {
				p.reportLiveInterruption(reason)
			}
		}
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

func (p *Player) reportPlaybackStarted() {
	reporter := p.PlaybackStartedReporter
	if reporter == nil {
		return
	}
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	// IPTV catch-up is deliberately not marked live (it must expose seek and
	// duration), but it is still a confirmed channel playback and belongs in
	// the same recently-watched history as its live counterpart.
	if (ctx.SourceType == "iptv" || ctx.SourceType == "iptv-catchup") && ctx.ChannelID != "" {
		go reporter(ctx)
	}
}

func (p *Player) reportLiveInterruption(reason string) {
	reporter := p.LiveInterruptionReporter
	if reporter == nil {
		return
	}
	p.mu.Lock()
	ctx := p.ctx
	if !ctx.IsLive || ctx.ChannelID == "" || p.liveRecoveryNotified {
		p.mu.Unlock()
		return
	}
	p.liveRecoveryNotified = true
	p.mu.Unlock()
	go reporter(ctx, reason)
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
		p.mu.Lock()
		sourceType := p.ctx.SourceType
		p.mu.Unlock()
		if sourceType == "dlna" {
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
		sessionID := p.playSessionID
		mediaSourceID := p.mediaSourceID
		p.mu.Unlock()
		reporter(serverID, itemID, sessionID, pos, duration, isPaused, mediaSourceID)
	}
}

func (p *Player) fireStopReport() {
	reporter := p.StopReporter
	p.mu.Lock()
	itemID := p.ctx.ItemID
	serverID := p.ctx.ServerID
	sourceType := p.ctx.SourceType
	sessionID := p.playSessionID
	mediaSourceID := p.mediaSourceID
	p.mu.Unlock()
	if reporter == nil || itemID == "" || sourceType == "dlna" {
		return
	}
	p.propsMu.Lock()
	duration := numeric(p.liveProps["duration"])
	p.propsMu.Unlock()
	go reporter(serverID, itemID, sessionID, atomic.LoadInt64(&p.lastPosTicks), duration, mediaSourceID)
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
	revision := p.playbackRevision
	p.mu.Unlock()
	diagnosticAvailable, diagnosticScope := p.DiagnosticStatus()
	return map[string]any{
		"running":                running,
		"playback_revision":      revision,
		"server_id":              ctx.ServerID,
		"item_id":                ctx.ItemID,
		"series_id":              ctx.SeriesID,
		"season_id":              ctx.SeasonID,
		"title":                  ctx.Title,
		"series_title":           ctx.SeriesTitle,
		"episode_label":          ctx.EpisodeLabel,
		"poster_item_id":         ctx.PosterItemID,
		"is_live":                ctx.IsLive,
		"channel_id":             ctx.ChannelID,
		"variant_index":          ctx.VariantIndex,
		"source_type":            ctx.SourceType,
		"playback_completed":     ctx.PlaybackCompleted,
		"debug_report_available": diagnosticAvailable,
		"debug_report_scope":     diagnosticScope,
	}
}

// bumpPlaybackRevisionLocked increments playbackRevision and wakes every waiter
// currently blocked in WaitPlaybackRevision. Caller must hold p.mu.
func (p *Player) bumpPlaybackRevisionLocked() {
	p.playbackRevision++
	prev := p.playbackRevWait
	p.playbackRevWait = make(chan struct{})
	if prev != nil {
		close(prev)
	}
}

// WaitPlaybackRevision blocks until playbackRevision differs from after, or
// until ctx is cancelled / times out. A mismatched revision (already advanced
// or otherwise not equal to after) returns immediately. Safe for concurrent
// callers; each waiter receives its own closed-channel wake-up.
func (p *Player) WaitPlaybackRevision(ctx context.Context, after uint64) {
	for {
		p.mu.Lock()
		if p.playbackRevision != after {
			p.mu.Unlock()
			return
		}
		wait := p.playbackRevWait
		if wait == nil {
			wait = make(chan struct{})
			p.playbackRevWait = wait
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-wait:
			// Re-check under the mutex; another bump may have landed while we
			// were unlocked, or this close may not yet match a change away from
			// after if a waiter was registered on a previous generation.
		}
	}
}

// allowedRemoteVerbs are the mpv IPC verbs the untrusted remote (the phone UI
// over HTTP, and DLNA senders) is permitted to send. The playback engine is
// driven internally via p.send(), which bypasses this gate; Command() is the
// only path reachable from the network, so restricting it here keeps mpv's
// more powerful IPC verbs — above all `run`, which executes arbitrary external
// programs — from being turned into a LAN command-execution channel.
var allowedRemoteVerbs = map[string]bool{
	"seek": true, "cycle": true, "set_property": true,
	"add": true, "multiply": true, "cycle-values": true,
	"frame-step": true, "frame-back-step": true,
}

// verbs whose first argument names a property; that property must itself be
// allow-listed below. Verbs not in this set (seek, frame-step, …) take numeric
// arguments and need no property check.
var propertyMutatingVerbs = map[string]bool{
	"set_property": true, "add": true, "multiply": true,
	"cycle": true, "cycle-values": true,
}

// allowedRemoteProps are the benign playback/rendering properties the remote
// may read-modify-write. Every entry only affects how the current file is
// decoded or displayed; none can load files/scripts or reach outside mpv.
var allowedRemoteProps = map[string]bool{
	"pause": true, "speed": true, "sid": true, "aid": true,
	"sub-delay": true, "audio-delay": true,
	"sub-visibility": true, "sub-scale": true, "sub-pos": true,
	"video-aspect-override": true, "video-unscaled": true,
	"panscan": true, "keepaspect": true,
	"loop-file": true, "loop-playlist": true,
	"contrast": true, "brightness": true, "gamma": true, "saturation": true, "hue": true,
	"volume": true, "mute": true, // DLNA RenderingControl sets these
}

// allowedRemoteCommand reports whether a command from the network is a benign
// playback control rather than an attempt to abuse mpv's fuller IPC surface.
func allowedRemoteCommand(cmd []any) bool {
	if len(cmd) == 0 {
		return false
	}
	verb, ok := cmd[0].(string)
	if !ok || !allowedRemoteVerbs[verb] {
		return false
	}
	if propertyMutatingVerbs[verb] {
		if len(cmd) < 2 {
			return false
		}
		prop, ok := cmd[1].(string)
		if !ok || !allowedRemoteProps[prop] {
			return false
		}
	}
	return true
}

func (p *Player) Command(cmd []any) map[string]any {
	if !allowedRemoteCommand(cmd) {
		return map[string]any{"ok": false, "error": "command not allowed"}
	}
	p.dismissScreensaver()
	return p.send(cmd)
}

// PlaySessionID exposes the current session id (used right after Play to report
// playback start, as api.py does).
func (p *Player) PlaySessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playSessionID
}

func initialMPVArgs(url, socket string) []string {
	return []string{
		url,
		"--input-ipc-server=" + socket,
		"--terminal=yes",
		"--fullscreen",
		// Playback is controlled from TinyPlay's phone remote. Keep mpv's
		// mouse-triggered on-screen controller out of the viewing experience.
		"--no-osc",
		// Size fullscreen against the display mpv is actually opened on.
		"--fs-screen=current",
		"--hwdec=auto-safe",
		// The phone slider drives OS volume, so mpv stays at unity gain.
		"--volume=100",
		// Override keep-open=yes from a user's mpv.conf. Host autoplay needs a
		// real process exit after the single-item playlist reaches natural EOF.
		"--keep-open=no",
	}
}

func (p *Player) Play(url string, opt PlayOptions) map[string]any {
	if p.BeforePlay != nil {
		p.BeforePlay()
	}
	p.hideScreensaver()
	p.beginDiagnostic(url, opt)

	// Switching to a different item while playing: stop-report the old one.
	p.mu.Lock()
	prev := p.ctx.ItemID
	p.mu.Unlock()
	if prev != "" && prev != opt.ItemID {
		p.fireStopReport()
	}

	// playSessionID/mediaSourceID are read by the progress + stop reporters
	// (other goroutines), so all access is guarded by p.mu.
	mediaSourceID := opt.MediaSourceID
	if mediaSourceID == "" {
		mediaSourceID = opt.ItemID
	}
	p.mu.Lock()
	p.playSessionID = randHex()
	p.mediaSourceID = mediaSourceID
	p.mu.Unlock()
	// Seed the position with the resume point rather than 0: if mpv dies (or the
	// user stops) before the first time-pos event ever arrives, the stop report
	// must not tell the server/local history the item was abandoned at 0:00 —
	// that would erase the saved resume point the play was about to honour.
	atomic.StoreInt64(&p.lastPosTicks, int64(opt.StartSeconds*1e7))

	startValue := "none"
	if opt.StartSeconds > 0 {
		startValue = formatFloat(opt.StartSeconds)
	}

	var result map[string]any
	if p.isRunning() {
		switch playbackCacheModeFor(opt.SourceType) {
		case cacheOnDemand:
			p.applyCacheOptions()
		case cacheLive:
			p.applyLiveCacheOptions()
		default:
			p.disableCacheOptions()
		}
		p.applyHTTPHeaders(opt.HTTPHeaders)
		p.send([]any{"set_property", "start", startValue})
		// Force completion even when the user's mpv.conf sets keep-open=yes;
		// otherwise natural EOF never exits and host autoplay cannot run.
		p.send([]any{"set_property", "keep-open", "no"})
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
		p.appendDiagnosticEvent("mpv_loadfile_requested", map[string]any{"reused_process": true})
	} else {
		playerLog, mpvLog := openPlayerLog()
		exe := p.mpvExe()
		args := initialMPVArgs(url, p.socket)
		switch playbackCacheModeFor(opt.SourceType) {
		case cacheOnDemand:
			args = append(args, cacheArgs()...)
		case cacheLive:
			args = append(args, liveCacheArgs()...)
		default:
			args = append(args, "--cache=no")
		}
		args = append(args, aspectArgs()...)
		args = append(args, mpvHTTPHeaderArgs(opt.HTTPHeaders)...)
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
			p.appendDiagnosticEvent("mpv_process_start_failed", map[string]any{})
			p.finalizeDiagnostic("engine_start_failed", "engine_start")
			return map[string]any{"ok": false, "error": "Could not start the player: " + exe}
		}
		log.Printf("Started player %s (pid=%d), log: %s", exe, cmd.Process.Pid, mpvLog)
		p.mu.Lock()
		p.proc = cmd
		p.running = true
		p.mu.Unlock()
		p.appendDiagnosticEvent("mpv_process_started", map[string]any{"source": DetectMPV().Source})
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
			isCurrent := p.proc == cmd
			if isCurrent {
				p.running = false
				p.proc = nil
			}
			p.mu.Unlock()
			if isCurrent {
				exit := "clean"
				if err != nil {
					exit = err.Error()
				}
				p.diagMu.Lock()
				if p.currentDiagnostic != nil {
					p.currentDiagnostic.ProcessExit = exit
				}
				p.diagMu.Unlock()
				p.appendDiagnosticEvent("mpv_process_exited", map[string]any{"result": exit})
				p.diagMu.Lock()
				naturalEOF := p.currentDiagnostic != nil && p.currentDiagnostic.MPVEndReason == "eof"
				p.diagMu.Unlock()
				p.fireStopReport()
				p.finalizeDiagnostic("engine_process_exit", "engine_process")
				p.mu.Lock()
				finished := p.ctx
				p.mu.Unlock()
				// An mpv crash can arrive without a final end-file event. Preserve
				// the same bounded live-recovery path, while explicit Stop() has
				// already cleared ctx and therefore cannot resurrect playback.
				if finished.IsLive && !naturalEOF {
					p.reportLiveInterruption("process_exit")
				}
				p.mu.Lock()
				completedSeries := naturalEOF && finished.SeriesID != "" && finished.ItemID != ""
				var completed PlayContext
				if completedSeries {
					completed = PlayContext{
						ServerID:          finished.ServerID,
						ItemID:            finished.ItemID,
						SeriesID:          finished.SeriesID,
						SeasonID:          finished.SeasonID,
						Title:             finished.Title,
						SeriesTitle:       finished.SeriesTitle,
						EpisodeLabel:      finished.EpisodeLabel,
						PosterItemID:      finished.PosterItemID,
						SourceType:        finished.SourceType,
						PlaybackCompleted: true,
					}
					p.ctx = completed
				} else {
					p.ctx = PlayContext{}
				}
				p.bumpPlaybackRevisionLocked()
				completedRevision := p.playbackRevision
				reporter := p.NaturalEOFReporter
				p.mu.Unlock()
				p.propsMu.Lock()
				p.liveProps = map[string]any{}
				p.propsMu.Unlock()
				if completedSeries && reporter != nil {
					reporter(completed, completedRevision)
				}
			}
		}()
		result = map[string]any{"ok": true}
	}

	p.mu.Lock()
	p.liveRecoveryNotified = false
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
		VariantIndex: opt.VariantIndex,
		SourceType:   opt.SourceType,
	}
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()
	return result
}

func (p *Player) Stop() map[string]any {
	p.hideScreensaver()
	p.appendDiagnosticEvent("stop_requested", map[string]any{})
	p.finalizeDiagnostic("user_stop", "user_action")
	p.fireStopReport()
	p.mu.Lock()
	proc := p.proc
	p.mu.Unlock()
	result := p.send([]any{"quit"})
	if proc != nil && proc.Process != nil {
		go func(target *exec.Cmd) {
			time.Sleep(2 * time.Second)
			p.mu.Lock()
			stillRunning := p.running && p.proc == target
			p.mu.Unlock()
			if stillRunning {
				_ = target.Process.Kill()
			}
		}(proc)
	}
	p.mu.Lock()
	p.liveRecoveryNotified = false
	p.ctx = PlayContext{}
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()
	p.propsMu.Lock()
	p.liveProps = map[string]any{}
	p.propsMu.Unlock()
	return result
}

// ClearCompletedPlayback drops a post-EOF completed series context (used when
// the host cancels pending autoplay without starting a new title).
func (p *Player) ClearCompletedPlayback() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.ctx.PlaybackCompleted {
		return
	}
	p.ctx = PlayContext{}
	p.bumpPlaybackRevisionLocked()
}
