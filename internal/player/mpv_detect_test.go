package player

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tvremote/internal/config"
)

// writeFailingScript drops an executable that starts and then fails, standing
// in for a bundled mpv the operating system refuses to load (dyld rejecting a
// binary whose deployment target is newer than this macOS). Skips on Windows
// for the same reason writeFakeScript does.
func writeFailingScript(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake mpv script requires a POSIX shell")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

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

// TestDetectMPVFallsThroughWhenShippedRuntimeCannotRun is the macOS 12/13
// bug: the bundled mpv (which the macOS shell hands over as TVREMOTE_MPV_EXE)
// is present and executable, but the system is older than its deployment
// target, so it never starts. A stat-only check cannot see that, and playback
// dies with no explanation. Detection must probe it, fall through to a user
// -installed mpv, and say why.
func TestDetectMPVFallsThroughWhenShippedRuntimeCannotRun(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	broken := writeFailingScript(t, dir, "env-mpv")
	t.Setenv("TVREMOTE_MPV_EXE", broken)
	config.SetMpvExe("")

	pathDir := t.TempDir()
	systemPath := writeFakeScript(t, pathDir, "mpv", "mpv 0.41.0")
	t.Setenv("PATH", pathDir)

	info := DetectMPV()
	if info.Source != "system" || info.Path != systemPath {
		t.Fatalf("expected fallback to the system mpv, got source=%s path=%s", info.Source, info.Path)
	}
	if !info.Available || !info.BundledUnusable {
		t.Fatalf("expected an available system mpv flagged as bundled-unusable, got %+v", info)
	}
}

// TestDetectMPVReportsUnusableShippedRuntimeWithNoAlternative pins the other
// half: with nothing to fall back to, the UI still has to be able to tell
// "TinyPlay shipped no player" apart from "TinyPlay's player cannot run
// here", because only the second one is fixed by installing mpv yourself.
func TestDetectMPVReportsUnusableShippedRuntimeWithNoAlternative(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	t.Setenv("TVREMOTE_MPV_EXE", writeFailingScript(t, dir, "env-mpv"))
	config.SetMpvExe("")
	t.Setenv("PATH", t.TempDir())

	info := DetectMPV()
	if !info.BundledUnusable {
		t.Fatalf("expected BundledUnusable, got %+v", info)
	}
	// Whether anything was found after the fall-through depends on the host
	// (a developer Mac has Homebrew's mpv in a fixed location), so assert
	// only that the dead runtime was not selected.
	if info.Source == "env" || info.Source == "bundled" {
		t.Fatalf("a runtime that cannot start must never be selected: %+v", info)
	}
}

// TestDetectMPVProbesShippedRuntimeOnlyOnce guards the cost of the probe.
// DetectMPV runs on every playback command and every settings read; forking
// mpv --version each time would put a process spawn on the hot path.
func TestDetectMPVProbesShippedRuntimeOnlyOnce(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	path := filepath.Join(dir, "counting-mpv")
	script := "#!/bin/sh\necho x >> '" + counter + "'\necho 'mpv 0.41.0'\n"
	if runtime.GOOS == "windows" {
		t.Skip("fake mpv script requires a POSIX shell")
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TVREMOTE_MPV_EXE", path)
	config.SetMpvExe("")

	for i := 0; i < 3; i++ {
		if info := DetectMPV(); info.Source != "env" {
			t.Fatalf("expected the env runtime, got %+v", info)
		}
	}
	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("probe never ran: %v", err)
	}
	if got := strings.Count(string(runs), "x"); got != 1 {
		t.Fatalf("probe ran %d times, want 1 (result must be cached)", got)
	}
}

// TestDarwinMPVCandidatesIncludeDraggedInMPVApp covers the path a macOS 12/13
// user actually takes: mpv.io's macOS download is an mpv.app, and it gets
// dragged into /Applications. Nothing about that puts mpv on a Finder-launched
// app's PATH, so without these candidates the person has to know to dig into
// the .app from the advanced-settings file picker. Asserted on the candidate
// list rather than through systemMPV(), which would otherwise depend on
// whether the machine running the test happens to have Homebrew's mpv.
func TestDarwinMPVCandidatesIncludeDraggedInMPVApp(t *testing.T) {
	got := darwinMPVCandidates("/Users/someone")
	want := []string{
		"/opt/homebrew/bin/mpv",
		"/usr/local/bin/mpv",
		"/Applications/mpv.app/Contents/MacOS/mpv",
		filepath.Join("/Users/someone", "Applications", "mpv.app", "Contents", "MacOS", "mpv"),
	}
	if len(got) != len(want) {
		t.Fatalf("darwinMPVCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d = %q, want %q (order decides which install wins)", i, got[i], want[i])
		}
	}
}

// A machine with no home directory must not produce a bogus relative path.
// darwin-only: these are POSIX paths, and filepath.IsAbs on Windows rejects
// them for a reason that says nothing about the code under test.
func TestDarwinMPVCandidatesSkipUserApplicationsWithoutHome(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only candidate list")
	}
	for _, c := range darwinMPVCandidates("") {
		if !filepath.IsAbs(c) {
			t.Fatalf("candidate %q is not absolute", c)
		}
	}
}

// TestSystemMPVSkipsACandidateThatCannotRun is the second half of the macOS
// 12/13 rescue. systemMPV walks a *list*, so a stat-only check does not just
// risk answering "missing" one fork sooner - an abandoned Homebrew mpv that is
// itself too new outranks the mpv.app the README tells the user to install,
// and the UI would report their own mpv as in use right up until playback
// dies.
func TestSystemMPVSkipsACandidateThatCannotRun(t *testing.T) {
	dir := t.TempDir()
	broken := writeFailingScript(t, dir, "mpv")
	t.Setenv("PATH", dir)

	if got := systemMPV(); got == broken {
		t.Fatalf("systemMPV() returned %q, which cannot start", got)
	}
}
