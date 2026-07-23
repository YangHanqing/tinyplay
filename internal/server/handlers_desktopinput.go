package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"tvremote/internal/desktopinput"
)

func (s *Server) desktopInputState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, desktopinput.Default.Snapshot())
}

func (s *Server) desktopInputAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		DX     int    `json:"dx"`
		DY     int    `json:"dy"`
		Text   string `json:"text"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	if _, ok := desktopinput.Default.Enqueue(body.Action, body.DX, body.DY, body.Text); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid desktop input action"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

// The native macOS app consumes this loopback-only endpoint. Windows runs the
// same broker in-process, avoiding an unnecessary HTTP round trip.
func (s *Server) desktopInputPoll(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd, ok := desktopinput.Default.WaitCommand(ctx, after)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]bool{"empty": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"command": cmd})
}

func (s *Server) desktopInputReport(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	var state desktopinput.Snapshot
	if !decode(r, &state) {
		invalidBody(w, r)
		return
	}
	desktopinput.Default.ReportState(state)
	writeJSON(w, http.StatusOK, state)
}
