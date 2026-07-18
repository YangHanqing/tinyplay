package website

import "testing"

func TestCatalogOrder(t *testing.T) {
	want := []string{SiteBilibili, SiteIQIYI, SiteTencent, SiteYouku}
	if len(Catalog) != len(want) {
		t.Fatalf("catalog len=%d want %d", len(Catalog), len(want))
	}
	for i, id := range want {
		if Catalog[i].ID != id {
			t.Errorf("catalog[%d]=%s want %s", i, Catalog[i].ID, id)
		}
		if Catalog[i].URL == "" || Catalog[i].Name == "" {
			t.Errorf("catalog[%d] missing name/url", i)
		}
		if MatchDomain(Catalog[i]) == "" {
			t.Errorf("catalog[%d] match domain empty", i)
		}
	}
	if _, ok := SiteByID("evil"); ok {
		t.Fatal("unknown site should not resolve")
	}
}

func TestSiteIDFromHostDomainMatching(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"www.bilibili.com", SiteBilibili},
		{"bilibili.com", SiteBilibili},
		{"m.bilibili.com", SiteBilibili},
		{"www.iqiyi.com", SiteIQIYI},
		{"iqiyi.com", SiteIQIYI},
		{"v.qq.com", SiteTencent},
		{"www.v.qq.com", SiteTencent},
		{"www.youku.com", SiteYouku},
		{"www.mgtv.com", ""},
		{"www.whatismybrowser.com", ""},
		// Near-miss / unknown must not match.
		{"evilbilibili.com", ""},
		{"notbilibili.com", ""},
		{"bilibili.com.evil.example", ""},
		{"qq.com", ""},
		{"www.qq.com", ""},
		{"example.com", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := SiteIDFromHost(tc.host); got != tc.want {
			t.Errorf("SiteIDFromHost(%q)=%q want %q", tc.host, got, tc.want)
		}
	}
}

func TestSiteIDFromURL(t *testing.T) {
	if got := SiteIDFromURL("https://www.bilibili.com/video/BV1xx?spm=1"); got != SiteBilibili {
		t.Fatalf("got %q", got)
	}
	if got := SiteIDFromURL("https://www.iqiyi.com/v_19.html"); got != SiteIQIYI {
		t.Fatalf("got %q", got)
	}
	if got := SiteIDFromURL("https://evilbilibili.com/"); got != "" {
		t.Fatalf("evil host must be empty, got %q", got)
	}
	if got := SiteIDFromURL("https://unknown.example/path?token=secret"); got != "" {
		t.Fatalf("unknown must be empty, got %q", got)
	}
}

func TestValidateTextAndLabel(t *testing.T) {
	if _, ok := ValidateText(string(make([]rune, MaxSearchText+1)), MaxSearchText); ok {
		t.Fatal("overlong text accepted")
	}
	if _, ok := ValidateText("hello\x00world", MaxSearchText); ok {
		t.Fatal("control char accepted")
	}
	got, ok := ValidateText("  bilibili  ", MaxSearchText)
	if !ok || got != "bilibili" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	// Exactly two symbols from AXY123456789; letters case-folded.
	lab, ok := ValidateHintLabel("ax")
	if !ok || lab != "AX" {
		t.Fatalf("label=%q ok=%v", lab, ok)
	}
	lab, ok = ValidateHintLabel("a1")
	if !ok || lab != "A1" {
		t.Fatalf("mixed label=%q ok=%v", lab, ok)
	}
	lab, ok = ValidateHintLabel("99")
	if !ok || lab != "99" {
		t.Fatalf("digit label=%q ok=%v", lab, ok)
	}
	// Reject wrong alphabet, length, or one-symbol Vimium leftovers.
	for _, bad := range []string{"", "A", "1", "ABC", "B1", "1B", "A0", "a ", "1a2"} {
		if _, ok := ValidateHintLabel(bad); ok {
			t.Fatalf("invalid label %q accepted", bad)
		}
	}
}

