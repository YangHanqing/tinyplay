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
	"errors"
	"fmt"
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
	// AudioOnly carries PlayOptions.AudioOnly into the state the phone reads.
	// Without it the remote cannot tell a song from a film and offers aspect
	// ratio / subtitle / audio-track controls for a music file. The verdict is
	// made once, from the same extension table (or DIDL class) that decides
	// mpv's artwork handling — the frontend must never re-derive it.
	AudioOnly bool `json:"audio_only"`
	// PlaybackCompleted is set only when mpv exited on a natural EOF (which
	// also covers a seek past the end — mpv reports both the same way) for a
	// media-server series episode or a folder-based file source. It survives
	// the ctx-clearing that otherwise happens on process exit so the host can
	// coordinate autoplay; a fresh Play() or explicit Stop() resets it.
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
	// AudioOnly marks an item with no video track (a cast song, an audio file
	// picked from a file source). It drives mpv's window and artwork handling
	// — see audioart.go, which explains why this must never be guessed "on"
	// for something that might be video.
	AudioOnly bool
	// CoverArtURL is the picture mpv displays in place of the missing video
	// track: the sender's album art when it supplied one, otherwise TinyPlay's
	// own now-playing artwork. Ignored unless AudioOnly.
	CoverArtURL string
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

	// starting is true from the moment Play() commits to a new title until mpv
	// confirms real playback (the "playback-restart" IPC event) or the attempt
	// ends (stop/crash/timeout). The desktop shells use it to show a transient
	// "connecting/buffering" indicator over an otherwise blank or frozen screen
	// while a slow source (remote Emby, IPTV) has not produced a frame yet.
	starting bool
	// startingEpoch guards the safety timer below against a stale AfterFunc
	// clearing a *later* Play() attempt's starting=true after it fires.
	startingEpoch uint64
	startingTimer *time.Timer

	propsMu   sync.Mutex
	liveProps map[string]any

	connMu sync.Mutex
	conn   net.Conn

	playSessionID string
	mediaSourceID string
	lastPosTicks  int64 // atomic
	// startPosTicks is where this play was asked to begin, kept so an EOF can
	// be judged by how far playback actually advanced rather than by where it
	// ended up. Written by Play, read on the process-exit path; atomic.
	startPosTicks int64 // atomic
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
	// EOF) for a non-live item with playback context. The server owns source
	// eligibility, resolution, and the grace-period timer so a disconnected
	// browser cannot block the transition. Live, error, and explicit-stop exits
	// never fire.
	NaturalEOFReporter func(context PlayContext, revision uint64)
	// LifecycleReporter is an optional, edge-triggered transport observer for
	// consumers such as the DLNA MediaRenderer GENA path. It is called from
	// player goroutines with a PlayContext snapshot; keep handlers short or
	// hand off work. Nil is fine.
	LifecycleReporter func(event LifecycleEvent, ctx PlayContext, durationSeconds float64, reason string)

	// ScreensaverImageProvider is injected by the server layer so player stays
	// decoupled from Emby. It should return backdrop bytes for the requested
	// image index, or nil when no backdrop is available.
	ScreensaverImageProvider func(serverID, itemID, posterItemID string, index int) []byte

	screensaverMu sync.Mutex
	screensaver   screensaverState

	// lastPause / durationNotified edge-trigger LifecycleReporter without
	// spamming every property tick.
	lastPause        *bool
	durationNotified bool

	// sessionSpeed is the last speed observed during this process lifetime.
	// It is never written to config.json: persisting it would mean a user who
	// left 2x on last week silently gets 2x today, and keep_playback_speed is
	// about continuity within a viewing session only.
	sessionSpeed float64
	// titleDirty is reset on every Play and set only by remote commands that
	// touch one of the five remembered properties.
	titleDirty titleSettingsDirty
	// pendingRestore holds a per-title record whose tracks still need a
	// track-list (applied on file-loaded / track-list updates).
	pendingRestore *pendingTitleRestore
}

// LifecycleEvent names engine-level transport transitions used by DLNA GENA.
type LifecycleEvent string

