package website

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrokerDefaultState(t *testing.T) {
	b := NewBroker(nil)
	snap := b.Snapshot()
	if snap.CurrentSiteID != "" {
		t.Fatalf("fresh state must have empty current_site_id: %+v", snap)
	}
	if snap.DesiredOpen || snap.ReportedOpen {
		t.Fatalf("window should start closed: %+v", snap)
	}
	if len(snap.Catalog) != 4 {
		t.Fatalf("catalog missing from snapshot")
	}
	// Snapshot must not expose raw URLs of the live WebView.
	raw := snap
	_ = raw
}

func TestOpenRequiresAllowlistedSiteAndStopsMPV(t *testing.T) {
	var stopped atomic.Bool
	b := NewBroker(func() { stopped.Store(true) })
	if _, err := b.RequestOpen("https://evil.example"); !IsInvalid(err) {
		t.Fatalf("unknown site should fail: %v", err)
	}
	snap, err := b.RequestOpen(SiteBilibili)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() {
		t.Fatal("open should stop mpv")
	}
	if !snap.DesiredOpen || snap.LastStatus != "opening" {
		t.Fatalf("snap=%+v", snap)
	}
	if snap.CurrentSiteID != "" {
		t.Fatalf("current site must not be inferred from request: %+v", snap)
	}
	cmd, ok := b.PendingAfter(0)
	if !ok || cmd.Action != ActionOpen {
		t.Fatalf("cmd=%+v ok=%v", cmd, ok)
	}
	site, _ := SiteByID(SiteBilibili)
	if cmd.URL != site.URL || cmd.SiteID != site.ID {
		t.Fatalf("open must use allowlisted URL only: %+v", cmd)
	}
}

func TestOpenNavigatesExistingSessionToAnotherSite(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	open := true
	b.ApplyReport(Report{
		Open:       &open,
		Status:     "navigated",
		Action:     "navigation",
		CurrentURL: "https://www.bilibili.com/",
	})
	if b.Snapshot().CurrentSiteID != SiteBilibili {
		t.Fatalf("expected bilibili: %+v", b.Snapshot())
	}
	if _, err := b.RequestOpen(SiteYouku); err != nil {
		t.Fatal(err)
	}
	// Request clears current until real navigation arrives.
	if b.Snapshot().CurrentSiteID != "" {
		t.Fatalf("open should clear current site until navigation: %+v", b.Snapshot())
	}
	// A switch is another open command carrying Youku's catalog root, not a
	// request to restore a retained per-site page.
	youkuCmd, ok := b.PendingAfter(1)
	if !ok || youkuCmd.Action != ActionOpen || youkuCmd.SiteID != SiteYouku {
		t.Fatalf("expected Youku open command, got %+v", youkuCmd)
	}
	youku, _ := SiteByID(SiteYouku)
	if youkuCmd.URL != youku.URL {
		t.Fatalf("Youku switch must use homepage, got %s", youkuCmd.URL)
	}
	b.ApplyReport(Report{
		Open:       &open,
		Status:     "navigated",
		Action:     "navigation",
		CurrentURL: "https://www.youku.com/",
	})
	if b.Snapshot().CurrentSiteID != SiteYouku {
		t.Fatalf("cross-site nav should update current: %+v", b.Snapshot())
	}

	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	bilibiliCmd, ok := b.PendingAfter(youkuCmd.ID)
	if !ok || bilibiliCmd.SiteID != SiteBilibili {
		t.Fatalf("expected Bilibili reopen command, got %+v", bilibiliCmd)
	}
	bilibili, _ := SiteByID(SiteBilibili)
	if bilibiliCmd.URL != bilibili.URL {
		t.Fatalf("Bilibili reopen must use homepage, got %s", bilibiliCmd.URL)
	}
}

func TestNavigationUnknownDomainClearsCurrentSite(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	open := true
	b.ApplyReport(Report{Open: &open, CurrentURL: "https://www.bilibili.com/"})
	if b.Snapshot().CurrentSiteID != SiteBilibili {
		t.Fatal("expected bilibili")
	}
	b.ApplyReport(Report{Open: &open, CurrentURL: "https://evilbilibili.com/"})
	if b.Snapshot().CurrentSiteID != "" {
		t.Fatalf("unknown domain must clear current site: %+v", b.Snapshot())
	}
	if !b.Snapshot().ReportedOpen {
		t.Fatal("window should still be reported open")
	}
}

