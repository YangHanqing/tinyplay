// Package website is the pure catalog/state/command layer for TinyPlay's
// experimental desktop-only website playback mode. Platform shells (Windows
// WebView2, macOS WKWebView) consume commands and report status; phone clients
// only see the allowlisted public API surface.
package website

import (
	"math"
	"net"
	"net/url"
	"regexp"
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
	SiteDouyin   = "douyin"
	SiteYouTube  = "youtube"
	SiteNetflix  = "netflix"

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
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	ChineseOnly bool   `json:"chinese_only,omitempty"`
	GenericOnly bool   `json:"generic_only,omitempty"`
}

// MoreAction is one page-specific, server-approved action shown in the
// phone's Website "More" sheet. The injected controller only reports action
// IDs; Go resolves them through the current site's fixed profile so a page can
// never invent a label, strategy, or executable command.
type MoreAction struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Strategy string `json:"strategy"`
}

const MoreActionStrategyShortcut = "site_shortcut"

// Catalog is the ordered fixed site list. Do not reorder without updating tests
// and the phone UI expectations.
var Catalog = []Site{
	{ID: SiteBilibili, Name: "哔哩哔哩", URL: "https://www.bilibili.com/", ChineseOnly: true},
	{ID: SiteIQIYI, Name: "爱奇艺", URL: "https://www.iqiyi.com/", ChineseOnly: true},
	{ID: SiteTencent, Name: "腾讯视频", URL: "https://v.qq.com/", ChineseOnly: true},
	{ID: SiteYouku, Name: "优酷", URL: "https://www.youku.com/", ChineseOnly: true},
	// Keep Douyin last: its official shortcut profile is present, while the
	// catalog order remains stable for existing phone clients.
	{ID: SiteDouyin, Name: "抖音精选", URL: "https://www.douyin.com/jingxuan", ChineseOnly: true},
	{ID: SiteYouTube, Name: "YouTube", URL: "https://www.youtube.com/"},
	{ID: SiteNetflix, Name: "Netflix", URL: "https://www.netflix.com/"},
}

var (
	hostnameLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	urlScheme     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
)

// NormalizeUserURL follows the useful subset of browser-address-bar input:
// a hostname, optional port/path/query, or a full HTTP(S) URL. An explicit
// scheme is always honoured; bare input picks one the way an address bar
// effectively does — see schemeForBareHost. Non-web schemes are deliberately
// not navigable by TinyPlay.
func NormalizeUserURL(raw string) (normalized, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || utf8.RuneCountInString(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t ") {
		return "", "", false
	}
	inferScheme := !urlScheme.MatchString(raw)
	if inferScheme {
		// Parse as HTTP first purely to split host/port off the path; the real
		// scheme is chosen below, once the host is known.
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !validWebsiteHost(host) {
		return "", "", false
	}
	if inferScheme {
		u.Scheme = schemeForBareHost(host, u.Port())
	}
	// Normalizing the scheme/host makes equivalent input share one stored form;
	// Preserve path, query, fragment, port, and an explicitly chosen HTTP scheme.
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	name = strings.TrimPrefix(host, "www.")
	return u.String(), name, true
}

// localHostSuffixes are the mDNS/LAN naming conventions that identify a box on
// the local network rather than a site on the public web.
var localHostSuffixes = []string{".local", ".lan", ".home", ".internal", ".localdomain"}

// schemeForBareHost picks the scheme for input typed without one. Public domain
// names get HTTPS, matching both what browsers do today and what those sites
// actually serve — plain HTTP typically only redirects, and macOS App Transport
// Security refuses to load it in the native WebView at all, so an HTTP default
// left entries like "mgtv.com" permanently unopenable. LAN addresses (IP
// literal, localhost, a single-label or *.local style name, or an explicit
// non-443 port) keep HTTP so self-hosted web UIs stay reachable.
func schemeForBareHost(host, port string) string {
	if port != "" && port != "443" {
		return "http"
	}
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") || !strings.Contains(host, ".") {
		return "http"
	}
	for _, suffix := range localHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return "http"
		}
	}
	return "https"
}

func validWebsiteHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if !hostnameLabel.MatchString(label) {
			return false
		}
	}
	return true
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
	// ActionSpeed sets an absolute playbackRate; ActionSpeedUp/ActionSpeedDown
	// step through the site's own speed ladder via its documented shortcut.
	// Which of the two forms a site gets — if any — is fixed by SpeedModeForSite.
	ActionSpeed      = "speed"
	ActionSpeedUp    = "speed_up"
	ActionSpeedDown  = "speed_down"
	ActionScrollUp   = "scroll_up"
	ActionScrollDown = "scroll_down"
	// ActionArrowUp/ActionArrowDown press the real ArrowUp/ArrowDown key,
	// distinct from ActionScrollUp/ActionScrollDown's plain page scroll — only
	// offered where the recognized site's own player reacts to that key
	// (Douyin's feed next/previous video, YouTube's volume step). See
	// ArrowNavAvailableForSite: an allowlist, not a blind key dispatch on an
	// arbitrary page.
	ActionArrowUp   = "arrow_up"
	ActionArrowDown = "arrow_down"
	ActionHintEnter = "hint_enter"
	ActionHintExit  = "hint_exit"
	ActionHintLabel = "hint_label"
	// ActionCapabilities is an internal, read-only page probe requested when
	// the phone opens Website "More". The shell returns only IDs and Go filters
	// them through MoreActionsForSite before exposing them to the phone.
	ActionCapabilities = "capabilities"
	// Site-profile actions remain typed public commands. Never expose a raw key
	// or script field to phone clients.
	ActionDanmakuToggle        = "danmaku_toggle"
	ActionBilibiliLike         = "bilibili_like"
	ActionBilibiliCoin         = "bilibili_coin"
	ActionBilibiliFav          = "bilibili_favorite"
	ActionBilibiliFollow       = "bilibili_follow"
	ActionBilibiliTriple       = "bilibili_triple"
	ActionIQIYIPrevious        = "iqiyi_previous_episode"
	ActionIQIYINext            = "iqiyi_next_episode"
	ActionIQIYIReplay          = "iqiyi_replay"
	ActionDouyinDanmakuToggle  = "douyin_danmaku_toggle"
	ActionDouyinLike           = "douyin_like"
	ActionDouyinFavorite       = "douyin_favorite"
	ActionDouyinFollow         = "douyin_follow"
	ActionDouyinComments       = "douyin_comments"
	ActionDouyinAutoplay       = "douyin_autoplay"
	ActionDouyinWatchLater     = "douyin_watch_later"
	ActionYouTubeCaptions      = "youtube_captions_toggle"
	ActionYouTubePreviousVideo = "youtube_previous_video"
	ActionYouTubeNextVideo     = "youtube_next_video"
	ActionNetflixSkipIntro     = "netflix_skip_intro"
)

// Speed modes. Playback speed is not a universal capability: it is offered only
// where we have verified a way to change it that respects the site's own player.
//
//	SpeedModeRate — write playbackRate directly (fixed 1×/1.25×/1.5×/2× buttons).
//	SpeedModeStep — no absolute set; press the site's own faster/slower shortcut
//	                and let it walk its own speed ladder.
//	SpeedModeNone — no speed control at all, and the phone hides the section.
//
// Deliberately an allowlist rather than a generic playbackRate write on every
// site: on players that serve advertising through the same <video> element as
// the programme, a blind rate write also accelerates the advert and bypasses the
// service's own eligibility checks (iQIYI is exactly that case), and sites with
// their own rate governor snap it back moments later.
const (
	SpeedModeNone = ""
	SpeedModeRate = "rate"
	SpeedModeStep = "step"
)

// SpeedModeForSite returns how the recognized site exposes playback speed.
// Unknown/custom hosts get SpeedModeNone.
func SpeedModeForSite(siteID string) string {
	switch strings.TrimSpace(siteID) {
	case SiteBilibili, SiteTencent:
		return SpeedModeRate
	case SiteYouTube:
		// YouTube has documented Shift+> / Shift+< shortcuts that move through
		// its own rate menu, which is a better fit than fighting its player for
		// an absolute value.
		return SpeedModeStep
	default:
		return SpeedModeNone
	}
}

// SearchAvailableForSite is deliberately an allowlist: the controller only
// navigates to a verified site-owned search URL and never guesses a form field
// on an arbitrary page.
func SearchAvailableForSite(siteID string) bool {
	switch strings.TrimSpace(siteID) {
	case SiteBilibili, SiteIQIYI, SiteTencent, SiteYouku, SiteYouTube:
		return true
	default:
		return false
	}
}