const (
	LifecyclePlaying  LifecycleEvent = "playing"  // playback-restart
	LifecyclePaused   LifecycleEvent = "paused"   // pause true
	LifecycleResumed  LifecycleEvent = "resumed"  // pause false after pause
	LifecycleStopped  LifecycleEvent = "stopped"  // Stop() or non-EOF process exit
	LifecycleEOF      LifecycleEvent = "eof"      // natural end-file
	LifecycleError    LifecycleEvent = "error"    // end-file error
	LifecycleDuration LifecycleEvent = "duration" // first duration > 0 for this load
)

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
		sessionSpeed:    1.0,
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
// custom (the user's persisted advanced-settings path), env
// (TVREMOTE_MPV_EXE), bundled, system, or missing.
type MPVInfo struct {
	Path      string
	Source    string
	Available bool
	// CustomConfigured is true whenever the user has a non-empty persisted
	// mpv path (config.MpvExe), regardless of whether it currently resolves.
	// CustomInvalid is true when that path is configured but did not resolve
	// to a usable executable, so DetectMPV fell through to a lower-priority
	// candidate — the case the UI should render as "custom path is stale,
	// falling back to bundled" rather than silently showing "bundled".
	CustomConfigured bool
	CustomInvalid    bool
}

// DetectMPV selects the first usable runtime, in priority order:
//  1. the user's persisted custom path (desktop shell "高级设置", stored as
//     config.MpvExe) — the explicit, user-chosen escape hatch;
//  2. TVREMOTE_MPV_EXE — an advanced *development* override, and also what
//     the macOS AppKit shell always sets to point at the bundled mpv (see
//     macos/Sources/main.swift's startCore), which is why it must rank below
//     the user's own choice: otherwise a user-configured path would be
//     silently overridden on every macOS launch;
//  3. the bundled runtime shipped with the app;
//  4. mpv found on the system PATH (development fallback only).
//
// Invalid candidates at any step deliberately fall through rather than
// reporting "missing", so a stale developer env var or a moved/deleted
// custom executable can never disable a working bundled build.
func DetectMPV() MPVInfo {
	custom := customMPVPath()
	if custom != "" {
		if exe := resolveMPV(custom); exe != "" {
			return MPVInfo{Path: exe, Source: "custom", Available: true, CustomConfigured: true}
		}
	}
	if exe := resolveMPV(os.Getenv("TVREMOTE_MPV_EXE")); exe != "" {
		return MPVInfo{Path: exe, Source: "env", Available: true, CustomConfigured: custom != "", CustomInvalid: custom != ""}
	}
	if exe := bundledMPV(); exe != "" {
		return MPVInfo{Path: exe, Source: "bundled", Available: true, CustomConfigured: custom != "", CustomInvalid: custom != ""}
	}
	if exe := systemMPV(); exe != "" {
		return MPVInfo{Path: exe, Source: "system", Available: true, CustomConfigured: custom != "", CustomInvalid: custom != ""}
	}
	return MPVInfo{Source: "missing", CustomConfigured: custom != "", CustomInvalid: custom != ""}
}

