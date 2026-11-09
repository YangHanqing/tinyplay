package filesource

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"tvremote/internal/config"
)

func TestMatchSidecarSubtitlesPairsByStem(t *testing.T) {
	got := MatchSidecarSubtitles("Movie.2024.mkv", []string{
		"Movie.2024.zh.srt", "movie.2024.SRT", "Movie.2024.chs.ass",
		"Other.srt", "Movie.2024.nfo", "Movie.20245.srt", "Movie.2024.mkv",
	})
	want := []string{"movie.2024.SRT", "Movie.2024.chs.ass", "Movie.2024.zh.srt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q (exact stem first, tagged after, nothing unrelated)", got, want)
	}
}

func TestMatchSidecarSubtitlesIsCapped(t *testing.T) {
	var names []string
	for i := 0; i < MaxSidecarSubtitles+5; i++ {
		names = append(names, "Ep01."+string(rune('a'+i))+".srt")
	}
	if got := MatchSidecarSubtitles("Ep01.mp4", names); len(got) != MaxSidecarSubtitles {
		t.Fatalf("got %d subtitles, want the cap of %d", len(got), MaxSidecarSubtitles)
	}
}

func TestLocalListingShowsImagesHidesSubtitles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Movie.mkv", "Movie.srt", "Movie.zh.ass", "photo.JPG", "notes.nfo"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := New(&config.Server{Name: "t", Type: "local", RootPath: dir})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]Entry{}
	for _, e := range listing.Entries {
		kinds[e.Name] = e
	}
	if e, ok := kinds["photo.JPG"]; !ok || !e.IsImage || e.IsVideo || e.IsAudio {
		t.Fatalf("photo.JPG should be listed as an image only, got %+v (present=%v)", e, ok)
	}
	for _, hidden := range []string{"Movie.srt", "Movie.zh.ass", "notes.nfo"} {
		if _, ok := kinds[hidden]; ok {
			t.Fatalf("%s must stay hidden from the folder browser", hidden)
		}
	}

	subs := c.SidecarSubtitles("Movie.mkv")
	if want := []string{"Movie.srt", "Movie.zh.ass"}; !reflect.DeepEqual(subs, want) {
		t.Fatalf("SidecarSubtitles = %q, want %q", subs, want)
	}
}

func TestImagesNeverAutoplay(t *testing.T) {
	entries := []Entry{
		{Name: "a.jpg", Path: "a.jpg", IsImage: true},
		{Name: "b.jpg", Path: "b.jpg", IsImage: true},
	}
	if _, ok := NextVideo(entries, "a.jpg"); ok {
		t.Fatal("a picture must not chain into the next file")
	}
}
