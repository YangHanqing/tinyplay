package filesource

import (
	"fmt"
	"testing"
)

func video(name string) Entry {
	return Entry{Name: name, Path: name, IsDir: false, IsVideo: true}
}

func videoIn(dir, name string) Entry {
	p := dir + "/" + name
	return Entry{Name: name, Path: p, IsDir: false, IsVideo: true}
}

func TestNextVideoSxxExxProgression(t *testing.T) {
	entries := []Entry{
		video("Show.S01E01.1080p.mkv"),
		video("Show.S01E02.1080p.mkv"),
		video("Show.S01E03.1080p.mkv"),
	}
	next, ok := NextVideo(entries, "Show.S01E02.1080p.mkv")
	if !ok || next.Name != "Show.S01E03.1080p.mkv" {
		t.Fatalf("got %+v ok=%v, want S01E03", next, ok)
	}
	_, ok = NextVideo(entries, "Show.S01E03.1080p.mkv")
	if ok {
		t.Fatal("expected end of folder after last episode")
	}
}

func TestNextVideoNaturalSortNotLexicographic(t *testing.T) {
	// Lexicographic order would put EP10 before EP2; natural sort must not.
	entries := []Entry{
		video("Show EP10.mkv"),
		video("Show EP2.mkv"),
		video("Show EP1.mkv"),
	}
	next, ok := NextVideo(entries, "Show EP1.mkv")
	if !ok || next.Name != "Show EP2.mkv" {
		t.Fatalf("after EP1 got %+v ok=%v, want EP2", next, ok)
	}
	next, ok = NextVideo(entries, "Show EP2.mkv")
	if !ok || next.Name != "Show EP10.mkv" {
		t.Fatalf("after EP2 got %+v ok=%v, want EP10", next, ok)
	}
}

func TestNextVideoRealWorldNames(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
		current string
		want    string
		wantOK  bool
	}{
		{
			name: "SxxExx with release tags",
			entries: []Entry{
				video("Show.S01E01.1080p.mkv"),
				video("Show.S01E02.1080p.mkv"),
			},
			current: "Show.S01E01.1080p.mkv",
			want:    "Show.S01E02.1080p.mkv",
			wantOK:  true,
		},
		{
			name: "fansub bracket style",
			entries: []Entry{
				video("[字幕组] 番名 - 01 [1080p].mkv"),
				video("[字幕组] 番名 - 02 [1080p].mkv"),
				video("[字幕组] 番名 - 03 [1080p].mkv"),
			},
			current: "[字幕组] 番名 - 02 [1080p].mkv",
			want:    "[字幕组] 番名 - 03 [1080p].mkv",
			wantOK:  true,
		},
		{
			name: "Chinese 第N集",
			entries: []Entry{
				video("第01集.mp4"),
				video("第02集.mp4"),
				video("第03集.mp4"),
			},
			current: "第02集.mp4",
			want:    "第03集.mp4",
			wantOK:  true,
		},
		{
			name: "Chinese 第N话",
			entries: []Entry{
				video("番名 第1话.mkv"),
				video("番名 第2话.mkv"),
			},
			current: "番名 第1话.mkv",
			want:    "番名 第2话.mkv",
			wantOK:  true,
		},
		{
			name: "1x02 form",
			entries: []Entry{
				video("Show 1x01.mkv"),
				video("Show 1x02.mkv"),
			},
			current: "Show 1x01.mkv",
			want:    "Show 1x02.mkv",
			wantOK:  true,
		},
		{
			name: "square-bracket episode",
			entries: []Entry{
				video("Show Name [01].mkv"),
				video("Show Name [02].mkv"),
			},
			current: "Show Name [01].mkv",
			want:    "Show Name [02].mkv",
			wantOK:  true,
		},
		{
			name: "arbitrary movie names follow filename order",
			entries: []Entry{
				video("Movie (2019).mkv"),
				video("Other Film (2020).mkv"),
				video("Third Title (2021).mkv"),
			},
			current: "Movie (2019).mkv",
			want:    "Other Film (2020).mkv",
			wantOK:  true,
		},
		{
			name: "two unrelated names still follow filename order",
			entries: []Entry{
				video("Inception (2010).mkv"),
				video("Interstellar (2014).mkv"),
			},
			current: "Inception (2010).mkv",
			want:    "Interstellar (2014).mkv",
			wantOK:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, ok := NextVideo(tc.entries, tc.current)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v (next=%+v)", ok, tc.wantOK, next)
			}
			if tc.wantOK && next.Name != tc.want {
				t.Fatalf("next=%q want %q", next.Name, tc.want)
			}
		})
	}
}

