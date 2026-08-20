package player

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
)

// ipcRecorder collects the command frames a reused process would receive.
type ipcRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *ipcRecorder) contains(fragment string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.lines {
		if strings.Contains(line, fragment) {
			return true
		}
	}
	return false
}

func recordingPlayer(t *testing.T) (*Player, *ipcRecorder) {
	t.Helper()
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })

	rec := &ipcRecorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(server)
		for scanner.Scan() {
			rec.mu.Lock()
			rec.lines = append(rec.lines, scanner.Text())
			rec.mu.Unlock()
		}
	}()
	return &Player{running: true, conn: client}, rec
}

// The loop state is a property of the title on screen, so a reused mpv must be
// told what it is for every load. Sending it only for looping titles would let
// a repeating movie trap the next episode in an endless replay.
func TestReusedProcessAlwaysSetsTheLoopState(t *testing.T) {
	p, rec := recordingPlayer(t)
	p.Play("http://example/movie.mkv", PlayOptions{ServerID: "srv", ItemID: "movie", LoopFile: true})
	if !rec.contains(`"loop-file","inf"`) {
		t.Fatalf("looping play did not set loop-file=inf: %v", rec.lines)
	}

	p2, rec2 := recordingPlayer(t)
	p2.Play("http://example/e01.mkv", PlayOptions{ServerID: "srv", ItemID: "e01"})
	if !rec2.contains(`"loop-file","no"`) {
		t.Fatalf("ordinary play did not clear loop-file: %v", rec2.lines)
	}
}

func TestLoopFileValue(t *testing.T) {
	if loopFileValue(true) != "inf" || loopFileValue(false) != "no" {
		t.Fatal("loop-file values must be mpv's own inf/no")
	}
}
