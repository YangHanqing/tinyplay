package auth

import (
	"testing"
	"time"
)

type memStore struct {
	hashes  []string
	labels  []string
	touched []string
}

func (s *memStore) AddToken(hash, label string) {
	s.hashes = append(s.hashes, hash)
	s.labels = append(s.labels, label)
}
func (s *memStore) TokenHashes() []string  { return s.hashes }
func (s *memStore) RevokeAllTokens()       { s.hashes, s.labels = nil, nil }
func (s *memStore) TouchToken(hash string) { s.touched = append(s.touched, hash) }

// newTestManager removes the anti-brute-force delay so the table below runs in
// milliseconds. Every test that cares about the delay sets it explicitly.
func newTestManager() (*Manager, *memStore) {
	store := &memStore{}
	m := New(store)
	m.pairingDelay = 0
	return m, store
}

func TestQRPairingIssuesUsableToken(t *testing.T) {
	m, store := newTestManager()
	token, err := m.PairWithSecret(m.Secret(), "iPhone")
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}
	if !m.Verify(token) {
		t.Fatal("issued token did not verify")
	}
	if m.Verify(token + "x") {
		t.Fatal("a mutated token verified")
	}
	if len(store.hashes) != 1 {
		t.Fatalf("expected 1 stored hash, got %d", len(store.hashes))
	}
	if store.hashes[0] == token {
		t.Fatal("token was stored in the clear")
	}
	if store.labels[0] != "iPhone" {
		t.Fatalf("label not recorded: %q", store.labels[0])
	}
}

func TestQRSecretIsCaseInsensitiveForTypedInput(t *testing.T) {
	m, _ := newTestManager()
	if _, err := m.PairWithSecret(" "+lower(m.Secret())+" ", ""); err != nil {
		t.Fatalf("hand-typed secret rejected: %v", err)
	}
}

func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}

func TestTenWrongSecretsLockPairing(t *testing.T) {
	m, _ := newTestManager()
	for i := 0; i < maxSecretFailures-1; i++ {
		if _, err := m.PairWithSecret("WRONGWRONGWRONG1", ""); err != ErrBadSecret {
			t.Fatalf("attempt %d: expected ErrBadSecret, got %v", i+1, err)
		}
	}
	if _, err := m.PairWithSecret("WRONGWRONGWRONG1", ""); err != ErrPairingLocked {
		t.Fatalf("final attempt should lock, got %v", err)
	}
	if !m.Locked() {
		t.Fatal("manager should report locked")
	}
	// The real secret must not work while locked, or the lock is decorative.
	if _, err := m.PairWithSecret(m.Secret(), ""); err != ErrPairingLocked {
		t.Fatalf("locked pairing accepted the correct secret: %v", err)
	}
}

func TestFailuresOutsideWindowDoNotLock(t *testing.T) {
	m, _ := newTestManager()
	base := time.Now()
	m.now = func() time.Time { return base }
	for i := 0; i < maxSecretFailures-1; i++ {
		_, _ = m.PairWithSecret("WRONGWRONGWRONG1", "")
	}
	// A day later, the old failures are irrelevant: this is a new incident.
	m.now = func() time.Time { return base.Add(24 * time.Hour) }
	if _, err := m.PairWithSecret("WRONGWRONGWRONG1", ""); err != ErrBadSecret {
		t.Fatalf("stale failures still counted: %v", err)
	}
	if m.Locked() {
		t.Fatal("locked on failures from outside the window")
	}
}

func TestRefreshReopensPairingWithANewSecret(t *testing.T) {
	m, _ := newTestManager()
	old := m.Secret()
	for i := 0; i < maxSecretFailures; i++ {
		_, _ = m.PairWithSecret("WRONGWRONGWRONG1", "")
	}
	m.Refresh()
	if m.Locked() {
		t.Fatal("refresh did not unlock")
	}
	if m.Secret() == old {
		t.Fatal("refresh did not rotate the secret")
	}
	if _, err := m.PairWithSecret(old, ""); err != ErrBadSecret {
		t.Fatalf("the pre-refresh secret still pairs: %v", err)
	}
	if _, err := m.PairWithSecret(m.Secret(), ""); err != nil {
		t.Fatalf("new secret rejected: %v", err)
	}
}

func TestConsentFlowIssuesTokenOnlyToTheApprovedNonce(t *testing.T) {
	m, _ := newTestManager()
	req, err := m.RequestPairing("Safari on iPhone")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if len(req.Code) != 4 {
		t.Fatalf("expected a 4-digit code, got %q", req.Code)
	}
	if state, _ := m.Claim(req.Nonce); state != "pending" {
		t.Fatalf("expected pending, got %q", state)
	}
	shown, ok := m.Pending()
	if !ok || shown.Code != req.Code {
		t.Fatalf("computer should see the same code: %+v", shown)
	}
	if err := m.Approve(req.Nonce); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	state, token := m.Claim(req.Nonce)
	if state != "approved" || token == "" {
		t.Fatalf("expected an approved token, got %q/%q", state, token)
	}
	if !m.Verify(token) {
		t.Fatal("approved token does not verify")
	}
	// The token is handed out once; a replayed claim gets nothing.
	if state, token := m.Claim(req.Nonce); state == "approved" || token != "" {
		t.Fatalf("token handed out twice: %q/%q", state, token)
	}
}

