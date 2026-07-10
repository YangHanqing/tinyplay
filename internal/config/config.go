// Package config is the Go port of app/core/config.py. It reads and writes the
// same data/config.json schema as the Python branch, so the two are
// interchangeable. It must stay free of any dependency on the HTTP server or
// the mpv player.
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

// ServerPatch holds the editable fields of a server; nil means "leave as-is".
// Mirrors UpdateServerRequest in schemas.py.
type ServerPatch struct {
	Name        *string   `json:"name"`
	Type        *string   `json:"type"`
	Protocol    *string   `json:"protocol"`
	Hosts       *[]string `json:"hosts"`
	Port        *int      `json:"port"`
	Share       *string   `json:"share"`
	Domain      *string   `json:"domain"`
	RootPath    *string   `json:"root_path"`
	Username    *string   `json:"username"`
	Password    *string   `json:"password"`
	PlaylistURL *string   `json:"playlist_url"`
	EPGURL      *string   `json:"epg_url"`
}

func (p ServerPatch) apply(s *Server) {
	if p.Name != nil {
		s.Name = *p.Name
	}
	if p.Type != nil {
		s.Type = NormalizeServerType(*p.Type)
	}
	if p.Protocol != nil {
		s.Protocol = *p.Protocol
	}
	if p.Hosts != nil {
		s.Hosts = normalizeHosts(*p.Hosts)
	}
	if p.Port != nil {
		s.Port = *p.Port
	}
	if p.Share != nil {
		s.Share = strings.TrimSpace(*p.Share)
	}
	if p.Domain != nil {
		s.Domain = strings.TrimSpace(*p.Domain)
	}
	if p.RootPath != nil {
		s.RootPath = strings.TrimSpace(*p.RootPath)
	}
	if p.Username != nil {
		s.Username = *p.Username
	}
	if p.Password != nil {
		s.Password = *p.Password
	}
	if p.PlaylistURL != nil {
		s.PlaylistURL = strings.TrimSpace(*p.PlaylistURL)
	}
	if p.EPGURL != nil {
		s.EPGURL = strings.TrimSpace(*p.EPGURL)
	}
}

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
			if e.PositionSeconds < 5 || (e.DurationSeconds > 0 && e.PositionSeconds >= e.DurationSeconds*.95) {
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
	MinMpvCacheSecs     = 60
	MaxMpvCacheSecs     = 7200
)

func NormalizeMpvCacheSecs(secs int) int {
	if secs <= 0 {
		return DefaultMpvCacheSecs
	}
	if secs < MinMpvCacheSecs {
		return MinMpvCacheSecs
	}
	if secs > MaxMpvCacheSecs {
		return MaxMpvCacheSecs
	}
	return secs
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
	}
	raw, err := os.ReadFile(ConfigFile())
	if err != nil {
		return cfg, false
	}
	_ = json.Unmarshal(raw, cfg) // partial parse keeps defaults on error
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
	return os.WriteFile(filepath.Join(dir, "config.json"), buf, 0o644)
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

// ── Server CRUD (port of config.py) ──────────────────────────────────────────

func Servers() []*Server { return Load().Servers }

func GetServer(id string) *Server {
	for _, s := range Servers() {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// ActiveServer returns the active server, falling back to the first one.
func ActiveServer() *Server {
	cfg := Load()
	if len(cfg.Servers) == 0 {
		return nil
	}
	if cfg.ActiveServerID != "" {
		for _, s := range cfg.Servers {
			if s.ID == cfg.ActiveServerID {
				return s
			}
		}
	}
	return cfg.Servers[0]
}

// AddServer merges data over the server defaults, assigns a uuid, and persists.
func AddServer(in Server) *Server {
	srv := serverDefaults()
	mergeServer(&srv, in)
	// Checked against the caller's raw input, not srv.Name: serverDefaults()
	// already pre-fills Name with a generic placeholder, so checking the
	// merged value here would never see it as blank and this per-type
	// fallback (host, or the playlist's own host for iptv) would never run.
	if strings.TrimSpace(in.Name) == "" {
		srv.Name = defaultServerName(&srv)
	}
	srv.ID = newID()
	out := srv
	patch(func(cfg *Config) {
		cfg.Servers = append(cfg.Servers, &out)
		if cfg.ActiveServerID == "" {
			cfg.ActiveServerID = out.ID
		}
	})
	return &out
}

// UpdateServer applies the non-empty fields of data over the existing server.
func UpdateServer(id string, data ServerPatch) *Server {
	var result *Server
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID != id {
				continue
			}
			data.apply(s)
			if data.Name != nil && strings.TrimSpace(*data.Name) == "" {
				s.Name = defaultServerName(s)
			}
			result = s
			break
		}
	})
	return result
}

