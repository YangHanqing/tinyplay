package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tvremote/internal/i18n"
	"tvremote/internal/website"
)

const websiteShellPollTimeout = 25 * time.Second

// websiteBroker is the process-global control plane for website mode.
// Tests may replace it with a fresh broker.
var websiteBroker = website.Default

func initWebsiteBroker(stopMPV func()) {
	websiteBroker.Configure(stopMPV)
}

// RequestWebsiteClose is the mutual-exclusion entry point used before mpv play.
func RequestWebsiteClose() {
	if websiteBroker == nil {
		return
	}
	snap := websiteBroker.Snapshot()
	if snap.DesiredOpen || snap.ReportedOpen {
		websiteBroker.RequestClose()
	}
}

func resetWebsiteState() {
	if websiteBroker != nil {
		websiteBroker.Reset()
	}
}

func (s *Server) websiteState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, websiteBroker.Snapshot())
}

func (s *Server) websiteOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SiteID string `json:"site_id"`
	}
	// Body is required to carry an allowlisted site_id.
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	snap, err := websiteBroker.RequestOpen(body.SiteID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"detail": websiteErrorDetail(r, err),
		})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) websiteClose(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, websiteBroker.RequestClose())
}

func (s *Server) websiteAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Text   string `json:"text"`
		Label  string `json:"label"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	snap, err := websiteBroker.EnqueueAction(body.Action, body.Text, body.Label)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"detail": websiteErrorDetail(r, err),
		})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// websiteShellPoll is loopback-only: native shells long-poll for typed commands.
func (s *Server) websiteShellPoll(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	after := uint64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			after = n
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), websiteShellPollTimeout)
	defer cancel()
	cmd, ok := websiteBroker.WaitCommand(ctx, after)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "empty": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "empty": false, "command": cmd})
}

// websiteShellReport is loopback-only status from the native website window.
func (s *Server) websiteShellReport(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	var body website.Report
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	writeJSON(w, http.StatusOK, websiteBroker.ApplyReport(body))
}

// websiteControllerJS serves the shared injected controller (loopback preferred;
// also readable by shells via local URL). Content is fixed embedded JS — never
// caller-controlled.
func (s *Server) websiteControllerJS(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(website.ControllerJS))
}

func websiteErrorDetail(r *http.Request, err error) string {
	code := website.ErrorCode(err)
	switch code {
	case "unknown_site":
		return i18n.Request(r, "website_unknown_site")
	case "unknown_action":
		return i18n.Request(r, "website_unknown_action")
	case "action_unavailable":
		return i18n.Request(r, "website_unknown_action")
	case "text_too_long":
		return i18n.Request(r, "website_text_too_long")
	case "text_required":
		return i18n.Request(r, "website_text_required")
	case "invalid_label":
		return i18n.Request(r, "website_invalid_label")
	case "invalid_number":
		return i18n.Request(r, "website_invalid_number")
	case "window_not_open":
		return i18n.Request(r, "website_window_not_open")
	case "site_required":
		return i18n.Request(r, "website_unknown_site")
	case "home_unavailable":
		return i18n.Request(r, "website_home_unavailable")
	case "login_unavailable":
		return i18n.Request(r, "website_login_unavailable")
	default:
		if code != "" {
			return code
		}
		return i18n.Request(r, "invalid_body")
	}
}

func isLoopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	// Strip IPv6 brackets if present without port.
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
