package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"tvremote/internal/i18n"
)

// ServerPatch holds the editable fields of a server; nil means "leave as-is".
// Mirrors UpdateServerRequest in schemas.py.
type ServerPatch struct {
	Name        *string   `json:"name"`
	Type        *string   `json:"type"`
	Protocol    *string   `json:"protocol"`
	Hosts       *[]string `json:"hosts"`
	Port        *int      `json:"port"`
	BasePath    *string   `json:"base_path"`
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
	if p.BasePath != nil {
		s.BasePath = strings.Trim(*p.BasePath, "/ ")
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

// ApplyServerPatch returns the normalized result of applying a patch without
// persisting it. Server handlers use this to verify a candidate connection
// before replacing a known-good saved source.
func ApplyServerPatch(current Server, data ServerPatch) Server {
	data.apply(&current)
	if data.Name != nil && strings.TrimSpace(*data.Name) == "" {
		current.Name = defaultServerName(&current)
	}
	return current
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
			updated := ApplyServerPatch(*s, data)
			*s = updated
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

// BuildServerURL builds the active server's HTTP(S) URL, including an optional
// reverse-proxy base path. A legacy/direct API client may still pass a complete
// URL in Hosts; parse that at this boundary so every caller follows the same
// scheme, port, and path precedence as the phone form.
func BuildServerURL(s *Server) string {
	if s == nil || len(s.Hosts) == 0 {
		return ""
	}
	idx := s.ActiveHost
	if idx < 0 || idx >= len(s.Hosts) {
		idx = 0
	}
	rawHost := strings.TrimSpace(s.Hosts[idx])
	proto := s.Protocol
	if proto == "" {
		proto = "http"
	}
	port := s.Port
	basePath := strings.Trim(s.BasePath, "/ ")
	if parsed, err := url.Parse(rawHost); err == nil && parsed.Scheme != "" && parsed.Hostname() != "" {
		proto = strings.ToLower(parsed.Scheme)
		rawHost = parsed.Hostname()
		if parsed.Port() != "" {
			port, _ = strconv.Atoi(parsed.Port())
		} else if proto == "https" {
			port = 443
		} else if proto == "http" {
			port = 80
		}
		if basePath == "" {
			basePath = strings.Trim(parsed.EscapedPath(), "/ ")
		}
	}
	if proto != "http" && proto != "https" {
		return ""
	}
	if port == 0 {
		if proto == "https" {
			port = 443
		} else if NormalizeServerType(s.Type) == "plex" {
			port = 32400
		} else {
			port = 8096
		}
	}
	base := proto + "://" + rawHost + ":" + strconv.Itoa(port)
	if basePath == "" {
		return base
	}
	return base + "/" + basePath
}

func defaultServerName(s *Server) string {
	kind := NormalizeServerType(s.Type)
	label := serverTypeLabel(kind)
	if len(s.Hosts) > 0 {
		if h := strings.TrimSpace(s.Hosts[0]); h != "" {
			return fmt.Sprintf("%s - %s", label, h)
		}
	}
	if kind == "iptv" {
		if u, err := url.Parse(s.PlaylistURL); err == nil && u.Hostname() != "" {
			return fmt.Sprintf("%s - %s", label, u.Hostname())
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
			return fmt.Sprintf("%s - %s", label, base)
		}
	}
	return i18n.System("default_server_name")
}

func serverTypeLabel(kind string) string {
	switch kind {
	case "jellyfin":
		return "Jellyfin"
	case "plex":
		return "Plex"
	case "webdav":
		return "WebDAV"
	case "smb":
		return "SMB"
	case "local":
		return "Local"
	case "nfs":
		return "NFS"
	case "iptv":
		return "IPTV"
	default:
		return "Emby"
	}
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
	if in.BasePath != "" {
		dst.BasePath = strings.Trim(in.BasePath, "/ ")
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
