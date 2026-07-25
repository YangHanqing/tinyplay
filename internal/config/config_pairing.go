package config

import "time"

// Paired-device storage for the LAN remote's token auth. Only SHA-256 hashes of
// the issued tokens are written, so someone reading config.json cannot use it to
// control the computer — unlike the media-server passwords in the same file,
// these are verifiable without being recoverable.

// PairedDevice is one phone that completed pairing.
type PairedDevice struct {
	// TokenHash is the hex SHA-256 of the bearer token handed to the device.
	TokenHash  string `json:"token_hash"`
	Label      string `json:"label,omitempty"`
	PairedAt   string `json:"paired_at,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// MaxPairedDevices bounds the list so a persistent pairing loop cannot grow
// config.json without limit. Households pair a handful of phones; the oldest
// entry is dropped once the ceiling is reached.
const MaxPairedDevices = 32

// StaleDeviceTTL retires a token nobody has used in three months. Browsers
// (Safari's ITP in particular) periodically wipe a phone's stored token, so a
// device that keeps coming back regularly just re-pairs under a brand new
// token instead of ever presenting this one again. Without a TTL that
// abandoned entry sits in the list forever and "paired devices" stops
// reflecting real devices in use.
const StaleDeviceTTL = 90 * 24 * time.Hour

// touchThrottle caps how often a verified request rewrites config.json just to
// bump a timestamp. TouchPairedToken runs on every authenticated request, and
// a three-month retirement window does not need better than day-level
// precision.
const touchThrottle = 24 * time.Hour

// AddPairedToken records a newly issued token hash, first retiring any device
// that has gone unused for StaleDeviceTTL. A new pairing is the natural point
// to sweep: it's the moment the list would otherwise grow.
func AddPairedToken(hash, label string) {
	if hash == "" {
		return
	}
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339)
	patch(func(cfg *Config) {
		cfg.PairedDevices = pruneStaleDevices(cfg.PairedDevices, now)
		cfg.PairedDevices = append(cfg.PairedDevices, PairedDevice{
			TokenHash: hash, Label: label, PairedAt: stamp, LastUsedAt: stamp,
		})
		if overflow := len(cfg.PairedDevices) - MaxPairedDevices; overflow > 0 {
			cfg.PairedDevices = cfg.PairedDevices[overflow:]
		}
	})
}

// TouchPairedToken records that hash just authenticated a request, so it
// survives the next stale-device sweep in AddPairedToken. Writes are
// throttled to once per touchThrottle window per device via the cheap
// read-only check below, so this does not turn every API call into a disk
// write.
func TouchPairedToken(hash string) {
	if hash == "" {
		return
	}
	for _, device := range Load().PairedDevices {
		if device.TokenHash != hash {
			continue
		}
		if last, err := time.Parse(time.RFC3339, device.LastUsedAt); err == nil && time.Since(last) < touchThrottle {
			return
		}
		break
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	patch(func(cfg *Config) {
		for i := range cfg.PairedDevices {
			if cfg.PairedDevices[i].TokenHash == hash {
				cfg.PairedDevices[i].LastUsedAt = stamp
				return
			}
		}
	})
}

// pruneStaleDevices drops devices unused for StaleDeviceTTL. A device that
// was paired but never actually verified a request falls back to PairedAt, so
// a token that was issued and never used once still retires on schedule.
func pruneStaleDevices(devices []PairedDevice, now time.Time) []PairedDevice {
	kept := devices[:0]
	for _, device := range devices {
		last := device.LastUsedAt
		if last == "" {
			last = device.PairedAt
		}
		if lastAt, err := time.Parse(time.RFC3339, last); err == nil && now.Sub(lastAt) > StaleDeviceTTL {
			continue
		}
		kept = append(kept, device)
	}
	return kept
}

// PairedTokenHashes returns every valid token hash.
func PairedTokenHashes() []string {
	cfg := Load()
	hashes := make([]string, 0, len(cfg.PairedDevices))
	for _, device := range cfg.PairedDevices {
		if device.TokenHash != "" {
			hashes = append(hashes, device.TokenHash)
		}
	}
	return hashes
}

// PairedDevices lists paired phones for the computer's own window.
func PairedDevices() []PairedDevice {
	return append([]PairedDevice(nil), Load().PairedDevices...)
}

// RevokeAllPairedTokens unpairs every device. It is deliberately all-or-nothing
// and deliberately reachable only from the computer itself.
func RevokeAllPairedTokens() {
	patch(func(cfg *Config) { cfg.PairedDevices = nil })
}
