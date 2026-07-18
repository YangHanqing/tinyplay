// Package website is the pure catalog/state/command layer for TinyPlay's
// experimental desktop-only website playback mode. Platform shells (Windows
// WebView2, macOS WKWebView) consume commands and report status; phone clients
// only see the allowlisted public API surface.
package website

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Fixed ordered allowlist. There is no default "selected" site on fresh startup;
// current site is derived only from the native WebView's actual URL.
const (
	SiteBilibili = "bilibili"
	SiteIQIYI    = "iqiyi"
	SiteTencent  = "tencent"
	SiteYouku    = "youku"

	MaxSearchText = 200
	MaxTypeText   = 200

	// Hint keypad alphabet (phone 3×4 pad) and fixed two-symbol labels.
	// Order is deliberate: labels are generated as every pair in this order
	// (AA…99), yielding len(HintAlphabet)^2 = MaxHintTargets candidates.
	// X/Y are deliberately used instead of B/C: B is too easy to confuse
	// with the 8 key in the compact phone keypad.
	HintAlphabet   = "AXY123456789"
	HintLabelLen   = 2
	MaxHintTargets = 12 * 12 // 144

	// MinSeekSeconds/MaxSeekSeconds bound a single seek command's delta.
	MinSeekSeconds = -3600
	MaxSeekSeconds = 3600
	// MinPlaybackRate/MaxPlaybackRate bound the playbackRate a phone may set.
	MinPlaybackRate = 0.25
	MaxPlaybackRate = 4
)

// Site is one fixed entry in the website allowlist.
type Site struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Catalog is the ordered fixed site list. Do not reorder without updating tests
// and the phone UI expectations.
var Catalog = []Site{
	{ID: SiteBilibili, Name: "哔哩哔哩", URL: "https://www.bilibili.com/"},
	{ID: SiteIQIYI, Name: "爱奇艺", URL: "https://www.iqiyi.com/"},
	{ID: SiteTencent, Name: "腾讯视频", URL: "https://v.qq.com/"},
	{ID: SiteYouku, Name: "优酷", URL: "https://www.youku.com/"},
}

// Known public phone actions. Anything else is rejected.
const (
	ActionOpen      = "open"
	ActionClose     = "close"
	ActionBack      = "back"
	ActionForward   = "forward"
	ActionHome      = "home"
	ActionRefresh   = "refresh"
	ActionPlayPause = "play_pause"
	// Deliberately two directional actions, not one toggle: the injected
	// script cannot reliably remember "are we fullscreen" across calls (the
	// user can exit natively with their own mouse/Esc in between phone
	// presses), so each button re-derives state live instead of trusting a
	// stale flag. See controller.js's currentlyFullscreen().
	ActionFullscreenEnter = "fullscreen_enter"
	ActionFullscreenExit  = "fullscreen_exit"
	ActionSearch          = "search"
	ActionType            = "type"
	ActionEnter           = "enter"
	ActionSeek            = "seek"
	ActionSpeed           = "speed"
	ActionScrollUp        = "scroll_up"
	ActionScrollDown      = "scroll_down"
	ActionLogin           = "login"
	ActionHintEnter       = "hint_enter"
	ActionHintExit        = "hint_exit"
	ActionHintLabel       = "hint_label"
)

// SiteByID returns a catalog entry or false when the id is not allowlisted.
func SiteByID(id string) (Site, bool) {
	id = strings.TrimSpace(id)
	for _, site := range Catalog {
		if site.ID == id {
			return site, true
		}
	}
	return Site{}, false
}

// LoginURL returns the fixed per-site login page for a recognized catalog id.
// Phone clients never supply a free-form URL; shells navigate only to these.
func LoginURL(siteID string) (string, bool) {
	switch strings.TrimSpace(siteID) {
	case SiteBilibili:
		return "https://passport.bilibili.com/", true
	case SiteIQIYI:
		return "https://www.iqiyi.com/iframe/loginreg?show_back=1", true
	case SiteTencent:
		return "https://v.qq.com/s/videoplus/host", true
	case SiteYouku:
		return "https://account.youku.com/", true
	default:
		return "", false
	}
}