func DeleteServer(id string) bool {
	removed := false
	patch(func(cfg *Config) {
		kept := cfg.Servers[:0:0]
		for _, s := range cfg.Servers {
			if s.ID == id {
				removed = true
				continue
			}
			kept = append(kept, s)
		}
		cfg.Servers = kept
		history := cfg.LocalPlaybackHistory[:0:0]
		for _, entry := range cfg.LocalPlaybackHistory {
			if entry.ServerID != id {
				history = append(history, entry)
			}
		}
		cfg.LocalPlaybackHistory = history
		if cfg.ActiveServerID == id {
			if len(cfg.Servers) > 0 {
				cfg.ActiveServerID = cfg.Servers[0].ID
			} else {
				cfg.ActiveServerID = ""
			}
		}
	})
	return removed
}

func SetActiveServer(id string) bool {
	exists := GetServer(id) != nil
	if exists {
		patch(func(cfg *Config) { cfg.ActiveServerID = id })
	}
	return exists
}

func SetActiveHost(id string, hostIndex int) bool {
	ok := false
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID == id {
				if hostIndex >= 0 && hostIndex < len(s.Hosts) {
					s.ActiveHost = hostIndex
					ok = true
				}
				break
			}
		}
	})
	return ok
}

// SetAuth records login results (token + user id) for a server.
func SetAuth(id, username, token, userID string) {
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID == id {
				s.Username = username
				s.AccessToken = token
				s.UserID = userID
				break
			}
		}
	})
}

// ToggleIPTVFavorite adds/removes channelID from a server's favorites and
// returns the updated list.
func ToggleIPTVFavorite(serverID, channelID string) []string {
	var result []string
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID != serverID {
				continue
			}
			idx := -1
			for i, id := range s.IPTVFavorites {
				if id == channelID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				s.IPTVFavorites = append(s.IPTVFavorites[:idx], s.IPTVFavorites[idx+1:]...)
			} else {
				s.IPTVFavorites = append(s.IPTVFavorites, channelID)
			}
			result = s.IPTVFavorites
			break
		}
	})
	return result
}

// IPTVFavorites returns a server's favorite channel ids.
func IPTVFavorites(serverID string) []string {
	if s := GetServer(serverID); s != nil {
		return s.IPTVFavorites
	}
	return nil
}

// RecordIPTVRecent pushes a channel watch to the front of a server's
// recently-watched list, deduplicating and capping it at limit entries.
func RecordIPTVRecent(serverID, channelID string, limit int) []RecentChannel {
	var result []RecentChannel
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID != serverID {
				continue
			}
			entry := RecentChannel{ChannelID: channelID, WatchedAt: nowUTC()}
			kept := make([]RecentChannel, 0, len(s.IPTVRecent)+1)
			kept = append(kept, entry)
			for _, r := range s.IPTVRecent {
				if r.ChannelID != channelID {
					kept = append(kept, r)
				}
			}
			if limit > 0 && len(kept) > limit {
				kept = kept[:limit]
			}
			s.IPTVRecent = kept
			result = s.IPTVRecent
			break
		}
	})
	return result
}

// IPTVRecent returns a server's recently-watched channels, most recent first.
func IPTVRecent(serverID string) []RecentChannel {
	if s := GetServer(serverID); s != nil {
		return s.IPTVRecent
	}
	return nil
}

// Settings returns user-editable app settings.
func Settings() map[string]any {
	cfg := Load()
	return map[string]any{
		"mpv_cache_secs":        NormalizeMpvCacheSecs(cfg.MpvCacheSecs),
		"seek_backward_secs":    normalizeSeek(cfg.SeekBackwardSecs, 5),
		"seek_forward_secs":     normalizeSeek(cfg.SeekForwardSecs, 30),
		"language":              NormalizeLanguage(cfg.Language),
		"dlna_receiver_enabled": cfg.DLNAReceiverEnabled,
		// The source-type picker filters its file-source cards against this
		// list, so a build that can't actually serve a given kind doesn't
		// offer it as an option.
		"supported_file_protocols": []string{"local", "smb", "webdav", "nfs"},
	}
}

// ResetAll clears every server/account and user preference back to defaults
// — the settings danger-zone "reset everything" action. Installation-level
// settings with no phone-UI control (listen port, mpv path) are left alone.
func ResetAll() map[string]any {
	patch(func(cfg *Config) {
		cfg.Servers = []*Server{}
		cfg.ActiveServerID = ""
		cfg.MpvCacheSecs = DefaultMpvCacheSecs
		cfg.SeekBackwardSecs = 5
		cfg.SeekForwardSecs = 30
		cfg.Language = ""
		cfg.DLNAReceiverEnabled = true
		cfg.DLNAReceiverID = newID()
		cfg.LocalPlaybackHistory = nil
	})
	return Settings()
}

// DLNAReceiverID is a stable UPnP device UUID, generated once and persisted.
func DLNAReceiverID() string { return Load().DLNAReceiverID }