// ArrowNavAvailableForSite is deliberately an allowlist, same reasoning as
// SearchAvailableForSite: ArrowUp/ArrowDown only does something a viewer would
// recognize on a site we've verified reacts to it — Douyin's feed switches to
// the next/previous video, YouTube steps its own volume. Everywhere else the
// phone hides the row rather than dispatch a keypress with no visible effect.
func ArrowNavAvailableForSite(siteID string) bool {
	switch strings.TrimSpace(siteID) {
	case SiteDouyin, SiteYouTube:
		return true
	default:
		return false
	}
}

var siteMoreActions = map[string][]MoreAction{
	SiteBilibili: {
		{ID: ActionDanmakuToggle, Name: "开关弹幕（D）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionBilibiliLike, Name: "点赞（Q）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionBilibiliCoin, Name: "投币（W）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionBilibiliFav, Name: "收藏（E）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionBilibiliFollow, Name: "关注 UP 主（G）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionBilibiliTriple, Name: "一键三连（长按 R）", Strategy: MoreActionStrategyShortcut},
	},
	SiteIQIYI: {
		{ID: ActionIQIYIPrevious, Name: "上一集（Shift+P）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionIQIYINext, Name: "下一集（Shift+N）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionIQIYIReplay, Name: "重播（0）", Strategy: MoreActionStrategyShortcut},
	},
	SiteDouyin: {
		{ID: ActionDouyinDanmakuToggle, Name: "开关弹幕（B）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionDouyinLike, Name: "点赞（Z）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionDouyinFavorite, Name: "收藏（C）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionDouyinFollow, Name: "关注（G）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionDouyinComments, Name: "打开/关闭评论（X）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionDouyinAutoplay, Name: "自动连播开关（K）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionDouyinWatchLater, Name: "稍后再看（L）", Strategy: MoreActionStrategyShortcut},
	},
	SiteYouTube: {
		{ID: ActionYouTubeCaptions, Name: "开关字幕（C）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionYouTubePreviousVideo, Name: "上一个视频（Shift+P）", Strategy: MoreActionStrategyShortcut},
		{ID: ActionYouTubeNextVideo, Name: "下一个视频（Shift+N）", Strategy: MoreActionStrategyShortcut},
	},
	SiteNetflix: {
		{ID: ActionNetflixSkipIntro, Name: "跳过片头（S）", Strategy: MoreActionStrategyShortcut},
	},
}

// MoreActionsForSite returns a defensive copy of the fixed action profile.
func MoreActionsForSite(siteID string) []MoreAction {
	actions := siteMoreActions[strings.TrimSpace(siteID)]
	return append([]MoreAction(nil), actions...)
}

// FilterMoreActions accepts only IDs declared by the recognized current site,
// preserves profile order, and removes duplicates/unknown page claims.
func FilterMoreActions(siteID string, reported []string) []MoreAction {
	if len(reported) == 0 {
		return []MoreAction{}
	}
	wanted := make(map[string]bool, len(reported))
	for _, id := range reported {
		wanted[strings.TrimSpace(id)] = true
	}
	profile := MoreActionsForSite(siteID)
	out := make([]MoreAction, 0, len(profile))
	for _, action := range profile {
		if wanted[action.ID] {
			out = append(out, action)
		}
	}
	return out
}

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
		ActionEnter, ActionSeek, ActionSpeed, ActionSpeedUp, ActionSpeedDown,
		ActionScrollUp, ActionScrollDown, ActionArrowUp, ActionArrowDown,
		ActionHintEnter, ActionHintExit, ActionHintLabel, ActionCapabilities,
		ActionDanmakuToggle, ActionBilibiliLike, ActionBilibiliCoin, ActionBilibiliFav,
		ActionBilibiliFollow, ActionBilibiliTriple, ActionIQIYIPrevious, ActionIQIYINext,
		ActionIQIYIReplay, ActionDouyinDanmakuToggle,
		ActionDouyinLike, ActionDouyinFavorite, ActionDouyinFollow, ActionDouyinComments,
		ActionDouyinAutoplay, ActionDouyinWatchLater,
		ActionYouTubeCaptions, ActionYouTubePreviousVideo, ActionYouTubeNextVideo,
		ActionNetflixSkipIntro:
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
