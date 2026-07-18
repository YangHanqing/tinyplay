package config

import (
	"strings"

	"tvremote/internal/i18n"
)

// Settings returns user-editable app settings.
func Settings() map[string]any {
	cfg := Load()
	lang := NormalizeLanguage(cfg.Language)
	resolved := lang
	if lang == "auto" {
		// Explicit selections pass through as-is; only auto uses the system resolver.
		resolved = i18n.SystemLang()
	}
	return map[string]any{
		"mpv_cache_secs":        NormalizeMpvCacheSecs(cfg.MpvCacheSecs),
		"seek_backward_secs":    normalizeSeek(cfg.SeekBackwardSecs, 5),
		"seek_forward_secs":     normalizeSeek(cfg.SeekForwardSecs, 30),
		"language":              lang,
		"resolved_language":     resolved,
		"dlna_receiver_enabled": cfg.DLNAReceiverEnabled,
		"autoplay_next_episode": cfg.AutoplayNextEpisode,
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
		cfg.AutoplayNextEpisode = true
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

// SetAutoplayNextEpisode persists the autoplay-next-episode toggle.
func SetAutoplayNextEpisode(enabled bool) map[string]any {
	patch(func(cfg *Config) { cfg.AutoplayNextEpisode = enabled })
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

// NormalizeLanguage maps a user/config language preference onto the shared
// persisted contract: auto | en | zh-CN | zh-TW | ja | ko | es | fr | de.
// Matching is case-insensitive; common aliases are accepted; unknown/empty
// values remain auto for backward compatibility.
func NormalizeLanguage(lang string) string {
	s := strings.ToLower(strings.TrimSpace(lang))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "en":
		return "en"
	case "zh", "zh-cn", "zh-hans":
		return "zh-CN"
	case "zh-tw", "zh-hant":
		return "zh-TW"
	case "ja", "ja-jp":
		return "ja"
	case "ko", "ko-kr":
		return "ko"
	case "es":
		return "es"
	case "fr":
		return "fr"
	case "de":
		return "de"
	case "auto", "":
		return "auto"
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