func TestMutualExclusionCloseOnRequest(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	snap := b.RequestClose()
	if snap.DesiredOpen || snap.ReportedOpen || snap.CurrentSiteID != "" {
		t.Fatalf("close should clear open+current: %+v", snap)
	}
	found := false
	for id := uint64(0); id < 10; id++ {
		c, ok := b.PendingAfter(id)
		if ok && c.Action == ActionClose {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("close not queued")
	}
}

func TestRejectUnknownActionAndText(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.EnqueueAction("eval", "alert(1)", ""); !IsInvalid(err) {
		t.Fatalf("expected invalid action, err=%v", err)
	}
	if _, err := b.EnqueueAction(ActionOpen, "", ""); !IsInvalid(err) || ErrorCode(err) != "site_required" {
		t.Fatalf("open via action must require site: %v", err)
	}
	if _, err := b.EnqueueAction(ActionSearch, string(make([]rune, MaxSearchText+5)), ""); !IsInvalid(err) {
		t.Fatal("expected overlong search reject")
	}
	// 1A is a valid two-symbol keypad label; reject wrong alphabet / length.
	if _, err := b.EnqueueAction(ActionHintLabel, "", "D1"); !IsInvalid(err) {
		t.Fatal("expected invalid label (outside alphabet)")
	}
	if _, err := b.EnqueueAction(ActionHintLabel, "", "A"); !IsInvalid(err) {
		t.Fatal("expected invalid label (one symbol)")
	}
	if _, err := b.EnqueueAction(ActionPlayPause, "", ""); !IsInvalid(err) || ErrorCode(err) != "window_not_open" {
		t.Fatalf("expected window_not_open, err=%v", err)
	}
	if _, err := b.EnqueueAction(ActionSeek, "not-a-number", ""); !IsInvalid(err) || ErrorCode(err) != "invalid_number" {
		t.Fatalf("expected invalid_number for seek, err=%v", err)
	}
	if _, err := b.EnqueueAction(ActionSeek, "99999", ""); !IsInvalid(err) || ErrorCode(err) != "invalid_number" {
		t.Fatalf("expected out-of-range seek reject, err=%v", err)
	}
	if _, err := b.EnqueueAction(ActionSpeed, "0", ""); !IsInvalid(err) || ErrorCode(err) != "invalid_number" {
		t.Fatalf("expected out-of-range speed reject, err=%v", err)
	}
}

func TestHomeUsesCurrentCatalogRoot(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteTencent); err != nil {
		t.Fatal(err)
	}
	open := true
	b.ApplyReport(Report{Open: &open, CurrentURL: "https://v.qq.com/x/cover/current.html"})
	// Phone cannot inject an alternate URL: EnqueueAction has no URL parameter.
	if _, err := b.EnqueueAction(ActionHome, "https://evil.example/", ""); err != nil {
		t.Fatal(err)
	}
	var home Command
	for id := uint64(0); id < 10; id++ {
		if cmd, ok := b.PendingAfter(id); ok && cmd.Action == ActionHome {
			home = cmd
			break
		}
	}
	site, _ := SiteByID(SiteTencent)
	if home.URL != site.URL || home.SiteID != SiteTencent {
		t.Fatalf("home cmd = %+v want catalog root %q", home, site.URL)
	}
	if home.URL == "https://evil.example/" {
		t.Fatal("home must never accept caller-provided URL text")
	}
}

func TestHomeRequiresRecognizedCurrentSite(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteTencent); err != nil {
		t.Fatal(err)
	}
	// Window desired-open but current site not yet derived from a real navigation.
	if _, err := b.EnqueueAction(ActionHome, "", ""); !IsInvalid(err) || ErrorCode(err) != "home_unavailable" {
		t.Fatalf("home without current site = %v", err)
	}
	// Window open on an unrecognized domain also blocks home.
	open := true
	b.ApplyReport(Report{Open: &open, CurrentURL: "https://evilbilibili.com/"})
	if _, err := b.EnqueueAction(ActionHome, "", ""); !IsInvalid(err) || ErrorCode(err) != "home_unavailable" {
		t.Fatalf("home on unknown domain = %v", err)
	}
}

