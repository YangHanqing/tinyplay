package player

import "strconv"

// FallbackCoverArtPath is TinyPlay's own now-playing artwork, embedded in the
// frontend FS and therefore already served by the core's static handler.
const FallbackCoverArtPath = "/static/nowplaying.png"

// FallbackCoverArtURL addresses that artwork for mpv. Loopback rather than the
// advertised LAN address: mpv runs on this machine, and loopback callers are
// the one class exempt from device pairing, so the artwork resolves even
// before a phone has ever paired.
//
// An unusable port yields "", which callers pass straight through — audio then
// plays in a forced black window instead of failing.
func FallbackCoverArtURL(port int) string {
	if port <= 0 || port > 65535 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + FallbackCoverArtPath
}

// Audio presentation for mpv.
//
// mpv creates no window at all for an audio file that carries no embedded
// cover art, so a cast song plays with the screen showing nothing — which
// reads as "the cast failed" even though the music is audible. TinyPlay
// therefore hands mpv a picture to display and forces a window.
//
// The picture is supplied as --cover-art-files, which mpv turns into a video
// track. That has a sharp edge: an explicitly supplied cover art file
// OUTRANKS a real video track, so a leftover cover art entry makes the next
// video play as a still image (verified: the h264 track is deselected and mpv
// decodes the mjpeg instead). Two rules follow, and coverArtValue exists to
// make them hard to get wrong:
//
//   - Cover art is set ONLY for audio. Never pass it "just in case".
//   - Every play sets the option, including to empty. Never leave the previous
//     item's value in place on the reused-process path.
type audioPresentation struct {
	// audioOnly requests a window even when the artwork fails to load, so an
	// unreachable album art URL degrades to a black screen rather than to no
	// screen at all.
	audioOnly bool
	// coverArtURL is the image mpv displays in place of a video track. Empty
	// means "clear it", which is what a video item needs.
	coverArtURL string
}

func (a audioPresentation) forceWindowValue() string {
	if a.audioOnly {
		return "yes"
	}
	return "no"
}

// coverArtValue is the mpv cover-art-files list. It is a list rather than a
// single path because that is the option's own type; TinyPlay only ever
// supplies one entry.
func (a audioPresentation) coverArtValue() []string {
	if !a.audioOnly || a.coverArtURL == "" {
		return []string{}
	}
	return []string{a.coverArtURL}
}

// args returns the startup flags for a fresh mpv process. A video item
// contributes nothing: mpv's defaults are already what video wants, and
// staying silent keeps the command line free of no-op flags.
func (a audioPresentation) args() []string {
	if !a.audioOnly {
		return nil
	}
	out := []string{"--force-window=yes"}
	if a.coverArtURL != "" {
		out = append(out, "--cover-art-file="+a.coverArtURL)
	}
	return out
}

// apply configures a reused mpv process. Unlike args() this must run for every
// item including video, because it is responsible for clearing the previous
// item's cover art.
func (p *Player) applyAudioPresentation(a audioPresentation) {
	p.send([]any{"set_property", "cover-art-files", a.coverArtValue()})
	p.send([]any{"set_property", "force-window", a.forceWindowValue()})
}
