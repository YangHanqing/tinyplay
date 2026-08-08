package server

import (
	"testing"

	"tvremote/internal/player"
)

func TestApplyFileAudioPresentation(t *testing.T) {
	var audioOpts player.PlayOptions
	applyFileAudioPresentation(&audioOpts, "Album/01 Intro.mp3", 1980)
	if !audioOpts.AudioOnly {
		t.Fatal("mp3 must set AudioOnly")
	}
	want := player.FallbackCoverArtURL(1980)
	if audioOpts.CoverArtURL != want {
		t.Fatalf("CoverArtURL = %q, want %q", audioOpts.CoverArtURL, want)
	}

	var videoOpts player.PlayOptions
	applyFileAudioPresentation(&videoOpts, "Show/S01E01.mkv", 1980)
	if videoOpts.AudioOnly || videoOpts.CoverArtURL != "" {
		t.Fatalf("video must leave audio presentation zero: %+v", videoOpts)
	}

	var unknownOpts player.PlayOptions
	applyFileAudioPresentation(&unknownOpts, "notes.txt", 1980)
	if unknownOpts.AudioOnly || unknownOpts.CoverArtURL != "" {
		t.Fatalf("uncertain path must fail closed: %+v", unknownOpts)
	}

	// Invalid port still sets AudioOnly; empty cover art is fine (black window).
	var noPort player.PlayOptions
	applyFileAudioPresentation(&noPort, "track.flac", 0)
	if !noPort.AudioOnly || noPort.CoverArtURL != "" {
		t.Fatalf("zero port: %+v", noPort)
	}
}
