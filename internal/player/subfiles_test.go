package player

import (
	"reflect"
	"strings"
	"testing"
)

// A reused mpv keeps sub-files from the previous play unless it is overwritten,
// so a film without sidecars must send an empty list, not skip the property.
func TestSubtitleFilesClearedForItemWithoutSidecars(t *testing.T) {
	if got := subtitleFileValue(nil); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("subtitleFileValue(nil) = %#v, want an empty list", got)
	}
	if got := subtitleFileArgs(nil); got != nil {
		t.Fatalf("subtitleFileArgs(nil) = %q, want no flags", got)
	}
}

// --sub-files would split a URL on ':' (the macOS path-list separator); one
// --sub-file per URL keeps each URL whole.
func TestSubtitleFileArgsOnePerURL(t *testing.T) {
	urls := []string{"http://127.0.0.1:1980/api/files/stream?path=a.srt", "http://127.0.0.1:1980/api/files/stream?path=a.zh.ass"}
	got := subtitleFileArgs(urls)
	if len(got) != 2 || got[0] != "--sub-file="+urls[0] || got[1] != "--sub-file="+urls[1] {
		t.Fatalf("subtitleFileArgs = %q", got)
	}
}

func TestInitialArgsKeepPicturesOnScreen(t *testing.T) {
	args := strings.Join(initialMPVArgs("x", "sock"), " ")
	if !strings.Contains(args, "--image-display-duration=inf") {
		t.Fatalf("a picture would vanish after mpv's 1s default: %s", args)
	}
}
