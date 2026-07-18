package player

import (
	"testing"
	"time"
)

// A play that never produced a time-pos event (mpv failed to open the URL, or
// was replaced while still loading) must stop-report the resume point it was
// about to honour — reporting 0 would erase the server/local bookmark.
func TestStopReportKeepsResumePointWhenNoPositionObserved(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	reports := make(chan int64, 1)
	p := &Player{running: true}
	p.StopReporter = func(serverID, itemID, sessionID string, posTicks int64, duration float64, mediaSourceID string) {
		if itemID == "item-a" {
			reports <- posTicks
		}
	}
	p.Play("http://example/a", PlayOptions{ServerID: "srv", ItemID: "item-a", StartSeconds: 1800})
	p.Play("http://example/b", PlayOptions{ServerID: "srv", ItemID: "item-b"})
	select {
	case pos := <-reports:
		if want := int64(1800 * 1e7); pos != want {
			t.Fatalf("stop report position = %d ticks, want %d", pos, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no stop report for the replaced item")
	}
}
