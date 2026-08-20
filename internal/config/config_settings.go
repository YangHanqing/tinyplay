package config

import (
	"strings"
	"time"

	"tvremote/internal/i18n"
)

// Settings returns user-editable app settings, resolving "auto" against this
// machine's own locale. Prefer SettingsFor when a request is in hand.
func Settings() map[string]any { return SettingsFor("") }

// SettingsFor returns the same settings with "auto" resolved against the
// language the *caller* asked for (the phone's Accept-Language, via
// i18n.RequestLang).
//
// This UI is read on a phone, not on the desktop, so "automatic" has to mean
// the phone's language — resolving it against the desktop's locale is why a
// Chinese Mac served an English page to a Chinese phone while every other
// website on that phone came up in Chinese. Two phones set to different
// languages each get their own, which is what "automatic" already promises.
// An explicit selection is a deliberate choice and is never overridden.
//
// requestLang may be empty (no request in hand), in which case the desktop's
// locale is the fallback exactly as before.
func SettingsFor(requestLang string) map[string]any {
	cfg := Load()
	lang := NormalizeLanguage(cfg.Language)
	resolved := lang
	if lang == "auto" {
		// Explicit selections pass through as-is; only auto is resolved.
		// Empty means "no request in hand" and must fall back to the desktop —
		// it cannot go through Normalize, which answers "en" for anything it
		// does not recognise, including "".
		resolved = i18n.SystemLang()
		if requestLang != "" {
			if normalized := i18n.Normalize(requestLang); i18n.Supported(normalized) {
				resolved = normalized
			}
		}
	}
	return map[string]any{
		"mpv_cache_secs":            NormalizeMpvCacheSecs(cfg.MpvCacheSecs),
		"seek_backward_secs":        normalizeSeek(cfg.SeekBackwardSecs, 5),
		"seek_forward_secs":         normalizeSeek(cfg.SeekForwardSecs, 30),
		"language":                  lang,
		"resolved_language":         resolved,
		"dlna_receiver_enabled":     cfg.DLNAReceiverEnabled,
		"autoplay_next_episode":     cfg.AutoplayNextEpisode,
		"autoplay_countdown_secs":   NormalizeAutoplayCountdownSecs(cfg.AutoplayCountdownSecs),
		"autoplay_loop_list":        cfg.AutoplayLoopList,
		"keep_playback_speed":       cfg.KeepPlaybackSpeed,
		"remember_title_settings":   cfg.RememberTitleSettings,
		"force_fullscreen_playback": cfg.ForceFullscreenPlayback,
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
		cfg.WebsiteCustomSites = nil
		cfg.DLNAReceiverEnabled = true
		cfg.DLNAReceiverID = newID()
		cfg.LocalPlaybackHistory = nil
		cfg.TitleSettingsHistory = nil
		cfg.AutoplayNextEpisode = true
		cfg.AutoplayCountdownSecs = DefaultAutoplayCountdownSecs
		cfg.AutoplayLoopList = false
		cfg.KeepPlaybackSpeed = true
		cfg.RememberTitleSettings = false
		cfg.ForceFullscreenPlayback = true
		cfg.UpdateSkippedVersion = ""
		cfg.UpdateRemindVersion = ""
		cfg.UpdateRemindAfter = ""
		// PairedDevices deliberately survives. This reset is a phone-initiated
		// action, and unpairing is a physical-presence one: a phone must not be
		// able to kick every other device off the remote — and clearing the list
		// here would also lock out the phone making the request. The computer's
		// own window owns "unpair all devices".
	})
	return Settings()
}

// ShouldOfferUpdate reports whether an automatically discovered release may
// interrupt the user. Manual "Check for Updates" actions deliberately bypass
// this policy so people can revisit a skipped release on their own terms.
func ShouldOfferUpdate(version string, now time.Time) bool {
	cfg := Load()
	if version == "" || cfg.UpdateSkippedVersion == version {
		return false
	}
	if cfg.UpdateRemindVersion == version {
		until, err := time.Parse(time.RFC3339, cfg.UpdateRemindAfter)
		if err == nil && until.After(now) {
			return false
		}
	}
	return true
}

func SkipUpdate(version string) {
	if version == "" {
		return
	}
	patch(func(cfg *Config) {
		cfg.UpdateSkippedVersion = version
		cfg.UpdateRemindVersion = ""
		cfg.UpdateRemindAfter = ""
	})
}

func RemindAboutUpdateAfter(version string, until time.Time) {
	if version == "" {
		return
	}
	patch(func(cfg *Config) {
		// A prior skip is only for that exact release; "remind later" is an
		// affirmative re-enable of its prompt.
		if cfg.UpdateSkippedVersion == version {
			cfg.UpdateSkippedVersion = ""
		}
		cfg.UpdateRemindVersion = version
		cfg.UpdateRemindAfter = until.UTC().Format(time.RFC3339)
	})
}

// DLNAReceiverID is a stable UPnP device UUID, generated once and persisted.
func DLNAReceiverID() string { return Load().DLNAReceiverID }

// InstallationID is the stable identity advertised to paired native clients.
// ResetAll intentionally leaves it untouched so a saved iPhone target does not
// become a different device merely because the user reset media settings.
func InstallationID() string { return Load().InstallationID }

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

