package web

import (
	"os"
	"strings"
	"testing"
)

// The phone UI has no JavaScript test runner, so keep this small source-level
// contract beside the embedded asset. It pins the ordering boundary: process
// exit clears chrome, while only the acknowledged stop generation refreshes
// Continue Watching.
func TestResumeShelfRefreshUsesAcknowledgedGeneration(t *testing.T) {
	source, err := os.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	app := string(source)

	observe := javascriptFunction(app, "observeResumeRefreshGeneration")
	if !strings.Contains(observe, "resume_refresh_generation") || !strings.Contains(observe, "loadResume().catch") {
		t.Fatal("resume refresh observer must reload only from resume_refresh_generation")
	}
	if !strings.Contains(observe, "!state.running && !state.item_id && !state.player_starting") {
		t.Fatal("resume refresh observer must retain the backend idle invariant")
	}
	for _, name := range []string{"_handlePlaybackEnded", "stopPlayer", "cancelAutoplayNext"} {
		if strings.Contains(javascriptFunction(app, name), "loadResume(") {
			t.Fatalf("%s must not refresh resume directly; it races the stop report", name)
		}
	}
}

func javascriptFunction(source, name string) string {
	start := strings.Index(source, "function "+name+"(")
	if start < 0 {
		return ""
	}
	remainder := source[start+1:]
	end := len(remainder)
	for _, marker := range []string{"\nfunction ", "\nasync function "} {
		if next := strings.Index(remainder, marker); next >= 0 && next < end {
			end = next
		}
	}
	if end == len(remainder) {
		return source[start:]
	}
	return source[start : start+1+end]
}

// The countdown length is the one numeric setting where 0 is a real value.
// `Number(x) || 5` silently turns "no countdown" back into five seconds, which
// is exactly the bug this pins — in the settings read and in the countdown
// overlay's own fallback.
func TestAutoplayCountdownTreatsZeroAsAChoice(t *testing.T) {
	source, err := os.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	app := string(source)

	for _, fn := range []string{"loadSettings", "_autoplayLocalDeadline", "setAutoplayCountdown"} {
		body := javascriptFunction(app, fn)
		if body == "" {
			t.Fatalf("%s not found in app.js", fn)
		}
		if strings.Contains(body, "autoplay_countdown_secs) || ") {
			t.Fatalf("%s coerces a zero countdown back to the default", fn)
		}
	}
	if !strings.Contains(javascriptFunction(app, "_autoplayLocalDeadline"), "_settings.autoplay_countdown_secs") {
		t.Fatal("the countdown fallback must follow the configured length, not a hardcoded 5s")
	}
}
