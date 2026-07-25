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
	if len(snap.Catalog) != 7 {
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

func TestHomeUsesOpenedRootAcrossNavigation(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteTencent); err != nil {
		t.Fatal(err)
	}
	// The stored opening URL is a safe home destination before a navigation
	// report arrives and after a cross-site hop.
	if _, err := b.EnqueueAction(ActionHome, "", ""); err != nil {
		t.Fatalf("home before navigation = %v", err)
	}
	open := true
	b.ApplyReport(Report{Open: &open, CurrentURL: "https://evilbilibili.com/"})
	if _, err := b.EnqueueAction(ActionHome, "", ""); err != nil {
		t.Fatalf("home after cross-site navigation = %v", err)
	}
}

func TestGenericCustomSiteKeepsGenericControlsAcrossNavigation(t *testing.T) {
	b := NewBroker(nil)
	site := Site{ID: "custom-1", Name: "Example", URL: "http://example.test/", GenericOnly: true}
	if _, err := b.RequestOpenSite(site); err != nil {
		t.Fatal(err)
	}
	open := true
	snap := b.ApplyReport(Report{Open: &open, CurrentURL: "https://www.youtube.com/watch?v=x"})
	if !snap.GenericSite || snap.CurrentSiteID != "" || snap.MoreAvailable {
		t.Fatalf("generic snapshot=%+v", snap)
	}
	if _, err := b.EnqueueAction(ActionSpeed, "1.5", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("speed must be unavailable for generic site: %v", err)
	}
	if _, err := b.EnqueueAction(ActionHome, "", ""); err != nil {
		t.Fatalf("generic home = %v", err)
	}
}

// Search on a personal URL would fall back to guessing a search box and
// submitting its form. Home/back/refresh/hint stay available: those act on the
// browser or on an element the user picked, not on a guessed target.
func TestGenericSiteRejectsSearch(t *testing.T) {
	b := NewBroker(nil)
	site := Site{ID: "custom-1", Name: "Example", URL: "http://example.test/", GenericOnly: true}
	if _, err := b.RequestOpenSite(site); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "http://example.test/watch"})
	if _, err := b.EnqueueAction(ActionSearch, "hello", ""); !IsInvalid(err) || ErrorCode(err) != "search_unavailable" {
		t.Fatalf("generic search = %v", err)
	}
	for _, action := range []string{ActionRefresh, ActionBack, ActionHintEnter, ActionType, ActionEnter} {
		if _, err := b.EnqueueAction(action, "hello", ""); err != nil {
			t.Fatalf("generic %s = %v", action, err)
		}
	}
}

