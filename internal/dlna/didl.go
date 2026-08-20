package dlna

import (
	"regexp"
	"strings"
)

// didlTitle extracts dc:title (or plain <title>) from UPnP CurrentURIMetaData.
// Stay dependency-free with a small regex (no XML library required).
// Returns "" when metadata is empty or unparseable so callers can fall back.
func didlTitle(meta string) string {
	meta = strings.TrimSpace(meta)
	if meta == "" {
		return ""
	}
	// Prefer Dublin Core title, then unqualified title.
	patterns := []string{
		`(?is)<(?:[\w.-]+:)?title\b[^>]*>(.*?)</(?:[\w.-]+:)?title>`,
	}
	for _, p := range patterns {
		m := regexp.MustCompile(p).FindStringSubmatch(meta)
		if len(m) == 2 {
			t := htmlUnescape(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m[1], "<![CDATA["), "]]>")))
			if t != "" {
				return t
			}
		}
	}
	return ""
}

var (
	didlClassRe  = regexp.MustCompile(`(?is)<(?:[\w.-]+:)?class\b[^>]*>(.*?)</(?:[\w.-]+:)?class>`)
	didlResRe    = regexp.MustCompile(`(?is)<(?:[\w.-]+:)?res\b[^>]*\bprotocolInfo\s*=\s*"([^"]*)"`)
	didlAlbumArt = regexp.MustCompile(`(?is)<(?:[\w.-]+:)?albumArtURI\b[^>]*>(.*?)</(?:[\w.-]+:)?albumArtURI>`)
	didlTagInner = func(m []string) string {
		if len(m) != 2 {
			return ""
		}
		return htmlUnescape(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(m[1]), "<![CDATA["), "]]>")))
	}
)

// didlIsAudio reports whether the cast item is music rather than video.
//
// This answer decides whether mpv is handed cover art, and cover art supplied
// for something that actually has a video track replaces that video with a
// still image (see internal/player/audioart.go). So the function is
// deliberately built out of positive evidence only: it says true when the
// sender declared audio, and false whenever it is unsure. A song misread as
// video plays with a blank screen; a film misread as audio does not play at
// all. Only the first mistake is acceptable.
//
// Two independent declarations are accepted because senders disagree about
// which they populate: UPnP's own item class, and the MIME type in the
// resource's protocolInfo.
func didlIsAudio(meta string) bool {
	if strings.TrimSpace(meta) == "" {
		return false
	}
	class := strings.ToLower(didlTagInner(didlClassRe.FindStringSubmatch(meta)))
	// Match the class prefix rather than the whole string: real senders append
	// their own subtypes (object.item.audioItem.musicTrack, .audioBroadcast).
	if strings.HasPrefix(class, "object.item.audioitem") {
		return true
	}
	// A declared video class is definitive; do not let a stray audio-only res
	// entry (some senders list the audio track separately) override it.
	if strings.HasPrefix(class, "object.item.videoitem") {
		return false
	}
	hasAudioResource := false
	for _, m := range didlResRe.FindAllStringSubmatch(meta, -1) {
		if len(m) != 2 {
			continue
		}
		// protocolInfo is "<protocol>:<network>:<mime>:<extras>".
		fields := strings.Split(m[1], ":")
		if len(fields) < 3 {
			continue
		}
		mime := strings.ToLower(strings.TrimSpace(fields[2]))
		if strings.HasPrefix(mime, "video/") {
			return false
		}
		if strings.HasPrefix(mime, "audio/") {
			hasAudioResource = true
		}
	}
	return hasAudioResource
}

// didlAlbumArtURL returns the sender's cover art, or "" when it supplied none
// or supplied something mpv should not be pointed at.
//
// The value arrives from another device on the network, so only http(s) is
// accepted: it is handed to mpv as a file to open, and a file:// or similarly
// exotic URL would make the renderer read from this machine on a sender's say-so.
func didlAlbumArtURL(meta string) string {
	if strings.TrimSpace(meta) == "" {
		return ""
	}
	raw := didlTagInner(didlAlbumArt.FindStringSubmatch(meta))
	if raw == "" {
		return ""
	}
	if _, err := neturl(raw); err != nil {
		return ""
	}
	return raw
}
