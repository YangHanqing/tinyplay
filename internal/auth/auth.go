// Package auth owns device pairing and token verification for the LAN remote.
//
// The remote used to be open to anyone who could reach the port, which meant
// any device on the same Wi-Fi could read files, drive mpv, and inject desktop
// input. Pairing closes that without asking users to invent a password:
//
//   - Scanning the QR code on the computer's own screen is the normal path. The
//     QR carries a high-entropy secret (80 bits), so it is not guessable; the
//     endpoint may stay open because a camera never mistypes. Ten wrong secrets
//     inside ten minutes still locks it, because at that point someone is
//     guessing rather than scanning.
//   - Typing the LAN address by hand falls back to on-screen consent: the phone
//     shows a four-digit code and the person approves that exact code on the
//     computer. The digits are not a password — approval is the credential, and
//     it cannot happen without physical access to the machine.
//
// Either path ends in one long-lived token per device, stored hashed. Physical
// presence therefore always beats the lockout: the person at the keyboard can
// refresh the QR secret or approve a request even while an attacker is
// hammering the pairing endpoint.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Pairing failures that callers distinguish for the user.
var (
	ErrPairingLocked = errors.New("auth: qr pairing is locked")
	ErrBadSecret     = errors.New("auth: wrong pairing secret")
	ErrPairingBusy   = errors.New("auth: another pairing request is waiting for approval")
	ErrNoRequest     = errors.New("auth: no pairing request is waiting")
)

const (
	// secretAlphabet omits I/L/O/U so a person can read the secret off the
	// screen if a camera is unavailable.
	secretAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ0123456789"
	secretLength   = 16 // 32^16 ≈ 2^80

	// Ten wrong secrets in ten minutes is far past any scanning accident.
	maxSecretFailures = 10
	failureWindow     = 10 * time.Minute

	// A consent request has to be approved while the person is still standing
	// at the computer; it is not a queue.
	requestTTL = 2 * time.Minute
	// After a denial, refuse new requests briefly so a rejected device cannot
	// immediately fill the screen with consent prompts again.
	denyCooldown = 30 * time.Second

	tokenBytes = 32
	maxLabel   = 40
)

// Store persists issued device tokens. Only hashes are handed to it: a leaked
// config file must not grant access.
type Store interface {
	AddToken(hash, label string)
	TokenHashes() []string
	RevokeAllTokens()
	TouchToken(hash string)
}

// PendingRequest is one consent request waiting on the computer's screen.
type PendingRequest struct {
	Nonce string `json:"nonce"`
	Code  string `json:"code"`
	Label string `json:"label"`
}

type pendingRequest struct {
	PendingRequest
	created  time.Time
	approved bool
	token    string
}

// Manager is safe for concurrent use by HTTP handlers.
type Manager struct {
	mu           sync.Mutex
	store        Store
	secret       string
	locked       bool
	failures     []time.Time
	pending      *pendingRequest
	deniedUntil  time.Time
	now          func() time.Time
	pairingDelay time.Duration
}

// New starts with a fresh secret, so a photo of yesterday's QR code stops
// working as soon as the app restarts.
func New(store Store) *Manager {
	return &Manager{
		store:        store,
		secret:       randomSecret(),
		now:          time.Now,
		pairingDelay: 700 * time.Millisecond,
	}
}

// Secret is the value encoded into the QR code.
func (m *Manager) Secret() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.secret
}

// Locked reports whether repeated wrong secrets closed the QR path.
func (m *Manager) Locked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.locked
}

// Refresh reopens QR pairing with a new secret. It is reachable only from the
// computer itself (the refresh button on the intro window), which is why it may
// clear a lockout an attacker caused.
func (m *Manager) Refresh() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secret = randomSecret()
	m.locked = false
	m.failures = nil
}

// Paired reports whether at least one device holds a token. A fresh install has
// none, which is what the intro window uses to explain the QR code.
func (m *Manager) Paired() bool {
	return len(m.store.TokenHashes()) > 0
}

// PairWithSecret exchanges the QR secret for a device token.
//
// Every attempt — right or wrong — pays a fixed delay while holding the lock,
// so attempts are serialized and cannot be parallelized into a fast guessing
// loop. Wrong secrets are counted; ten inside the window close the path.
func (m *Manager) PairWithSecret(secret, label string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.locked {
		return "", ErrPairingLocked
	}
	m.sleepLocked(m.pairingDelay)
	if subtle.ConstantTimeCompare([]byte(strings.ToUpper(strings.TrimSpace(secret))), []byte(m.secret)) != 1 {
		m.recordFailureLocked()
		if m.locked {
			return "", ErrPairingLocked
		}
		return "", ErrBadSecret
	}
	m.failures = nil
	return m.issueTokenLocked(label), nil
}