// SetDLNAReceiverEnabled persists the receiver toggle. The server owns the
// socket lifecycle; callers apply this result immediately after saving.
func SetDLNAReceiverEnabled(enabled bool) map[string]any {
	patch(func(cfg *Config) { cfg.DLNAReceiverEnabled = enabled })
	return Settings()
}

func normalizeSeek(v, fallback int) int {
	if v < 5 || v > 60 || v%5 != 0 {
		return fallback
	}
	return v
}

func SetSeekSeconds(backward, forward int) map[string]any {
	patch(func(cfg *Config) {
		cfg.SeekBackwardSecs = normalizeSeek(backward, 5)
		cfg.SeekForwardSecs = normalizeSeek(forward, 30)
	})
	return Settings()
}

func SetMpvCacheSecs(secs int) map[string]any {
	patch(func(cfg *Config) { cfg.MpvCacheSecs = NormalizeMpvCacheSecs(secs) })
	return Settings()
}

func NormalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn":
		return "zh-CN"
	case "en":
		return "en"
	default:
		return "auto"
	}
}

func SetLanguage(lang string) map[string]any {
	lang = NormalizeLanguage(lang)
	patch(func(cfg *Config) { cfg.Language = lang })
	i18n.SetPreferred(lang)
	return Settings()
}

// MaxServerHosts is the backup-address cap: 1 primary + 2 backups is enough
// for the "server moved to a new IP / has a LAN + WAN address" cases this
// exists for, and keeps the address list short enough to actually manage
// from a phone keyboard. Enforced here (not just in the UI) so the API can't
// be used to stash more.
const MaxServerHosts = 3

func normalizeHosts(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
		if len(out) == MaxServerHosts {
			break
		}
	}
	return out
}

func NormalizeServerType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "jellyfin", "plex", "webdav", "smb", "local", "nfs", "iptv":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "emby"
	}
}

// IsFileServerType reports whether kind is one of the file-browsing source
// kinds (as opposed to a poster-wall media server or IPTV).
func IsFileServerType(kind string) bool {
	switch NormalizeServerType(kind) {
	case "webdav", "smb", "local", "nfs":
		return true
	default:
		return false
	}
}

// ── URL helpers ──────────────────────────────────────────────────────────────

// BuildServerURL builds protocol://host:port for the active host.
func BuildServerURL(s *Server) string {
	if s == nil || len(s.Hosts) == 0 {
		return ""
	}
	idx := s.ActiveHost
	if idx < 0 || idx >= len(s.Hosts) {
		idx = 0
	}
	proto := s.Protocol
	if proto == "" {
		proto = "http"
	}
	port := s.Port
	if port == 0 {
		if NormalizeServerType(s.Type) == "plex" {
			port = 32400
		} else {
			port = 8096
		}
	}
	return proto + "://" + s.Hosts[idx] + ":" + strconv.Itoa(port)
}

func defaultServerName(s *Server) string {
	kind := NormalizeServerType(s.Type)
	if len(s.Hosts) > 0 {
		if h := strings.TrimSpace(s.Hosts[0]); h != "" {
			return h
		}
	}
	if kind == "iptv" {
		if u, err := url.Parse(s.PlaylistURL); err == nil && u.Host != "" {
			return u.Host
		}
		return i18n.System("default_iptv_name")
	}
	// local/nfs have no host to fall back on; use the chosen folder's own
	// name (e.g. RootPath ".../NAS/Movies" -> "Movies") when there is one.
	if (kind == "local" || kind == "nfs") && strings.TrimSpace(s.RootPath) != "" {
		base := strings.TrimRight(strings.ReplaceAll(s.RootPath, "\\", "/"), "/")
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if base != "" {
			return base
		}
	}
	return i18n.System("default_server_name")
}

func mergeServer(dst *Server, in Server) {
	if in.Name != "" {
		dst.Name = in.Name
	}
	if in.Type != "" {
		dst.Type = NormalizeServerType(in.Type)
	}
	if in.Protocol != "" {
		dst.Protocol = in.Protocol
	}
	if in.Hosts != nil {
		dst.Hosts = normalizeHosts(in.Hosts)
	}
	if in.Port != 0 {
		dst.Port = in.Port
	}
	if in.Share != "" {
		dst.Share = strings.TrimSpace(in.Share)
	}
	if in.Domain != "" {
		dst.Domain = strings.TrimSpace(in.Domain)
	}
	if in.RootPath != "" {
		dst.RootPath = strings.TrimSpace(in.RootPath)
	}
	if in.Username != "" {
		dst.Username = in.Username
	}
	if in.Password != "" {
		dst.Password = in.Password
	}
	if in.AccessToken != "" {
		dst.AccessToken = in.AccessToken
	}
	if in.PlaylistURL != "" {
		dst.PlaylistURL = strings.TrimSpace(in.PlaylistURL)
	}
	if in.EPGURL != "" {
		dst.EPGURL = strings.TrimSpace(in.EPGURL)
	}
}
