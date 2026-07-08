package player

import (
	"strings"
	"testing"
	"time"
)

func TestRenderBackdropOverlayFallsBackToDimBlack(t *testing.T) {
	got := renderBackdropOverlay(nil, 2, 2)
	if len(got) != 16 {
		t.Fatalf("got %d bytes, want 16", len(got))
	}
	for i := 0; i < len(got); i += 4 {
		if got[i] != 0 || got[i+1] != 0 || got[i+2] != 0 || got[i+3] != 168 {
			t.Fatalf("pixel %d = BGRA(%d,%d,%d,%d), want dim black", i/4, got[i], got[i+1], got[i+2], got[i+3])
		}
	}
}

func TestScreensaverASSUsesSeriesAndEpisodeLabel(t *testing.T) {
	ctx := PlayContext{
		Title:        "Episode Title",
		SeriesTitle:  "Series Name",
		EpisodeLabel: "S01E02",
	}
	got := screensaverASS(ctx, 1920, 1080, time.Date(2026, 6, 20, 21, 3, 0, 0, time.Local))
	if !strings.Contains(got, `\an3\pos(`) {
		t.Fatalf("expected bottom-right ASS positioning, got %q", got)
	}
	if !strings.Contains(got, "21:03") {
		t.Fatalf("expected clock text, got %q", got)
	}
	if !strings.Contains(got, "Series Name  S01E02") {
		t.Fatalf("expected series episode label, got %q", got)
	}
}
