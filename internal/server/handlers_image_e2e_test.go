package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

// embyImage is the one handler whose whole point is what it does *not* do —
// contact the media server. The unit tests in internal/imagecache cover the
// policy; this covers the wiring, over the real router, with a real upstream.
//
// Returns the router and an addSource function, so a test can register more
// than one source against the same upstream and the same data directory.
func newImageTestServer(t *testing.T, upstreamHits *int64) (http.Handler, func(name string) string) {
	t.Helper()
	data := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", data)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/Users/AuthenticateByName"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"AccessToken":"demo-token","User":{"Id":"demo-user"}}`))
		case strings.Contains(r.URL.Path, "/Images/"):
			// Counted here, so every assertion below is about how often the
			// media server actually had to render a poster.
			atomic.AddInt64(upstreamHits, 1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("poster-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	h := testHandler(New(player.New()))
	addSource := func(name string) string {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"name": name, "type": "emby", "protocol": parsed.Scheme,
			"hosts": []string{parsed.Hostname()}, "port": port,
			"username": "demo", "password": "",
		})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(http.MethodPost, "/api/servers", string(body)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create source %d: %s", rec.Code, rec.Body.String())
		}
		var created map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		id, _ := created["id"].(string)
		if id == "" {
			t.Fatalf("created source has no id: %s", rec.Body.String())
		}
		return id
	}
	return h, addSource
}

func getImage(t *testing.T, h http.Handler, path, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestImageEndpointServesRepeatViewsWithoutTheMediaServer(t *testing.T) {
	var hits int64
	h, addSource := newImageTestServer(t, &hits)
	addSource("Emby demo")
	const path = "/api/library/image/item-1?max_height=400"

	first := getImage(t, h, path, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first request %d: %s", first.Code, first.Body.String())
	}
	if first.Body.String() != "poster-bytes" {
		t.Fatalf("first body = %q", first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) {
		t.Fatalf("ETag = %q, want a quoted validator", etag)
	}
	if got := first.Header().Get("Cache-Control"); got != imageCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, imageCacheControl)
	}

	for i := 0; i < 4; i++ {
		again := getImage(t, h, path, "")
		if again.Code != http.StatusOK || again.Body.String() != "poster-bytes" {
			t.Fatalf("repeat %d = %d %q", i, again.Code, again.Body.String())
		}
		if got := again.Header().Get("ETag"); got != etag {
			t.Errorf("repeat %d ETag = %q, want %q", i, got, etag)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("media server was asked %d times, want 1", got)
	}
}

func TestImageEndpointAnswers304ForAValidatorItAlreadyIssued(t *testing.T) {
	var hits int64
	h, addSource := newImageTestServer(t, &hits)
	addSource("Emby demo")
	const path = "/api/library/image/item-1?max_height=400"

	first := getImage(t, h, path, "")
	etag := first.Header().Get("ETag")

	conditional := getImage(t, h, path, etag)
	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional request = %d, want 304", conditional.Code)
	}
	if conditional.Body.Len() != 0 {
		t.Errorf("304 carried a %d byte body", conditional.Body.Len())
	}
	if got := conditional.Header().Get("ETag"); got != etag {
		t.Errorf("304 ETag = %q, want %q", got, etag)
	}
	if got := conditional.Header().Get("Cache-Control"); got != imageCacheControl {
		t.Errorf("304 Cache-Control = %q, want %q", got, imageCacheControl)
	}

	stale := getImage(t, h, path, `"not-the-current-digest"`)
	if stale.Code != http.StatusOK || stale.Body.String() != "poster-bytes" {
		t.Fatalf("non-matching validator = %d %q, want the full image", stale.Code, stale.Body.String())
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("media server was asked %d times, want 1", got)
	}
}

// Renditions are separate cache entries, so asking for a different size must
// still reach the media server rather than reusing the wrong bytes.
func TestImageEndpointCachesPerRendition(t *testing.T) {
	var hits int64
	h, addSource := newImageTestServer(t, &hits)
	addSource("Emby demo")

	for _, path := range []string{
		"/api/library/image/item-1?max_height=400",
		"/api/library/image/item-1?max_height=700",
		"/api/library/image/item-1?max_height=400&type=Backdrop",
		"/api/library/image/item-2?max_height=400",
	} {
		if rec := getImage(t, h, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rec.Code)
		}
		if rec := getImage(t, h, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("%s (repeat) = %d", path, rec.Code)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 4 {
		t.Errorf("media server was asked %d times, want 4 (one per rendition)", got)
	}
}

// Cached artwork must not outlive the source that produced it — and deleting one
// source must not evict another's.
func TestDeletingASourceDropsOnlyItsOwnCachedArtwork(t *testing.T) {
	var hits int64
	h, addSource := newImageTestServer(t, &hits)
	doomed := addSource("Doomed")
	kept := addSource("Kept")

	doomedPath := "/api/library/image/item-1?max_height=400&server_id=" + doomed
	keptPath := "/api/library/image/item-1?max_height=400&server_id=" + kept

	// Two sources, same item id: each has to cache its own copy, so this is
	// also where a key that forgot the source would show up.
	for _, path := range []string{doomedPath, keptPath} {
		for i := 0; i < 2; i++ {
			if rec := getImage(t, h, path, ""); rec.Code != http.StatusOK {
				t.Fatalf("seed %s = %d", path, rec.Code)
			}
		}
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Fatalf("media server was asked %d times while seeding, want 2 (one per source)", got)
	}

	cached := func() int {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(config.DataDir(), "image-cache", "*.img"))
		if err != nil {
			t.Fatal(err)
		}
		return len(files)
	}
	if got := cached(); got != 2 {
		t.Fatalf("cached artwork files = %d, want 2", got)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodDelete, "/api/servers/"+doomed, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete source = %d: %s", rec.Code, rec.Body.String())
	}

	// The deleted source can no longer be addressed through the API, so this is
	// the direct evidence that its bytes left the disk rather than being
	// orphaned there.
	if got := cached(); got != 1 {
		t.Errorf("cached artwork files after deletion = %d, want 1", got)
	}

	if rec := getImage(t, h, keptPath, ""); rec.Code != http.StatusOK {
		t.Fatalf("surviving source = %d", rec.Code)
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("deleting a source evicted another source's artwork (%d fetches, want 2)", got)
	}
}