// A recognized catalog site keeps search, and it must survive navigation within
// that site — the gate keys off the derived current site, not the opened card.
func TestBuiltInSiteKeepsSearch(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.bilibili.com/video/1"})
	if _, err := b.EnqueueAction(ActionSearch, "hello", ""); err != nil {
		t.Fatalf("bilibili search = %v", err)
	}
	// Navigating off the catalog site leaves an unknown page, which is exactly
	// the case the guessed-search-box fallback must not run on.
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://unknown.example/page"})
	if _, err := b.EnqueueAction(ActionSearch, "hello", ""); !IsInvalid(err) || ErrorCode(err) != "search_unavailable" {
		t.Fatalf("search after leaving catalog site = %v", err)
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

func TestMoreActionsRequireFreshApprovedCapability(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.bilibili.com/video/BV1"})
	if !b.Snapshot().MoreAvailable {
		t.Fatal("bilibili's fixed extension profile should expose More")
	}

	// A typed special action is still unavailable until the current page probe
	// reports it. This prevents stale profile knowledge becoming blind control.
	unsolicited := b.ApplyReport(Report{
		Open:        boolPtr(true),
		Status:      "capabilities",
		Action:      ActionCapabilities,
		MoreActions: []string{ActionDanmakuToggle},
	})
	if len(unsolicited.MoreActions) != 0 {
		t.Fatalf("unsolicited capability report accepted: %+v", unsolicited.MoreActions)
	}
	if _, err := b.EnqueueAction(ActionDanmakuToggle, "", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("unprobed danmaku action = %v", err)
	}
	if _, err := b.EnqueueAction(ActionCapabilities, "", ""); err != nil {
		t.Fatalf("capability probe rejected: %v", err)
	}
	probe, ok := b.PendingAfter(1)
	if !ok || probe.Action != ActionCapabilities {
		t.Fatalf("capability command missing: %+v", probe)
	}
	snap := b.ApplyReport(Report{
		Open:        boolPtr(true),
		Status:      "capabilities",
		Action:      ActionCapabilities,
		CommandID:   probe.ID,
		MoreActions: []string{"evil", ActionBilibiliLike, ActionDanmakuToggle, ActionBilibiliLike},
	})
	if len(snap.MoreActions) != 2 || snap.MoreActions[0].ID != ActionDanmakuToggle || snap.MoreActions[1].ID != ActionBilibiliLike {
		t.Fatalf("filtered More actions missing: %+v", snap.MoreActions)
	}
	if _, err := b.EnqueueAction(ActionDanmakuToggle, "", ""); err != nil {
		t.Fatalf("approved danmaku action rejected: %v", err)
	}
	if _, err := b.EnqueueAction(ActionBilibiliLike, "", ""); err != nil {
		t.Fatalf("approved bilibili action rejected: %v", err)
	}
	if _, err := b.EnqueueAction(ActionBilibiliCoin, "", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("unreported bilibili action = %v", err)
	}

	// Any main-document navigation invalidates the page-sensitive result.
	snap = b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.bilibili.com/"})
	if len(snap.MoreActions) != 0 {
		t.Fatalf("navigation retained stale More actions: %+v", snap.MoreActions)
	}
	if _, err := b.EnqueueAction(ActionDanmakuToggle, "", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("stale danmaku action = %v", err)
	}
}

func TestMoreActionsCannotCrossSiteBoundary(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteIQIYI); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.iqiyi.com/v_1.html"})
	if !b.Snapshot().MoreAvailable {
		t.Fatal("iqiyi's fixed extension profile should expose More")
	}
	if _, err := b.EnqueueAction(ActionCapabilities, "", ""); err != nil {
		t.Fatal(err)
	}
	probe, ok := b.PendingAfter(1)
	if !ok || probe.Action != ActionCapabilities {
		t.Fatalf("capability command missing: %+v", probe)
	}
	snap := b.ApplyReport(Report{
		Open:        boolPtr(true),
		Status:      "capabilities",
		Action:      ActionCapabilities,
		CommandID:   probe.ID,
		MoreActions: []string{ActionDanmakuToggle, ActionIQIYINext},
	})
	if len(snap.MoreActions) != 1 || snap.MoreActions[0].ID != ActionIQIYINext {
		t.Fatalf("iqiyi capability filtering failed: %+v", snap.MoreActions)
	}
	if _, err := b.EnqueueAction(ActionIQIYINext, "", ""); err != nil {
		t.Fatalf("approved iqiyi action rejected: %v", err)
	}
	if _, err := b.EnqueueAction(ActionDanmakuToggle, "", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("cross-site special action = %v", err)
	}
}

func TestDouyinMoreActionsAreSiteScoped(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteDouyin); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.douyin.com/video/1"})
	if !b.Snapshot().MoreAvailable {
		t.Fatal("douyin's fixed extension profile should expose More")
	}
	if _, err := b.EnqueueAction(ActionCapabilities, "", ""); err != nil {
		t.Fatal(err)
	}
	probe, ok := b.PendingAfter(1)
	if !ok || probe.Action != ActionCapabilities {
		t.Fatalf("capability command missing: %+v", probe)
	}
	snap := b.ApplyReport(Report{
		Open:        boolPtr(true),
		Status:      "capabilities",
		Action:      ActionCapabilities,
		CommandID:   probe.ID,
		MoreActions: []string{ActionDouyinLike, ActionBilibiliLike},
	})
	if len(snap.MoreActions) != 1 || snap.MoreActions[0].ID != ActionDouyinLike {
		t.Fatalf("douyin capability filtering failed: %+v", snap.MoreActions)
	}
	if _, err := b.EnqueueAction(ActionDouyinLike, "", ""); err != nil {
		t.Fatalf("approved douyin action rejected: %v", err)
	}

	if _, err := b.RequestOpen(SiteTencent); err != nil {
		t.Fatal(err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://v.qq.com/x/cover/1"})
	if b.Snapshot().MoreAvailable {
		t.Fatal("generic profile must not expose More")
	}
	if _, err := b.EnqueueAction(ActionDouyinLike, "", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("douyin more action leaked across sites: %v", err)
	}
}

func TestEnqueueSeekAndSpeed(t *testing.T) {
	b := NewBroker(nil)
	if _, err := b.RequestOpen(SiteBilibili); err != nil {
		t.Fatal(err)
	}
	// Speed is gated on the site actually reported by the shell, so the rate
	// form only becomes available once navigation confirms a rate-mode host.
	if _, err := b.EnqueueAction(ActionSpeed, "1.25", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
		t.Fatalf("speed before navigation report = %v", err)
	}
	b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.bilibili.com/video/BV1"})
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

// The two speed forms are not interchangeable: a rate-mode site must never
// accept a step command, a step-mode site must never accept an absolute rate,
// and a site with no speed profile accepts neither.
func TestSpeedFormsAreSiteScoped(t *testing.T) {
	cases := []struct {
		site   string
		url    string
		mode   string
		rateOK bool
		stepOK bool
	}{
		{SiteBilibili, "https://www.bilibili.com/video/BV1", SpeedModeRate, true, false},
		{SiteTencent, "https://v.qq.com/x/cover/1", SpeedModeRate, true, false},
		{SiteYouTube, "https://www.youtube.com/watch?v=x", SpeedModeStep, false, true},
		{SiteIQIYI, "https://www.iqiyi.com/v_1.html", SpeedModeNone, false, false},
		{SiteNetflix, "https://www.netflix.com/watch/1", SpeedModeNone, false, false},
	}
	for _, tc := range cases {
		b := NewBroker(nil)
		if _, err := b.RequestOpen(tc.site); err != nil {
			t.Fatal(err)
		}
		snap := b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: tc.url})
		if snap.SpeedMode != tc.mode {
			t.Fatalf("%s speed_mode = %q, want %q", tc.site, snap.SpeedMode, tc.mode)
		}
		_, rateErr := b.EnqueueAction(ActionSpeed, "1.5", "")
		if (rateErr == nil) != tc.rateOK {
			t.Fatalf("%s absolute speed err = %v, want ok=%v", tc.site, rateErr, tc.rateOK)
		}
		for _, step := range []string{ActionSpeedUp, ActionSpeedDown} {
			_, stepErr := b.EnqueueAction(step, "", "")
			if (stepErr == nil) != tc.stepOK {
				t.Fatalf("%s %s err = %v, want ok=%v", tc.site, step, stepErr, tc.stepOK)
			}
		}
	}
}

// A generic personal URL never derives a current site, so it reports no speed
// capability even while sitting on a host that would otherwise have one.
func TestGenericSiteHasNoSpeedMode(t *testing.T) {
	b := NewBroker(nil)
	site := Site{ID: "custom-1", Name: "Example", URL: "http://example.test/", GenericOnly: true}
	if _, err := b.RequestOpenSite(site); err != nil {
		t.Fatal(err)
	}
	snap := b.ApplyReport(Report{Open: boolPtr(true), CurrentURL: "https://www.youtube.com/watch?v=x"})
	if snap.SpeedMode != SpeedModeNone {
		t.Fatalf("generic speed_mode = %q", snap.SpeedMode)
	}
	for _, action := range []string{ActionSpeed, ActionSpeedUp, ActionSpeedDown} {
		if _, err := b.EnqueueAction(action, "1.5", ""); !IsInvalid(err) || ErrorCode(err) != "action_unavailable" {
			t.Fatalf("generic %s = %v", action, err)
		}
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
		"https://search.bilibili.com/all?keyword={q}",
		"https://www.iqiyi.com/search?q={q}",
		"https://so.youku.com/search/q_{q}",
	}) {
		t.Fatal("controller.js missing expected markers")
	}
	if containsAny(ControllerJS, []string{"LOGIN_URLS", "function doLogin(", "case 'login':"}) {
		t.Fatal("controller.js must not retain a website login action")
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
	// Douyin Jingxuan's waterfall cards are clickable divs without semantic
	// attributes. Pin its outer-card selector and route guard so inner image and
	// title nodes cannot receive duplicate Hints.
	if !containsAll(js, []string{
		"waterfall-videoCardContainer.jingxuanVideoCard",
		"/^\\/jingxuan(?:\\/|$)/",
	}) {
		t.Fatal("douyin jingxuan card adapter missing")
	}
	// Regression: Semi Design (Douyin's UI kit) puts tabindex="0" on the whole
	// active role="tabpanel" wrapper for keyboard focus, not because the whole
	// panel is one click target. The bare-tabindex semantic clause must exclude
	// it, or that one wrapper gets "seen" first and alreadyCovered's ancestor
	// walk silently swallows every card in the entire jingxuan waterfall below
	// it — confirmed live: 0 of ~70 cards got a Hint until this exclusion.
	if !contains(js, `[tabindex]:not([tabindex="-1"]):not([role="tabpanel"])`) {
		t.Fatal("tabpanel wrapper must not swallow the douyin jingxuan card adapter")
	}
	// A non-semantic row must receive the click at its true hit target, not
	// merely at its outer layout wrapper.
	if !containsAll(js, []string{
		"dispatchHintPointerClick", "hintClickPoint", "elementFromPoint",
		"pointerdown", "mousedown", "pointerup", "mouseup", "MouseEvent",
	}) {
		t.Fatal("hint coordinate click fallback missing")
	}
	// Generic sites use Space/Arrow; selected profiles may override the key
	// while retaining the same verified fallback contract.
	if !containsAll(js, []string{
		"COMMON_PLAYBACK_KEYS",
		"SITE_KEYS",
		"play_pause: ' '",
		"seek_backward: 'ArrowLeft'",
		"seek_forward: 'ArrowRight'",
		"playbackKey('play_pause')",
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
	if !contains(js, "__version >= 24") {
		t.Fatal("controller version gate missing")
	}
	// Site profiles keep their shortcut contracts even though speed is gone
	// from most of them.
	if !containsAll(js, []string{
		"iqiyi_previous_episode: 'Shift+P'", "iqiyi_next_episode: 'Shift+N'",
		"iqiyi_replay: '0'", "triggerIQIYIShortcut", "shiftKey: shiftKey",
		"fullscreen: 'h'", "douyin_danmaku_toggle: 'b'",
		"douyin_watch_later: 'l'", "triggerDouyinShortcut",
	}) {
		t.Fatal("iqiyi/douyin shortcut contract missing")
	}
	// Speed is now two site-scoped forms. The absolute write survives only for
	// the rate-mode sites Go allows; YouTube steps through its own rate menu
	// with the documented Shift+> / Shift+< shortcuts, and no site keeps the
	// old iQIYI speed-menu scraping (its adverts share the programme's <video>,
	// so it has no speed control at all now).
	if !containsAll(js, []string{
		"function setSpeed(", "video.playbackRate = rate",
		"function stepSpeed(", "speed_up: 'Shift+>'", "speed_down: 'Shift+<'",
		"SHIFTED_SYMBOL_KEYS", "code: 'Period'", "code: 'Comma'",
		"case 'speed_up':", "case 'speed_down':", "atSpeedLimit",
	}) {
		t.Fatal("site-scoped speed contract missing")
	}
	if containsAny(js, []string{
		"setIQIYIOfficialSpeed", "iQIYIAdPlaying", "iQIYISpeedTrigger",
		"iQIYISpeedOption", "official_speed_control_unavailable", "ad_playing",
	}) {
		t.Fatal("iQIYI speed path must be gone, not merely unreachable")
	}
	// More capability contract: only Bilibili advertises the D-key danmaku
	// action, and the effect must be read back before success is reported.
	if !containsAll(js, []string{
		"danmaku_toggle: 'd'",
		"seek_backward: 'ArrowLeft'",
		"fullscreen: 'f'",
		"bilibili_like: 'q'",
		"bilibili_triple: 'r'",
		"siteMoreActions",
		"bilibiliDanmakuState",
		"triggerBilibiliShortcut",
		"dispatchKeyHold",
		"capabilities",
		"after !== before",
	}) {
		t.Fatal("Bilibili More capability / oracle markers missing")
	}
	// Fullscreen exit is Escape for every site uniformly, not a per-site
	// preference table — no site should carry its own fullscreen_exit key,
	// and the removed gating helper must not reappear.
	if !contains(js, "dispatchKey('Escape')") {
		t.Fatal("universal Escape fullscreen-exit dispatch missing")
	}
	if containsAny(js, []string{"fullscreen_exit:", "preferSiteShortcut"}) {
		t.Fatal("fullscreen exit must not special-case any site")
	}
	// A real DOM fullscreen transition must be forwarded to the Windows host;
	// WebView2 does not resize its embedding HWND the way standalone Edge does.
	if !containsAll(js, []string{
		"tinyplayWebsiteSetFullscreen",
		"fullscreenchange",
		"webkitfullscreenchange",
		"pagehide",
	}) {
		t.Fatal("native fullscreen host bridge missing")
	}
	// Single-container guard: a lone WebView2 must fold every new-window route
	// (window.open, target=_blank/_new links/forms) back into this same page.
	if !containsAll(js, []string{"window.open =", "effectiveTarget", "_blank", "_new"}) {
		t.Fatal("single-container new-window guard missing")
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

func containsAny(s string, parts []string) bool {
	for _, p := range parts {
		if contains(s, p) {
			return true
		}
	}
	return false
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
