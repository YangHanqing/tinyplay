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
	"log"
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
	BasePath      string   `json:"base_path,omitempty"`
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
	Servers        []*Server `json:"servers"`
	ActiveServerID string    `json:"active_server_id"`
	// InstallationID identifies this TinyPlay target to paired native clients.
	// It is independent of media-server DeviceID values and the DLNA receiver
	// UUID, which each have their own protocol semantics.
	InstallationID string `json:"installation_id,omitempty"`
	ListenPort     int    `json:"listen_port"`
	MpvPipe        string `json:"mpv_pipe"`
	// MpvExe is the user's advanced-settings custom mpv executable path (tray
	// "高级设置 → 自定义 mpv 播放器…"), not a normal Go-desktop user setting in
	// the everyday sense. Empty means "no custom path configured" — the
	// bundled → system fallback stays the default, supported path; see
	// internal/player.DetectMPV for the full priority order.
	MpvExe               string               `json:"mpv_exe"`
	MpvCacheSecs         int                  `json:"mpv_cache_secs"`
	SeekBackwardSecs     int                  `json:"seek_backward_secs,omitempty"`
	SeekForwardSecs      int                  `json:"seek_forward_secs,omitempty"`
	Language             string               `json:"language,omitempty"`
	WebsiteCustomSites   []WebsiteCustomSite  `json:"website_custom_sites,omitempty"`
	DLNAReceiverEnabled  bool                 `json:"dlna_receiver_enabled"`
	DLNAReceiverID       string               `json:"dlna_receiver_id,omitempty"`
	LocalPlaybackHistory []LocalPlaybackEntry `json:"local_playback_history,omitempty"`
	AutoplayNextEpisode  bool                 `json:"autoplay_next_episode"`
	// AutoplayCountdownSecs is how long the next-episode countdown runs before
	// the transition fires. 0 is a legitimate value ("no countdown"), so this
	// field must not use omitempty and must not be normalized through a
	// zero-means-unset rule: loadLocked seeds the default before unmarshal, so
	// an absent key keeps 5 while an explicit 0 survives a round trip.
	AutoplayCountdownSecs int `json:"autoplay_countdown_secs"`
	// AutoplayLoopList makes the chain wrap instead of stopping: the last item
	// of a series/folder is followed by the first one, forever. It is a
	// sub-option of AutoplayNextEpisode and does nothing while that is off.
	AutoplayLoopList bool `json:"autoplay_loop_list"`
	// KeepPlaybackSpeed: when true (default), a new title reuses the speed the
	// previous title was playing at within this process. The remembered speed
	// itself is session-scoped in the player and is never written here —
	// persisting it would mean a user who left 2x on last week silently gets
	// 2x today, and this setting is about continuity within a viewing session.
	KeepPlaybackSpeed bool `json:"keep_playback_speed"`
	// RememberTitleSettings: when true, per-title subtitle/audio/delays/speed
	// the user explicitly changed are restored the next time that same title
	// plays. Default off so a fresh install never overrides the server's own
	// track defaults without the user opting in.
	RememberTitleSettings bool `json:"remember_title_settings"`
	// ForceFullscreenPlayback: when true (default), the desktop player is
	// pinned fullscreen and re-forced back to fullscreen on every title change
	// — TinyPlay's usual "phone remote controls a TV" posture. When false, a
	// window the user manually resized/un-fullscreened is left alone across
	// title changes for the rest of the running mpv process; it still opens
	// fullscreen on a fresh mpv spawn (no OS-level API to read back the exact
	// pixel geometry the user last dragged it to, so nothing is persisted to
	// disk — this is a session-scoped preference, not a remembered window
	// rect).
	ForceFullscreenPlayback bool                 `json:"force_fullscreen_playback"`
	TitleSettingsHistory    []TitleSettingsEntry `json:"title_settings_history,omitempty"`
	PairedDevices           []PairedDevice       `json:"paired_devices,omitempty"`
	// Desktop update prompts are intentionally small, local preferences. A
	// skipped version never suppresses a newer release, while RemindAfter keeps
	// an app restart from immediately asking the same question again.
	UpdateSkippedVersion string `json:"update_skipped_version,omitempty"`
	UpdateRemindVersion  string `json:"update_remind_version,omitempty"`
	UpdateRemindAfter    string `json:"update_remind_after,omitempty"`
	// WebsiteCacheLimitMB bounds the built-in browser's WebView profile, the
	// one directory under DataDir() with no size ceiling of its own (logs
	// rotate at 20MB, artwork is capped by imagecache.MaxBytes). Like MpvExe
	// this is a property of this installed copy, reachable only from the
	// native shell's "高级设置" menu — never from the phone-facing settings.
	//
	// Zero/absent means "not chosen", which resolves to DefaultWebsiteCacheLimitMB
	// rather than to "unlimited"; unlimited is the explicit -1. A brand-new
	// install and an upgraded one therefore land on the same default without a
	// migration, and a user who deliberately turned the cap off keeps it off.
	//
	// Both fields are Windows-only in practice. The macOS shell's browser store
	// belongs to WKWebsiteDataStore, not to anything the core can see, so it
	// keeps the same policy in UserDefaults (see WebCache in macos/Sources/main.swift)
	// rather than routing a preference the core cannot act on through config.json.
	WebsiteCacheLimitMB int `json:"website_cache_limit_mb,omitempty"`
	// WebsiteCacheDir relocates that profile off the system drive. Empty means
	// the default under DataDir(). A path that later becomes unwritable is not
	// an error worth blocking on: the caller falls back to the default and says
	// so. There is no macOS counterpart — WKWebsiteDataStore owns its own paths,
	// and single-volume Macs make relocation a trade not worth making.
	WebsiteCacheDir string `json:"website_cache_dir,omitempty"`
}