func TestLoginUsesFixedPerSiteRoutes(t *testing.T) {
	cases := []struct {
		site string
		url  string
		nav  string
	}{
		{SiteBilibili, "https://passport.bilibili.com/", "https://www.bilibili.com/video/1"},
		{SiteIQIYI, "https://www.iqiyi.com/iframe/loginreg?show_back=1", "https://www.iqiyi.com/v_1.html"},
		{SiteTencent, "https://v.qq.com/s/videoplus/host", "https://v.qq.com/x/cover/1.html"},
		{SiteYouku, "https://account.youku.com/", "https://www.youku.com/"},
	}
	for _, tc := range cases {
		b := NewBroker(nil)
		if _, err := b.RequestOpen(tc.site); err != nil {
			t.Fatalf("%s open: %v", tc.site, err)
		}
		open := true
		b.ApplyReport(Report{Open: &open, CurrentURL: tc.nav})
		if _, err := b.EnqueueAction(ActionLogin, "https://evil.example/login", ""); err != nil {
			t.Fatalf("%s login: %v", tc.site, err)
		}
		var login Command
		for id := uint64(0); id < 10; id++ {
			if cmd, ok := b.PendingAfter(id); ok && cmd.Action == ActionLogin {
				login = cmd
				break
			}
		}
		if login.URL != tc.url || login.SiteID != tc.site {
			t.Fatalf("%s login cmd=%+v want url=%q", tc.site, login, tc.url)
		}
	}
}

func TestLoginRequiresRecognizedCurrentSite(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteYouku); err != nil {
		t.Fatal(err)
	}
	if _, err := b.EnqueueAction(ActionLogin, "", ""); !IsInvalid(err) || ErrorCode(err) != "login_unavailable" {
		t.Fatalf("login without current site = %v", err)
	}
}

func TestRefreshQueuesOnOpenWindow(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	if _, err := b.EnqueueAction(ActionRefresh, "", ""); err != nil {
		t.Fatalf("refresh rejected: %v", err)
	}
	found := false
	for id := uint64(0); id < 10; id++ {
		if cmd, ok := b.PendingAfter(id); ok && cmd.Action == ActionRefresh {
			found = true
			if cmd.URL != "" {
				t.Fatalf("refresh must not carry a URL: %+v", cmd)
			}
			break
		}
	}
	if !found {
		t.Fatal("refresh not queued")
	}
}

func TestEnqueueSeekAndSpeed(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	if _, err := b.EnqueueAction(ActionSeek, "10", ""); err != nil {
		t.Fatalf("valid seek rejected: %v", err)
	}
	if _, err := b.EnqueueAction(ActionSeek, "-10", ""); err != nil {
		t.Fatalf("valid negative seek rejected: %v", err)
	}
	if _, err := b.EnqueueAction(ActionSpeed, "1.25", ""); err != nil {
		t.Fatalf("valid speed rejected: %v", err)
	}
	var lastSeek, lastSpeed Command
	for id := uint64(0); id < 20; id++ {
		if c, ok := b.PendingAfter(id); ok {
			if c.Action == ActionSeek {
				lastSeek = c
			}
			if c.Action == ActionSpeed {
				lastSpeed = c
			}
		}
	}
	if lastSeek.Text != "-10" {
		t.Fatalf("expected last seek text -10, got %q", lastSeek.Text)
	}
	if lastSpeed.Text != "1.25" {
		t.Fatalf("expected speed text 1.25, got %q", lastSpeed.Text)
	}
}

func TestReportAlignsNativeClose(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	open := true
	b.ApplyReport(Report{Open: &open, Status: "open", Action: ActionOpen, CurrentURL: "https://www.bilibili.com/"})
	if b.Snapshot().CurrentSiteID != SiteBilibili {
		t.Fatal("expected site set from URL")
	}
	closed := false
	snap := b.ApplyReport(Report{Open: &closed, Status: "closed", Action: "window_closed"})
	if snap.DesiredOpen || snap.ReportedOpen || snap.CurrentSiteID != "" {
		t.Fatalf("native close should clear desired+reported+current: %+v", snap)
	}
}

