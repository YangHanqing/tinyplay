package auth

import "tvremote/internal/config"

// configStore keeps issued tokens in the same per-user config file as the rest
// of the desktop state. TouchToken runs on every API request but is throttled
// inside config.TouchPairedToken, so it does not turn every request into a
// config.json write.
type configStore struct{}

func (configStore) AddToken(hash, label string) { config.AddPairedToken(hash, label) }
func (configStore) TokenHashes() []string       { return config.PairedTokenHashes() }
func (configStore) RevokeAllTokens()            { config.RevokeAllPairedTokens() }
func (configStore) TouchToken(hash string)      { config.TouchPairedToken(hash) }

// Default is the process-wide pairing manager. Its QR secret lives only in
// memory, so restarting TinyPlay invalidates every QR code it ever displayed
// while leaving already-paired phones working.
var Default = New(configStore{})