// WebsiteCustomSite is one shared, user-saved URL in the desktop Website
// Library. It intentionally contains no credentials; browser sessions remain
// in the platform WebView profile.
type WebsiteCustomSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type LocalPlaybackEntry struct {
	ServerID        string  `json:"server_id"`
	Path            string  `json:"path"`
	PositionSeconds float64 `json:"position_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
	UpdatedAt       string  `json:"updated_at"`
}

// TrackChoice identifies a subtitle/audio track by language + title, never by
// mpv's positional sid/aid. The same item can change track order between plays
// (a re-negotiated media source, a file rescan, a re-encode), so a stored
// sid=3 can restore the director's commentary instead of the intended track.
// Disabled means the user explicitly turned the track off ("no").
type TrackChoice struct {
	Disabled bool   `json:"disabled,omitempty"`
	Lang     string `json:"lang,omitempty"`
	Title    string `json:"title,omitempty"`
}

// TitleSettingsEntry is one per-title playback preference record. Only fields
// the user actually changed during a session are populated (dirty fields); an
// absent field means "leave the player's/server default alone". Keyed by
// server_id + item_key (item_id for media servers, relative path for file
// sources). Cap and LRU match LocalPlaybackHistory.
type TitleSettingsEntry struct {
	ServerID   string       `json:"server_id"`
	ItemKey    string       `json:"item_key"`
	Subtitle   *TrackChoice `json:"subtitle,omitempty"`
	Audio      *TrackChoice `json:"audio,omitempty"`
	SubDelay   *float64     `json:"sub_delay,omitempty"`
	AudioDelay *float64     `json:"audio_delay,omitempty"`
	Speed      *float64     `json:"speed,omitempty"`
	UpdatedAt  string       `json:"updated_at"`
}

// TitleSettingsPatch is the set of dirty fields observed during one playback.
// Nil pointers are left unchanged in the stored record; non-nil pointers
// overwrite. An empty patch (all nil) is a no-op so an untouched playback never
// freezes the server's auto-selected tracks as permanent overrides.
type TitleSettingsPatch struct {
	Subtitle   *TrackChoice
	Audio      *TrackChoice
	SubDelay   *float64
	AudioDelay *float64
	Speed      *float64
}

// LookupTitleSettings returns the stored per-title settings for serverID+itemKey.
func LookupTitleSettings(serverID, itemKey string) (TitleSettingsEntry, bool) {
	if serverID == "" || itemKey == "" {
		return TitleSettingsEntry{}, false
	}
	for _, e := range Load().TitleSettingsHistory {
		if e.ServerID == serverID && e.ItemKey == itemKey {
			return e, true
		}
	}
	return TitleSettingsEntry{}, false
}

// RecordTitleSettings merges a dirty-field delta into the LRU table. Only the
// fields present in the delta are written, so a later session that only
// touches speed cannot erase a previously remembered subtitle track.
func RecordTitleSettings(serverID, itemKey string, delta TitleSettingsPatch) {
	if serverID == "" || itemKey == "" {
		return
	}
	if delta.Subtitle == nil && delta.Audio == nil && delta.SubDelay == nil &&
		delta.AudioDelay == nil && delta.Speed == nil {
		return
	}
	patch(func(cfg *Config) {
		var merged TitleSettingsEntry
		found := false
		for _, e := range cfg.TitleSettingsHistory {
			if e.ServerID == serverID && e.ItemKey == itemKey {
				merged = e
				found = true
				break
			}
		}
		if !found {
			merged = TitleSettingsEntry{ServerID: serverID, ItemKey: itemKey}
		}
		if delta.Subtitle != nil {
			cp := *delta.Subtitle
			merged.Subtitle = &cp
		}
		if delta.Audio != nil {
			cp := *delta.Audio
			merged.Audio = &cp
		}
		if delta.SubDelay != nil {
			v := *delta.SubDelay
			merged.SubDelay = &v
		}
		if delta.AudioDelay != nil {
			v := *delta.AudioDelay
			merged.AudioDelay = &v
		}
		if delta.Speed != nil {
			v := *delta.Speed
			merged.Speed = &v
		}
		merged.UpdatedAt = nowUTC()

		kept := []TitleSettingsEntry{merged}
		for _, e := range cfg.TitleSettingsHistory {
			if e.ServerID != serverID || e.ItemKey != itemKey {
				kept = append(kept, e)
			}
		}
		if len(kept) > 1000 {
			kept = kept[:1000]
		}
		cfg.TitleSettingsHistory = kept
	})
}

func LocalPlaybackPosition(serverID, path string) float64 {
	for _, e := range Load().LocalPlaybackHistory {
		if e.ServerID == serverID && e.Path == path {
			// Same tail rule as the Emby/Plex providers: only "within the last
			// 15s" or "past 99%" counts as finished. A percentage alone is too
			// aggressive for long files (95% of 3h leaves 9 minutes unwatched).
			//
			// An unknown duration means the tail cannot be ruled out, so it
			// restarts rather than skipping the check. Resuming into an
			// unverifiable tail makes a short clip reach EOF the instant it
			// opens, which the host then reads as a completed playback and
			// hands to autoplay; replaying a short clip from 0 is the far
			// cheaper mistake.
			if e.PositionSeconds < 5 || e.DurationSeconds <= 0 ||
				e.PositionSeconds >= e.DurationSeconds-15 ||
				e.PositionSeconds/e.DurationSeconds >= 0.99 {
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
		// position and duration come from different clocks: the position is the
		// player's last observed value and survives, while the duration is read
		// live and can be absent at exit (mpv drops it at end-of-file, and a
		// clip short enough to finish inside one progress interval may never
		// have published it). Writing that 0 over a duration we already knew
		// would permanently disable the tail check above for this entry, so the
		// old value is carried forward instead.
		if duration <= 0 {
			for _, e := range cfg.LocalPlaybackHistory {
				if e.ServerID == serverID && e.Path == path {
					duration = e.DurationSeconds
					break
				}
			}
		}
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
	// DefaultAutoplayCountdownSecs keeps the countdown users already know.
	DefaultAutoplayCountdownSecs = 5
)

// AutoplayCountdownPresetSecs are the only accepted countdown lengths. 0 means
// "no countdown at all" — the transition fires as soon as the next item is
// resolved, and the phone renders nothing (see autoplayState.silent).
var AutoplayCountdownPresetSecs = []int{0, 5, 10}

// NormalizeAutoplayCountdownSecs keeps 0 as a real choice. Anything outside
// the preset list falls back to the default rather than to the nearest value:
// these are three discrete options in a segmented control, not a slider.
func NormalizeAutoplayCountdownSecs(secs int) int {
	for _, preset := range AutoplayCountdownPresetSecs {
		if secs == preset {
			return secs
		}
	}
	return DefaultAutoplayCountdownSecs
}

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
		logSaveFailure(saveLocked(cfg))
	}
	return cfg
}

func loadLocked() (*Config, bool) {
	cfg := &Config{
		Servers:             []*Server{},
		ListenPort:          1980,
		MpvPipe:             `\\.\pipe\mpvsocket`,
		MpvExe:              "",
		MpvCacheSecs:        DefaultMpvCacheSecs,
		SeekBackwardSecs:    5,
		SeekForwardSecs:     30,
		DLNAReceiverEnabled: true,
		AutoplayNextEpisode: true,
		// Seeded before json.Unmarshal so an absent key means "default" while
		// an explicit 0 means "no countdown" — see the field comment.
		AutoplayCountdownSecs: DefaultAutoplayCountdownSecs,
		// KeepPlaybackSpeed defaults on: continuity within a session is the
		// less-surprising default once someone has reached for the speed
		// control. RememberTitleSettings defaults off so an untouched install
		// never freezes auto-selected tracks as permanent overrides.
		KeepPlaybackSpeed:     true,
		RememberTitleSettings: false,
		// ForceFullscreenPlayback defaults on: matches the behavior every
		// existing install already has, so this ships with zero migration.
		ForceFullscreenPlayback: true,
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
	if cfg.WebsiteCustomSites == nil {
		cfg.WebsiteCustomSites = []WebsiteCustomSite{}
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
	// Unlike MpvCacheSecs below, MpvExe is not filled in with a default here:
	// "" is the meaningful "no custom path configured" state (see MpvExe doc),
	// not a hole. internal/player.DetectMPV treats "" as "fall through to the
	// next candidate in the priority chain".
	//
	// The bare "mpv" it is cleared to below is the *old* default, which every
	// pre-2026-08 install wrote to disk. Under the current priority order a
	// non-empty MpvExe is a user-chosen custom runtime that outranks the
	// bundled one, so leaving that legacy value in place would silently demote
	// every existing installation to whatever mpv happens to be on PATH. A
	// bare name is not something the shell's file picker can ever produce
	// (it yields an absolute path), and as a custom value it would mean
	// exactly what candidate 4 already does, so treating it as unset costs
	// nothing.
	if cfg.MpvExe == "mpv" {
		cfg.MpvExe = ""
		migrated = true
	}
	cfg.MpvCacheSecs = NormalizeMpvCacheSecs(cfg.MpvCacheSecs)
	if cfg.InstallationID == "" {
		cfg.InstallationID = newID()
		migrated = true
	}
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
//
// A failed write cannot be surfaced to the caller without changing every
// accessor's signature, but it must not be invisible either: a config.json that
// silently refuses to persist looks like the app forgetting things at random —
// a paired phone whose token never lands is rejected on its very next request,
// which reads as pairing being broken rather than as a disk problem.
func patch(fn func(*Config)) *Config {
	mu.Lock()
	defer mu.Unlock()
	cfg, _ := loadLocked() // always saved below regardless of migration
	fn(cfg)
	logSaveFailure(saveLocked(cfg))
	return cfg
}

func logSaveFailure(err error) {
	if err != nil {
		log.Printf("config: could not write %s: %v", ConfigFile(), err)
	}
}
