// Package config is the Go port of app/core/config.py. It reads and writes the
// same data/config.json schema as the Python branch, so the two are
// interchangeable. It must stay free of any dependency on the HTTP server or
// the mpv player.
//
// This file holds the schema types plus the load/save/migrate core; the
// user-facing accessors are split by domain into config_servers.go,
// config_settings.go, and config_iptv.go.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"tvremote/internal/i18n"
)

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// newID returns a random uuid-ish hex string (no external dependency).
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b)
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// RecentChannel records a single IPTV channel watch for the "recently
// watched" pseudo-category.
type RecentChannel struct {
	ChannelID string `json:"channel_id"`
	WatchedAt string `json:"watched_at"`
}

// Server mirrors one entry of config.json "servers".
type Server struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"type,omitempty"`
	Protocol      string   `json:"protocol"`
	Hosts         []string `json:"hosts"`
	Port          int      `json:"port"`
	ActiveHost    int      `json:"active_host"`
	Username      string   `json:"username"`
	AccessToken   string   `json:"access_token"`
	UserID        string   `json:"user_id"`
	DeviceID      string   `json:"device_id"`
	ClientVersion string   `json:"client_version"`
	LastLibraryID string   `json:"last_library_id"`
	Share         string   `json:"share,omitempty"`
	Domain        string   `json:"domain,omitempty"`
	RootPath      string   `json:"root_path,omitempty"`
	Password      string   `json:"password,omitempty"`

	// FileProtocol and Root are the pre-2026-07 shape of a file source (a
	// single opaque "root" URL/path plus a protocol tag). They are read-only
	// migration inputs now: loadLocked parses any legacy value it finds into
	// Type/Hosts/Port/Share/Domain/RootPath and clears both, so they never
	// round-trip back into config.json once migrated.
	FileProtocol string `json:"file_protocol,omitempty"`
	Root         string `json:"root,omitempty"`

	// IPTV-only fields.
	PlaylistURL   string          `json:"playlist_url,omitempty"`
	EPGURL        string          `json:"epg_url,omitempty"`
	IPTVFavorites []string        `json:"favorites,omitempty"`
	IPTVRecent    []RecentChannel `json:"recently_watched,omitempty"`
}

// Config mirrors the top level of config.json.
type Config struct {
	Servers              []*Server            `json:"servers"`
	ActiveServerID       string               `json:"active_server_id"`
	ListenPort           int                  `json:"listen_port"`
	MpvPipe              string               `json:"mpv_pipe"`
	MpvExe               string               `json:"mpv_exe"`
	MpvCacheSecs         int                  `json:"mpv_cache_secs"`
	SeekBackwardSecs     int                  `json:"seek_backward_secs,omitempty"`
	SeekForwardSecs      int                  `json:"seek_forward_secs,omitempty"`
	Language             string               `json:"language,omitempty"`
	DLNAReceiverEnabled  bool                 `json:"dlna_receiver_enabled"`
	DLNAReceiverID       string               `json:"dlna_receiver_id,omitempty"`
	LocalPlaybackHistory []LocalPlaybackEntry `json:"local_playback_history,omitempty"`
	AutoplayNextEpisode  bool                 `json:"autoplay_next_episode"`
	// Desktop update prompts are intentionally small, local preferences. A
	// skipped version never suppresses a newer release, while RemindAfter keeps
	// an app restart from immediately asking the same question again.
	UpdateSkippedVersion string `json:"update_skipped_version,omitempty"`
	UpdateRemindVersion  string `json:"update_remind_version,omitempty"`
	UpdateRemindAfter    string `json:"update_remind_after,omitempty"`
}

type LocalPlaybackEntry struct {
	ServerID        string  `json:"server_id"`
	Path            string  `json:"path"`
	PositionSeconds float64 `json:"position_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
	UpdatedAt       string  `json:"updated_at"`
}

