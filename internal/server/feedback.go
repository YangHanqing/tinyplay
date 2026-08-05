package server

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tvremote/internal/auth"
	"tvremote/internal/config"
	"tvremote/internal/player"
)

// processStart approximates the app's launch time. Package init runs before
// the HTTP server binds, so "how long has it been up" is accurate enough to
// tell a fresh launch apart from a session that has been running for days.
var processStart = time.Now()

// Log tail sizes for the feedback report. The core log is the one that
// actually explains a non-playback failure (a source that won't connect, a
// pairing rejection, a browse that returned nothing), so it gets the larger
// budget; mpv's log only matters for playback, which has its own dedicated
// report with its own tail.
const (
	feedbackCoreLogLines = 200
	feedbackMPVLogLines  = 80
)

// feedbackReport backs the Settings → Feedback sheet. Unlike
// playerDebugReport, which is scoped to one playback attempt and 404s when
// there has never been one, this always answers: most reports are about
// something that happened *before* playback (a source that won't connect,
// pairing, an empty folder listing), and a user who cannot produce any report
// at all just sends "it doesn't work".
//
// It carries no credentials and no identifying detail about the user's
// servers: source entries describe shape (kind, protocol, port, whether
// credentials are set), never names, hosts, or paths. The frontend redacts a
// second time before the text reaches the clipboard.
func (s *Server) feedbackReport(w http.ResponseWriter, r *http.Request) {
	cfg := config.Load()
	mpvInfo := player.DetectMPV()

	report := map[string]any{
		"report_schema_version": 1,
		"report_kind":           "feedback",
		"generated_at":          time.Now().UTC().Format(time.RFC3339),
		"app": map[string]any{
			"version":        s.version,
			"platform":       runtime.GOOS,
			"arch":           runtime.GOARCH,
			"os_version":     osVersion(),
			"go_version":     runtime.Version(),
			"uptime_seconds": int(time.Since(processStart).Seconds()),
			"listen_port":    s.port,
			"data_dir_kind":  dataDirKind(),
			"language":       cfg.Language,
		},
		"mpv": map[string]any{
			"source":            mpvInfo.Source,
			"available":         mpvInfo.Available,
			"custom_configured": mpvInfo.CustomConfigured,
			"custom_invalid":    mpvInfo.CustomInvalid,
		},
		"settings": map[string]any{
			"cache_seconds":         cfg.MpvCacheSecs,
			"seek_backward_secs":    cfg.SeekBackwardSecs,
			"seek_forward_secs":     cfg.SeekForwardSecs,
			"autoplay_next_episode": cfg.AutoplayNextEpisode,
			"dlna_receiver_enabled": cfg.DLNAReceiverEnabled,
			"custom_website_count":  len(cfg.WebsiteCustomSites),
		},
		"pairing": map[string]any{
			"device_count": auth.Default.DeviceCount(),
			"locked":       auth.Default.Locked(),
		},
		"sources":  sourcesSummary(cfg),
		"playback": playbackSection(s.player),
		"logs": map[string]any{
			"core_log_tail": tailLines(filepath.Join(config.LogDir(), "tvremote.log"), feedbackCoreLogLines),
			"mpv_log_tail":  tailLines(filepath.Join(config.LogDir(), "mpv.log"), feedbackMPVLogLines),
		},
		// Deliberately not a blanket promise about addresses: the structured
		// sections above carry none, but a log line like "dial tcp
		// 192.168.1.100:8096: connection refused" is the single most useful
		// thing in a "my NAS stopped working" report, and stripping it to keep
		// a tidier promise would gut the report's purpose. Credentials, which
		// are not diagnosable and never belong here, are excluded outright.
		"privacy": "Access tokens, passwords, request headers, and server names are excluded. Log lines may still contain local network addresses and file names.",
	}
	writeJSON(w, http.StatusOK, report)
}

// playbackSection reuses the playback diagnostic when one exists. A feedback
// report about something other than playback is the normal case, so its
// absence is reported as data rather than as an error.
func playbackSection(p *player.Player) map[string]any {
	diagnostics, available := p.Diagnostics()
	if !available {
		return map[string]any{"available": false}
	}
	section := map[string]any{"available": true, "session": p.State()}
	for key, value := range diagnostics {
		section[key] = value
	}
	return section
}

// sourcesSummary describes every configured source by shape only. Which kinds
// a user has configured, and whether the one they were using is signed in, is
// most of what separates "my Emby broke" from "my SMB share broke" — and none
// of it needs the server's name or address.
func sourcesSummary(cfg *config.Config) map[string]any {
	list := make([]map[string]any, 0, len(cfg.Servers))
	for _, srv := range cfg.Servers {
		list = append(list, sourceShape(srv, srv.ID == cfg.ActiveServerID))
	}
	return map[string]any{
		"count":  len(cfg.Servers),
		"list":   list,
		"active": activeSourceSummary(),
	}
}

func sourceShape(srv *config.Server, active bool) map[string]any {
	kind := config.NormalizeServerType(srv.Type)
	shape := map[string]any{
		"type":       kind,
		"active":     active,
		"protocol":   srv.Protocol,
		"port":       srv.Port,
		"host_count": len(srv.Hosts),
		"host_kind":  hostKind(srv),
	}
	switch {
	case kind == "iptv":
		shape["has_playlist_url"] = srv.PlaylistURL != ""
		shape["has_epg_url"] = srv.EPGURL != ""
		shape["favorite_count"] = len(srv.IPTVFavorites)
	case config.IsFileServerType(kind):
		shape["has_share"] = srv.Share != ""
		shape["has_root_path"] = srv.RootPath != ""
		shape["has_domain"] = srv.Domain != ""
		shape["has_credentials"] = srv.Username != "" || srv.Password != ""
	default:
		// Same predicate the servers list reports as "logged_in", so a user
		// describing what they see on screen and this report agree. user_id is
		// broken out because a token without one is its own diagnosable state
		// on Emby/Jellyfin.
		shape["signed_in"] = srv.AccessToken != ""
		shape["has_user_id"] = srv.UserID != ""
		shape["has_base_path"] = srv.BasePath != ""
	}
	return shape
}

// hostKind reports whether the user typed an address or a name, which is the
// part of a host that matters when a connection fails (name resolution on the
// LAN is a recurring cause), without recording the host itself.
func hostKind(srv *config.Server) string {
	if len(srv.Hosts) == 0 {
		return "none"
	}
	host := strings.TrimSpace(srv.Hosts[0])
	switch {
	case host == "":
		return "none"
	case net.ParseIP(host) != nil:
		return "ip"
	default:
		return "hostname"
	}
}

// dataDirKind names which of config.DataDir's branches won, without exposing
// the path (which contains the account name). "config not saving" and "my
// sources vanished after an update" both come down to this answer.
func dataDirKind() string {
	if os.Getenv("TVREMOTE_DATA_DIR") != "" {
		return "env-override"
	}
	if wd, err := os.Getwd(); err == nil {
		if st, err := os.Stat(filepath.Join(wd, "data")); err == nil && st.IsDir() {
			return "working-dir"
		}
	}
	return "user-config-dir"
}
