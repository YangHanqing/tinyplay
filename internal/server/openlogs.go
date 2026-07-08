package server

import (
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"tvremote/internal/config"
)

// openLogs reveals the logs directory in the OS file manager. It is wired to a
// menu item in the native shells so a user can grab logs (to report a bug)
// without hunting through ~/Library/Application Support. The core runs on the
// same machine, so launching Finder / Explorer from here is fine.
func (s *Server) openLogs(w http.ResponseWriter, r *http.Request) {
	dir := config.LogDir()
	_ = os.MkdirAll(dir, 0o755)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	// Start (not Run): explorer.exe in particular returns a non-zero exit code
	// even on success, and we don't need to wait for the window.
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": dir})
}
