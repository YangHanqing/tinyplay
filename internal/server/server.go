// Package server is the Go port of the FastAPI app (app/main.py + routes). It
// exposes the same REST surface and serves the embedded frontend.
package server

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/dlna"
	"tvremote/internal/iptv"
	"tvremote/internal/player"
	"tvremote/web"
)

// Server holds the shared dependencies for the HTTP handlers.
type Server struct {
	player         *player.Player
	webFS          fs.FS
	port           int // the actual bound port (may differ from config if it was taken)
	switchMu       sync.Mutex
	latestSwitch   map[string]int
	playMu         sync.Mutex
	playGeneration uint64
	dlna           *dlna.Receiver
	iptvRecoveryMu sync.Mutex
	iptvRecoveries map[string]*iptvRecovery

	// Host-owned next-episode autoplay (see autoplay.go). Hooks below are
	// optional and only used by tests to inject time/lookup/play behaviour.
	autoplayMu     sync.Mutex
	autoplay       autoplayState
	autoplayCancel func()
	// Reject a natural-EOF callback that was already in flight when an
	// explicit play/stop/cancel invalidated its playback revision.
	autoplayCancelledThrough uint64
	autoplayNow              func() time.Time
	autoplayAfter            func(d time.Duration, fn func()) (cancel func())
	resolveNextEpisode       func(finished player.PlayContext) (*autoplayNext, error)
	playAutoplayNext         func(next autoplayNext) map[string]any
	clearCompleted           func()
}

type iptvRecovery struct {
	serverID  string
	channelID string
	revision  uint64
	attempts  int
	inFlight  bool
}

const maximumIPTVRecoveryAttempts = 3

// beginPlay assigns the linearization point for a play request. Slow source
// negotiation is allowed to run concurrently, but only the newest request may
// finally mutate the one physical player.
func (s *Server) beginPlay() uint64 {
	s.playMu.Lock()
	defer s.playMu.Unlock()
	s.playGeneration++
	return s.playGeneration
}

func (s *Server) isCurrentPlay(generation uint64) bool {
	s.playMu.Lock()
	defer s.playMu.Unlock()
	return generation == s.playGeneration
}

// invalidatePlay also protects Stop from a previously-started, slow play
// request that would otherwise resurrect mpv after the stop.
func (s *Server) invalidatePlay() {
	s.playMu.Lock()
	s.playGeneration++
	s.playMu.Unlock()
}

// New builds the server. The player's reporters should already be wired.
func New(p *player.Player) *Server {
	s := &Server{player: p, webFS: web.FS(), latestSwitch: map[string]int{}, iptvRecoveries: map[string]*iptvRecovery{}}
	s.dlna = dlna.New(p, func() int { return s.port })
	p.PlaybackStartedReporter = s.recordIPTVPlaybackStarted
	p.LiveInterruptionReporter = s.recoverIPTV
	// Website window and mpv are mutually exclusive.
	initWebsiteBroker(func() {
		s.CancelAutoplay(false)
		s.invalidatePlay()
		_ = s.player.Stop()
	})
	p.BeforePlay = RequestWebsiteClose
	s.wireAutoplay()
	return s
}

func iptvRecoveryKey(serverID, channelID string) string { return serverID + "\x00" + channelID }

func playerStateRevision(state map[string]any) uint64 {
	switch value := state["playback_revision"].(type) {
	case uint64:
		return value
	case int:
		return uint64(value)
	case float64:
		return uint64(value)
	default:
		return 0
	}
}

func (s *Server) liveContextCurrent(ctx player.PlayContext, revision uint64) bool {
	state := s.player.State()
	isLive, _ := state["is_live"].(bool)
	return isLive && state["server_id"] == ctx.ServerID && state["channel_id"] == ctx.ChannelID &&
		playerStateRevision(state) == revision
}

