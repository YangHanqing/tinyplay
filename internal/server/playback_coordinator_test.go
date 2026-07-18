package server

import "testing"

func TestPlaybackGenerationOnlyAllowsNewestRequest(t *testing.T) {
	s := &Server{}
	first := s.beginPlay()
	second := s.beginPlay()
	if s.isCurrentPlay(first) {
		t.Fatal("older play request remained current after a newer request arrived")
	}
	if !s.isCurrentPlay(second) {
		t.Fatal("newest play request was not current")
	}
}

func TestStopInvalidatesInFlightPlay(t *testing.T) {
	s := &Server{}
	generation := s.beginPlay()
	s.invalidatePlay()
	if s.isCurrentPlay(generation) {
		t.Fatal("stop did not invalidate an in-flight play request")
	}
}