func TestNextVideoEpisodeGapAllowed(t *testing.T) {
	entries := []Entry{
		video("Show.S01E02.mkv"),
		video("Show.S01E05.mkv"),
	}
	next, ok := NextVideo(entries, "Show.S01E02.mkv")
	if !ok || next.Name != "Show.S01E05.mkv" {
		t.Fatalf("gap E02→E05 should be allowed, got %+v ok=%v", next, ok)
	}
	gap, from, to := EpisodeGap("Show.S01E02.mkv", next.Name)
	if !gap || from != 2 || to != 5 {
		t.Fatalf("EpisodeGap = %v %d %d, want true 2 5", gap, from, to)
	}
}

func TestNextVideoUsesFilenameOrderAcrossSeasonLabels(t *testing.T) {
	entries := []Entry{
		video("Show.S01E12.mkv"),
		video("Show.S02E01.mkv"),
	}
	next, ok := NextVideo(entries, "Show.S01E12.mkv")
	if !ok || next.Name != "Show.S02E01.mkv" {
		t.Fatalf("filename order should continue to S02E01, got %+v ok=%v", next, ok)
	}
}

func TestNextVideoSkipsExtrasAndISO(t *testing.T) {
	entries := []Entry{
		video("Show.S01E01.mkv"),
		video("Show.S01E01-sample.mkv"),
		video("Show.S01E01-trailer.mkv"),
		video("Show NCOP.mkv"),
		video("Show OP1.mkv"),
		video("Show ED.mkv"),
		video("Show ED2.mkv"),
		video("Show.preview.mkv"),
		video("Show 预告.mkv"),
		video("Show 花絮.mkv"),
		video("Show 特典.mkv"),
		video("Show.S01E02.mkv"),
		video("Disc.iso"), // playable manually, never autoplay-chained
		{Name: "notes.nfo", Path: "notes.nfo", IsVideo: false},
		{Name: "poster.jpg", Path: "poster.jpg", IsVideo: false},
		{Name: "subs.srt", Path: "subs.srt", IsVideo: false},
		{Name: "Season 2", Path: "Season 2", IsDir: true},
	}
	next, ok := NextVideo(entries, "Show.S01E01.mkv")
	if !ok || next.Name != "Show.S01E02.mkv" {
		t.Fatalf("should skip extras/iso and land on E02, got %+v ok=%v", next, ok)
	}
	_, ok = NextVideo(entries, "Disc.iso")
	if ok {
		t.Fatal("iso must not start an autoplay chain")
	}
}

func TestNextVideoArbitraryNamesNeedNoSeriesHeuristic(t *testing.T) {
	entries := []Entry{
		video("MyShow_part_alpha.mkv"),
		video("MyShow_part_beta.mkv"),
		video("MyShow_part_gamma.mkv"),
	}
	next, ok := NextVideo(entries, "MyShow_part_alpha.mkv")
	if !ok || next.Name != "MyShow_part_beta.mkv" {
		t.Fatalf("arbitrary names did not follow filename order: %+v ok=%v", next, ok)
	}

	two := []Entry{
		video("MyShow_part_alpha.mkv"),
		video("MyShow_part_beta.mkv"),
	}
	next, ok = NextVideo(two, "MyShow_part_alpha.mkv")
	if !ok || next.Name != "MyShow_part_beta.mkv" {
		t.Fatalf("two unnumbered videos should chain by filename: %+v ok=%v", next, ok)
	}

	mixed := []Entry{
		video("Alpha_one.mkv"),
		video("Beta_two.mkv"),
		video("Gamma_three.mkv"),
	}
	next, ok = NextVideo(mixed, "Alpha_one.mkv")
	if !ok || next.Name != "Beta_two.mkv" {
		t.Fatalf("unrelated names should still chain by filename: %+v ok=%v", next, ok)
	}
}

