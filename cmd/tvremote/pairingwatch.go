package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Watches for a consent request that nobody can answer.
//
// The intro window polls /desktop/pairing itself and paints the approve/deny
// card, but only while it is open — and this app lives in the tray, so most of
// the time it is not. A phone that typed the LAN address by hand would then show
// four digits and wait out the full two minutes while the computer said nothing,
// which reads as the app being broken. Raising the window is the whole fix: the
// card the window already knows how to draw becomes visible, and the person sees
// what their phone is asking for.
//
// Deliberately does not raise repeatedly for the same request: someone who
// dismisses the window has answered in their own way, and a window that keeps
// clawing its way back to the front is worse than a missed pairing.
//
// The tvOS branch needs no equivalent — its prompt owns a UIWindow above
// everything, so it cannot be hidden in the first place. The macOS shell is
// Swift and carries its own copy (PairingRequestWatcher in macos/Sources/main.swift);
// this file is untagged rather than windows-only so the logic stays testable on
// the machine it is usually written on.

// pairingPollIntervalForTest matches the intro window's own cadence, so a shell
// that is watching costs no more than a window that is open. It is a variable
// only so the test can drive the same loop without waiting real seconds.
var pairingPollIntervalForTest = 2 * time.Second

// watchPairingRequests calls raise whenever a new consent request appears.
// It runs until ctx is cancelled and is safe to start before the core is
// listening: failures are simply retried on the next tick.
func watchPairingRequests(ctx context.Context, coreURL string, raise func()) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(pairingPollIntervalForTest)
	defer ticker.Stop()

	var lastRaised string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		nonce := pendingPairingNonce(ctx, client, coreURL)
		if nonce == "" {
			// Cleared, so the next request is a new event and may raise again.
			lastRaised = ""
			continue
		}
		if nonce == lastRaised {
			continue
		}
		lastRaised = nonce
		raise()
	}
}

func pendingPairingNonce(ctx context.Context, client *http.Client, coreURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreURL+"/desktop/pairing", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Cache-Control", "no-store")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var payload struct {
		Pending *struct {
			Nonce string `json:"nonce"`
		} `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || payload.Pending == nil {
		return ""
	}
	return payload.Pending.Nonce
}
