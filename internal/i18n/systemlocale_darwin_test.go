//go:build darwin

package i18n

import "testing"

// `defaults read -g AppleLanguages` prints a plist array whose entries are
// quoted only when they need to be, so the parser has to survive both spellings
// and take the first entry — the rest are the user's fallback order.
func TestFirstAppleLanguage(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"single quoted", "(\n    \"zh-Hans-CN\"\n)\n", "zh-Hans-CN"},
		{"unquoted", "(\n    en\n)\n", "en"},
		{"first of many wins", "(\n    \"zh-Hans-CN\",\n    en-US,\n    ja\n)\n", "zh-Hans-CN"},
		{"empty output", "", ""},
		{"empty array", "(\n)\n", ""},
	}
	for _, tc := range cases {
		if got := firstAppleLanguage(tc.out); got != tc.want {
			t.Errorf("%s: firstAppleLanguage = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The whole point of reading the preference is that it survives Normalize into
// a language we actually ship — "zh-Hans-CN" is the form macOS reports for a
// Chinese Mac, and it must not land on English.
func TestDarwinLocaleNormalizesToShippedLanguage(t *testing.T) {
	if got := Normalize(firstAppleLanguage("(\n    \"zh-Hans-CN\"\n)\n")); got != ZH {
		t.Fatalf("Normalize(macOS Chinese) = %q, want %q", got, ZH)
	}
}
