package server

import (
	"net/http"
	"os"
	"runtime"
	"strings"

	"tvremote/internal/config"
)

const (
	remoteProtocolMajor = 1
	remoteProtocolMinor = 0
	proProductID        = "cn.hqyang.TinyPlay.pro"
)

var desktopSourceTypes = []string{
	"emby", "jellyfin", "plex", "webdav", "smb", "local", "nfs", "iptv",
}

var desktopFeatures = []string{
	"library",
	"files",
	"iptv",
	"player_control",
	"player_long_poll",
	"volume",
	"desktop_input",
	"website",
	"diagnostics",
}

func desktopDeviceName() string {
	if name, err := os.Hostname(); err == nil {
		name = strings.TrimSpace(name)
		if name != "" {
			return name
		}
	}
	return "TinyPlay Desktop"
}

// capabilities is the versioned discovery document for native controllers.
// Existing routes deliberately remain unprefixed: major changes are negotiated
// here, while additive fields/features increment the minor version.
func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol": map[string]any{
			"major": remoteProtocolMajor,
			"minor": remoteProtocolMinor,
		},
		"device": map[string]any{
			"id":          config.InstallationID(),
			"name":        desktopDeviceName(),
			"platform":    runtime.GOOS,
			"app_version": s.version,
		},
		"source_types": desktopSourceTypes,
		"features":     desktopFeatures,
	})
}

// entitlements reports only grants contributed by the target device. TinyPlay
// desktop remains completely free and never contributes an Apple purchase to a
// paired controller; the iOS app may still unlock from its own StoreKit account.
func (s *Server) entitlements(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"product_id":                proProductID,
		"target_pro":                false,
		"grants_paired_controllers": false,
	})
}