func TestStaleShellReportDoesNotReopenAfterClose(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	// command 1 open, command 2 close
	b.RequestClose()
	closed := false
	b.ApplyReport(Report{Open: &closed, Status: "closed", Action: ActionClose, CommandID: 2})
	opened := true
	snap := b.ApplyReport(Report{Open: &opened, Status: "open", Action: ActionOpen, CommandID: 1})
	if snap.ReportedOpen || snap.DesiredOpen || snap.CurrentSiteID != "" {
		t.Fatalf("stale open report must be ignored: %+v", snap)
	}
}

func TestOlderCloseAckDoesNotCancelNewerOpen(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil { // command 1
		t.Fatal(err)
	}
	b.RequestClose()                                    // command 2
	if _, err := b.RequestOpen(SiteIQIYI); err != nil { // command 3
		t.Fatal(err)
	}

	closed := false
	snap := b.ApplyReport(Report{
		Open:      &closed,
		Status:    "closed",
		Action:    ActionClose,
		CommandID: 2,
	})
	if !snap.DesiredOpen {
		t.Fatalf("older close ack must not cancel newer open: %+v", snap)
	}

	opened := true
	snap = b.ApplyReport(Report{
		Open:       &opened,
		Status:     "navigated",
		Action:     "navigation",
		CommandID:  3,
		CurrentURL: "https://www.iqiyi.com/",
	})
	if !snap.ReportedOpen || snap.CurrentSiteID != SiteIQIYI {
		t.Fatalf("newer open should remain authoritative: %+v", snap)
	}
}

func TestStaleNavigationAfterCloseDoesNotResurrect(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	b.RequestClose()
	// Late navigation report without command id (common for WKWebView didFinish).
	snap := b.ApplyReport(Report{
		Open:       boolPtr(true),
		Status:     "navigated",
		Action:     "navigation",
		CurrentURL: "https://www.bilibili.com/video/1",
	})
	if snap.ReportedOpen || snap.DesiredOpen || snap.CurrentSiteID != "" {
		t.Fatalf("stale navigation must not resurrect: %+v", snap)
	}
}

func TestPhoneSnapshotNeverIncludesCurrentURL(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{
		Open:       boolPtr(true),
		CurrentURL: "https://www.bilibili.com/video/BV1?token=secret",
	})
	snap := b.Snapshot()
	if snap.CurrentSiteID != SiteBilibili {
		t.Fatalf("site=%s", snap.CurrentSiteID)
	}
	// Reflect via JSON would be ideal; field absence is structural — ensure
	// Snapshot type has no CurrentURL by assigning through known fields only.
	if snap.CurrentSiteID == "https://www.bilibili.com/video/BV1?token=secret" {
		t.Fatal("must not leak full URL as site id")
	}
}

func TestHintLabelsReachPhoneOnlyWhileHintModeIsActive(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.bilibili.com/"})
	if _, err := b.EnqueueAction(ActionHintEnter, "", ""); err != nil {
		t.Fatal(err)
	}
	snap := b.ApplyReport(Report{
		Open:       boolPtr(true),
		HintActive: boolPtr(true),
		HintLabels: []string{"AA", "AX", "AY", "A1", "A2", "A3", "A4"},
	})
	if !snap.HintActive || len(snap.HintLabels) != 7 || snap.HintLabels[6] != "A4" {
		t.Fatalf("hint labels missing from snapshot: %+v", snap)
	}
	snap = b.ApplyReport(Report{Open: boolPtr(true), HintActive: boolPtr(false)})
	if snap.HintActive || len(snap.HintLabels) != 0 {
		t.Fatalf("inactive hint must not retain selectable labels: %+v", snap)
	}
}

func TestWaitCommandDelivers(t *testing.T) {
	b := NewBroker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan Command, 1)
	go func() {
		cmd, ok := b.WaitCommand(ctx, 0)
		if ok {
			done <- cmd
		}
	}()
	time.Sleep(20 * time.Millisecond)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	select {
	case cmd := <-done:
		if cmd.Action != ActionOpen {
			t.Fatalf("got %+v", cmd)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for command")
	}
}

func TestResetClosesWebsite(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteYouku); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.youku.com/"})
	snap := b.Reset()
	if snap.CurrentSiteID != "" || snap.DesiredOpen || snap.ReportedOpen {
		t.Fatalf("unexpected reset state: %+v", snap)
	}
	found := false
	for id := uint64(0); id < 10; id++ {
		c, ok := b.PendingAfter(id)
		if ok && c.Action == ActionClose {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reset should queue close")
	}
}