// recordIPTVPlaybackStarted writes a recently-watched row only after mpv has
// accepted and loaded the input. A rejected URL is no longer advertised as a
// successful watch just because the HTTP play request started a process.
func (s *Server) recordIPTVPlaybackStarted(ctx player.PlayContext) {
	if (ctx.SourceType != "iptv" && ctx.SourceType != "iptv-catchup") || ctx.ServerID == "" || ctx.ChannelID == "" {
		return
	}
	config.RecordIPTVRecent(ctx.ServerID, ctx.ChannelID, 50)
	state := s.player.State()
	revision := playerStateRevision(state)
	key := iptvRecoveryKey(ctx.ServerID, ctx.ChannelID)
	go func() {
		time.Sleep(time.Minute)
		if !s.liveContextCurrent(ctx, revision) {
			return
		}
		s.iptvRecoveryMu.Lock()
		if recovery := s.iptvRecoveries[key]; recovery != nil && recovery.revision == revision {
			recovery.attempts = 0
		}
		s.iptvRecoveryMu.Unlock()
	}()
}

// recoverIPTV is the desktop counterpart to tvOS's bounded live-stream
// recovery: retry the same URL, rotate M3U mirrors, then refresh expiring
// playlist URLs before the final retry. The callback is engine-driven, so it
// remains active even if the phone closes or loses Wi-Fi.
func (s *Server) recoverIPTV(ctx player.PlayContext, reason string) {
	if !ctx.IsLive || ctx.ServerID == "" || ctx.ChannelID == "" {
		return
	}
	state := s.player.State()
	revision := playerStateRevision(state)
	if !s.liveContextCurrent(ctx, revision) {
		return
	}
	key := iptvRecoveryKey(ctx.ServerID, ctx.ChannelID)
	s.iptvRecoveryMu.Lock()
	recovery := s.iptvRecoveries[key]
	if recovery == nil || recovery.revision != revision {
		recovery = &iptvRecovery{serverID: ctx.ServerID, channelID: ctx.ChannelID, revision: revision}
		s.iptvRecoveries[key] = recovery
	}
	if recovery.inFlight || recovery.attempts >= maximumIPTVRecoveryAttempts {
		s.iptvRecoveryMu.Unlock()
		if recovery.attempts >= maximumIPTVRecoveryAttempts && s.liveContextCurrent(ctx, revision) {
			log.Printf("IPTV recovery exhausted for channel %s", ctx.ChannelID)
			_ = s.player.Stop()
		}
		return
	}
	recovery.attempts++
	attempt := recovery.attempts
	recovery.inFlight = true
	s.iptvRecoveryMu.Unlock()

	delay := time.Second
	if attempt == 2 {
		delay = 3 * time.Second
	} else if attempt >= 3 {
		delay = 8 * time.Second
	}
	log.Printf("IPTV stream interrupted (%s), reconnecting channel %s (%d/%d)", reason, ctx.ChannelID, attempt, maximumIPTVRecoveryAttempts)
	go func() {
		time.Sleep(delay)
		if !s.liveContextCurrent(ctx, revision) {
			s.finishIPTVRecoveryAttempt(key, revision)
			return
		}

		client, err := iptv.FromServer(ctx.ServerID)
		if err == nil && attempt == maximumIPTVRecoveryAttempts {
			refreshCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			err = client.Refresh(refreshCtx)
			cancel()
		}
		if err != nil {
			s.finishIPTVRecoveryAttempt(key, revision)
			if s.liveContextCurrent(ctx, revision) {
				s.recoverIPTV(ctx, "source_refresh_failed")
			}
			return
		}
		channel := client.ChannelByID(ctx.ChannelID)
		if channel == nil || len(channel.Variants) == 0 {
			s.finishIPTVRecoveryAttempt(key, revision)
			s.recoverIPTV(ctx, "channel_unavailable")
			return
		}
		variant := ctx.VariantIndex
		if attempt > 1 {
			variant = (ctx.VariantIndex + attempt - 1) % len(channel.Variants)
		}
		result := s.player.Play(channel.Variants[variant].URL, player.PlayOptions{
			ServerID: ctx.ServerID, Title: channel.Name, IsLive: true,
			ChannelID: channel.ID, VariantIndex: variant, SourceType: "iptv",
			HTTPHeaders: channel.Variants[variant].HTTPHeaders,
		})
		nextContext := ctx
		nextContext.VariantIndex = variant
		nextRevision := playerStateRevision(s.player.State())
		s.finishIPTVRecoveryAttempt(key, revision, nextRevision)
		if ok, _ := result["ok"].(bool); !ok && s.liveContextCurrent(nextContext, nextRevision) {
			s.recoverIPTV(nextContext, "reopen_failed")
		}
	}()
}

