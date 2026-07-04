package player

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"

	"tvremote/internal/config"
)

// mpvLogPath returns the path mpv should write its verbose --log-file to, or ""
// if the logs dir can't be created. Truncated on each fresh mpv launch.
func mpvLogPath() string {
	dir := config.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return filepath.Join(dir, "mpv.log")
}

// openMpvStderr opens (truncating) the file that captures mpv's stdout/stderr,
// or nil if it can't be created.
func openMpvStderr() *os.File {
	dir := config.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "mpv-stderr.log"),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil
	}
	return f
}

func randHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// formatFloat mirrors Python's f"{x:.3f}".
func formatFloat(x float64) string {
	return strconv.FormatFloat(x, 'f', 3, 64)
}
