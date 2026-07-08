package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DataDir returns the directory holding config.json. It is resolved in this
// order:
//
//  1. $TVREMOTE_DATA_DIR (explicit override, used by dev / tests)
//  2. ./data next to the current working directory, IF it already exists
//     (lets a developer share the same data/config.json as the Python branch)
//  3. portable: a "data" folder next to the executable, when that location is
//     writable. This is the Windows unzip-and-run case, so deleting the app
//     folder also removes all config/logs (a clean, self-contained uninstall).
//     Deliberately skipped inside a macOS .app bundle: its contents are signed
//     and sealed, and the app lives in /Applications, so it cannot host
//     writable runtime files — macOS therefore falls through to step 4.
//  4. the per-user OS config dir, e.g.
//       macOS:   ~/Library/Application Support/TinyPlay
//       Windows: %AppData%\TinyPlay   (only if the portable dir was unusable)
func DataDir() string {
	if env := os.Getenv("TVREMOTE_DATA_DIR"); env != "" {
		return env
	}
	if wd, err := os.Getwd(); err == nil {
		local := filepath.Join(wd, "data")
		if st, err := os.Stat(local); err == nil && st.IsDir() {
			return local
		}
	}
	if dir, ok := portableDir(); ok {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "TinyPlay")
}

// portableDir returns a "data" directory next to the running executable, and
// whether it is usable. It returns false inside a macOS .app bundle (never
// write into a signed bundle) and whenever the location is not writable (e.g.
// the app was installed under Program Files / a read-only mount).
func portableDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(filepath.ToSlash(exe), ".app/Contents/") {
		return "", false
	}
	dir := filepath.Join(filepath.Dir(exe), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		return "", false
	}
	os.Remove(probe)
	return dir, true
}

// ConfigFile is the absolute path to config.json.
func ConfigFile() string {
	return filepath.Join(DataDir(), "config.json")
}

// LogDir returns the directory for runtime logs (the Go core log + mpv's own
// log). It sits next to config.json so a user can find and send it when a video
// makes mpv crash. The caller is responsible for MkdirAll.
func LogDir() string {
	return filepath.Join(DataDir(), "logs")
}
