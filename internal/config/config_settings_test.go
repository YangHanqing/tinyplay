package config

import (
	"testing"
	"time"
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