// customMPVPath returns the user's persisted custom mpv path, or "" when none
// is configured. Only an absolute path counts: the desktop shells' file picker
// can only ever produce one, whereas a bare name would be resolved through
// PATH and so would mean exactly what the lowest-priority "system" candidate
// already means — while wrongly outranking the bundled runtime. config.Load
// already clears the historical bare "mpv" default for the same reason; this
// is the second line of defence for a hand-edited config.json.
func customMPVPath() string {
	custom := strings.TrimSpace(config.MpvExe())
	if custom == "" || !filepath.IsAbs(custom) {
		return ""
	}
	return custom
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

// validateMPVTimeout bounds how long a user-supplied "custom mpv" path is
// allowed to take to answer `--version` before it is rejected. A hung or
// misbehaving binary must not stall the desktop shell's settings UI.
const validateMPVTimeout = 4 * time.Second

// ValidateMPV runs `<path> --version` and confirms both that the executable
// starts and that its output is recognizably mpv's, so a path that happens to
// point at some other program (or nothing at all) is rejected before it is
// ever persisted to config.MpvExe. Used by the desktop shells' "自定义 mpv
// 播放器…" flow (POST /desktop/mpv) — never call exec on an unvalidated
// user-supplied path elsewhere.
func ValidateMPV(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("empty path")
	}
	// Reject a bare name here rather than at the call site: a relative path
	// would validate happily through PATH and then be ignored by DetectMPV
	// (see customMPVPath), leaving the user with a setting that saved
	// successfully and did nothing.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s is not an absolute path", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory, not an executable", path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), validateMPVTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s did not respond within %s", path, validateMPVTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s --version failed: %w", path, err)
	}
	if !strings.Contains(strings.ToLower(string(out)), "mpv") {
		return fmt.Errorf("%s does not look like mpv", path)
	}
	return nil
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
			// Tracks cannot be selected before mpv has a track-list, so restore
			// sid/aid here (and again on track-list updates if still empty).
			// Speed/delays were applied earlier; re-trying tracks is silent.
			p.tryRestoreTitleTracks()
			p.reportPlaybackStarted()
		case "playback-restart":
			// mpv's confirmation that a frame is actually being produced, not
			// just that the file was opened — the fastest signal that ends the
			// desktop "connecting/buffering" indicator (see armStartingLocked).
			// It is not the only one, and must not be: see the core-idle branch
			// below for why an event cannot own this on its own.
			p.clearStarting()
			p.fireLifecycle(LifecyclePlaying, "", p.durationSeconds())
		case "end-file":
			reason, _ := fields["reason"].(string)
			if reason == "error" {
				p.reportLiveInterruption(reason)
				p.fireLifecycle(LifecycleError, reason, p.durationSeconds())
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
	if msg.Name == "core-idle" {
		// The authoritative end of a starting attempt, and the reason
		// "playback-restart" above cannot be trusted alone.
		//
		// mpv events are one-shot and are only delivered to clients that are
		// already connected. propReaderRun dials *after* the process is flagged
		// running, on a 500ms retry cadence (the socket does not exist for the
		// first moments of a cold start), so a title that reaches playback
		// before that subscription lands loses "playback-restart" permanently —
		// nothing replays it. Measured at roughly 1 in 10 cold starts with local
		// audio, where mpv opens the file and its bundled cover art almost
		// instantly; the desktop indicator then sat on screen for the full 20s
		// safety timeout while the music was audibly playing.
		//
		// An observed property has the opposite delivery semantics: mpv pushes
		// the *current* value the moment observe_property is issued. So this
		// arrives however late the subscription is, which is exactly the
		// property a state flag needs. core-idle false means mpv is actively
		// decoding and presenting — the same fact "playback-restart" announces,
		// expressed as state rather than as an edge.
		if idle, ok := data.(bool); ok && !idle {
			p.clearStarting()
		}
	}
	if msg.Name == "speed" {
		p.observeSessionSpeed(data)
	}
	if msg.Name == "track-list" {
		p.tryRestoreTitleTracks()
	}
	if msg.Name == "pause" {
		paused, ok := data.(bool)
		if ok {
			p.mu.Lock()
			prev := p.lastPause
			p.lastPause = &paused
			p.mu.Unlock()
			if prev == nil || *prev != paused {
				if paused {
					p.fireLifecycle(LifecyclePaused, "", p.durationSeconds())
				} else {
					p.fireLifecycle(LifecycleResumed, "", p.durationSeconds())
				}
			}
		}
	}
	if msg.Name == "duration" {
		if f, ok := data.(float64); ok && f > 0 {
			p.mu.Lock()
			already := p.durationNotified
			if !already {
				p.durationNotified = true
			}
			p.mu.Unlock()
			if !already {
				p.fireLifecycle(LifecycleDuration, "", f)
			}
		}
	}
}

func (p *Player) durationSeconds() float64 {
	p.propsMu.Lock()
	defer p.propsMu.Unlock()
	if f, ok := p.liveProps["duration"].(float64); ok {
		return f
	}
	return 0
}

