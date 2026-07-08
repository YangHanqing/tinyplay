package player

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"tvremote/internal/config"
)

type boundedLogFile struct {
	mu       sync.Mutex
	file     *os.File
	maxBytes int64
}

func (b *boundedLogFile) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	offset, err := b.file.Seek(0, 2)
	if err != nil {
		return 0, err
	}
	if offset+int64(len(p)) > b.maxBytes {
		if err = b.file.Truncate(0); err != nil {
			return 0, err
		}
		if _, err = b.file.Seek(0, 0); err != nil {
			return 0, err
		}
	}
	return b.file.Write(p)
}
func (b *boundedLogFile) Close() error { b.mu.Lock(); defer b.mu.Unlock(); return b.file.Close() }

// openPlayerLog captures verbose player output while enforcing a hard size
// ceiling even when one mpv process runs for days. The file is reset for every
// fresh process and again whenever it reaches 5 MiB.
func openPlayerLog() (*boundedLogFile, string) {
	dir := config.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, ""
	}
	path := filepath.Join(dir, "mpv.log")
	f, err := os.OpenFile(path,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, ""
	}
	return &boundedLogFile{file: f, maxBytes: 5 << 20}, path
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
