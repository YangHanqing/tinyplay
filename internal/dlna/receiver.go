// Package dlna implements the small, interoperable core of a UPnP AV
// MediaRenderer: SSDP discovery plus ConnectionManager and AVTransport SOAP.
// It deliberately has no media-library concept; a sender supplies a URL and
// the injected player replaces whatever is currently playing.
package dlna

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/ipv4"

	"tvremote/internal/config"
	"tvremote/internal/player"
)

const (
	group                 = "239.255.255.250:1900"
	rendererType          = "urn:schemas-upnp-org:device:MediaRenderer:1"
	avTransportType       = "urn:schemas-upnp-org:service:AVTransport:1"
	connectionManagerType = "urn:schemas-upnp-org:service:ConnectionManager:1"
	renderingControlType  = "urn:schemas-upnp-org:service:RenderingControl:1"
)

// groupUDP is the SSDP multicast destination as a resolved address, reused for
// every NOTIFY and for joining the group on each interface.
var groupUDP = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

// ifaceSock is one multicast listener bound to a single interface. Binding
// per-interface (rather than a single nil-interface socket) is what makes
// discovery reliable on multi-homed hosts — a Windows box with Hyper-V / WSL /
// VMware adapters would otherwise join the group on one arbitrary NIC and never
// see an M-SEARCH from the phone's LAN. Knowing the interface also lets each
// reply carry a LOCATION on the subnet the query came from.
type ifaceSock struct {
	ifi  net.Interface
	ip   net.IP
	conn *net.UDPConn
}

// Receiver is safe to start/stop repeatedly as the Settings toggle changes.
type Receiver struct {
	p    *player.Player
	port func() int
	// playURI is overridden by protocol tests. Production uses p.Play.
	playURI func(string, player.PlayOptions) map[string]any
	// playMu preserves SetAVTransportURI arrival order while the actual player
	// work happens off the SOAP request path.
	playMu sync.Mutex

	mu      sync.Mutex
	socks   map[int]*ifaceSock // one receive socket per interface, keyed by index
	send    *ipv4.PacketConn   // shared sender; egress interface pinned per write
	stop    chan struct{}
	running bool
	wg      sync.WaitGroup
	// GENA event subscribers, keyed by SID ("uuid:…"). Protected by mu.
	subs map[string]*subscription
	// state is the authoritative DMR snapshot for SOAP + GENA.
	state *rendererState
	// pendingAVT / pendingRCS mark services that need a GENA flush (event loop).
	pendingAVT bool
	pendingRCS bool
	// playGen rejects stale SetAVTransportURI completions when URIs race.
	playGen uint64
}

func New(p *player.Player, port func() int) *Receiver {
	r := &Receiver{p: p, port: port, subs: map[string]*subscription{}, state: newRendererState()}
	if p != nil {
		r.playURI = p.Play
	}
	if p != nil {
		// Engine lifecycle → GENA. Filter to dlna sessions so Emby playback does
		// not spam control points that happen to stay subscribed.
		p.LifecycleReporter = r.onPlayerLifecycle
	}
	return r
}

// FriendlyName is the stable name shown in DLNA sender device pickers.
// Keep the same value available to the desktop standby screen so users can
// immediately identify which target to choose on their phone.
func FriendlyName() string {
	id := config.DLNAReceiverID()
	suffix := id
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	return "TinyPlay (" + strings.ToUpper(suffix) + ")"
}

func (r *Receiver) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	// One unconnected sender for the whole receiver: NOTIFY and M-SEARCH
	// replies both pin their egress interface through a per-write control
	// message, so each packet leaves the right NIC with a matching source IP.
	sendConn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		r.mu.Unlock()
		log.Printf("DLNA receiver unavailable (send socket): %v", err)
		return
	}
	send := ipv4.NewPacketConn(sendConn)
	_ = send.SetMulticastTTL(4)
	r.send = send
	r.socks = map[int]*ifaceSock{}
	r.subs = map[string]*subscription{}
	r.stop = make(chan struct{})
	r.running = true
	r.bindInterfacesLocked()
	joined := len(r.socks)
	r.mu.Unlock()

	r.advertise("ssdp:alive")
	r.wg.Add(2)
	go r.maintainLoop(r.stop)
	go r.eventLoop(r.stop)
	if joined == 0 {
		log.Printf("DLNA receiver: no interface joined yet; will keep scanning")
	} else {
		log.Printf("DLNA receiver enabled on UDP 1900 across %d interface(s)", joined)
	}
}

// Running reports whether discovery is actually live: the receiver owns its
// sender and has joined the group on at least one interface. It reflects the
// live sockets rather than the persisted setting, since another application or
// a down link may keep us off every interface.
func (r *Receiver) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running && len(r.socks) > 0
}

func (r *Receiver) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stop)
	socks := r.socks
	send := r.send
	r.socks = nil
	r.send = nil
	r.subs = map[string]*subscription{}
	r.pendingAVT, r.pendingRCS = false, false
	r.mu.Unlock()
	if r.state != nil {
		r.state.setTransport("NO_MEDIA_PRESENT", "OK")
		r.state.setURI("", "", "")
	}

	// Announce departure while the sockets are still open, then tear down.
	list := make([]*ifaceSock, 0, len(socks))
	for _, s := range socks {
		list = append(list, s)
	}
	r.sendNotify(list, send, "ssdp:byebye")
	for _, s := range socks {
		_ = s.conn.Close()
	}
	if send != nil {
		_ = send.Close()
	}
	r.wg.Wait()
	log.Printf("DLNA receiver disabled")
}

