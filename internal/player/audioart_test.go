package player

import (
	"reflect"
	"strings"
	"testing"
)

// The rule these tests exist for was established against a real mpv (v0.41):
// with --cover-art-file supplied, a normal h264 file reports video-codec
// "Motion JPEG" and the real video track is deselected. Cover art is therefore
// only ever correct for an item that genuinely has no video.

func TestVideoItemGetsNoCoverArt(t *testing.T) {
	a := audioPresentation{audioOnly: false, coverArtURL: "http://127.0.0.1:1980/art.png"}
	if got := a.args(); got != nil {
		t.Fatalf("video item contributed mpv args %q; a cover art flag would make the film play as a still image", got)
	}
	if got := a.coverArtValue(); len(got) != 0 {
		t.Fatalf("coverArtValue = %q, want empty for a video item", got)
	}
}

// A leftover cover-art-files value hijacks the *next* item, so switching from a
// song to a film must actively clear it rather than simply not setting it.
func TestSwitchingFromAudioToVideoClearsCoverArt(t *testing.T) {
	song := audioPresentation{audioOnly: true, coverArtURL: "http://127.0.0.1:1980/art.png"}
	if len(song.coverArtValue()) != 1 {
		t.Fatalf("song should carry exactly one cover art entry, got %q", song.coverArtValue())
	}
	film := audioPresentation{audioOnly: false}
	if got := film.coverArtValue(); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("coverArtValue = %#v, want an empty list that overwrites the song's artwork", got)
	}
}

func TestAudioItemShowsArtwork(t *testing.T) {
	a := audioPresentation{audioOnly: true, coverArtURL: "http://127.0.0.1:1980/art.png"}
	args := strings.Join(a.args(), " ")
	if !strings.Contains(args, "--cover-art-file=http://127.0.0.1:1980/art.png") {
		t.Fatalf("args = %q, want the artwork passed to mpv", args)
	}
}

// Artwork can be missing (a sender that sent no album art, and a fallback URL
// that could not be built). The window itself is guaranteed by the spawn flags
// (see TestSpawnArgsForceAWindowSoAudioAndTransitionsAreVisible), so the
// failure mode here is a black screen rather than "nothing on screen".
func TestAudioWithoutArtworkAddsNoFlags(t *testing.T) {
	a := audioPresentation{audioOnly: true}
	args := strings.Join(a.args(), " ")
	if strings.Contains(args, "--cover-art-file") {
		t.Fatalf("args = %q, want no cover art flag when there is no artwork", args)
	}
	if got := a.coverArtValue(); len(got) != 0 {
		t.Fatalf("coverArtValue = %q, want empty", got)
	}
}