func TestNextVideoNeverCrossesDirectories(t *testing.T) {
	entries := []Entry{
		videoIn("Show", "S01E01.mkv"),
		videoIn("Show", "S01E02.mkv"),
		// A ListDir of "Show" would not include nested entries; this just
		// confirms path matching uses the full relative path.
		videoIn("Show/Extra", "S01E03.mkv"),
	}
	// When listing is only the parent of the finished file, NextVideo sees
	// only same-folder entries. Parent-dir filtering is the caller's job;
	// here the Extra path is a different path and not "next" after E01 when
	// E02 is present.
	next, ok := NextVideo(entries, "Show/S01E01.mkv")
	if !ok || next.Path != "Show/S01E02.mkv" {
		t.Fatalf("got %+v ok=%v", next, ok)
	}
}

func TestNextVideoPathologicalCap(t *testing.T) {
	entries := make([]Entry, MaxAutoplayDirEntries+1)
	for i := range entries {
		name := fmt.Sprintf("Show.S01E%03d.mkv", i+1)
		entries[i] = video(name)
	}
	_, ok := NextVideo(entries, entries[0].Path)
	if ok {
		t.Fatal("must refuse autoplay above MaxAutoplayDirEntries")
	}
}

func TestNextVideoMixedNamingStillUsesFilenameOrder(t *testing.T) {
	entries := []Entry{
		video("Show.S01E01.mkv"),
		video("Show between episodes.mkv"),
		video("Show.S01E02.mkv"),
	}
	next, ok := NextVideo(entries, "Show.S01E01.mkv")
	if !ok || next.Name != "Show.S01E02.mkv" {
		t.Fatalf("mixed naming should use natural filename order, got %+v ok=%v", next, ok)
	}
}

func TestParseEpisodeRefPriority(t *testing.T) {
	// SxxExx wins over trailing numbers / brackets that may also appear.
	ref := parseEpisodeRef("Show.S01E02.1080p.mkv")
	if !ref.OK || ref.Season != 1 || ref.Episode != 2 {
		t.Fatalf("SxxExx parse = %+v", ref)
	}
	ref = parseEpisodeRef("[字幕组] 番名 - 02 [1080p].mkv")
	if !ref.OK || ref.Episode != 2 {
		// "02" may come from the bare " - 02 " via E-style or trailing / brackets.
		// Bracket [1080p] is not digits-only; trailing bare after strip is not
		// pure digits. EP-less " - 02 " should still yield episode 2.
		t.Fatalf("fansub 02 parse = %+v", ref)
	}
	ref = parseEpisodeRef("第02集.mp4")
	if !ref.OK || ref.Episode != 2 {
		t.Fatalf("Chinese parse = %+v", ref)
	}
	// Years must not become episode numbers (4 digits).
	ref = parseEpisodeRef("Movie (2019).mkv")
	if ref.OK {
		t.Fatalf("year must not parse as episode, got %+v", ref)
	}
}

func TestNaturalLessDigitRuns(t *testing.T) {
	if !naturalLess("EP2.mkv", "EP10.mkv") {
		t.Fatal("EP2 should sort before EP10")
	}
	if naturalLess("EP10.mkv", "EP2.mkv") {
		t.Fatal("EP10 should not sort before EP2")
	}
	if !naturalLess("show02a.mkv", "show02b.mkv") {
		t.Fatal("equal numeric runs fall through to text")
	}
}

func TestTitleWithoutExtensionAndParentDir(t *testing.T) {
	if got := TitleWithoutExtension("Show.S01E02.1080p.mkv"); got != "Show.S01E02.1080p" {
		t.Fatalf("title = %q", got)
	}
	if got := ParentDir("Season 1/Show.S01E02.mkv"); got != "Season 1" {
		t.Fatalf("parent = %q", got)
	}
	if got := ParentDir("Show.S01E02.mkv"); got != "" {
		t.Fatalf("root parent = %q, want empty", got)
	}
}

func TestIsAutoplayExtra(t *testing.T) {
	extras := []string{
		"Show-sample.mkv", "trailer.mp4", "preview clip.mkv",
		"Show NCOP.mkv", "Show.OP.mkv", "Show.OP01.mkv",
		"Show.ED.mkv", "Show.ED2.mkv", "番名 预告.mkv", "番名 花絮.mkv", "番名 特典.mkv",
	}
	for _, n := range extras {
		if !isAutoplayExtra(n) {
			t.Errorf("%q should be extra", n)
		}
	}
	if isAutoplayExtra("Show.S01E02.mkv") {
		t.Error("normal episode must not be extra")
	}
}
