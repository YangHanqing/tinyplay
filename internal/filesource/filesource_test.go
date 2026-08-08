package filesource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"tvremote/internal/config"
)

func TestWebDAVBrowseAndResolve(t *testing.T) {
	const password = "s3cret!"
	var authOK bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		authOK = ok && u == "alice" && p == password
		if r.Method == "GET" {
			if r.Header.Get("Range") != "bytes=2-4" {
				t.Errorf("range=%q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 2-4/6")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("cde"))
			return
		}
		if r.Method != "PROPFIND" {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>%s/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response><d:response><d:href>%s/Movies/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response><d:response><d:href>%s/demo%%20movie.mkv</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>1234</d:getcontentlength></d:prop></d:propstat></d:response></d:multistatus>`, r.URL.Path[:len(r.URL.Path)-1], r.URL.Path[:len(r.URL.Path)-1], r.URL.Path[:len(r.URL.Path)-1])
	}))
	defer ts.Close()
	tsURL, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(tsURL.Port())
	c := New(&config.Server{Name: "DAV", Type: "webdav", Protocol: tsURL.Scheme, Hosts: []string{tsURL.Hostname()}, Port: port, RootPath: "dav", Username: "alice", Password: password})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if !authOK || len(listing.Entries) != 2 || !listing.Entries[0].IsDir || !listing.Entries[1].IsVideo {
		t.Fatalf("listing=%#v auth=%v", listing, authOK)
	}
	u, err := c.ResolvePlayURL("demo movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if u == "" || u[:4] != "http" {
		t.Fatalf("url=%q", u)
	}
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := httptest.NewRecorder()
	if err := c.Serve(rec, req, "demo movie.mkv"); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "cde" {
		t.Fatalf("stream %d %q", rec.Code, rec.Body.String())
	}
}

func TestLocalBrowseFiltersNonVideo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(&config.Server{Name: "Local", Type: "local", RootPath: dir})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 || !listing.Entries[0].IsDir || listing.Entries[1].Name != "movie.mkv" {
		t.Fatalf("%#v", listing.Entries)
	}
}

func TestLocalBrowseIncludesAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"track.mp3", "song.flac", "clip.m4a", "readme.txt", "cover.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := New(&config.Server{Name: "Local", Type: "local", RootPath: dir})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 3 {
		t.Fatalf("want 3 audio files, got %#v", listing.Entries)
	}
	for _, e := range listing.Entries {
		if e.IsDir || e.IsVideo || !e.IsAudio {
			t.Fatalf("audio listing entry must be is_audio only: %#v", e)
		}
	}
}

func TestLocalBrowseMixedVideoAndAudio(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"episode.mkv", "theme.mp3", "notes.nfo", "poster.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := New(&config.Server{Name: "Local", Type: "local", RootPath: dir})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	// dirs first, then files sorted by name: episode.mkv, theme.mp3
	if len(listing.Entries) != 3 {
		t.Fatalf("want folder + video + audio, got %#v", listing.Entries)
	}
	if !listing.Entries[0].IsDir || listing.Entries[0].Name != "Folder" {
		t.Fatalf("first entry should be Folder: %#v", listing.Entries[0])
	}
	vid, aud := listing.Entries[1], listing.Entries[2]
	if vid.Name != "episode.mkv" || !vid.IsVideo || vid.IsAudio {
		t.Fatalf("video contract broken: %#v", vid)
	}
	if aud.Name != "theme.mp3" || !aud.IsAudio || aud.IsVideo {
		t.Fatalf("audio contract broken: %#v", aud)
	}
}

func TestIsAudioExtensionSet(t *testing.T) {
	for _, name := range []string{
		"a.mp3", "b.FLAC", "c.m4a", "d.aac", "e.ogg", "f.opus", "g.wav",
		"h.wma", "i.alac", "j.aiff", "k.aif", "l.ape", "m.wv", "n.dsf",
		"o.dff", "p.mka", "q.mp2", "r.ac3", "s.dts", "t.tta", "u.spx",
		"v.oga", "w.m4b",
	} {
		if !IsAudio(name) {
			t.Fatalf("expected IsAudio(%q)", name)
		}
	}
	for _, name := range []string{"movie.mkv", "clip.mp4", "disc.iso", "note.txt", "track.qmc", "song.ncm"} {
		if IsAudio(name) {
			t.Fatalf("expected !IsAudio(%q)", name)
		}
	}
}

func TestLocalBrowseWithoutRootPathListsOSRoot(t *testing.T) {
	// No RootPath configured yet (fresh "local" source, still picking a
	// folder): browsing must list the real OS root instead of erroring or
	// silently resolving to the process's CWD.
	c := New(&config.Server{Name: "Local", Type: "local"})
	listing, err := c.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) == 0 {
		t.Fatalf("expected the OS root to have at least one browsable entry, got none")
	}
}

func TestLocalRelativeRootPathResolvesFromOSRootNotCWD(t *testing.T) {
	// The folder picker persists a bare picked segment (e.g. "Applications"),
	// not an absolute path — it must resolve against the true OS root, not
	// the Go process's own working directory (regression: it silently
	// resolved against os.Getwd() and returned "Folder not found").
	osRoot, err := filepath.Abs(string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(osRoot)
	if err != nil {
		t.Skip("OS root not listable in this sandbox")
	}
	var name string
	for _, e := range entries {
		if e.IsDir() {
			name = e.Name()
			break
		}
	}
	if name == "" {
		t.Skip("no top-level directory found under the OS root")
	}
	c := New(&config.Server{Name: "Local", Type: "local", RootPath: name})
	if _, err := c.ListDir(""); err != nil {
		t.Fatalf("relative root_path %q should resolve from the OS root: %v", name, err)
	}
}

func TestSMBTargetUsesFirstPathSegmentAsShareWhenUnset(t *testing.T) {
	c := New(&config.Server{Type: "smb", Hosts: []string{"nas"}})
	share, path, err := c.smbTarget([]string{"Media", "Movies", "demo.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if share != "Media" || len(path) != 2 || path[0] != "Movies" || path[1] != "demo.mkv" {
		t.Fatalf("share=%q path=%#v", share, path)
	}
}

func TestSMBTargetWithoutShareOrPathRequiresShareSelection(t *testing.T) {
	c := New(&config.Server{Type: "smb", Hosts: []string{"nas"}})
	if _, _, err := c.smbTarget(nil); err == nil {
		t.Fatal("expected share-selection error")
	}
}

func TestSMBListWithoutShareRequiresHost(t *testing.T) {
	// Share == "" and no path segments means "enumerate the host's shares" —
	// exercised for real in TestWebDAVBrowseAndResolve-style integration only
	// (needs a live SMB server); here we just confirm the missing-host guard
	// fires before any network dial is attempted.
	c := New(&config.Server{Type: "smb"})
	if _, err := c.ListDir(""); err == nil {
		t.Fatal("expected missing host error")
	}
}