func (s *Server) finishIPTVRecoveryAttempt(key string, revision uint64, nextRevision ...uint64) {
	s.iptvRecoveryMu.Lock()
	if recovery := s.iptvRecoveries[key]; recovery != nil && recovery.revision == revision {
		if len(nextRevision) > 0 && nextRevision[0] != 0 {
			recovery.revision = nextRevision[0]
		}
		recovery.inFlight = false
	}
	s.iptvRecoveryMu.Unlock()
}

// SetPort records the port the HTTP server actually bound to, so the QR / intro
// page advertise the right address even when we fell back off a busy port.
func (s *Server) SetPort(port int) {
	s.port = port
	if config.Load().DLNAReceiverEnabled {
		s.dlna.Start()
	}
}

func (s *Server) refreshDLNAReceiver(enabled bool) {
	if enabled {
		s.dlna.Start()
	} else {
		s.dlna.Stop()
	}
}

// dlnaReceiverStatus separates the user's saved preference from the live
// socket state. In particular, enabled does not guarantee discoverability
// when another application has claimed SSDP's UDP port 1900.
func (s *Server) dlnaReceiverStatus() string {
	if !config.Load().DLNAReceiverEnabled {
		return "disabled"
	}
	if s.dlna != nil && s.dlna.Running() {
		return "available"
	}
	return "unavailable"
}