// fireLifecycle invokes LifecycleReporter with a ctx snapshot. Safe with a nil reporter.
func (p *Player) fireLifecycle(event LifecycleEvent, reason string, durationSeconds float64) {
	reporter := p.LifecycleReporter
	if reporter == nil {
		return
	}
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	reporter(event, ctx, durationSeconds, reason)
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
	starting := p.starting
	revision := p.playbackRevision
	p.mu.Unlock()
	diagnosticAvailable, diagnosticScope := p.DiagnosticStatus()
	return map[string]any{
		"running":                running,
		"player_starting":        starting,
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
		"audio_only":             ctx.AudioOnly,
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
	// Observe dirty fields here rather than from property-change events: mpv
	// also auto-selects tracks at load time, and those must never count as a
	// user choice that freezes the server's default forever.
	p.noteDirtyFromCommand(cmd)
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

// mpvCommandOK reads the {ok, error} shape send() returns. A nil result (the
// command was never attempted) is not ok.
func mpvCommandOK(result map[string]any) bool {
	ok, _ := result["ok"].(bool)
	return ok
}

// reuseFallbackWait bounds how long Play waits for a dying mpv to finish
// exiting before it starts a fresh one. Long enough to cover the ordinary
// exit-bookkeeping gap, short enough that a caller — a DLNA control point
// waiting on SetAVTransportURI, say — is not left hanging.
const reuseFallbackWait = 2 * time.Second

// awaitProcessExit blocks until no mpv process is flagged running, or until
// timeout. It reports whether the process is gone, which is the precondition
// for launching a replacement.
func (p *Player) awaitProcessExit(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !p.isRunning() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// loadInReusedProcess hands a new item to an mpv that is already up. It
// returns the loadfile result; every other command here is fire-and-forget
// because none of them decides whether playback started.
func (p *Player) loadInReusedProcess(url string, opt PlayOptions, startValue string, speed float64, subDelay, audioDelay *float64) map[string]any {
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
	// Before loadfile: mpv reads cover-art-files while opening the file, so
	// setting it afterwards would apply to the item after this one. Sent
	// unconditionally because this is also what clears a previous song's
	// artwork — left in place it outranks the incoming item's video track
	// and the film plays as a still image.
	p.applyAudioPresentation(audioPresentation{opt.AudioOnly, opt.CoverArtURL})
	result := p.send([]any{"loadfile", url, "replace"})
	if !mpvCommandOK(result) {
		// Nothing was loaded, so the settings below would only decorate the
		// item that is still on screen. Leave them alone and let Play decide.
		return result
	}
	// loadfile keeps the previous pause flag. A prior Pause (remote or
	// DLNA) would otherwise leave the new title frozen while still
	// "loaded" — the classic stuck-cast symptom for reused mpv.
	p.send([]any{"set_property", "pause", false})
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
	// Deterministic speed/delay application on the reused-process path:
	// without this, speed only survives a title change when mpv happens to
	// be reused, which looks random to users.
	p.applyEarlyTitleSettings(speed, subDelay, audioDelay)
	p.appendDiagnosticEvent("mpv_loadfile_requested", map[string]any{"reused_process": true})
	return result
}

func (p *Player) Play(url string, opt PlayOptions) map[string]any {
	if p.BeforePlay != nil {
		p.BeforePlay()
	}
	p.hideScreensaver()
	p.beginDiagnostic(url, opt)

	// Switching to a different item while playing: snapshot per-title settings
	// and stop-report the old one before the context is overwritten.
	p.mu.Lock()
	prev := p.ctx.ItemID
	p.mu.Unlock()
	if prev != "" && prev != opt.ItemID {
		p.snapshotTitleSettings()
		p.fireStopReport()
	}

	speed, subDelay, audioDelay := p.prepareTitleRestore(opt)

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
	atomic.StoreInt64(&p.startPosTicks, int64(opt.StartSeconds*1e7))

	startValue := "none"
	if opt.StartSeconds > 0 {
		startValue = formatFloat(opt.StartSeconds)
	}

	var result map[string]any
	reused := false
	if p.isRunning() {
		result = p.loadInReusedProcess(url, opt, startValue, speed, subDelay, audioDelay)
		reused = mpvCommandOK(result)
		if !reused {
			// mpv was flagged running but would not take the load. The usual
			// cause is the moment the previous process is exiting: propReader
			// has already dropped the IPC connection while the exit bookkeeping
			// has not yet cleared `running`. A DLNA cast lands here often,
			// because a cast typically arrives right after something else
			// stopped — and the sender is then left on "connecting" forever
			// against a renderer that quietly reported failure.
			//
			// Retrying in a fresh process is only safe once the old one is
			// really gone. mpv can also be alive-but-not-yet-connected (a
			// second play issued during a cold start's first moments), and a
			// second player beside a live one is a worse outcome than the
			// failure this repairs. So wait for the exit, and give up if it
			// does not come.
			log.Printf("mpv reuse failed (%v); waiting for the old process to exit before a fresh start", result["error"])
			p.appendDiagnosticEvent("mpv_reuse_failed", map[string]any{"error": fmt.Sprint(result["error"])})
			if !p.awaitProcessExit(reuseFallbackWait) {
				p.appendDiagnosticEvent("mpv_reuse_fallback_abandoned", nil)
				p.finalizeDiagnostic("engine_start_failed", "engine_start")
				return result
			}
		}
	}
	if !reused {
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
		args = append(args, audioPresentation{opt.AudioOnly, opt.CoverArtURL}.args()...)
		args = append(args, mpvHTTPHeaderArgs(opt.HTTPHeaders)...)
		// Startup flags, not set_property: the IPC pipe is not connected yet
		// on a fresh process. Explicit --speed also covers keep_playback_speed
		// off (1.0) so a fresh process never inherits a stale mpv.conf default.
		args = append(args, earlyTitleSettingsArgs(speed, subDelay, audioDelay)...)
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
			// mpv writes nothing when it never starts, so this error is the
			// only record of why playback silently did nothing.
			log.Printf("Could not start player %s: %v", exe, err)
			p.appendDiagnosticEvent("mpv_process_start_failed", map[string]any{"error": err.Error()})
			p.finalizeDiagnostic("engine_start_failed", "engine_start")
			return map[string]any{"ok": false, "error": "Could not start the player: " + exe}
		}
		log.Printf("Started player %s (pid=%d), log: %s", exe, cmd.Process.Pid, mpvLog)
		// Play() runs on an HTTP handler goroutine with no window/input behind
		// it, which Windows' foreground-activation lock treats as unprivileged;
		// without this, mpv's own window opens behind whatever currently holds
		// the foreground (typically the QR window). No-op on other platforms.
		allowForegroundActivation(cmd.Process.Pid)
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
			hint := exitStatusHint(err)
			if err != nil {
				// A non-zero exit or signal is the process crash the user observes.
				// Surfacing it here (with the log path) gives us something to act on.
				if hint != "" {
					log.Printf("mpv exited unexpectedly: %v — %s", err, hint)
				} else {
					log.Printf("mpv exited unexpectedly: %v; see %s", err, mpvLog)
				}
			}
			p.mu.Lock()
			isCurrent := p.proc == cmd
			// Revision as of this exit. The bookkeeping below ends by clearing
			// the play context, but it runs asynchronously and passes through
			// a network stop report first — long enough for a *new* Play to
			// have started and installed its own context, which clearing would
			// then wipe. Comparing the revision is how the clear stays scoped
			// to the playback that actually ended. Reachable in ordinary use:
			// the reuse fallback above deliberately starts a fresh process the
			// moment this goroutine clears `running`.
			exitRevision := p.playbackRevision
			if isCurrent {
				p.running = false
				p.proc = nil
				p.clearStartingLocked()
			}
			p.mu.Unlock()
			if isCurrent {
				exit := "clean"
				if err != nil {
					exit = err.Error()
					if hint != "" {
						exit += " (" + hint + ")"
					}
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
				// Same write timing as fireStopReport: once on process exit,
				// not on every property change (config.json is rewritten whole).
				p.snapshotTitleSettings()
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
				if naturalEOF {
					p.fireLifecycle(LifecycleEOF, "eof", p.durationSeconds())
				} else if exit != "clean" {
					p.fireLifecycle(LifecycleError, exit, p.durationSeconds())
				} else {
					p.fireLifecycle(LifecycleStopped, "process_exit", p.durationSeconds())
				}
				p.mu.Lock()
				progressed := time.Duration(atomic.LoadInt64(&p.lastPosTicks)-atomic.LoadInt64(&p.startPosTicks)) * 100
				completed, autoplayEOF := completedAutoplayContext(finished, naturalEOF, progressed)
				// A newer playback has already claimed the player: it owns the
				// context and the live props now. Clearing them here would
				// blank a running title, and handing its revision to autoplay
				// would chain the next item off something still playing.
				superseded := p.playbackRevision != exitRevision
				if superseded {
					autoplayEOF = false
				} else {
					if autoplayEOF {
						p.ctx = completed
					} else {
						p.ctx = PlayContext{}
					}
					p.bumpPlaybackRevisionLocked()
				}
				completedRevision := p.playbackRevision
				reporter := p.NaturalEOFReporter
				p.mu.Unlock()
				if !superseded {
					p.propsMu.Lock()
					p.liveProps = map[string]any{}
					p.propsMu.Unlock()
				}
				if autoplayEOF && reporter != nil {
					reporter(completed, completedRevision)
				}
			}
		}()
		result = map[string]any{"ok": true}
	}

	p.mu.Lock()
	p.liveRecoveryNotified = false
	p.lastPause = nil
	p.durationNotified = false
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
		AudioOnly:    opt.AudioOnly,
	}
	p.armStartingLocked()
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()
	return result
}

// minAutoplayProgress is how far playback must advance past its start position
// before an end-of-file counts as having finished the title. A file that opens
// at (or just before) its own tail reaches EOF within a frame or two, which is
// a degenerate open rather than a completed viewing — chaining autoplay off it
// would walk the whole folder in seconds. The threshold only has to clear that
// case, so it is deliberately small: a genuine viewing of even a very short
// clip passes it easily.
const minAutoplayProgress = 2 * time.Second

// completedAutoplayContext is the production EOF hand-off gate. File-source
// plays intentionally have no SeriesID: their ItemID is the relative path used
// by the folder resolver. The player reports every completed non-live item and
// leaves source-specific eligibility to the server, which already owns that
// policy (and rejects movies, IPTV, DLNA, or disabled autoplay as appropriate).
//
// progressed is how much playback actually advanced during this attempt; see
// minAutoplayProgress. A negative value (the position moved backwards, which a
// resume seed can produce when no time-pos ever arrived) is not progress.
func completedAutoplayContext(finished PlayContext, naturalEOF bool, progressed time.Duration) (PlayContext, bool) {
	if !naturalEOF || finished.ItemID == "" || finished.IsLive {
		return PlayContext{}, false
	}
	if progressed < minAutoplayProgress {
		return PlayContext{}, false
	}
	finished.PlaybackCompleted = true
	return finished, true
}

// startingSafetyTimeout bounds how long the desktop "connecting/buffering"
// indicator can stay on if mpv never confirms playback (e.g. a source that
// hangs without ever erroring). Chosen well above ordinary slow-start latency
// (a few seconds for a remote Emby/IPTV first byte) but short enough that a
// truly stuck load does not leave the indicator on screen indefinitely.
const startingSafetyTimeout = 20 * time.Second

// armStartingLocked marks a new Play() attempt as starting and (re)arms the
// safety timer. Caller must hold p.mu.
func (p *Player) armStartingLocked() {
	p.starting = true
	p.startingEpoch++
	epoch := p.startingEpoch
	if p.startingTimer != nil {
		p.startingTimer.Stop()
	}
	p.startingTimer = time.AfterFunc(startingSafetyTimeout, func() { p.clearStartingEpoch(epoch) })
}

// clearStarting unconditionally ends the current starting attempt (mpv
// confirmed playback via "playback-restart", or the attempt stopped/crashed).
// Bumping the epoch invalidates any still-pending safety timer from this
// attempt so it cannot later clobber a newer Play().
func (p *Player) clearStarting() {
	p.mu.Lock()
	p.clearStartingLocked()
	p.mu.Unlock()
}

func (p *Player) clearStartingLocked() {
	p.startingEpoch++
	if p.startingTimer != nil {
		p.startingTimer.Stop()
		p.startingTimer = nil
	}
	changed := p.starting
	p.starting = false
	if changed {
		p.bumpPlaybackRevisionLocked()
	}
}

// clearStartingEpoch is the safety-timer callback. It only acts if epoch still
// matches the attempt that armed it, so a stale timer from an attempt that was
// already confirmed/stopped/superseded cannot clear a newer one's starting=true.
func (p *Player) clearStartingEpoch(epoch uint64) {
	p.mu.Lock()
	if p.startingEpoch == epoch {
		p.clearStartingLocked()
	}
	p.mu.Unlock()
}

func (p *Player) Stop() map[string]any {
	p.hideScreensaver()
	p.appendDiagnosticEvent("stop_requested", map[string]any{})
	p.finalizeDiagnostic("user_stop", "user_action")
	// Snapshot before fireStopReport/clear so DLNA GENA still sees SourceType
	// and per-title dirty fields still have a live context + props to read.
	dur := p.durationSeconds()
	p.fireLifecycle(LifecycleStopped, "user_stop", dur)
	p.snapshotTitleSettings()
	p.fireStopReport()
	p.clearStarting()
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
	p.lastPause = nil
	p.durationNotified = false
	p.ctx = PlayContext{}
	p.bumpPlaybackRevisionLocked()
	p.mu.Unlock()
	p.propsMu.Lock()
	p.liveProps = map[string]any{}
	p.propsMu.Unlock()
	return result
}

// ClearCompletedPlayback drops a post-EOF autoplay context (used when the host
// cancels pending autoplay without starting a new title).
func (p *Player) ClearCompletedPlayback() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.ctx.PlaybackCompleted {
		return
	}
	p.ctx = PlayContext{}
	p.bumpPlaybackRevisionLocked()
}
