package filesource

import (
	"path"
	"sort"
	"strings"
)

// MaxSidecarSubtitles bounds how many external subtitle files are handed to
// mpv for one video, so a folder of hundreds of .srt files cannot turn a play
// into hundreds of HTTP fetches.
const MaxSidecarSubtitles = 12

// SidecarSubtitles returns the subtitle files sitting next to videoPath that
// belong to it, as source-relative paths ready for the stream proxy.
//
// Why this exists: desktop playback hands mpv a loopback proxy URL
// (/api/files/stream?...), never the file's own path, so mpv's sub-auto
// cannot look in the video's folder. The pairing has to be done here.
//
// A subtitle belongs to the video when, ignoring case, its name without the
// extension equals the video's name without the extension, or starts with it
// followed by "." — "Movie.srt", "Movie.zh.srt", "Movie.chs.ass" all pair with
// "Movie.mkv". Listing failures are not playback failures: they return nil.
func (c *Client) SidecarSubtitles(videoPath string) []string {
	segs, err := segments(videoPath)
	if err != nil || len(segs) == 0 {
		return nil
	}
	listing, err := c.listDir(segs[:len(segs)-1], isSubtitle)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(listing.Entries))
	for _, e := range listing.Entries {
		if !e.IsDir {
			names = append(names, e.Name)
		}
	}
	matched := MatchSidecarSubtitles(segs[len(segs)-1], names)
	dir := strings.Join(segs[:len(segs)-1], "/")
	out := make([]string, 0, len(matched))
	for _, name := range matched {
		if dir == "" {
			out = append(out, name)
		} else {
			out = append(out, dir+"/"+name)
		}
	}
	return out
}

// MatchSidecarSubtitles is the pure pairing rule behind SidecarSubtitles.
// The exact-name match sorts first (mpv selects the first external track it
// prefers), then the rest by name.
func MatchSidecarSubtitles(videoName string, candidates []string) []string {
	stem := strings.ToLower(strings.TrimSuffix(videoName, path.Ext(videoName)))
	if stem == "" {
		return nil
	}
	var exact, tagged []string
	for _, name := range candidates {
		if !isSubtitle(name) {
			continue
		}
		subStem := strings.ToLower(strings.TrimSuffix(name, path.Ext(name)))
		switch {
		case subStem == stem:
			exact = append(exact, name)
		case strings.HasPrefix(subStem, stem+"."):
			tagged = append(tagged, name)
		}
	}
	sort.Strings(exact)
	sort.Strings(tagged)
	out := append(exact, tagged...)
	if len(out) > MaxSidecarSubtitles {
		out = out[:MaxSidecarSubtitles]
	}
	return out
}
