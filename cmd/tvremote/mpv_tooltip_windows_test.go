//go:build windows

package main

import (
	"testing"

	"tvremote/internal/i18n"
)

// TestMPVTooltipPrefersUnusableBundledOverStaleCustom pins the ordering the
// tray shares with desktopNotices. Both flags are true at once in the real
// case this feature exists for -- a machine too old for the bundled mpv that
// also still has an old custom path saved -- and the stale-custom wording
// says the bundled player is in use, which is exactly the false statement the
// intro page was rewritten to stop making. The two surfaces must not disagree.
func TestMPVTooltipPrefersUnusableBundledOverStaleCustom(t *testing.T) {
	status := mpvStatusResponse{
		Path:             `C:\mpv\mpv.exe`,
		Source:           "system",
		Available:        true,
		CustomConfigured: true,
		CustomValid:      false,
		BundledUnusable:  true,
	}
	got := mpvStatusTooltip(status)
	if got == i18n.System("mpv_custom_stale_tip") {
		t.Fatal("a bundled runtime that cannot start must outrank a stale custom path")
	}
	if got == i18n.System("mpv_default_tip") {
		t.Fatal("must not claim the bundled player is in use")
	}
}

// With nothing left to play with, the tooltip has to say so rather than fall
// back to the "using the bundled mpv player" default.
func TestMPVTooltipReportsUnusableBundledWithNoFallback(t *testing.T) {
	got := mpvStatusTooltip(mpvStatusResponse{Source: "missing", BundledUnusable: true})
	if want := i18n.System("mpv_too_new_missing_tip"); got != want {
		t.Fatalf("tooltip = %q, want %q", got, want)
	}
}

// The ordinary case must be untouched: a working bundled runtime still reads
// as the bundled runtime.
func TestMPVTooltipUnchangedForWorkingBundledRuntime(t *testing.T) {
	got := mpvStatusTooltip(mpvStatusResponse{Source: "env", Available: true})
	if want := i18n.System("mpv_default_tip"); got != want {
		t.Fatalf("tooltip = %q, want %q", got, want)
	}
}
