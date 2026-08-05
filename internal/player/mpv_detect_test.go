package player

import (
	"testing"

	"tvremote/internal/config"
)

// TestDetectMPVPrefersPersistedCustomOverEnv is the exact scenario the
// background section of the "custom mpv path" feature calls out: the macOS
// AppKit shell always sets TVREMOTE_MPV_EXE to the bundled mpv before
// launching the core, so a persisted user choice (config.MpvExe) must outrank
// it — otherwise the user's setting would be silently overridden on every
// launch.
func TestDetectMPVPrefersPersistedCustomOverEnv(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	customPath := writeFakeScript(t, dir, "custom-mpv", "mpv 0.99.0")
	envPath := writeFakeScript(t, dir, "env-mpv", "mpv 0.11.0")
	t.Setenv("TVREMOTE_MPV_EXE", envPath)
	config.SetMpvExe(customPath)
	t.Cleanup(func() { config.SetMpvExe("") })

	info := DetectMPV()
	if info.Source != "custom" || info.Path != customPath {
		t.Fatalf("expected custom path to win, got source=%s path=%s", info.Source, info.Path)
	}
	if !info.Available || !info.CustomConfigured || info.CustomInvalid {
		t.Fatalf("unexpected info: %+v", info)
	}
}

// TestDetectMPVFallsThroughWhenCustomPathIsStale exercises the "silent
// fallback" requirement: a persisted path that no longer resolves must not
// disable playback, and MPVInfo must say so (CustomConfigured && CustomInvalid)
// so the UI can show "custom path went stale, using bundled" instead of just
// "bundled".
func TestDetectMPVFallsThroughWhenCustomPathIsStale(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	envPath := writeFakeScript(t, dir, "env-mpv", "mpv 0.11.0")
	t.Setenv("TVREMOTE_MPV_EXE", envPath)
	config.SetMpvExe("/no/such/mpv/binary")
	t.Cleanup(func() { config.SetMpvExe("") })

	info := DetectMPV()
	if info.Source != "env" || info.Path != envPath {
		t.Fatalf("expected fallback to env, got source=%s path=%s", info.Source, info.Path)
	}
	if !info.CustomConfigured || !info.CustomInvalid {
		t.Fatalf("expected CustomConfigured+CustomInvalid, got %+v", info)
	}
}

// TestDetectMPVNoCustomConfigured is the default/common case: nothing set,
// nothing should look "invalid".
func TestDetectMPVNoCustomConfigured(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	t.Setenv("TVREMOTE_MPV_EXE", "")
	config.SetMpvExe("")

	info := DetectMPV()
	if info.CustomConfigured || info.CustomInvalid {
		t.Fatalf("expected no custom state, got %+v", info)
	}
}

// TestDetectMPVIgnoresLegacyBareMpvDefault pins the upgrade path. Every
// pre-2026-08 install persisted the old default `"mpv_exe": "mpv"`, and under
// the current priority order a non-empty MpvExe outranks the bundled runtime.
// Without config.Load's migration (and customMPVPath's absolute-path guard as
// a second line of defence), upgrading would silently demote every existing
// installation from its bundled mpv to whatever happens to sit on PATH.
func TestDetectMPVIgnoresLegacyBareMpvDefault(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	envPath := writeFakeScript(t, dir, "env-mpv", "mpv 0.11.0")
	t.Setenv("TVREMOTE_MPV_EXE", envPath)
	config.SetMpvExe("mpv")
	t.Cleanup(func() { config.SetMpvExe("") })

	info := DetectMPV()
	if info.Source != "env" || info.Path != envPath {
		t.Fatalf("legacy bare \"mpv\" must not act as a custom runtime, got source=%s path=%s", info.Source, info.Path)
	}
	if info.CustomConfigured || info.CustomInvalid {
		t.Fatalf("legacy value must not surface as a configured custom path: %+v", info)
	}
}

// TestValidateMPVRejectsRelativePath guards the other half of the same trap:
// a bare name validates fine through PATH, so without this check the shell
// would report a successful save for a setting DetectMPV then ignores.
func TestValidateMPVRejectsRelativePath(t *testing.T) {
	if err := ValidateMPV("mpv"); err == nil {
		t.Fatal("expected a relative path to be rejected")
	}
}
