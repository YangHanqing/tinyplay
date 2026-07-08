package server

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

// playerDebugReport backs the remote's "Playback issue?" button (mirrors the
// tvOS branch's GET /api/player/debug-report). It bundles enough state to
// diagnose a bad playback session without exposing credentials: the frontend
// still redacts tokens/URLs and strips non-English text before the user
// copies the report into a support email, but keeping this endpoint's own
// output free of secrets is a second line of defense.
func (s *Server) playerDebugReport(w http.ResponseWriter, r *http.Request) {
	mpvInfo := player.DetectMPV()
	report := map[string]any{
		"report_schema_version": 1,
		"generated_at":          time.Now().UTC().Format(time.RFC3339),
		"platform":              runtime.GOOS,
		"arch":                  runtime.GOARCH,
		"mpv": map[string]any{
			"source":    mpvInfo.Source,
			"available": mpvInfo.Available,
		},
		"session": s.player.State(),
		"source":  activeSourceSummary(),
		"logs": map[string]any{
			"core_log_tail": tailLines(filepath.Join(config.LogDir(), "tvremote.log"), 60),
			"mpv_log_tail":  tailLines(filepath.Join(config.LogDir(), "mpv.log"), 60),
		},
		"privacy": "Access tokens, passwords, and request headers are excluded.",
	}
	writeJSON(w, http.StatusOK, report)
}

// activeSourceSummary reports only the shape of the active media source, not
// its identifying details (name, hosts, credentials).
func activeSourceSummary() map[string]any {
	srv := config.ActiveServer()
	if srv == nil {
		return map[string]any{"configured": false}
	}
	return map[string]any{
		"configured": true,
		"type":       config.NormalizeServerType(srv.Type),
		"protocol":   srv.Protocol,
	}
}

// tailLines returns up to maxLines of the file's last lines, oldest first. It
// returns an empty slice (never an error) so a missing/rotated log file just
// yields an empty section in the report.
func tailLines(path string, maxLines int) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer f.Close()

	buf := make([]string, 0, maxLines)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		buf = append(buf, scanner.Text())
		if len(buf) > maxLines {
			buf = buf[1:]
		}
	}
	return buf
}
