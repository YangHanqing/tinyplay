// Package config is the Go port of app/core/config.py. It reads and writes the
// same data/config.json schema as the Python branch, so the two are
// interchangeable. It must stay free of any dependency on the HTTP server or
// the mpv player.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"tvremote/internal/i18n"
)

// ServerPatch holds the editable fields of a server; nil means "leave as-is".
// Mirrors UpdateServerRequest in schemas.py.
type ServerPatch struct {
	Name     *string   `json:"name"`
	Protocol *string   `json:"protocol"`
	Hosts    *[]string `json:"hosts"`
	Port     *int      `json:"port"`
}

func (p ServerPatch) apply(s *Server) {
	if p.Name != nil {
		s.Name = *p.Name
	}
	if p.Protocol != nil {
		s.Protocol = *p.Protocol
	}
	if p.Hosts != nil {
		s.Hosts = *p.Hosts
	}
	if p.Port != nil {
		s.Port = *p.Port
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

// Server mirrors one entry of config.json "servers".
type Server struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
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
}

// Config mirrors the top level of config.json.
type Config struct {
	Servers        []*Server `json:"servers"`
	ActiveServerID string    `json:"active_server_id"`
	ListenPort     int       `json:"listen_port"`
	MpvPipe        string    `json:"mpv_pipe"`
	MpvExe         string    `json:"mpv_exe"`
	MpvCacheSecs   int       `json:"mpv_cache_secs"`
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
// On any error it returns defaults rather than failing.
func Load() *Config {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked()
}

func loadLocked() *Config {
	cfg := &Config{
		Servers:      []*Server{},
		ListenPort:   8080,
		MpvPipe:      `\\.\pipe\mpvsocket`,
		MpvExe:       "mpv",
		MpvCacheSecs: DefaultMpvCacheSecs,
	}
	raw, err := os.ReadFile(ConfigFile())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(raw, cfg) // partial parse keeps defaults on error
	if cfg.Servers == nil {
		cfg.Servers = []*Server{}
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 8080
	}
	if cfg.MpvExe == "" {
		cfg.MpvExe = "mpv"
	}
	cfg.MpvCacheSecs = NormalizeMpvCacheSecs(cfg.MpvCacheSecs)
	return cfg
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
	cfg := loadLocked()
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
	if strings.TrimSpace(srv.Name) == "" {
		srv.Name = defaultServerName(srv.Hosts)
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
				s.Name = defaultServerName(s.Hosts)
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

// Settings returns user-editable app settings.
func Settings() map[string]any {
	cfg := Load()
	return map[string]any{"mpv_cache_secs": NormalizeMpvCacheSecs(cfg.MpvCacheSecs)}
}

func SetMpvCacheSecs(secs int) map[string]any {
	patch(func(cfg *Config) { cfg.MpvCacheSecs = NormalizeMpvCacheSecs(secs) })
	return Settings()
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
		port = 8096
	}
	return proto + "://" + s.Hosts[idx] + ":" + strconv.Itoa(port)
}

func defaultServerName(hosts []string) string {
	if len(hosts) > 0 {
		if h := strings.TrimSpace(hosts[0]); h != "" {
			return h
		}
	}
	return i18n.System("default_server_name")
}

func mergeServer(dst *Server, in Server) {
	if in.Name != "" {
		dst.Name = in.Name
	}
	if in.Protocol != "" {
		dst.Protocol = in.Protocol
	}
	if in.Hosts != nil {
		dst.Hosts = in.Hosts
	}
	if in.Port != 0 {
		dst.Port = in.Port
	}
}