// bindInterfacesLocked joins the SSDP group on every suitable interface that
// does not already have a socket. It is called both at Start and periodically,
// so an interface that comes up later (e.g. Wi-Fi after launch) is picked up
// without a restart. Callers must hold r.mu.
func (r *Receiver) bindInterfacesLocked() {
	for _, ifi := range suitableInterfaces() {
		if _, ok := r.socks[ifi.Index]; ok {
			continue
		}
		ip := firstIPv4(ifi)
		if ip == nil {
			continue
		}
		ifi := ifi
		conn, err := net.ListenMulticastUDP("udp4", &ifi, groupUDP)
		if err != nil {
			// A single interface refusing the join (permissions, races on a
			// vanishing NIC) must not sink the others.
			log.Printf("DLNA receiver: interface %s not joined: %v", ifi.Name, err)
			continue
		}
		s := &ifaceSock{ifi: ifi, ip: ip, conn: conn}
		r.socks[ifi.Index] = s
		r.wg.Add(1)
		go r.serve(s, r.stop)
	}
}

// dropSock forgets a receive socket whose read failed (interface removed, or a
// close during Stop) so a later re-scan can re-bind it if it returns.
func (r *Receiver) dropSock(s *ifaceSock) {
	r.mu.Lock()
	if cur, ok := r.socks[s.ifi.Index]; ok && cur == s {
		delete(r.socks, s.ifi.Index)
	}
	r.mu.Unlock()
	_ = s.conn.Close()
}

func (r *Receiver) serve(s *ifaceSock, stop <-chan struct{}) {
	defer r.wg.Done()
	buf := make([]byte, 8192)
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-stop:
					return
				default:
					continue
				}
			}
			// Non-timeout error: the socket was closed (Stop) or the
			// interface went away. Drop it and let a re-scan re-bind later.
			r.dropSock(s)
			return
		}
		r.handleSearch(s, addr, string(buf[:n]))
	}
}

// maintainLoop keeps the announcement alive and adopts interfaces that appear
// after Start. The re-scan cadence is short so a phone on a newly-connected LAN
// becomes reachable quickly; the alive NOTIFY cadence stays under the 1800s
// max-age it advertises.
func (r *Receiver) maintainLoop(stop <-chan struct{}) {
	defer r.wg.Done()
	rescan := time.NewTicker(30 * time.Second)
	notify := time.NewTicker(3 * time.Minute)
	defer rescan.Stop()
	defer notify.Stop()
	for {
		select {
		case <-stop:
			return
		case <-rescan.C:
			r.mu.Lock()
			before := len(r.socks)
			r.bindInterfacesLocked()
			added := len(r.socks) - before
			r.mu.Unlock()
			if added > 0 {
				r.advertise("ssdp:alive")
			}
		case <-notify.C:
			r.advertise("ssdp:alive")
		}
	}
}

func (r *Receiver) handleSearch(s *ifaceSock, addr *net.UDPAddr, request string) {
	upper := strings.ToUpper(request)
	if !strings.HasPrefix(upper, "M-SEARCH * HTTP/1.1") || !strings.Contains(upper, "SSDP:DISCOVER") {
		return
	}
	targets := r.searchTargets(header(request, "st"))
	if len(targets) == 0 {
		return
	}
	r.mu.Lock()
	send := r.send
	r.mu.Unlock()
	if send == nil {
		return
	}
	delayLimit := searchDelay(header(request, "mx"))
	cm := &ipv4.ControlMessage{IfIndex: s.ifi.Index}
	for _, target := range targets {
		resp := r.response(target, s.ip)
		delay := time.Duration(0)
		if delayLimit > 0 {
			delay = time.Duration(rand.Int63n(int64(delayLimit) + 1))
		}
		time.AfterFunc(delay, func() {
			_, _ = send.WriteTo([]byte(resp), cm, addr)
		})
	}
}

// suitableInterfaces returns the up, multicast-capable, non-loopback interfaces
// that carry a usable IPv4 address.
func suitableInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.Interface
	for _, ifi := range all {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if firstIPv4(ifi) == nil {
			continue
		}
		out = append(out, ifi)
	}
	return out
}

// firstIPv4 returns the interface's first routable IPv4 address, skipping
// loopback and 169.254 link-local addresses that no phone would reach us on.
func firstIPv4(ifi net.Interface) net.IP {
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
			return ip4
		}
	}
	return nil
}

func (r *Receiver) targets() []string {
	return []string{
		"upnp:rootdevice",
		"uuid:" + config.DLNAReceiverID(),
		rendererType,
		renderingControlType,
		connectionManagerType,
		avTransportType,
	}
}

func (r *Receiver) searchTargets(requested string) []string {
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(requested, "ssdp:all") {
		return r.targets()
	}
	for _, target := range r.targets() {
		if strings.EqualFold(requested, target) {
			return []string{target}
		}
	}
	return nil
}

func searchDelay(mx string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(mx))
	if err != nil || seconds <= 0 {
		return 0
	}
	if seconds > 5 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

// advertise snapshots the live sockets and multicasts a NOTIFY out each one.
func (r *Receiver) advertise(nts string) {
	r.mu.Lock()
	send := r.send
	socks := make([]*ifaceSock, 0, len(r.socks))
	for _, s := range r.socks {
		socks = append(socks, s)
	}
	r.mu.Unlock()
	r.sendNotify(socks, send, nts)
}

// sendNotify multicasts a NOTIFY (alive or byebye) for every advertised target
// out each interface, with a LOCATION that resolves on that interface's subnet.
func (r *Receiver) sendNotify(socks []*ifaceSock, send *ipv4.PacketConn, nts string) {
	if send == nil {
		return
	}
	for _, s := range socks {
		cm := &ipv4.ControlMessage{IfIndex: s.ifi.Index}
		for _, target := range r.targets() {
			message := strings.Join([]string{
				"NOTIFY * HTTP/1.1", "HOST: " + group, "CACHE-CONTROL: max-age=1800",
				"LOCATION: " + r.location(s.ip), "NT: " + target, "NTS: " + nts,
				"SERVER: TinyPlay/1.0 UPnP/1.1", "USN: " + r.usn(target), "", "",
			}, "\r\n")
			_, _ = send.WriteTo([]byte(message), cm, groupUDP)
		}
	}
}