// Handler returns the full router with request logging applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// ── Server management ──
	mux.HandleFunc("GET /api/servers", s.listServers)
	mux.HandleFunc("POST /api/servers", s.createServer)
	mux.HandleFunc("PUT /api/servers/{id}", s.editServer)
	mux.HandleFunc("DELETE /api/servers/{id}", s.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/activate", s.activateServer)
	mux.HandleFunc("PUT /api/servers/{id}/host", s.switchHost)
	mux.HandleFunc("POST /api/servers/{id}/login", s.loginServer)
	mux.HandleFunc("POST /api/servers/{id}/connect", s.connectServer)

	// ── App settings ──
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.updateSettings)
	mux.HandleFunc("POST /api/settings/reset", s.resetSettings)
	// Register methods explicitly so Go's method-aware ServeMux can keep the
	// frontend's GET / catch-all without an ambiguity.
	mux.HandleFunc("GET /dlna/{path...}", s.dlna.ServeHTTP)
	mux.HandleFunc("POST /dlna/{path...}", s.dlna.ServeHTTP)
	mux.HandleFunc("SUBSCRIBE /dlna/{path...}", s.dlna.ServeHTTP)

	// ── Player ──
	mux.HandleFunc("GET /api/player/state", s.playerState)
	mux.HandleFunc("POST /api/player/play", s.playItem)
	mux.HandleFunc("POST /api/player/next", s.playerNext)
	mux.HandleFunc("POST /api/player/next/cancel", s.playerNextCancel)
	mux.HandleFunc("POST /api/player/command", s.playerCommand)
	mux.HandleFunc("POST /api/player/stop", s.playerStop)
	mux.HandleFunc("GET /api/player/props", s.playerProps)
	mux.HandleFunc("GET /api/player/debug-report", s.playerDebugReport)

	// ── Emby ──
	mux.HandleFunc("GET /api/emby/libraries", s.embyLibraries)
	mux.HandleFunc("GET /api/emby/resume", s.embyResume)
	mux.HandleFunc("GET /api/emby/items", s.embyItems)
	mux.HandleFunc("GET /api/emby/items/{item_id}", s.embyItemDetail)
	mux.HandleFunc("GET /api/emby/episodes", s.embyEpisodes)
	mux.HandleFunc("GET /api/emby/seasons", s.embySeasons)
	mux.HandleFunc("GET /api/emby/image/{item_id}", s.embyImage)
	// Provider-neutral aliases used by the newer shared source UI.
	mux.HandleFunc("GET /api/library/libraries", s.embyLibraries)
	mux.HandleFunc("GET /api/library/resume", s.embyResume)
	mux.HandleFunc("GET /api/library/items", s.embyItems)
	mux.HandleFunc("GET /api/library/items/{item_id}", s.embyItemDetail)
	mux.HandleFunc("GET /api/library/episodes", s.embyEpisodes)
	mux.HandleFunc("GET /api/library/seasons", s.embySeasons)
	mux.HandleFunc("GET /api/library/image/{item_id}", s.embyImage)

	// ── File sources (local / SMB / WebDAV / NFS mount) ──
	mux.HandleFunc("GET /api/files/list", s.filesList)
	mux.HandleFunc("GET /api/files/stream", s.filesStream)

	// ── IPTV (M3U/M3U8 + optional XMLTV EPG) ──
	mux.HandleFunc("GET /api/iptv/summary", s.iptvSummary)
	mux.HandleFunc("POST /api/iptv/refresh", s.iptvRefresh)
	mux.HandleFunc("GET /api/iptv/categories", s.iptvCategories)
	mux.HandleFunc("GET /api/iptv/channels", s.iptvChannels)
	mux.HandleFunc("GET /api/iptv/channel/{id}", s.iptvChannelDetail)
	mux.HandleFunc("GET /api/iptv/programme", s.iptvProgramme)
	mux.HandleFunc("POST /api/iptv/favorite", s.iptvFavorite)
	mux.HandleFunc("POST /api/iptv/recent", s.iptvRecent)

	// ── Desktop intro + QR (used by the native shells) ──
	mux.HandleFunc("GET /desktop", s.desktopPage)
	mux.HandleFunc("GET /desktop/qr.png", s.desktopQR)
	mux.HandleFunc("GET /desktop/background.jpg", s.desktopBackground)
	mux.HandleFunc("GET /desktop/open-logs", s.openLogs)

	// ── System output volume (remote's volume slider) ──
	mux.HandleFunc("GET /api/system/volume", s.systemVolumeGet)
	mux.HandleFunc("POST /api/system/volume", s.systemVolumeSet)

	// ── Website playback (desktop experimental; fixed allowlist) ──
	// Phone Media/Website workspace is client-local; no /api/website/mode.
	// Site selection is not persisted: open requires an allowlisted site_id.
	mux.HandleFunc("GET /api/website/state", s.websiteState)
	mux.HandleFunc("POST /api/website/open", s.websiteOpen)
	mux.HandleFunc("POST /api/website/close", s.websiteClose)
	mux.HandleFunc("POST /api/website/action", s.websiteAction)
	// Loopback-only native shell transport.
	mux.HandleFunc("GET /desktop/website/poll", s.websiteShellPoll)
	mux.HandleFunc("POST /desktop/website/report", s.websiteShellReport)
	mux.HandleFunc("GET /desktop/website/controller.js", s.websiteControllerJS)

	// ── Frontend (embedded) ──
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.webFS))))
	mux.HandleFunc("GET /manifest.webmanifest", s.webManifest)
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
	mux.HandleFunc("GET /", s.index)

	return withLogging(withGuard(mux))
}