// MatchDomain returns the registrable/base host used for exact-domain-or-subdomain
// matching for a catalog entry. Leading "www." is stripped so both www and
// mobile hosts still resolve to the same site.
func MatchDomain(site Site) string {
	u, err := url.Parse(site.URL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if strings.HasPrefix(host, "www.") {
		host = strings.TrimPrefix(host, "www.")
	}
	return host
}

// SiteIDFromURL derives the allowlisted site id from an actual WebView URL.
// Unknown third-party hosts (and near-misses like evilbilibili.com) return "".
// Full URLs are never echoed back to phone clients — only this id is.
func SiteIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// Tolerate bare hosts if a shell ever reports them.
		if !strings.Contains(raw, "://") {
			return SiteIDFromHost(raw)
		}
		return ""
	}
	return SiteIDFromHost(u.Hostname())
}

// SiteIDFromHost matches host against the catalog using exact domain or subdomain.
func SiteIDFromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	if host == "" {
		return ""
	}
	// Prefer the longest matching domain so v.qq.com wins over a hypothetical
	// broader qq.com entry if one is ever added.
	bestID := ""
	bestLen := -1
	for _, site := range Catalog {
		domain := MatchDomain(site)
		if domain == "" {
			continue
		}
		if hostMatchesDomain(host, domain) && len(domain) > bestLen {
			bestID = site.ID
			bestLen = len(domain)
		}
	}
	return bestID
}

func hostMatchesDomain(host, domain string) bool {
	if host == domain {
		return true
	}
	// Subdomain only: "evilbilibili.com" must NOT match "bilibili.com".
	return strings.HasSuffix(host, "."+domain)
}

// IsKnownAction reports whether action is part of the public allowlist.
func IsKnownAction(action string) bool {
	switch strings.TrimSpace(action) {
	case ActionOpen, ActionClose, ActionBack, ActionForward, ActionHome, ActionRefresh,
		ActionPlayPause, ActionFullscreenEnter, ActionFullscreenExit, ActionSearch, ActionType,
		ActionEnter, ActionSeek, ActionSpeed, ActionScrollUp, ActionScrollDown,
		ActionLogin, ActionHintEnter, ActionHintExit, ActionHintLabel:
		return true
	}
	return false
}

// ValidateText bounds free-text fields. Empty is allowed for actions that do
// not require text; callers enforce presence separately.
func ValidateText(text string, max int) (string, bool) {
	text = strings.TrimSpace(text)
	if max <= 0 {
		max = MaxSearchText
	}
	if utf8.RuneCountInString(text) > max {
		return "", false
	}
	// Reject control characters other than ordinary space.
	for _, r := range text {
		if r < 0x20 && r != '\t' {
			return "", false
		}
	}
	return text, true
}

// ValidateNumber parses text as a finite float within [min, max] and returns
// it re-formatted canonically, so a native shell never receives
// attacker-controlled number formatting (leading zeros, exponents, etc.).
func ValidateNumber(text string, min, max float64) (string, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < min || v > max {
		return "", false
	}
	return strconv.FormatFloat(v, 'f', -1, 64), true
}

// ValidateHintLabel accepts exactly HintLabelLen symbols from HintAlphabet.
// Letters are normalized to uppercase; any other length or symbol is rejected
// before the shell/controller ever sees the label.
func ValidateHintLabel(label string) (string, bool) {
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) != HintLabelLen {
		return "", false
	}
	var b strings.Builder
	for _, r := range label {
		if r >= 'a' && r <= 'z' {
			r = r - 'a' + 'A'
		}
		if !strings.ContainsRune(HintAlphabet, r) {
			return "", false
		}
		b.WriteRune(r)
	}
	return b.String(), true
}

// GenerateHintLabels returns the first count two-symbol labels in fixed keypad
// order (AA, AX, …, 99). Count is clamped to [0, MaxHintTargets].
func GenerateHintLabels(count int) []string {
	if count <= 0 {
		return nil
	}
	if count > MaxHintTargets {
		count = MaxHintTargets
	}
	keys := []rune(HintAlphabet)
	out := make([]string, 0, count)
	for i := 0; i < len(keys) && len(out) < count; i++ {
		for j := 0; j < len(keys) && len(out) < count; j++ {
			out = append(out, string([]rune{keys[i], keys[j]}))
		}
	}
	return out
}

// validHintLabels returns a safe, de-duplicated copy of labels reported by the
// injected controller. Shell reports are local-only, but keeping the snapshot
// constrained to the keypad contract means the phone never enables a key for
// a malformed label.
func validHintLabels(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}
	out := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		clean, ok := ValidateHintLabel(label)
		if !ok {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
		if len(out) == MaxHintTargets {
			break
		}
	}
	return out
}
