// Package discovery advertises this TinyPlay desktop on the local network over
// mDNS/Bonjour so a paired phone can find it again after its address changes.
//
// The pairing token is bound to the device, not to its address, so a phone that
// has lost its target does not need to re-pair — it only needs the new address.
// Without this, a DHCP lease change stranded the phone completely: the app had
// no discovery, and the only cure was walking to the computer and re-scanning
// the QR code.
//
// Advertising is deliberately best-effort. A failure here must never stop the
// HTTP server from serving, because everything except rediscovery still works
// without it.
package discovery

import (
	"log"
	"os"
	"sync"

	"github.com/grandcat/zeroconf"
)

// ServiceType is also declared in the iOS app's Info.plist (NSBonjourServices)
// and advertised by the tvOS app. Changing it here changes it in three places.
const ServiceType = "_tinyplay._tcp"

// Advertiser owns at most one registration at a time.
type Advertiser struct {
	mu     sync.Mutex
	server *zeroconf.Server
}

// New returns an advertiser that is not yet publishing anything.
func New() *Advertiser { return &Advertiser{} }

// Start publishes the service on `port`, replacing any previous registration.
//
// `instanceID` is the stable installation id the phone also reads from
// /api/capabilities: it is what lets a controller tell "my TinyPlay moved to a
// new address" apart from "there is a different TinyPlay on this network", and
// silent re-pointing of a token is only safe when that answer is certain.
func (a *Advertiser) Start(port int, instanceID, deviceName string) {
	if port <= 0 {
		return
	}
	a.Stop()

	name := deviceName
	if name == "" {
		if host, err := os.Hostname(); err == nil {
			name = host
		} else {
			name = "TinyPlay"
		}
	}

	// TXT values stay short and non-secret: anyone on the LAN can read them,
	// which is precisely why the pairing token is never among them.
	text := []string{
		"id=" + instanceID,
		"name=" + name,
		"platform=desktop",
	}

	server, err := zeroconf.Register(name, ServiceType, "local.", port, text, nil)
	if err != nil {
		log.Printf("[discovery] not advertising: %v", err)
		return
	}

	a.mu.Lock()
	a.server = server
	a.mu.Unlock()
}

// Stop withdraws the registration if there is one.
func (a *Advertiser) Stop() {
	a.mu.Lock()
	server := a.server
	a.server = nil
	a.mu.Unlock()
	if server != nil {
		server.Shutdown()
	}
}