func (r *Receiver) response(target string, ip net.IP) string {
	return strings.Join([]string{
		"HTTP/1.1 200 OK", "CACHE-CONTROL: max-age=1800", "EXT:", "LOCATION: " + r.location(ip),
		"SERVER: TinyPlay/1.0 UPnP/1.1", "DATE: " + time.Now().UTC().Format(http.TimeFormat),
		"ST: " + target, "USN: " + r.usn(target), "", "",
	}, "\r\n")
}

func (r *Receiver) location(ip net.IP) string {
	return fmt.Sprintf("http://%s:%d/dlna/device.xml", ip.String(), r.port())
}
func (r *Receiver) usn(target string) string {
	base := "uuid:" + config.DLNAReceiverID()
	if strings.EqualFold(target, base) {
		return base
	}
	if target == "upnp:rootdevice" {
		return base + "::upnp:rootdevice"
	}
	return base + "::" + target
}

func header(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, ":"); i > 0 && strings.EqualFold(strings.TrimSpace(line[:i]), key) {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
}

// ServeHTTP handles the UPnP HTTP surface under /dlna/. It is registered
// before the frontend catch-all but has no UI route of its own.
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/dlna/device.xml":
		writeXML(w, http.StatusOK, r.deviceXML())
	case req.Method == http.MethodGet && req.URL.Path == "/dlna/ConnectionManager.xml":
		writeXML(w, http.StatusOK, connectionManagerSCPD)
	case req.Method == http.MethodGet && req.URL.Path == "/dlna/AVTransport.xml":
		writeXML(w, http.StatusOK, avTransportSCPD)
	case req.Method == http.MethodGet && req.URL.Path == "/dlna/RenderingControl.xml":
		writeXML(w, http.StatusOK, renderingControlSCPD)
	case req.Method == "SUBSCRIBE" && strings.HasPrefix(req.URL.Path, "/dlna/"):
		r.handleSubscribe(w, req)
	case req.Method == "UNSUBSCRIBE" && strings.HasPrefix(req.URL.Path, "/dlna/"):
		r.handleUnsubscribe(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/dlna/ConnectionManager/control":
		r.connectionManager(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/dlna/AVTransport/control":
		r.avTransport(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/dlna/RenderingControl/control":
		r.renderingControl(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (r *Receiver) deviceXML() string {
	id := config.DLNAReceiverID()
	// The UUID remains the protocol identity. Its short, stable suffix keeps
	// multiple TinyPlay receivers distinguishable in a sender's device picker.
	name := xmlEscape(FriendlyName())
	return `<?xml version="1.0" encoding="UTF-8"?><root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><device><deviceType>` + rendererType + `</deviceType><friendlyName>` + name + `</friendlyName><manufacturer>TinyPlay</manufacturer><manufacturerURL>https://github.com/YangHanqing/tinyplay</manufacturerURL><modelDescription>TinyPlay DLNA Media Renderer</modelDescription><modelName>TinyPlay DLNA Receiver</modelName><modelNumber>1.0</modelNumber><modelURL>https://github.com/YangHanqing/tinyplay</modelURL><serialNumber>` + id + `</serialNumber><UDN>uuid:` + id + `</UDN><dlna:X_DLNADOC>DMR-1.50</dlna:X_DLNADOC><serviceList><service><serviceType>` + avTransportType + `</serviceType><serviceId>urn:upnp-org:serviceId:AVTransport</serviceId><SCPDURL>/dlna/AVTransport.xml</SCPDURL><controlURL>/dlna/AVTransport/control</controlURL><eventSubURL>/dlna/AVTransport/event</eventSubURL></service><service><serviceType>` + renderingControlType + `</serviceType><serviceId>urn:upnp-org:serviceId:RenderingControl</serviceId><SCPDURL>/dlna/RenderingControl.xml</SCPDURL><controlURL>/dlna/RenderingControl/control</controlURL><eventSubURL>/dlna/RenderingControl/event</eventSubURL></service><service><serviceType>` + connectionManagerType + `</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/dlna/ConnectionManager.xml</SCPDURL><controlURL>/dlna/ConnectionManager/control</controlURL><eventSubURL>/dlna/ConnectionManager/event</eventSubURL></service></serviceList></device></root>`
}

func (r *Receiver) connectionManager(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	action := soapAction(req, string(body))
	switch action {
	case "GetProtocolInfo":
		soapOK(w, "GetProtocolInfo", "ConnectionManager", map[string]string{"Source": "", "Sink": sinkProtocolInfo})
	case "GetCurrentConnectionIDs":
		soapOK(w, "GetCurrentConnectionIDs", "ConnectionManager", map[string]string{"ConnectionIDs": "0"})
	case "GetCurrentConnectionInfo":
		soapOK(w, "GetCurrentConnectionInfo", "ConnectionManager", map[string]string{"RcsID": "0", "AVTransportID": "0", "ProtocolInfo": "http-get:*:video/*:*", "PeerConnectionManager": "", "PeerConnectionID": "-1", "Direction": "Input", "Status": "OK"})
	default:
		log.Printf("DLNA SOAP ConnectionManager unsupported action=%q", action)
		soapFault(w, 401, "Invalid Action")
	}
}

func (r *Receiver) avTransport(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	action := soapAction(req, string(body))
	switch action {
	case "SetAVTransportURI":
		// Many phone DMCs only send SetAVTransportURI (no separate Play), or they
		// mirror TransportState with Pause/Play. Reporting PAUSED_PLAYBACK after we
		// already started mpv made senders issue Pause and freeze the cast.
		// Keep TRANSITIONING until Play() succeeds, then PLAYING (engine may also
		// confirm via LifecyclePlaying).
		raw := xmlTag(string(body), "CurrentURI")
		meta := xmlTag(string(body), "CurrentURIMetaData")
		u, err := neturl(raw)
		if err != nil {
			log.Printf("DLNA SetAVTransportURI rejected: unsupported URI %q err=%v", raw, err)
			if r.state != nil {
				r.state.setTransport("STOPPED", "ERROR_OCCURRED")
				r.notifyTransport()
			}
			soapFault(w, 714, "Illegal MIME-Type")
			return
		}
		title := didlTitle(meta)
		if title == "" {
			title = xmlTag(string(body), "title")
		}
		if title == "" {
			title = u.Path[strings.LastIndex(u.Path, "/")+1:]
		}
		r.mu.Lock()
		r.playGen++
		gen := r.playGen
		r.mu.Unlock()
		if r.state != nil {
			r.state.setURI(raw, meta, title)
			r.state.setTransport("TRANSITIONING", "OK")
			r.notifyTransport()
		}
		// SetAVTransportURI acknowledges assignment, not decoder readiness. A
		// remote URL can take several seconds to open; holding this SOAP response
		// until then makes control points time out even though playback eventually
		// appears on screen. Start the engine in arrival order off-request and
		// report its outcome through the authoritative state + GENA channel.
		go r.startURIPlayback(gen, raw, title)
		soapOK(w, action, "AVTransport", nil)
	case "Play":
		// Always force unpause — reading "pause" can race with a just-started load.
		r.command([]any{"set_property", "pause", false})
		if r.state != nil {
			r.state.setTransport("PLAYING", "OK")
			if speed := xmlTag(string(body), "Speed"); speed != "" {
				r.state.mu.Lock()
				r.state.Speed = speed
				r.state.mu.Unlock()
			}
			r.notifyTransport()
		}
		soapOK(w, action, "AVTransport", nil)
	case "Pause":
		r.command([]any{"set_property", "pause", true})
		if r.state != nil {
			r.state.setTransport("PAUSED_PLAYBACK", "OK")
			r.notifyTransport()
		}
		soapOK(w, action, "AVTransport", nil)
	case "Stop":
		r.mu.Lock()
		r.playGen++ // invalidate an in-flight SetAVTransportURI completion
		r.mu.Unlock()
		r.stopPlayer()
		if r.state != nil {
			r.state.setTransport("STOPPED", "OK")
			r.notifyTransport()
		}
		soapOK(w, action, "AVTransport", nil)
	case "Seek":
		// Unit decides what Target means. Only the time units are a position;
		// TRACK_NR / *_COUNT targets are small integers, and feeding one to a
		// time parser used to seek a single-URI renderer back to 00:00:00.
		unit := strings.ToUpper(strings.TrimSpace(xmlTag(string(body), "Unit")))
		target := xmlTag(string(body), "Target")
		if unit != "" && unit != "REL_TIME" && unit != "ABS_TIME" {
			soapOK(w, action, "AVTransport", nil)
			return
		}
		r.command([]any{"seek", parseTime(target), "absolute"})
		if r.state != nil {
			r.state.setRelTime(target)
		}
		soapOK(w, action, "AVTransport", nil)
	case "Next", "Previous":
		// Single-URI DMR: accept as no-op success so probing control points
		// do not treat 401 as a broken renderer.
		soapOK(w, action, "AVTransport", nil)
	case "SetPlayMode":
		mode := xmlTag(string(body), "NewPlayMode")
		if mode == "" {
			mode = "NORMAL"
		}
		if r.state != nil && r.state.setPlayMode(mode) {
			r.notifyTransport()
		}
		soapOK(w, action, "AVTransport", nil)
	case "GetCurrentTransportActions":
		actions := "Play"
		if r.state != nil {
			actions = r.state.transportActions()
		}
		soapOK(w, action, "AVTransport", map[string]string{"Actions": actions})
	case "GetTransportInfo":
		snap := r.snapState()
		soapOK(w, action, "AVTransport", map[string]string{
			"CurrentTransportState":  snap.TransportState,
			"CurrentTransportStatus": snap.TransportStatus,
			"CurrentSpeed":           snap.Speed,
		})
	case "GetPositionInfo":
		p := r.props()
		snap := r.snapState()
		dur := snap.Duration
		if d := number(p["duration"]); d > 0 {
			dur = dlnaTime(d)
		}
		rel := dlnaTime(number(p["time-pos"]))
		track := "0"
		if snap.URI != "" {
			track = "1"
		}
		soapOK(w, action, "AVTransport", map[string]string{
			"Track": track, "TrackDuration": dur, "TrackMetaData": snap.URIMeta, "TrackURI": snap.URI,
			"RelTime": rel, "AbsTime": rel, "RelCount": "2147483647", "AbsCount": "2147483647",
		})
	case "GetMediaInfo":
		p := r.props()
		snap := r.snapState()
		dur := snap.Duration
		if d := number(p["duration"]); d > 0 {
			dur = dlnaTime(d)
		}
		nr := "0"
		if snap.URI != "" {
			nr = "1"
		}
		soapOK(w, action, "AVTransport", map[string]string{
			"NrTracks": nr, "MediaDuration": dur, "CurrentURI": snap.URI, "CurrentURIMetaData": snap.URIMeta,
			"NextURI": "", "NextURIMetaData": "", "PlayMedium": "NETWORK", "RecordMedium": "NOT_IMPLEMENTED", "WriteStatus": "NOT_IMPLEMENTED",
		})
	case "GetDeviceCapabilities":
		soapOK(w, action, "AVTransport", map[string]string{"PlayMedia": "NETWORK", "RecMedia": "NOT_IMPLEMENTED", "RecQualityModes": "NOT_IMPLEMENTED"})
	case "GetTransportSettings":
		snap := r.snapState()
		soapOK(w, action, "AVTransport", map[string]string{"PlayMode": snap.PlayMode, "RecQualityMode": "NOT_IMPLEMENTED"})
	default:
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		log.Printf("DLNA SOAP AVTransport unsupported action=%q body=%q", action, snippet)
		soapFault(w, 401, "Invalid Action")
	}
}

func (r *Receiver) renderingControl(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	action := soapAction(req, string(body))
	switch action {
	case "GetVolume":
		volume := 100
		if r.state != nil {
			volume = r.state.snapshot().Volume
		}
		if value, ok := r.props()["volume"]; ok {
			volume = max(0, min(100, int(number(value))))
		}
		soapOK(w, action, "RenderingControl", map[string]string{"CurrentVolume": strconv.Itoa(volume)})
	case "SetVolume":
		volume, err := strconv.Atoi(xmlTag(string(body), "DesiredVolume"))
		if err != nil || volume < 0 || volume > 100 {
			log.Printf("DLNA SOAP RenderingControl.SetVolume invalid volume tag")
			soapFault(w, 402, "Invalid Args")
			return
		}
		r.command([]any{"set_property", "volume", volume})
		if r.state != nil && r.state.setVolume(volume) {
			r.notifyRendering()
		}
		soapOK(w, action, "RenderingControl", nil)
	case "GetMute":
		muted := false
		if r.state != nil {
			muted = r.state.snapshot().Mute
		}
		if m, ok := r.props()["mute"].(bool); ok {
			muted = m
		}
		value := "0"
		if muted {
			value = "1"
		}
		soapOK(w, action, "RenderingControl", map[string]string{"CurrentMute": value})
	case "SetMute":
		value := xmlTag(string(body), "DesiredMute")
		if value != "0" && value != "1" && !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
			log.Printf("DLNA SOAP RenderingControl.SetMute invalid value=%q", value)
			soapFault(w, 402, "Invalid Args")
			return
		}
		muted := value == "1" || strings.EqualFold(value, "true")
		r.command([]any{"set_property", "mute", muted})
		if r.state != nil && r.state.setMute(muted) {
			r.notifyRendering()
		}
		soapOK(w, action, "RenderingControl", nil)
	case "ListPresets":
		soapOK(w, action, "RenderingControl", map[string]string{"CurrentPresetNameList": "FactoryDefaults"})
	case "SelectPreset":
		soapOK(w, action, "RenderingControl", nil)
	case "GetVolumeDB", "GetVolumeDBRange":
		// Compatibility stubs for fuller RCS SCPD probes.
		if action == "GetVolumeDB" {
			soapOK(w, action, "RenderingControl", map[string]string{"CurrentVolume": "0"})
		} else {
			soapOK(w, action, "RenderingControl", map[string]string{"MinValue": "-5120", "MaxValue": "0"})
		}
	default:
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		log.Printf("DLNA SOAP RenderingControl unsupported action=%q body=%q", action, snippet)
		soapFault(w, 401, "Invalid Action")
	}
}

func (r *Receiver) transportState() string {
	return r.snapState().TransportState
}

func (r *Receiver) startURIPlayback(gen uint64, raw, title string) {
	r.playMu.Lock()
	defer r.playMu.Unlock()

	// A Stop or newer URI may have arrived before this goroutine was scheduled.
	r.mu.Lock()
	staleBeforeStart := gen != r.playGen
	r.mu.Unlock()
	if staleBeforeStart {
		return
	}

	play := r.playURI
	if play == nil {
		result := map[string]any{"ok": false, "error": "player unavailable"}
		r.finishURIPlayback(gen, raw, title, result)
		return
	}
	result := play(raw, player.PlayOptions{ItemID: raw, Title: title, SourceType: "dlna"})
	r.finishURIPlayback(gen, raw, title, result)
}

func (r *Receiver) finishURIPlayback(gen uint64, raw, title string, result map[string]any) {
	r.mu.Lock()
	stale := gen != r.playGen
	r.mu.Unlock()
	if stale {
		// If this generation was stopped (rather than replaced by another URI),
		// do not let a late process launch resurrect playback.
		snap := r.snapState()
		if snap.URI == raw && snap.TransportState == "STOPPED" {
			r.stopPlayer()
		}
		return
	}
	if ok, _ := result["ok"].(bool); !ok {
		log.Printf("DLNA SetAVTransportURI play failed title=%q result=%v", title, result)
		if r.state != nil {
			r.state.setTransport("STOPPED", "ERROR_OCCURRED")
			r.notifyTransport()
		}
		return
	}

	// Preserve a Pause that arrived while the URL was opening. A Play received
	// during the same window changes the desired state to PLAYING and lands here.
	snap := r.snapState()
	r.command([]any{"set_property", "pause", snap.TransportState == "PAUSED_PLAYBACK"})
	if snap.TransportState != "PAUSED_PLAYBACK" && snap.TransportState != "STOPPED" && snap.URI == raw {
		if r.state != nil {
			r.state.setTransport("PLAYING", "OK")
			r.notifyTransport()
		}
	}
}

// props / command / stopPlayer are the only paths from a SOAP handler to the
// engine. They tolerate a nil player so a control point gets a protocol answer
// (and the whole HTTP server survives) even when no engine is attached — which
// is also what lets the protocol tests drive the full action set.
func (r *Receiver) props() map[string]any {
	if r == nil || r.p == nil {
		return nil
	}
	return r.p.Props()
}

func (r *Receiver) command(command []any) {
	if r == nil || r.p == nil {
		return
	}
	r.p.Command(command)
}

func (r *Receiver) stopPlayer() {
	if r == nil || r.p == nil {
		return
	}
	r.p.Stop()
}

func (r *Receiver) snapState() rendererState {
	if r != nil && r.state != nil {
		return r.state.snapshot()
	}
	return rendererState{
		TransportState: "NO_MEDIA_PRESENT", TransportStatus: "OK",
		Duration: "00:00:00", RelTime: "00:00:00", Volume: 100, PlayMode: "NORMAL", Speed: "1",
	}
}

func neturl(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("unsupported URL")
	}
	return u, nil
}

func xmlTag(body, wanted string) string {
	pattern := `(?is)<(?:[\w-]+:)?` + regexp.QuoteMeta(wanted) + `\b[^>]*>(.*?)</(?:[\w-]+:)?` + regexp.QuoteMeta(wanted) + `>`
	match := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return htmlUnescape(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match[1], "<![CDATA["), "]]>")))
}
// htmlUnescape decodes the entity forms a SOAP argument can legally carry.
// It must resolve in a single left-to-right pass: decoding "&amp;" first and
// then rescanning would turn a literal "&amp;lt;" inside a DIDL payload into
// "<" and corrupt the metadata (and, in a CurrentURI, the query string).
// Numeric references matter because senders escape apostrophes as &#39;.
func htmlUnescape(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	named := map[string]string{"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'"}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], ';')
		// An entity reference is short; a stray '&' must stay a literal '&'.
		if end < 0 || end > 10 {
			b.WriteByte('&')
			i++
			continue
		}
		entity := s[i+1 : i+end]
		if value, ok := named[strings.ToLower(entity)]; ok {
			b.WriteString(value)
			i += end + 1
			continue
		}
		if code, ok := numericEntity(entity); ok {
			b.WriteRune(code)
			i += end + 1
			continue
		}
		b.WriteByte('&')
		i++
	}
	return b.String()
}

