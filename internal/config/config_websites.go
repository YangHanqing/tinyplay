package config

import (
	"errors"
	"strings"

	"tvremote/internal/website"
)

// MaxWebsiteCustomSites caps the shared Website Library. The catalog is a flat
// list scrolled on a phone, so it stops being usable long before it is a
// storage problem; the number is a deliberate round ceiling that also keeps a
// LAN client from growing config.json without bound.
const MaxWebsiteCustomSites = 100

// Add failures the API layer distinguishes for the user.
var (
	ErrWebsiteInvalidURL = errors.New("website: invalid url")
	ErrWebsiteListFull   = errors.New("website: custom site list is full")
)

// WebsiteCustomSites returns a detached copy of the shared Website Library
// entries. The list is ordered exactly as users arranged it (new entries are
// appended at the bottom, above the add-url card).
func WebsiteCustomSites() []WebsiteCustomSite {
	cfg := Load()
	return append([]WebsiteCustomSite(nil), cfg.WebsiteCustomSites...)
}

// AddWebsiteCustomSite validates browser-style URL input and saves it for all
// phones attached to this TinyPlay instance. A blank name is derived from the
// normalized host by website.NormalizeUserURL.
func AddWebsiteCustomSite(rawURL string) (WebsiteCustomSite, error) {
	normalized, name, ok := website.NormalizeUserURL(rawURL)
	if !ok {
		return WebsiteCustomSite{}, ErrWebsiteInvalidURL
	}
	var added WebsiteCustomSite
	full := false
	// The cap is enforced inside patch so two phones adding at once cannot both
	// read a not-yet-full list and push the catalog past the limit.
	patch(func(cfg *Config) {
		if len(cfg.WebsiteCustomSites) >= MaxWebsiteCustomSites {
			full = true
			return
		}
		added = WebsiteCustomSite{ID: "custom-" + newID(), Name: name, URL: normalized}
		cfg.WebsiteCustomSites = append(cfg.WebsiteCustomSites, added)
	})
	if full {
		return WebsiteCustomSite{}, ErrWebsiteListFull
	}
	if added.ID == "" {
		return WebsiteCustomSite{}, ErrWebsiteInvalidURL
	}
	return added, nil
}

// DeleteWebsiteCustomSite removes one shared Website Library entry. Built-in
// sites never pass through this path because their ids are not custom- ids.
func DeleteWebsiteCustomSite(id string) bool {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "custom-") {
		return false
	}
	deleted := false
	patch(func(cfg *Config) {
		out := cfg.WebsiteCustomSites[:0]
		for _, site := range cfg.WebsiteCustomSites {
			if site.ID == id {
				deleted = true
				continue
			}
			out = append(out, site)
		}
		cfg.WebsiteCustomSites = out
	})
	return deleted
}

// WebsiteCustomSiteByID resolves a persisted entry without exposing config
// mutation to callers.
func WebsiteCustomSiteByID(id string) (WebsiteCustomSite, bool) {
	for _, site := range WebsiteCustomSites() {
		if site.ID == id {
			return site, true
		}
	}
	return WebsiteCustomSite{}, false
}
