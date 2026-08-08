package config

import (
	"testing"
	"time"

	"tvremote/internal/i18n"
)

func TestNormalizeLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Supported values (canonical)
		{"auto", "auto"},
		{"en", "en"},
		{"zh-CN", "zh-CN"},
		{"zh-TW", "zh-TW"},
		{"ja", "ja"},
		{"ko", "ko"},
		{"es", "es"},
		{"fr", "fr"},
		{"de", "de"},

		// Case-insensitive
		{"EN", "en"},
		{"Zh-Cn", "zh-CN"},
		{"ZH-TW", "zh-TW"},
		{"JA", "ja"},
		{"Ko", "ko"},
		{"ES", "es"},
		{"Fr", "fr"},
		{"DE", "de"},
		{"Auto", "auto"},
		{"  en  ", "en"},

		// Common aliases
		{"zh", "zh-CN"},
		{"ZH", "zh-CN"},
		{"zh-hans", "zh-CN"},
		{"zh-Hans", "zh-CN"},
		{"zh-hant", "zh-TW"},
		{"zh-Hant", "zh-TW"},
		{"ja-jp", "ja"},
		{"ja-JP", "ja"},
		{"ko-kr", "ko"},
		{"ko-KR", "ko"},

		// Underscore variant separators normalize before matching
		{"zh_CN", "zh-CN"},
		{"zh_TW", "zh-TW"},
		{"ja_JP", "ja"},
		{"ko_KR", "ko"},

		// Unknown / empty → auto (backward compatibility)
		{"", "auto"},
		{"   ", "auto"},
		{"pt", "auto"},
		{"it", "auto"},
		{"zh-HK", "auto"},
		{"en-US", "auto"},
		{"not-a-lang", "auto"},
	}

	for _, tc := range cases {
		if got := NormalizeLanguage(tc.in); got != tc.want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUpdatePromptPreferences(t *testing.T) {
	useTempData(t)
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if !ShouldOfferUpdate("v1.0.0", now) {
		t.Fatal("a fresh update should be offered")
	}

	SkipUpdate("v1.0.0")
	if ShouldOfferUpdate("v1.0.0", now) {
		t.Fatal("skipped version should not be offered")
	}
	if !ShouldOfferUpdate("v1.0.1", now) {
		t.Fatal("newer version should not inherit a skip")
	}

	RemindAboutUpdateAfter("v1.0.0", now.Add(72*time.Hour))
	if ShouldOfferUpdate("v1.0.0", now.Add(time.Hour)) {
		t.Fatal("snoozed version should not be offered early")
	}
	if !ShouldOfferUpdate("v1.0.1", now.Add(time.Hour)) {
		t.Fatal("a newer version should not inherit a snooze")
	}
	if !ShouldOfferUpdate("v1.0.0", now.Add(73*time.Hour)) {
		t.Fatal("snoozed version should be offered after the reminder time")
	}
}

// "Automatic" is read on the phone, so it has to mean the phone's language.
// Resolving it against the desktop is what served an English page to a Chinese
// phone from a Chinese Mac. An explicit selection is a deliberate choice and
// must survive whatever the caller's Accept-Language says.
func TestSettingsForResolvesAutoAgainstTheRequester(t *testing.T) {
	useTempData(t)

	SetLanguage("auto")
	if got := SettingsFor("ja")["resolved_language"]; got != "ja" {
		t.Errorf("auto + Japanese requester = %v, want ja", got)
	}
	if got := SettingsFor("zh-Hans-CN")["resolved_language"]; got != "zh-CN" {
		t.Errorf("auto + zh-Hans-CN requester = %v, want zh-CN", got)
	}
	// No request in hand: fall back to this machine, never to a Normalize()
	// default. "" must not be read as English.
	if got := SettingsFor("")["resolved_language"]; got != i18nSystemLangForTest() {
		t.Errorf("auto + no requester = %v, want the desktop locale %v", got, i18nSystemLangForTest())
	}
	// A language we do not ship is not a reason to ignore the desktop either.
	if got := SettingsFor("cy-GB")["resolved_language"]; got != i18nSystemLangForTest() {
		t.Errorf("auto + unshipped language = %v, want the desktop locale", got)
	}

	SetLanguage("en")
	if got := SettingsFor("ja")["resolved_language"]; got != "en" {
		t.Errorf("explicit en + Japanese requester = %v, want en", got)
	}
	if got := SettingsFor("ja")["language"]; got != "en" {
		t.Errorf("explicit en should round-trip as the stored setting, got %v", got)
	}
}

func i18nSystemLangForTest() string { return i18n.SystemLang() }
