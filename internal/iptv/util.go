package iptv

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

var errInputTooLarge = errors.New("input exceeds the safe size limit")

// boundedReader makes limits effective while streaming, including for gzip
// input. It deliberately fails at the boundary instead of silently truncating
// a provider response and treating a partial playlist/guide as valid data.
type boundedReader struct {
	r         io.Reader
	remaining int64
}

func newBoundedReader(r io.Reader, maximum int64) io.Reader {
	return &boundedReader{r: r, remaining: maximum}
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errInputTooLarge
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	if n > 0 && r.remaining == 0 && err == nil {
		return n, errInputTooLarge
	}
	return n, err
}

// maybeGunzip transparently decompresses gzip-encoded input (common for
// large hosted XMLTV feeds, e.g. guide.xml.gz), detected by magic bytes
// rather than by URL suffix since redirects/CDNs can hide the extension.
func maybeGunzip(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && err != io.EOF {
		return br, nil
	}
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return gzip.NewReader(br)
	}
	return br, nil
}

// channelID derives a stable id for a playlist entry: tvg-id when present
// (the common case, and required for EPG matching), else a short hash of
// name+group so favorites/recents still survive a playlist refresh as long
// as the channel's name and category don't change upstream.
func channelID(tvgID, name, group string) string {
	if tvgID != "" {
		return "tvg:" + strings.ToLower(tvgID)
	}
	h := sha1.Sum([]byte(strings.ToLower(name) + "|" + strings.ToLower(group)))
	return "h:" + hex.EncodeToString(h[:])[:12]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
