package server

import (
	"strconv"
	"strings"
	"testing"

	"tvremote/internal/imagecache"
)

// A poster that is already in the browser's cache should cost a 304 and no
// body, so the matcher has to accept every shape a client may send the
// validator back in.
func TestETagMatches(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{"absent", "", false},
		{"exact", `"abc123"`, true},
		{"weak", `W/"abc123"`, true},
		{"wildcard", "*", true},
		{"in a list", `"other", "abc123"`, true},
		{"in a list with weak entries", `W/"other",W/"abc123"`, true},
		{"different tag", `"def456"`, false},
		{"unquoted", "abc123", false},
		{"prefix only", `"abc"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := etagMatches(tc.ifNoneMatch, etag); got != tc.want {
				t.Errorf("etagMatches(%q, %q) = %v, want %v", tc.ifNoneMatch, etag, got, tc.want)
			}
		})
	}
}

// The header is what actually keeps a phone from re-requesting artwork, and it
// has to stay tied to the windows the cache enforces rather than drifting to a
// hand-written number.
func TestImageCacheControlMatchesCachePolicy(t *testing.T) {
	if !strings.Contains(imageCacheControl, "private") {
		t.Errorf("Cache-Control = %q, want it marked private", imageCacheControl)
	}
	fresh := int(imagecache.FreshFor.Seconds())
	hard := int(imagecache.HardTTL.Seconds())
	if !strings.Contains(imageCacheControl, "max-age="+strconv.Itoa(fresh)) {
		t.Errorf("Cache-Control = %q, want max-age=%d", imageCacheControl, fresh)
	}
	if !strings.Contains(imageCacheControl, "stale-while-revalidate="+strconv.Itoa(hard)) {
		t.Errorf("Cache-Control = %q, want stale-while-revalidate=%d", imageCacheControl, hard)
	}
}