func numericEntity(entity string) (rune, bool) {
	if len(entity) < 2 || entity[0] != '#' {
		return 0, false
	}
	digits, base := entity[1:], 10
	if digits[0] == 'x' || digits[0] == 'X' {
		digits, base = digits[1:], 16
	}
	value, err := strconv.ParseInt(digits, base, 32)
	if err != nil || value <= 0 || value > 0x10FFFF {
		return 0, false
	}
	return rune(value), true
}

// sanitizeXMLText drops what can never appear in a well-formed XML 1.0
// document: invalid UTF-8 and the disallowed control characters. Escaping does
// not help with either — they are illegal as raw bytes and have no entity form
// — so a single bad byte from a sender would otherwise make the whole SOAP
// response or GENA NOTIFY undecodable at the control point.
func sanitizeXMLText(s string) string {
	clean := true
	for _, r := range s {
		if !allowedXMLRune(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if allowedXMLRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func allowedXMLRune(r rune) bool {
	// utf8.RuneError decoded from a single byte means the input was not UTF-8.
	if r == utf8.RuneError {
		return false
	}
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	}
	return false
}
func number(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}
func parseTime(s string) float64 {
	p := strings.Split(s, ":")
	if len(p) != 3 {
		return 0
	}
	h, _ := strconv.ParseFloat(p[0], 64)
	m, _ := strconv.ParseFloat(p[1], 64)
	sec, _ := strconv.ParseFloat(p[2], 64)
	return h*3600 + m*60 + sec
}
func dlnaTime(seconds float64) string {
	n := int(seconds)
	if n < 0 {
		n = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", n/3600, (n/60)%60, n%60)
}
func soapAction(req *http.Request, body string) string {
	raw := strings.TrimSpace(req.Header.Get("SOAPAction"))
	if raw != "" {
		parts := strings.Split(raw, "#")
		if action := strings.Trim(strings.TrimSpace(parts[len(parts)-1]), "\"'"); action != "" {
			return action
		}
	}
	// A few otherwise valid UPnP stacks omit SOAPAction and rely on the body.
	// Parse only the first child of s:Body so Envelope is never mistaken for an
	// action name.
	re := regexp.MustCompile(`(?is)<(?:[\w-]+:)?Body\b[^>]*>\s*<(?:[\w-]+:)?([\w.-]+)\b`)
	if match := re.FindStringSubmatch(body); len(match) == 2 {
		return match[1]
	}
	return ""
}
func writeXML(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(value))
}
func soapOK(w http.ResponseWriter, action, service string, values map[string]string) {
	var body strings.Builder
	for _, k := range orderedSOAPKeys(action, values) {
		v := values[k]
		fmt.Fprintf(&body, "<%s>%s</%s>", k, xmlEscape(v), k)
	}
	w.Header().Set("EXT", "")
	writeXML(w, http.StatusOK, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:`+action+`Response xmlns:u="urn:schemas-upnp-org:service:`+service+`:1">`+body.String()+`</u:`+action+`Response></s:Body></s:Envelope>`)
}

// SOAP action output arguments are an ordered sequence in the SCPD. Generated
// control-point stacks can decode them positionally, so ranging over a Go map
// here creates intermittent protocol failures (notably Source/Sink swapping).
func orderedSOAPKeys(action string, values map[string]string) []string {
	declared := map[string][]string{
		"GetProtocolInfo":            {"Source", "Sink"},
		"GetCurrentConnectionIDs":    {"ConnectionIDs"},
		"GetCurrentConnectionInfo":   {"RcsID", "AVTransportID", "ProtocolInfo", "PeerConnectionManager", "PeerConnectionID", "Direction", "Status"},
		"GetTransportInfo":           {"CurrentTransportState", "CurrentTransportStatus", "CurrentSpeed"},
		"GetPositionInfo":            {"Track", "TrackDuration", "TrackMetaData", "TrackURI", "RelTime", "AbsTime", "RelCount", "AbsCount"},
		"GetMediaInfo":               {"NrTracks", "MediaDuration", "CurrentURI", "CurrentURIMetaData", "NextURI", "NextURIMetaData", "PlayMedium", "RecordMedium", "WriteStatus"},
		"GetDeviceCapabilities":      {"PlayMedia", "RecMedia", "RecQualityModes"},
		"GetTransportSettings":       {"PlayMode", "RecQualityMode"},
		"GetCurrentTransportActions": {"Actions"},
		"GetVolume":                  {"CurrentVolume"},
		"GetMute":                    {"CurrentMute"},
		"ListPresets":                {"CurrentPresetNameList"},
		"GetVolumeDB":                {"CurrentVolume"},
		"GetVolumeDBRange":           {"MinValue", "MaxValue"},
	}
	keys := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, key := range declared[action] {
		if _, ok := values[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	var rest []string
	for key := range values {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}
func soapFault(w http.ResponseWriter, code int, description string) {
	// A fault is still a UPnP control response, so it carries EXT like a
	// success does; the Swift receiver has always sent it on both paths.
	w.Header().Set("EXT", "")
	writeXML(w, http.StatusInternalServerError, fmt.Sprintf(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code, xmlEscape(description)))
}
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(sanitizeXMLText(s))
}

// Cling/Platinum-based control points (iQIYI included) build their SOAP
// Action objects from the SCPD <argumentList>. A name-only action parses to an
// action with zero arguments, so action.setInput("CurrentURI", …) / the Sink
// output of GetProtocolInfo throw *before any SOAP is sent* — the phone stays on
// "投屏连接中" having subscribed to events but never invoked control. GENA does
// not need the argument definitions, which is exactly why SUBSCRIBE succeeds
// while SetAVTransportURI/GetProtocolInfo never arrive. So these must be the
// complete, standard UPnP service descriptions with full argument lists and the
// state variables each argument references (relatedStateVariable must resolve).
const connectionManagerSCPD = `<?xml version="1.0" encoding="UTF-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetProtocolInfo</name><argumentList><argument><name>Source</name><direction>out</direction><relatedStateVariable>SourceProtocolInfo</relatedStateVariable></argument><argument><name>Sink</name><direction>out</direction><relatedStateVariable>SinkProtocolInfo</relatedStateVariable></argument></argumentList></action><action><name>GetCurrentConnectionIDs</name><argumentList><argument><name>ConnectionIDs</name><direction>out</direction><relatedStateVariable>CurrentConnectionIDs</relatedStateVariable></argument></argumentList></action><action><name>GetCurrentConnectionInfo</name><argumentList><argument><name>ConnectionID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ConnectionID</relatedStateVariable></argument><argument><name>RcsID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_RcsID</relatedStateVariable></argument><argument><name>AVTransportID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_AVTransportID</relatedStateVariable></argument><argument><name>ProtocolInfo</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ProtocolInfo</relatedStateVariable></argument><argument><name>PeerConnectionManager</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionManager</relatedStateVariable></argument><argument><name>PeerConnectionID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionID</relatedStateVariable></argument><argument><name>Direction</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Direction</relatedStateVariable></argument><argument><name>Status</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_ConnectionStatus</relatedStateVariable></argument></argumentList></action></actionList><serviceStateTable><stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="yes"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionStatus</name><dataType>string</dataType><allowedValueList><allowedValue>OK</allowedValue><allowedValue>ContentFormatMismatch</allowedValue><allowedValue>InsufficientBandwidth</allowedValue><allowedValue>UnreliableChannel</allowedValue><allowedValue>Unknown</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionManager</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_Direction</name><dataType>string</dataType><allowedValueList><allowedValue>Input</allowedValue><allowedValue>Output</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_ProtocolInfo</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_ConnectionID</name><dataType>i4</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_AVTransportID</name><dataType>i4</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_RcsID</name><dataType>i4</dataType></stateVariable></serviceStateTable></scpd>`

// AVTransport:1 / RenderingControl:1 moderate all evented state through LastChange.
// Individual vars stay in the table for SOAP relatedStateVariable wiring but do
// not emit standalone GENA properties.
const avTransportSCPD = `<?xml version="1.0" encoding="UTF-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>SetAVTransportURI</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>CurrentURI</name><direction>in</direction><relatedStateVariable>AVTransportURI</relatedStateVariable></argument><argument><name>CurrentURIMetaData</name><direction>in</direction><relatedStateVariable>AVTransportURIMetaData</relatedStateVariable></argument></argumentList></action><action><name>GetMediaInfo</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>NrTracks</name><direction>out</direction><relatedStateVariable>NumberOfTracks</relatedStateVariable></argument><argument><name>MediaDuration</name><direction>out</direction><relatedStateVariable>CurrentMediaDuration</relatedStateVariable></argument><argument><name>CurrentURI</name><direction>out</direction><relatedStateVariable>AVTransportURI</relatedStateVariable></argument><argument><name>CurrentURIMetaData</name><direction>out</direction><relatedStateVariable>AVTransportURIMetaData</relatedStateVariable></argument><argument><name>NextURI</name><direction>out</direction><relatedStateVariable>NextAVTransportURI</relatedStateVariable></argument><argument><name>NextURIMetaData</name><direction>out</direction><relatedStateVariable>NextAVTransportURIMetaData</relatedStateVariable></argument><argument><name>PlayMedium</name><direction>out</direction><relatedStateVariable>PlaybackStorageMedium</relatedStateVariable></argument><argument><name>RecordMedium</name><direction>out</direction><relatedStateVariable>RecordStorageMedium</relatedStateVariable></argument><argument><name>WriteStatus</name><direction>out</direction><relatedStateVariable>RecordMediumWriteStatus</relatedStateVariable></argument></argumentList></action><action><name>GetTransportInfo</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>CurrentTransportState</name><direction>out</direction><relatedStateVariable>TransportState</relatedStateVariable></argument><argument><name>CurrentTransportStatus</name><direction>out</direction><relatedStateVariable>TransportStatus</relatedStateVariable></argument><argument><name>CurrentSpeed</name><direction>out</direction><relatedStateVariable>TransportPlaySpeed</relatedStateVariable></argument></argumentList></action><action><name>GetPositionInfo</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Track</name><direction>out</direction><relatedStateVariable>CurrentTrack</relatedStateVariable></argument><argument><name>TrackDuration</name><direction>out</direction><relatedStateVariable>CurrentTrackDuration</relatedStateVariable></argument><argument><name>TrackMetaData</name><direction>out</direction><relatedStateVariable>CurrentTrackMetaData</relatedStateVariable></argument><argument><name>TrackURI</name><direction>out</direction><relatedStateVariable>CurrentTrackURI</relatedStateVariable></argument><argument><name>RelTime</name><direction>out</direction><relatedStateVariable>RelativeTimePosition</relatedStateVariable></argument><argument><name>AbsTime</name><direction>out</direction><relatedStateVariable>AbsoluteTimePosition</relatedStateVariable></argument><argument><name>RelCount</name><direction>out</direction><relatedStateVariable>RelativeCounterPosition</relatedStateVariable></argument><argument><name>AbsCount</name><direction>out</direction><relatedStateVariable>AbsoluteCounterPosition</relatedStateVariable></argument></argumentList></action><action><name>GetDeviceCapabilities</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>PlayMedia</name><direction>out</direction><relatedStateVariable>PossiblePlaybackStorageMedia</relatedStateVariable></argument><argument><name>RecMedia</name><direction>out</direction><relatedStateVariable>PossibleRecordStorageMedia</relatedStateVariable></argument><argument><name>RecQualityModes</name><direction>out</direction><relatedStateVariable>PossibleRecordQualityModes</relatedStateVariable></argument></argumentList></action><action><name>GetTransportSettings</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>PlayMode</name><direction>out</direction><relatedStateVariable>CurrentPlayMode</relatedStateVariable></argument><argument><name>RecQualityMode</name><direction>out</direction><relatedStateVariable>CurrentRecordQualityMode</relatedStateVariable></argument></argumentList></action><action><name>Stop</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument></argumentList></action><action><name>Play</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Speed</name><direction>in</direction><relatedStateVariable>TransportPlaySpeed</relatedStateVariable></argument></argumentList></action><action><name>Pause</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument></argumentList></action><action><name>Seek</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Unit</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SeekMode</relatedStateVariable></argument><argument><name>Target</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SeekTarget</relatedStateVariable></argument></argumentList></action><action><name>GetCurrentTransportActions</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Actions</name><direction>out</direction><relatedStateVariable>CurrentTransportActions</relatedStateVariable></argument></argumentList></action><action><name>Next</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument></argumentList></action><action><name>Previous</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument></argumentList></action><action><name>SetPlayMode</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>NewPlayMode</name><direction>in</direction><relatedStateVariable>CurrentPlayMode</relatedStateVariable></argument></argumentList></action></actionList><serviceStateTable><stateVariable sendEvents="yes"><name>LastChange</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>TransportState</name><dataType>string</dataType><allowedValueList><allowedValue>STOPPED</allowedValue><allowedValue>PLAYING</allowedValue><allowedValue>PAUSED_PLAYBACK</allowedValue><allowedValue>TRANSITIONING</allowedValue><allowedValue>NO_MEDIA_PRESENT</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>TransportStatus</name><dataType>string</dataType><allowedValueList><allowedValue>OK</allowedValue><allowedValue>ERROR_OCCURRED</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>PlaybackStorageMedium</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>RecordStorageMedium</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>PossiblePlaybackStorageMedia</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>PossibleRecordStorageMedia</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentPlayMode</name><dataType>string</dataType><defaultValue>NORMAL</defaultValue><allowedValueList><allowedValue>NORMAL</allowedValue><allowedValue>REPEAT_ALL</allowedValue><allowedValue>REPEAT_ONE</allowedValue><allowedValue>SHUFFLE</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>TransportPlaySpeed</name><dataType>string</dataType><defaultValue>1</defaultValue></stateVariable><stateVariable sendEvents="no"><name>RecordMediumWriteStatus</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentRecordQualityMode</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>PossibleRecordQualityModes</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>NumberOfTracks</name><dataType>ui4</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentTrack</name><dataType>ui4</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentTrackDuration</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentMediaDuration</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentTrackMetaData</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentTrackURI</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>AVTransportURI</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>AVTransportURIMetaData</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>NextAVTransportURI</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>NextAVTransportURIMetaData</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>RelativeTimePosition</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>AbsoluteTimePosition</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>RelativeCounterPosition</name><dataType>i4</dataType></stateVariable><stateVariable sendEvents="no"><name>AbsoluteCounterPosition</name><dataType>i4</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentTransportActions</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_SeekMode</name><dataType>string</dataType><allowedValueList><allowedValue>TRACK_NR</allowedValue><allowedValue>REL_TIME</allowedValue><allowedValue>ABS_TIME</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_SeekTarget</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_InstanceID</name><dataType>ui4</dataType></stateVariable></serviceStateTable></scpd>`

const renderingControlSCPD = `<?xml version="1.0" encoding="UTF-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetMute</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>CurrentMute</name><direction>out</direction><relatedStateVariable>Mute</relatedStateVariable></argument></argumentList></action><action><name>SetMute</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>DesiredMute</name><direction>in</direction><relatedStateVariable>Mute</relatedStateVariable></argument></argumentList></action><action><name>GetVolume</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>CurrentVolume</name><direction>out</direction><relatedStateVariable>Volume</relatedStateVariable></argument></argumentList></action><action><name>SetVolume</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>DesiredVolume</name><direction>in</direction><relatedStateVariable>Volume</relatedStateVariable></argument></argumentList></action><action><name>ListPresets</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>CurrentPresetNameList</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument></argumentList></action><action><name>SelectPreset</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>PresetName</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument></argumentList></action><action><name>GetVolumeDB</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>CurrentVolume</name><direction>out</direction><relatedStateVariable>Volume</relatedStateVariable></argument></argumentList></action><action><name>GetVolumeDBRange</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>MinValue</name><direction>out</direction><relatedStateVariable>Volume</relatedStateVariable></argument><argument><name>MaxValue</name><direction>out</direction><relatedStateVariable>Volume</relatedStateVariable></argument></argumentList></action></actionList><serviceStateTable><stateVariable sendEvents="yes"><name>LastChange</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_InstanceID</name><dataType>ui4</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_Channel</name><dataType>string</dataType><allowedValueList><allowedValue>Master</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>Mute</name><dataType>boolean</dataType><defaultValue>0</defaultValue></stateVariable><stateVariable sendEvents="no"><name>Volume</name><dataType>ui2</dataType><defaultValue>100</defaultValue><allowedValueRange><minimum>0</minimum><maximum>100</maximum><step>1</step></allowedValueRange></stateVariable></serviceStateTable></scpd>`