func LocalPlaybackPosition(serverID, path string) float64 {
	for _, e := range Load().LocalPlaybackHistory {
		if e.ServerID == serverID && e.Path == path {
			// Same tail rule as the Emby/Plex providers: only "within the last
			// 15s" or "past 99%" counts as finished. A percentage alone is too
			// aggressive for long files (95% of 3h leaves 9 minutes unwatched).
			if e.PositionSeconds < 5 ||
				(e.DurationSeconds > 0 &&
					(e.PositionSeconds >= e.DurationSeconds-15 || e.PositionSeconds/e.DurationSeconds >= 0.99)) {
				return 0
			}
			return e.PositionSeconds
		}
	}
	return 0
}

func RecordLocalPlayback(serverID, path string, position, duration float64) {
	if serverID == "" || path == "" || position < 0 {
		return
	}
	patch(func(cfg *Config) {
		kept := []LocalPlaybackEntry{{ServerID: serverID, Path: path, PositionSeconds: position, DurationSeconds: duration, UpdatedAt: nowUTC()}}
		for _, e := range cfg.LocalPlaybackHistory {
			if e.ServerID != serverID || e.Path != path {
				kept = append(kept, e)
			}
		}
		if len(kept) > 1000 {
			kept = kept[:1000]
		}
		cfg.LocalPlaybackHistory = kept
	})
}

const (
	DefaultMpvCacheSecs = 300
)

// MpvCachePresetSecs is deliberately small: buffering duration is only a
// target (actual duration depends on bitrate), so arbitrary minute values add
// complexity without giving users a predictable result.
var MpvCachePresetSecs = []int{300, 900, 1800, 3600}