func TestGenerateHintLabels(t *testing.T) {
	if len(HintAlphabet) != 12 {
		t.Fatalf("HintAlphabet len=%d want 12", len(HintAlphabet))
	}
	if MaxHintTargets != 144 {
		t.Fatalf("MaxHintTargets=%d want 144", MaxHintTargets)
	}
	if got := GenerateHintLabels(0); got != nil {
		t.Fatalf("empty count should be nil, got %v", got)
	}
	labels := GenerateHintLabels(3)
	if len(labels) != 3 || labels[0] != "AA" || labels[1] != "AX" || labels[2] != "AY" {
		t.Fatalf("first labels = %v", labels)
	}
	all := GenerateHintLabels(MaxHintTargets + 50)
	if len(all) != MaxHintTargets {
		t.Fatalf("cap len=%d want %d", len(all), MaxHintTargets)
	}
	if all[0] != "AA" || all[len(all)-1] != "99" {
		t.Fatalf("range %q…%q", all[0], all[len(all)-1])
	}
	// No one-symbol labels; every entry is exactly two alphabet symbols.
	seen := make(map[string]bool, len(all))
	for _, lab := range all {
		if len(lab) != HintLabelLen {
			t.Fatalf("label %q wrong length", lab)
		}
		if _, ok := ValidateHintLabel(lab); !ok {
			t.Fatalf("generated label %q fails validation", lab)
		}
		if seen[lab] {
			t.Fatalf("duplicate label %q", lab)
		}
		seen[lab] = true
	}
}

func TestIsKnownAction(t *testing.T) {
	if !IsKnownAction(ActionPlayPause) {
		t.Fatal("play_pause should be known")
	}
	for _, action := range []string{ActionSeek, ActionSpeed, ActionScrollUp, ActionScrollDown, ActionLogin, ActionHome, ActionRefresh, ActionFullscreenEnter, ActionFullscreenExit} {
		if !IsKnownAction(action) {
			t.Fatalf("%s should be known", action)
		}
	}
	if IsKnownAction("eval") || IsKnownAction("shell") || IsKnownAction("navigate") {
		t.Fatal("dangerous actions must not be known")
	}
}

func TestLoginURLFixedRoutes(t *testing.T) {
	want := map[string]string{
		SiteBilibili: "https://passport.bilibili.com/",
		SiteIQIYI:    "https://www.iqiyi.com/iframe/loginreg?show_back=1",
		SiteTencent:  "https://v.qq.com/s/videoplus/host",
		SiteYouku:    "https://account.youku.com/",
	}
	for id, url := range want {
		got, ok := LoginURL(id)
		if !ok || got != url {
			t.Errorf("LoginURL(%s)=%q ok=%v want %q", id, got, ok, url)
		}
	}
	if _, ok := LoginURL("mgtv"); ok {
		t.Fatal("removed catalog sites must not expose login routes")
	}
	if _, ok := LoginURL("uacheck"); ok {
		t.Fatal("removed catalog sites must not expose login routes")
	}
	if _, ok := LoginURL(""); ok {
		t.Fatal("empty site must not expose a login route")
	}
	// Every public catalog entry must have a fixed login route.
	for _, site := range Catalog {
		if _, ok := LoginURL(site.ID); !ok {
			t.Fatalf("catalog site %s missing LoginURL", site.ID)
		}
	}
}

func TestValidateNumber(t *testing.T) {
	if _, ok := ValidateNumber("not-a-number", -10, 10); ok {
		t.Fatal("non-numeric text accepted")
	}
	if _, ok := ValidateNumber("NaN", -10, 10); ok {
		t.Fatal("NaN accepted")
	}
	if _, ok := ValidateNumber("Inf", -10, 10); ok {
		t.Fatal("Inf accepted")
	}
	if _, ok := ValidateNumber("11", 0, 10); ok {
		t.Fatal("out-of-range value accepted")
	}
	got, ok := ValidateNumber("  1.25  ", 0, 4)
	if !ok || got != "1.25" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
