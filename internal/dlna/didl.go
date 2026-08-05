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
