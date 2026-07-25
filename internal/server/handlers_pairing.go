package server

import (
	"errors"
	"net/http"
	"strings"

	"tvremote/internal/auth"
	"tvremote/internal/i18n"
)

// ── Phone-facing pairing (reachable without a token; see withAuth) ──

// pairWithQR is the normal path: the phone read the secret out of the QR code
// shown on the computer's screen.
func (s *Server) pairWithQR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string `json:"secret"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	token, err := auth.Default.PairWithSecret(body.Secret, deviceLabel(r))
	if err != nil {
		writePairingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}

// pairRequest is the hand-typed path: ask the person at the computer to approve
// this phone, and show them a four-digit code to compare.
func (s *Server) pairRequest(w http.ResponseWriter, r *http.Request) {
	pending, err := auth.Default.RequestPairing(deviceLabel(r))
	if err != nil {
		writePairingError(w, r, err)
		return
	}
	// Nonce goes back to this phone only; the code is what the human compares.
	writeJSON(w, http.StatusOK, map[string]any{"nonce": pending.Nonce, "code": pending.Code})
}

// pairClaim is the phone polling its own request. The token is released once.
func (s *Server) pairClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nonce string `json:"nonce"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	state, token := auth.Default.Claim(body.Nonce)
	out := map[string]any{"state": state}
	if token != "" {
		out["token"] = token
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Computer-facing pairing control (loopback only) ──

// pairingStatus feeds the intro window: what to ask about, and whether repeated
// wrong secrets have closed the QR path.
func (s *Server) pairingStatus(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	out := map[string]any{
		"locked":       auth.Default.Locked(),
		"device_count": auth.Default.DeviceCount(),
	}
	if pending, ok := auth.Default.Pending(); ok {
		out["pending"] = pending
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) pairingApprove(w http.ResponseWriter, r *http.Request) {
	s.pairingDecision(w, r, auth.Default.Approve)
}

func (s *Server) pairingDeny(w http.ResponseWriter, r *http.Request) {
	s.pairingDecision(w, r, auth.Default.Deny)
}

// pairingDecision carries the nonce so a click always resolves the request whose
// code the person actually read on the phone.
func (s *Server) pairingDecision(w http.ResponseWriter, r *http.Request, decide func(string) error) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	var body struct {
		Nonce string `json:"nonce"`
	}
	if !decode(r, &body) {
		invalidBody(w, r)
		return
	}
	if err := decide(body.Nonce); err != nil {
		writePairingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// pairingRefresh reopens QR pairing with a new secret. Being loopback-only is
// what makes it safe to let this clear a lockout: whoever presses it is standing
// at the computer, which outranks a lock caused by someone guessing.
func (s *Server) pairingRefresh(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	auth.Default.Refresh()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// pairingUnpairAll drops every paired phone. Deliberately not exposed to the
// phone API: revoking other people's devices requires physical access.
func (s *Server) pairingUnpairAll(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "loopback only"})
		return
	}
	auth.Default.RevokeAll()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device_count": 0})
}

func writePairingError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	key := "invalid_body"
	switch {
	case errors.Is(err, auth.ErrPairingLocked):
		status, key = http.StatusForbidden, "pair_locked"
	case errors.Is(err, auth.ErrBadSecret):
		status, key = http.StatusUnauthorized, "pair_bad_secret"
	case errors.Is(err, auth.ErrPairingBusy):
		status, key = http.StatusConflict, "pair_busy"
	case errors.Is(err, auth.ErrNoRequest):
		status, key = http.StatusNotFound, "pair_no_request"
	}
	writeJSON(w, status, map[string]any{"detail": i18n.Request(r, key), "code": key})
}

// deviceLabel names the phone in the paired-device count on the computer. The
// User-Agent is attacker-controlled text, so keep it to a coarse device family
// rather than echoing the raw string into the UI.
func deviceLabel(r *http.Request) string {
	ua := r.UserAgent()
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	default:
		return "Phone"
	}
}
