package auth

import (
	"log"

	"tvremote/internal/config"
)

// configStore keeps issued tokens in the same per-user config file as the rest
// of the desktop state. TouchToken runs on every API request but is throttled
// inside config.TouchPairedToken, so it does not turn every request into a
// config.json write.
type configStore struct{}

// AddToken verifies the write landed. A token that never reached disk is worse
// than a refused pairing: the phone stores it, believes it is paired, and is
// then rejected on its very next request — which looks like pairing being
// broken rather than like config.json being unwritable. The read-back is one
// file read per pairing, and it turns a silent disk problem into a log line.
func (configStore) AddToken(hash, label string) {
	config.AddPairedToken(hash, label)
	for _, stored := range config.PairedTokenHashes() {
		if stored == hash {
			return
		}
	}
	log.Printf("auth: paired device was not persisted to %s; the phone will be asked to pair again", config.ConfigFile())
}

func (configStore) TokenHashes() []string  { return config.PairedTokenHashes() }
func (configStore) RevokeAllTokens()       { config.RevokeAllPairedTokens() }
func (configStore) TouchToken(hash string) { config.TouchPairedToken(hash) }

// Default is the process-wide pairing manager. Its QR secret lives only in
// memory, so restarting TinyPlay invalidates every QR code it ever displayed
// while leaving already-paired phones working.
var Default = New(configStore{})
