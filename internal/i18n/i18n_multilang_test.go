package i18n

import "testing"

func TestMultiLangCoverage(t *testing.T) {
	langs := []string{EN, ZH, "zh-TW", "ja", "ko", "es", "fr", "de"}
	base := messages[EN]
	if len(base) == 0 {
		t.Fatal("empty EN")
	}
	for _, lang := range langs {
		m := messages[lang]
		if len(m) != len(base) {
			t.Fatalf("%s: got %d keys want %d", lang, len(m), len(base))
		}
		for k, enVal := range base {
			v := m[k]
			if v == "" {
				t.Fatalf("%s missing/empty %s", lang, k)
			}
			// format verbs preserved
			for _, tok := range []string{"%s", "%d", "%v"} {
				if contains(enVal, tok) && !contains(v, tok) {
					t.Fatalf("%s %s lost %s: %q", lang, k, tok, v)
				}
			}
			_ = T(lang, k)
		}
	}
	// known samples not English for non-en (settings label)
	if T("ja", "settings") == T(EN, "settings") {
		t.Fatalf("ja settings unexpectedly English: %s", T("ja", "settings"))
	}
	if T("zh-TW", "settings") == "" {
		t.Fatal("zh-TW empty")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
