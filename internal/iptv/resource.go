package iptv

import (
	"net/url"
	"strings"
)

// ResourceRequest is a provider-controlled URL together with the small,
// well-known set of request headers that M3U generators use for playback.
// It is intentionally not a general HTTP-header bag: accepting Host,
// Content-Length, or newline-containing values would make a playlist an
// unexpected request-smuggling/configuration surface.
type ResourceRequest struct {
	URL         *url.URL
	HTTPHeaders map[string]string
}

var allowedHeaderNames = map[string]string{
	"user-agent":         "User-Agent",
	"http-user-agent":    "User-Agent",
	"referer":            "Referer",
	"referrer":           "Referer",
	"http-referrer":      "Referer",
	"http-referer":       "Referer",
	"origin":             "Origin",
	"http-origin":        "Origin",
	"cookie":             "Cookie",
	"http-cookie":        "Cookie",
	"authorization":      "Authorization",
	"http-authorization": "Authorization",
	"accept":             "Accept",
	"http-accept":        "Accept",
}

// parseResourceRequest accepts the de-facto `URL|User-Agent=…&Referer=…`
// notation in addition to a plain absolute or playlist-relative URL.
func parseResourceRequest(raw string, base *url.URL) (ResourceRequest, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ResourceRequest{}, errf(400, "A URL is required")
	}
	parts := strings.SplitN(raw, "|", 2)
	address := strings.Trim(strings.TrimSpace(parts[0]), "\"'")
	u, err := url.Parse(address)
	if err != nil {
		return ResourceRequest{}, errf(400, "Invalid URL")
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme == "" {
		return ResourceRequest{}, errf(400, "Invalid URL")
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		if u.Host == "" {
			return ResourceRequest{}, errf(400, "Invalid URL")
		}
	}
	request := ResourceRequest{URL: u, HTTPHeaders: map[string]string{}}
	if len(parts) == 2 {
		for _, pair := range strings.Split(parts[1], "&") {
			name, value, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			if decoded, err := url.QueryUnescape(value); err == nil {
				value = decoded
			}
			addAllowedHeader(request.HTTPHeaders, name, value)
		}
	}
	return request, nil
}

func addAllowedHeader(dst map[string]string, name, value string) {
	canonical, ok := allowedHeaderNames[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") || len(value) > 4096 {
		return
	}
	dst[canonical] = value
}

func mergeHeaders(dst, src map[string]string) {
	for name, value := range src {
		addAllowedHeader(dst, name, value)
	}
}

func sameHost(a, b *url.URL) bool {
	return a != nil && b != nil && strings.EqualFold(a.Hostname(), b.Hostname()) && a.Port() == b.Port()
}

// inheritedHeadersForStream keeps cookies and Authorization only for streams
// hosted by the playlist origin. A provider's playlist credentials must never
// be forwarded to an unrelated CDN just because it appeared in an entry.
func inheritedHeadersForStream(headers map[string]string, playlistURL, streamURL *url.URL) map[string]string {
	out := map[string]string{}
	for name, value := range headers {
		if (name == "Cookie" || name == "Authorization") && !sameHost(playlistURL, streamURL) {
			continue
		}
		out[name] = value
	}
	return out
}

func httpURL(resource ResourceRequest) bool {
	return resource.URL != nil && (resource.URL.Scheme == "http" || resource.URL.Scheme == "https")
}