// RequestPairing opens a consent request for a phone that typed the address by
// hand. The returned code is shown on the phone and compared on the computer.
func (m *Manager) RequestPairing(label string) (PendingRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if now.Before(m.deniedUntil) {
		return PendingRequest{}, ErrPairingBusy
	}
	if m.pending != nil && !m.pending.approved && now.Sub(m.pending.created) < requestTTL {
		return PendingRequest{}, ErrPairingBusy
	}
	m.pending = &pendingRequest{
		PendingRequest: PendingRequest{
			Nonce: randomToken(),
			Code:  randomCode(),
			Label: sanitizeLabel(label),
		},
		created: now,
	}
	return m.pending.PendingRequest, nil
}

// Pending returns the request the computer should ask about, if any.
func (m *Manager) Pending() (PendingRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil || m.pending.approved || m.expiredLocked() {
		return PendingRequest{}, false
	}
	return m.pending.PendingRequest, true
}

// Approve issues a token for the named request. The nonce is required so that a
// click always approves the request whose code the person actually read — never
// a different one that took the slot in between.
func (m *Manager) Approve(nonce string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil || m.pending.Nonce != nonce || m.pending.approved || m.expiredLocked() {
		return ErrNoRequest
	}
	m.pending.approved = true
	m.pending.token = m.issueTokenLocked(m.pending.Label)
	return nil
}

// Deny drops the request and starts a short cooldown.
func (m *Manager) Deny(nonce string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil || m.pending.Nonce != nonce {
		return ErrNoRequest
	}
	m.pending = nil
	m.deniedUntil = m.now().Add(denyCooldown)
	return nil
}

// Claim is the phone polling its own request. An approved token is handed out
// exactly once, to the nonce that asked for it.
func (m *Manager) Claim(nonce string) (state string, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil || m.pending.Nonce != nonce {
		return "denied", ""
	}
	if m.pending.approved {
		token = m.pending.token
		m.pending = nil
		return "approved", token
	}
	if m.expiredLocked() {
		m.pending = nil
		return "expired", ""
	}
	return "pending", ""
}

// Verify checks a bearer token against the stored hashes.
func (m *Manager) Verify(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(sum[:])
	ok := false
	for _, hash := range m.store.TokenHashes() {
		// Compare every entry so timing does not reveal the match position.
		if subtle.ConstantTimeCompare([]byte(hash), []byte(want)) == 1 {
			ok = true
		}
	}
	if ok {
		m.store.TouchToken(want)
	}
	return ok
}

// RevokeAll drops every paired device and rotates the QR secret. Without the
// rotation, a photo of the old QR code would re-pair immediately and the
// revocation would be theatre.
func (m *Manager) RevokeAll() {
	m.store.RevokeAllTokens()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secret = randomSecret()
	m.locked = false
	m.failures = nil
	m.pending = nil
	m.deniedUntil = time.Time{}
}

// DeviceCount is shown on the computer's intro window.
func (m *Manager) DeviceCount() int { return len(m.store.TokenHashes()) }

func (m *Manager) issueTokenLocked(label string) string {
	token := randomToken()
	sum := sha256.Sum256([]byte(token))
	m.store.AddToken(hex.EncodeToString(sum[:]), sanitizeLabel(label))
	return token
}

func (m *Manager) recordFailureLocked() {
	now := m.now()
	kept := m.failures[:0]
	for _, at := range m.failures {
		if now.Sub(at) < failureWindow {
			kept = append(kept, at)
		}
	}
	m.failures = append(kept, now)
	if len(m.failures) >= maxSecretFailures {
		m.locked = true
	}
}

func (m *Manager) expiredLocked() bool {
	return m.pending != nil && m.now().Sub(m.pending.created) >= requestTTL
}

// sleepLocked keeps the mutex held on purpose: pairing attempts must not run in
// parallel, so wall-clock time is a real ceiling on guesses per second.
func (m *Manager) sleepLocked(d time.Duration) {
	if d > 0 {
		time.Sleep(d)
	}
}

func randomSecret() string {
	out := make([]byte, secretLength)
	for i := range out {
		out[i] = secretAlphabet[randomInt(len(secretAlphabet))]
	}
	return string(out)
}

func randomCode() string {
	digits := []byte("0123456789")
	out := make([]byte, 4)
	for i := range out {
		out[i] = digits[randomInt(10)]
	}
	return string(out)
}

func randomToken() string {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail in practice; refusing to hand out a weak
		// token is the only safe response if it ever does.
		panic("auth: no entropy available: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func randomInt(n int) int {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("auth: no entropy available: " + err.Error())
	}
	return int(value.Int64())
}

// sanitizeLabel keeps the device list readable: it is derived from a
// client-supplied User-Agent, so it is trimmed, single-lined, and bounded.
func sanitizeLabel(label string) string {
	label = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, label)
	label = strings.TrimSpace(strings.Join(strings.Fields(label), " "))
	if label == "" {
		return "Phone"
	}
	if len([]rune(label)) > maxLabel {
		label = strings.TrimSpace(string([]rune(label)[:maxLabel]))
	}
	return label
}