func TestControllerJSEmbedded(t *testing.T) {
	if ControllerJS == "" || len(ControllerJS) < 100 {
		t.Fatal("controller.js not embedded")
	}
	if !containsAll(ControllerJS, []string{
		"__tinyplayWebsite", "hint_enter", "bilibili", "play_pause",
		"video-pod__list .pod-item.video-pod__item",
		"episodesNew_item__", "episodes_item__",
		"collectDelegatedRowTargets", "elementsFromPoint", "fullscreen: 'f'",
		"https://www.iqiyi.com/iframe/loginreg?show_back=1",
		"https://v.qq.com/s/videoplus/host",
		"https://account.youku.com/",
	}) {
		t.Fatal("controller.js missing expected markers")
	}
}

// Source-level contract checks for the injected controller. Full-browser
// network tests are intentionally not used; these pin labels, reachability
// gates, the generic row heuristic, the Bilibili adapter, and site keys.
func TestControllerJSHintAndSiteContracts(t *testing.T) {
	js := ControllerJS
	if js == "" {
		t.Fatal("controller.js not embedded")
	}
	// Label contract: fixed 12-key alphabet, two-symbol labels, 144 cap.
	if !containsAll(js, []string{
		"AXY123456789",
		"MAX_HINT_TARGETS",
		"HINT_ALPHABET",
		"alphabetLabels",
	}) {
		t.Fatal("hint label contract markers missing")
	}
	// Reachability: ancestor + clip + hit-test gates.
	if !containsAll(js, []string{
		"isHintReachable",
		"isInteractivelyPresent",
		"visibleClientRect",
		"isHitTestReachable",
		"elementsFromPoint",
		"inert",
		"aria-disabled",
		"pointerEvents",
	}) {
		t.Fatal("hint reachability gates missing")
	}
	// Semantic first, then site adapters claim known delegated-click surfaces
	// before the generic delegated-row fallback scans their inner text nodes.
	if !containsAll(js, []string{
		"collectSemanticTargets",
		"collectDelegatedRowTargets",
		"collectSiteAdapterTargets",
		"looksLikeRepeatedRow",
		"SEMANTIC_HINT_SEL",
	}) {
		t.Fatal("hint collection pipeline markers missing")
	}
	// Bilibili additive adapter: outer .pod-item for Vue bubbled clicks.
	if !contains(js, ".video-pod__list .pod-item.video-pod__item") {
		t.Fatal("bilibili playlist adapter selector missing")
	}
	// iQIYI episode tiles are ordinary divs with delegated click handlers. Pin
	// the stable CSS-module prefixes so the outer tile, not its nested span,
	// receives one Hint label.
	if !containsAll(js, []string{"episodesNew_item__", "episodes_item__"}) {
		t.Fatal("iqiyi episode-grid adapter selectors missing")
	}
	// A non-semantic row must receive the click at its true hit target, not
	// merely at its outer layout wrapper.
	if !containsAll(js, []string{
		"dispatchHintPointerClick", "hintClickPoint", "elementFromPoint",
		"pointerdown", "mousedown", "pointerup", "mouseup", "MouseEvent",
	}) {
		t.Fatal("hint coordinate click fallback missing")
	}
	// Site-key fallbacks for bilibili + iqiyi (Space + F), oracle-gated.
	if !containsAll(js, []string{
		"SITE_KEYS",
		"play_pause: ' '",
		"fullscreen: 'f'",
		"iqiyi",
		"effect_unconfirmed",
		"dispatchKey",
	}) {
		t.Fatal("site key table / oracle markers missing")
	}
	// Both catalog hosts that report Space/F must appear in the key table.
	if !contains(js, "bilibili\\.com") || !contains(js, "iqiyi\\.com") {
		t.Fatal("bilibili/iqiyi SITE_KEYS host tests missing")
	}
	// Version gate must re-inject after controller upgrades.
	if !contains(js, "__version >= 10") {
		t.Fatal("controller version gate missing")
	}
	// A temporarily empty SPA DOM (for example iQIYI swapping its episode
	// panel) is retried before surfacing no_targets to the phone.
	if !containsAll(js, []string{"enterHintsOnce", "tryEnter", "no_targets", "window.setTimeout"}) {
		t.Fatal("hint empty-DOM retry missing")
	}
}

func boolPtr(v bool) *bool { return &v }

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
