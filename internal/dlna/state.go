package dlna

import (
	"sync"
)

// rendererState is the authoritative AVTransport / RenderingControl snapshot
// for SOAP replies and GENA LastChange. Control points and the local player
// both update this table; GENA publishes only when an observed field changes.
type rendererState struct {
	mu sync.Mutex

	TransportState  string // PLAYING | PAUSED_PLAYBACK | STOPPED | TRANSITIONING | NO_MEDIA_PRESENT
	TransportStatus string // OK | ERROR_OCCURRED
	URI             string
	URIMeta         string
	Title           string
	Duration        string // HH:MM:SS
	RelTime         string
	Volume          int
	Mute            bool
	PlayMode        string
	Speed           string
}

func newRendererState() *rendererState {
	return &rendererState{
		TransportState:  "NO_MEDIA_PRESENT",
		TransportStatus: "OK",
		Duration:        "00:00:00",
		RelTime:         "00:00:00",
		Volume:          100,
		Mute:            false,
		PlayMode:        "NORMAL",
		Speed:           "1",
	}
}

// snapshot returns a copy for event/SOAP builders without holding the lock.
func (s *rendererState) snapshot() rendererState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rendererState{
		TransportState:  s.TransportState,
		TransportStatus: s.TransportStatus,
		URI:             s.URI,
		URIMeta:         s.URIMeta,
		Title:           s.Title,
		Duration:        s.Duration,
		RelTime:         s.RelTime,
		Volume:          s.Volume,
		Mute:            s.Mute,
		PlayMode:        s.PlayMode,
		Speed:           s.Speed,
	}
}

func (s *rendererState) setTransport(state, status string) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state != "" && s.TransportState != state {
		s.TransportState = state
		changed = true
	}
	if status != "" && s.TransportStatus != status {
		s.TransportStatus = status
		changed = true
	}
	return changed
}

func (s *rendererState) setURI(uri, meta, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.URI = uri
	s.URIMeta = meta
	if title != "" {
		s.Title = title
	}
	s.RelTime = "00:00:00"
	s.Duration = "00:00:00"
}

func (s *rendererState) setDuration(d string) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d == "" || s.Duration == d {
		return false
	}
	s.Duration = d
	return true
}

func (s *rendererState) setRelTime(t string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RelTime = t
}

func (s *rendererState) setVolume(v int) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Volume == v {
		return false
	}
	s.Volume = v
	return true
}

func (s *rendererState) setMute(m bool) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mute == m {
		return false
	}
	s.Mute = m
	return true
}

func (s *rendererState) setPlayMode(mode string) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "" || s.PlayMode == mode {
		return false
	}
	s.PlayMode = mode
	return true
}

func (s *rendererState) transportActions() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return transportActions(s.TransportState)
}