// SetAutoplayCountdownSecs persists the countdown length (0/5/10).
func SetAutoplayCountdownSecs(secs int) map[string]any {
	patch(func(cfg *Config) { cfg.AutoplayCountdownSecs = NormalizeAutoplayCountdownSecs(secs) })
	return Settings()
}

// SetAutoplayLoopList persists the "wrap back to the first item" toggle.
func SetAutoplayLoopList(enabled bool) map[string]any {
	patch(func(cfg *Config) { cfg.AutoplayLoopList = enabled })
	return Settings()
}

// SetKeepPlaybackSpeed persists the "keep playback speed across titles" toggle.
func SetKeepPlaybackSpeed(enabled bool) map[string]any {
	patch(func(cfg *Config) { cfg.KeepPlaybackSpeed = enabled })
	return Settings()
}

// SetRememberTitleSettings persists the per-title settings memory toggle.
func SetRememberTitleSettings(enabled bool) map[string]any {
	patch(func(cfg *Config) { cfg.RememberTitleSettings = enabled })
	return Settings()
}

// SetForceFullscreenPlayback persists the "always force the desktop player
// fullscreen" toggle. When turned off, the player package stops re-forcing
// fullscreen on every title change, letting a manually windowed/resized mpv
// window stay put for the rest of the running process.
func SetForceFullscreenPlayback(enabled bool) map[string]any {
	patch(func(cfg *Config) { cfg.ForceFullscreenPlayback = enabled })
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

// MpvExe returns the user's persisted custom mpv executable path, or "" if
// none is configured. This is distinct from the settings the phone UI edits
// (Settings()/SetMpvCacheSecs/...): it is only reachable through the
// desktop shell's "高级设置" menu (see internal/server's /desktop/mpv route).
func MpvExe() string { return Load().MpvExe }

// SetMpvExe persists (or, given "", clears) the user's custom mpv path. The
// caller is responsible for validating a non-empty path before calling this
// — see player.ValidateMPV — so that an unusable path never lands on disk.
func SetMpvExe(path string) {
	patch(func(cfg *Config) { cfg.MpvExe = strings.TrimSpace(path) })
}

// DefaultWebsiteCacheLimitMB caps the built-in browser's profile when the user
// has not chosen otherwise. 1GB is picked against how the directory is actually
// filled: video sites cache segments through Service Workers, so a browser-sized
// default (Chromium's own HTTP cache lands near 320MB) would have the cache
// evicting itself constantly, while a multi-gigabyte one defeats the point on
// the small system drives this feature exists for.
const DefaultWebsiteCacheLimitMB = 1024

// WebsiteCacheLimitUnlimited is the stored value meaning "never sweep". It is
// deliberately not 0: 0 is what an absent field unmarshals to, and defaulting a
// missing field to "unlimited" would leave every existing install uncapped —
// the exact problem this setting was added for.
const WebsiteCacheLimitUnlimited = -1

// WebsiteCacheLimitBytes resolves the persisted preference into a byte budget.
// The second result is false when the user turned the cap off, which callers
// must distinguish from a zero budget (that would mean "delete everything").
func WebsiteCacheLimitBytes() (int64, bool) {
	mb := Load().WebsiteCacheLimitMB
	if mb == WebsiteCacheLimitUnlimited {
		return 0, false
	}
	if mb <= 0 {
		mb = DefaultWebsiteCacheLimitMB
	}
	return int64(mb) << 20, true
}

// WebsiteCacheLimitMB returns the raw persisted preference, resolving absent to
// the default so the shell's menu can tick the matching radio item. Unlimited
// round-trips as WebsiteCacheLimitUnlimited.
func WebsiteCacheLimitMB() int {
	mb := Load().WebsiteCacheLimitMB
	if mb == WebsiteCacheLimitUnlimited {
		return WebsiteCacheLimitUnlimited
	}
	if mb <= 0 {
		return DefaultWebsiteCacheLimitMB
	}
	return mb
}

// SetWebsiteCacheLimitMB persists a new budget. Anything that is neither a
// positive size nor the unlimited sentinel is stored as the default rather than
// rejected: the only callers are fixed menu items, so a bad value means a bug
// here, and silently capping is safer than persisting nonsense.
func SetWebsiteCacheLimitMB(mb int) {
	if mb != WebsiteCacheLimitUnlimited && mb <= 0 {
		mb = DefaultWebsiteCacheLimitMB
	}
	patch(func(cfg *Config) { cfg.WebsiteCacheLimitMB = mb })
}

// WebsiteCacheDir returns the user's relocated browser-profile directory, or ""
// for the default under DataDir(). Validation belongs to the caller at the
// moment the profile is opened — a path on a drive that is currently unplugged
// is not worth failing a config read over.
func WebsiteCacheDir() string { return Load().WebsiteCacheDir }

// SetWebsiteCacheDir persists (or, given "", clears) that relocation.
func SetWebsiteCacheDir(dir string) {
	patch(func(cfg *Config) { cfg.WebsiteCacheDir = strings.TrimSpace(dir) })
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
