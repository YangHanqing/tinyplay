package config

import (
	"testing"
	"time"
)

func TestAddPairedTokenSetsLastUsedAt(t *testing.T) {
	useTempData(t)
	AddPairedToken("hash1", "iPad")
	devices := PairedDevices()
	if len(devices) != 1 || devices[0].LastUsedAt == "" {
		t.Fatalf("expected LastUsedAt set on creation, got %+v", devices)
	}
}

// A device a browser abandoned (Safari's ITP wiped its stored token, or the
// phone is simply gone) must eventually drop out of the list on its own,
// rather than sitting there forever inflating the paired-device count.
func TestAddPairedTokenPrunesStaleDevices(t *testing.T) {
	useTempData(t)
	staleStamp := time.Now().Add(-(StaleDeviceTTL + 24*time.Hour)).Format(time.RFC3339)
	freshStamp := time.Now().Add(-time.Hour).Format(time.RFC3339)
	patch(func(cfg *Config) {
		cfg.PairedDevices = []PairedDevice{
			{TokenHash: "stale", PairedAt: staleStamp, LastUsedAt: staleStamp},
			{TokenHash: "fresh", PairedAt: freshStamp, LastUsedAt: freshStamp},
		}
	})

	AddPairedToken("new-device", "iPhone")

	want := map[string]bool{"fresh": true, "new-device": true}
	hashes := PairedTokenHashes()
	if len(hashes) != len(want) {
		t.Fatalf("expected %d devices, got %v", len(want), hashes)
	}
	for _, h := range hashes {
		if !want[h] {
			t.Fatalf("unexpected hash %q survived pruning: %v", h, hashes)
		}
	}
}

// A legacy or corrupt entry with no parseable timestamp must not be treated
// as stale by default — that would silently unpair a real device.
func TestAddPairedTokenKeepsUnparseableTimestamps(t *testing.T) {
	useTempData(t)
	patch(func(cfg *Config) {
		cfg.PairedDevices = []PairedDevice{{TokenHash: "legacy"}}
	})

	AddPairedToken("new-device", "")

	hashes := PairedTokenHashes()
	if len(hashes) != 2 {
		t.Fatalf("expected the undated legacy entry to survive, got %v", hashes)
	}
}

func TestTouchPairedTokenThrottlesWrites(t *testing.T) {
	useTempData(t)
	old := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	patch(func(cfg *Config) {
		cfg.PairedDevices = []PairedDevice{{TokenHash: "phone", PairedAt: old, LastUsedAt: old}}
	})

	TouchPairedToken("phone")
	touchedOnce := PairedDevices()[0].LastUsedAt
	if touchedOnce == old {
		t.Fatal("touch did not refresh a last-used timestamp outside the throttle window")
	}

	TouchPairedToken("phone")
	touchedTwice := PairedDevices()[0].LastUsedAt
	if touchedTwice != touchedOnce {
		t.Fatal("touch within the throttle window rewrote the timestamp")
	}
}
