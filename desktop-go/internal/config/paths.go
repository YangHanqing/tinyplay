package config

import (
	"os"
	"path/filepath"
)

// DataDir returns the directory holding config.json. It is resolved in this
// order:
//
//  1. $TVREMOTE_DATA_DIR (explicit override, used by dev / tests)
//  2. ./data next to the current working directory, IF it already exists
//     (lets a developer share the same data/config.json as the Python branch)
//  3. the per-user OS config dir, e.g.
//       macOS:   ~/Library/Application Support/tv-remote-mpv
//       Windows: %AppData%\tv-remote-mpv
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
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "tv-remote-mpv")
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