// The attack this guards against: an attacker polls for pairing, and the moment
// the user clicks "allow" for their own phone, the attacker's request is the one
// that gets approved. Approval is bound to the nonce whose code was displayed.
func TestApproveRejectsAnUnknownNonce(t *testing.T) {
	m, _ := newTestManager()
	req, _ := m.RequestPairing("phone")
	if err := m.Approve("some-other-nonce"); err != ErrNoRequest {
		t.Fatalf("expected ErrNoRequest, got %v", err)
	}
	if state, _ := m.Claim(req.Nonce); state != "pending" {
		t.Fatalf("original request should still be pending, got %q", state)
	}
	if len(m.store.TokenHashes()) != 0 {
		t.Fatal("a token was issued without approval")
	}
}

func TestOnlyOneRequestMayWaitAtATime(t *testing.T) {
	m, _ := newTestManager()
	first, err := m.RequestPairing("phone")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if _, err := m.RequestPairing("attacker"); err != ErrPairingBusy {
		t.Fatalf("expected ErrPairingBusy, got %v", err)
	}
	if shown, _ := m.Pending(); shown.Nonce != first.Nonce {
		t.Fatal("a second request displaced the pending one")
	}
}

func TestExpiredRequestFreesTheSlot(t *testing.T) {
	m, _ := newTestManager()
	base := time.Now()
	m.now = func() time.Time { return base }
	first, _ := m.RequestPairing("phone")

	m.now = func() time.Time { return base.Add(requestTTL + time.Second) }
	if _, ok := m.Pending(); ok {
		t.Fatal("an expired request is still displayed")
	}
	if state, _ := m.Claim(first.Nonce); state != "expired" {
		t.Fatalf("expected expired, got %q", state)
	}
	if err := m.Approve(first.Nonce); err != ErrNoRequest {
		t.Fatalf("an expired request was approvable: %v", err)
	}
	if _, err := m.RequestPairing("phone again"); err != nil {
		t.Fatalf("slot not freed after expiry: %v", err)
	}
}

func TestDenyStartsACooldown(t *testing.T) {
	m, _ := newTestManager()
	base := time.Now()
	m.now = func() time.Time { return base }
	req, _ := m.RequestPairing("phone")
	if err := m.Deny(req.Nonce); err != nil {
		t.Fatalf("deny failed: %v", err)
	}
	if _, ok := m.Pending(); ok {
		t.Fatal("denied request still displayed")
	}
	if _, err := m.RequestPairing("phone"); err != ErrPairingBusy {
		t.Fatalf("cooldown not enforced: %v", err)
	}
	m.now = func() time.Time { return base.Add(denyCooldown + time.Second) }
	if _, err := m.RequestPairing("phone"); err != nil {
		t.Fatalf("cooldown never expired: %v", err)
	}
}

func TestRevokeAllDropsTokensAndRotatesTheSecret(t *testing.T) {
	m, store := newTestManager()
	token, _ := m.PairWithSecret(m.Secret(), "phone")
	old := m.Secret()

	m.RevokeAll()

	if m.Verify(token) {
		t.Fatal("revoked token still verifies")
	}
	if len(store.hashes) != 0 {
		t.Fatal("store not cleared")
	}
	// Without rotation, a photo of the old QR code would immediately re-pair.
	if m.Secret() == old {
		t.Fatal("revoke did not rotate the QR secret")
	}
	if m.Paired() {
		t.Fatal("still reports paired devices")
	}
}

// Touching the store lets the config layer keep a last-used timestamp for
// stale-device pruning; a failed verify must not extend a token's life.
func TestVerifyTouchesTheStoreOnSuccessOnly(t *testing.T) {
	m, store := newTestManager()
	token, _ := m.PairWithSecret(m.Secret(), "phone")

	m.Verify("wrong-token")
	if len(store.touched) != 0 {
		t.Fatalf("a failed verify touched the store: %v", store.touched)
	}

	m.Verify(token)
	if len(store.touched) != 1 {
		t.Fatalf("expected exactly one touch, got %v", store.touched)
	}
}

func TestVerifyRejectsEmptyAndUnknownTokens(t *testing.T) {
	m, _ := newTestManager()
	_, _ = m.PairWithSecret(m.Secret(), "phone")
	for _, token := range []string{"", "   ", "not-a-token"} {
		if m.Verify(token) {
			t.Fatalf("verified %q", token)
		}
	}
}

// Every attempt is serialized behind the mutex and pays the delay, so a
// concurrent flood cannot turn into a fast guessing loop.
func TestPairingAttemptsAreSerializedAndDelayed(t *testing.T) {
	m, _ := newTestManager()
	m.pairingDelay = 20 * time.Millisecond
	start := time.Now()
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func() {
			_, _ = m.PairWithSecret("WRONGWRONGWRONG1", "")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 5; i++ {
		<-done
	}
	if elapsed := time.Since(start); elapsed < 5*20*time.Millisecond {
		t.Fatalf("5 attempts took %v; they were not serialized", elapsed)
	}
}

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"":                  "Phone",
		"   ":               "Phone",
		"Safari\niPhone":    "Safari iPhone",
		"  spaced   out   ": "spaced out",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0",
	}
	for in, want := range cases {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecretShapeIsUnambiguous(t *testing.T) {
	for i := 0; i < 50; i++ {
		secret := randomSecret()
		if len(secret) != secretLength {
			t.Fatalf("secret %q has length %d", secret, len(secret))
		}
		for _, r := range secret {
			switch r {
			case 'I', 'L', 'O', 'U':
				t.Fatalf("secret %q contains an ambiguous character", secret)
			}
		}
	}
}