func NormalizeMpvCacheSecs(secs int) int {
	if secs <= 0 {
		return DefaultMpvCacheSecs
	}
	nearest := MpvCachePresetSecs[0]
	for _, preset := range MpvCachePresetSecs[1:] {
		if absInt(secs-preset) < absInt(secs-nearest) {
			nearest = preset
		}
	}
	return nearest
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// serverDefaults mirrors _SERVER_DEFAULTS in config.py.
func serverDefaults() Server {
	return Server{
		Name:          i18n.System("default_server_name"),
		Type:          "emby",
		Protocol:      "http",
		Hosts:         []string{},
		Port:          8096,
		ActiveHost:    0,
		DeviceID:      "tv-remote-mpv-001",
		ClientVersion: "4.7.0.0",
	}
}

// mu guards reads/writes to the config file so concurrent goroutines (player +
// HTTP handlers) don't race. We re-read the file on each Load, matching the
// Python branch, so external edits are picked up.
var mu sync.Mutex

// Load returns the current config, applying global defaults for missing keys.
// On any error it returns defaults rather than failing. If loading triggered
// a one-time legacy-field migration, the result is written straight back so
// the old fields don't linger on disk indefinitely — only the very first
// Load() after an upgrade pays for the extra write.
func Load() *Config {
	mu.Lock()
	defer mu.Unlock()
	cfg, migrated := loadLocked()
	if migrated {
		_ = saveLocked(cfg)
	}
	return cfg
}

func loadLocked() (*Config, bool) {
	cfg := &Config{
		Servers:             []*Server{},
		ListenPort:          1980,
		MpvPipe:             `\\.\pipe\mpvsocket`,
		MpvExe:              "mpv",
		MpvCacheSecs:        DefaultMpvCacheSecs,
		SeekBackwardSecs:    5,
		SeekForwardSecs:     30,
		DLNAReceiverEnabled: true,
		AutoplayNextEpisode: true,
	}
	// A missing file is a fresh install, not an error: fall through with the
	// defaults so the normalization below still runs. Skipping it here used to
	// leave DLNAReceiverID empty until the user happened to save a setting,
	// which advertised an invalid empty UPnP UDN (uuid:) — strict control
	// points (phones) reject it, and the standby screen showed "TinyPlay ()".
	if raw, err := os.ReadFile(ConfigFile()); err == nil {
		_ = json.Unmarshal(raw, cfg) // partial parse keeps defaults on error
	}
	if cfg.Servers == nil {
		cfg.Servers = []*Server{}
	}
	migrated := false
	for _, srv := range cfg.Servers {
		if migrateLegacyFileServer(srv) {
			migrated = true
		}
		srv.Type = NormalizeServerType(srv.Type)
	}
	cfg.Language = NormalizeLanguage(cfg.Language)
	// 8080 was the old shared default across all three branches; treat a config
	// still carrying it as unset so existing installs move to the new default.
	if cfg.ListenPort == 0 || cfg.ListenPort == 8080 {
		cfg.ListenPort = 1980
	}
	if cfg.MpvExe == "" {
		cfg.MpvExe = "mpv"
	}
	cfg.MpvCacheSecs = NormalizeMpvCacheSecs(cfg.MpvCacheSecs)
	if cfg.DLNAReceiverID == "" {
		cfg.DLNAReceiverID = newID()
		migrated = true
	}
	return cfg, migrated
}

// migrateLegacyFileServer upgrades a pre-2026-07 file source (Type=="file",
// FileProtocol + a single opaque Root URL/path) into the current shape,
// where the protocol is the Type itself and Hosts/Port/Share/Domain/RootPath
// are separate fields. Reports whether it changed anything, so the caller
// can write the result straight back instead of leaving the old shape on
// disk until something else happens to save this server.
func migrateLegacyFileServer(srv *Server) bool {
	if strings.ToLower(strings.TrimSpace(srv.Type)) != "file" {
		return false
	}
	proto := strings.ToLower(strings.TrimSpace(srv.FileProtocol))
	switch proto {
	case "webdav":
		srv.Type = "webdav"
		if u, err := url.Parse(srv.Root); err == nil && u.Host != "" {
			if len(srv.Hosts) == 0 {
				srv.Hosts = []string{u.Hostname()}
			}
			if srv.Port == 0 {
				srv.Port = urlPort(u)
			}
			if srv.Protocol == "" {
				srv.Protocol = u.Scheme
			}
			if srv.RootPath == "" {
				srv.RootPath = strings.Trim(u.Path, "/")
			}
		}
	case "smb":
		srv.Type = "smb"
		if u, err := url.Parse(srv.Root); err == nil && strings.EqualFold(u.Scheme, "smb") {
			if len(srv.Hosts) == 0 {
				srv.Hosts = []string{u.Hostname()}
			}
			if srv.Port == 0 {
				if p := urlPort(u); p != 0 {
					srv.Port = p
				} else {
					srv.Port = 445
				}
			}
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) > 0 && parts[0] != "" {
				if srv.Share == "" {
					srv.Share = parts[0]
				}
				if srv.RootPath == "" && len(parts) > 1 {
					srv.RootPath = strings.Join(parts[1:], "/")
				}
			}
		}
	default: // "local", "nfs", or unset — both were always a bare filesystem path
		if proto == "nfs" {
			srv.Type = "nfs"
		} else {
			srv.Type = "local"
		}
		if srv.RootPath == "" {
			srv.RootPath = srv.Root
		}
	}
	srv.Root = ""
	srv.FileProtocol = ""
	return true
}

// urlPort returns u's explicit port as an int, or 0 if none/invalid.
func urlPort(u *url.URL) int {
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	if u.Scheme == "https" {
		return 443
	}
	if u.Scheme == "http" {
		return 80
	}
	return 0
}

func saveLocked(cfg *Config) error {
	dir := DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// ensure_ascii=False equivalent: Go's json keeps UTF-8 by default.
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0o600: config.json holds media-server passwords and access tokens in
	// clear text, so keep it readable only by the user who runs TinyPlay.
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return err
	}
	// WriteFile leaves an existing file's mode untouched; tighten configs that
	// predate this (they were written 0o644) so upgrades aren't left exposed.
	_ = os.Chmod(path, 0o600)
	return nil
}

// patch loads, mutates via fn, and saves atomically under the lock.
func patch(fn func(*Config)) *Config {
	mu.Lock()
	defer mu.Unlock()
	cfg, _ := loadLocked() // always saved below regardless of migration
	fn(cfg)
	_ = saveLocked(cfg)
	return cfg
}
