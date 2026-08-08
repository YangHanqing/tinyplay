package dlna

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The asymmetry these tests defend: a song misclassified as video plays with a
// blank screen, while a film misclassified as audio is replaced by a still
// image and does not play at all (mpv drops the real video track when cover
// art is supplied). Every ambiguous case must therefore resolve to "not audio".

func TestDIDLAudioClassification(t *testing.T) {
	const didl = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" ` +
		`xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item>%s</item></DIDL-Lite>`

	cases := []struct {
		name string
		item string
		want bool
	}{
		{"music track class", `<upnp:class>object.item.audioItem.musicTrack</upnp:class>`, true},
		{"bare audio class", `<upnp:class>object.item.audioItem</upnp:class>`, true},
		{"audio broadcast class", `<upnp:class>object.item.audioItem.audioBroadcast</upnp:class>`, true},
		{"video class", `<upnp:class>object.item.videoItem.movie</upnp:class>`, false},
		{"audio mime only", `<res protocolInfo="http-get:*:audio/flac:*">http://n/a.flac</res>`, true},
		{"video mime only", `<res protocolInfo="http-get:*:video/mp4:*">http://n/a.mp4</res>`, false},
		{"class and matching mime", `<upnp:class>object.item.audioItem.musicTrack</upnp:class>` +
			`<res protocolInfo="http-get:*:audio/mpeg:DLNA.ORG_PN=MP3">http://n/a.mp3</res>`, true},
		// A video item whose DIDL also lists a separate audio resource must stay
		// video: the declared class wins over a stray res entry.
		{"video class with extra audio res", `<upnp:class>object.item.videoItem</upnp:class>` +
			`<res protocolInfo="http-get:*:audio/mpeg:*">http://n/a.mp3</res>`, false},
		{"image class", `<upnp:class>object.item.imageItem.photo</upnp:class>`, false},
		{"no class, no res", `<dc:title>Unknown</dc:title>`, false},
		{"unparseable mime", `<res protocolInfo="garbage">http://n/a</res>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := fmtDIDL(didl, tc.item)
			if got := didlIsAudio(meta); got != tc.want {
				t.Fatalf("didlIsAudio = %v, want %v for %s", got, tc.want, tc.item)
			}
		})
	}
}

func TestDIDLAudioIgnoresEmptyAndJunkMetadata(t *testing.T) {
	for _, meta := range []string{"", "   ", "not-xml", "<DIDL-Lite></DIDL-Lite>"} {
		if didlIsAudio(meta) {
			t.Fatalf("didlIsAudio(%q) = true; unknown metadata must not claim audio", meta)
		}
	}
}

func TestDIDLAlbumArtURL(t *testing.T) {
	const wrap = `<DIDL-Lite xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item>%s</item></DIDL-Lite>`
	cases := []struct {
		name string
		item string
		want string
	}{
		{"http art", `<upnp:albumArtURI>http://nas.local/art.jpg</upnp:albumArtURI>`, "http://nas.local/art.jpg"},
		{"https art", `<upnp:albumArtURI>https://nas.local/art.jpg</upnp:albumArtURI>`, "https://nas.local/art.jpg"},
		{"escaped query", `<upnp:albumArtURI>http://nas.local/art?a=1&amp;b=2</upnp:albumArtURI>`, "http://nas.local/art?a=1&b=2"},
		{"no art", `<dc:title>x</dc:title>`, ""},
		// mpv is told to open this path, so a sender must not be able to point
		// the renderer at this machine's own filesystem.
		{"file scheme rejected", `<upnp:albumArtURI>file:///etc/passwd</upnp:albumArtURI>`, ""},
		{"relative rejected", `<upnp:albumArtURI>/art.jpg</upnp:albumArtURI>`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := didlAlbumArtURL(fmtDIDL(wrap, tc.item)); got != tc.want {
				t.Fatalf("didlAlbumArtURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// A song with no album art still gets a picture, so the screen is never blank.
func TestAudioFromMetaFallsBackToBundledArtwork(t *testing.T) {
	r := New(nil, func() int { return 1980 })

	song := `<DIDL-Lite xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item>` +
		`<upnp:class>object.item.audioItem.musicTrack</upnp:class></item></DIDL-Lite>`
	got := r.audioFromMeta(song)
	if !got.audioOnly {
		t.Fatal("a musicTrack cast must be marked audio")
	}
	if got.coverArtURL != "http://127.0.0.1:1980/static/nowplaying.png" {
		t.Fatalf("coverArtURL = %q, want the bundled artwork", got.coverArtURL)
	}

	// The sender's own art is preferred — but only once it has proved it
	// serves an image.
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})
	}))
	defer art.Close()
	withArt := songWithArt(art.URL + "/cover.jpg")
	if got := r.audioFromMeta(withArt); got.coverArtURL != art.URL+"/cover.jpg" {
		t.Fatalf("coverArtURL = %q, want the sender's own album art", got.coverArtURL)
	}

	// A declared albumArtURI that is not an image must not reach mpv: mpv
	// opens cover art asynchronously and reports nothing back, so the cast
	// would simply play into a black window. Two shapes seen in the wild —
	// an origin with no path, and a path that answers with a web page.
	notAnImage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>nope</html>"))
	}))
	defer notAnImage.Close()
	for _, bad := range []string{notAnImage.URL, "http://127.0.0.1:1/cover.jpg"} {
		got := r.audioFromMeta(songWithArt(bad))
		if got.coverArtURL != "http://127.0.0.1:1980/static/nowplaying.png" {
			t.Fatalf("album art %q: coverArtURL = %q, want the bundled artwork", bad, got.coverArtURL)
		}
	}

	film := `<DIDL-Lite xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item>` +
		`<upnp:class>object.item.videoItem</upnp:class>` +
		`<upnp:albumArtURI>http://nas.local/poster.jpg</upnp:albumArtURI></item></DIDL-Lite>`
	got = r.audioFromMeta(film)
	if got.audioOnly || got.coverArtURL != "" {
		t.Fatalf("video cast produced %+v; artwork here would replace the film with a still image", got)
	}
}

func songWithArt(artURL string) string {
	return `<DIDL-Lite xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item>` +
		`<upnp:class>object.item.audioItem.musicTrack</upnp:class>` +
		`<upnp:albumArtURI>` + artURL + `</upnp:albumArtURI></item></DIDL-Lite>`
}

func fmtDIDL(wrap, item string) string {
	for i := 0; i+1 < len(wrap); i++ {
		if wrap[i] == '%' && wrap[i+1] == 's' {
			return wrap[:i] + item + wrap[i+2:]
		}
	}
	return wrap
}
