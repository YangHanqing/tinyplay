//go:build darwin

package i18n

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// macOS GUI processes are launched by launchd, not a login shell, so LANG and
// friends are simply absent — which is why SystemLang() used to fall through to
// English on every Mac no matter what the system language was. The user-visible
// symptom was a Chinese menu bar (the AppKit shell reads Locale.current itself)
// beside an English phone UI.
//
// `defaults` is the pure-Go-friendly way to reach the same preference: the app
// must keep cross-compiling for the Intel leg on an arm64 runner, so cgo (and
// with it CFLocale) is off the table. Read once and cache — the answer only
// changes when the user changes it in System Settings, which restarts nothing
// but is also not worth a subprocess per request.
var darwinLocale struct {
	sync.Once
	value string
}

const darwinLocaleTimeout = 2 * time.Second

func systemLocale() string {
	darwinLocale.Do(func() { darwinLocale.value = readDarwinLocale() })
	return darwinLocale.value
}

func readDarwinLocale() string {
	ctx, cancel := context.WithTimeout(context.Background(), darwinLocaleTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return ""
	}
	return firstAppleLanguage(string(out))
}

// firstAppleLanguage pulls the preferred language out of the plist array
// `defaults` prints, e.g.
//
//	(
//	    "zh-Hans-CN",
//	    en-US
//	)
//
// Only the first entry matters: the rest are the user's fallback order, and
// Normalize already reduces "zh-Hans-CN" to zh-CN. Entries may or may not be
// quoted, and the last one carries no trailing comma.
func firstAppleLanguage(out string) string {
	for _, line := range strings.Split(out, "\n") {
		field := strings.Trim(strings.TrimSpace(line), "(),\"")
		if field == "" {
			continue
		}
		return field
	}
	return ""
}
