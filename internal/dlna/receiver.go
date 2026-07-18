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
	"strconv"
	"strings"
	"sync"
	"time"

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

	mu      sync.Mutex
	socks   map[int]*ifaceSock // one receive socket per interface, keyed by index
	send    *ipv4.PacketConn   // shared sender; egress interface pinned per write
	stop    chan struct{}
	running bool
	wg      sync.WaitGroup
}

func New(p *player.Player, port func() int) *Receiver { return &Receiver{p: p, port: port} }

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
	r.stop = make(chan struct{})
	r.running = true
	r.bindInterfacesLocked()
	joined := len(r.socks)
	r.mu.Unlock()

	r.advertise("ssdp:alive")
	r.wg.Add(1)
	go r.maintainLoop(r.stop)
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
	r.mu.Unlock()

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
	notify := time.NewTicker(9 * time.Minute)
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
		w.Header().Set("SID", "uuid:"+config.DLNAReceiverID())
		w.Header().Set("TIMEOUT", "Second-300")
		w.WriteHeader(http.StatusOK)
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
	return `<?xml version="1.0" encoding="UTF-8"?><root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><device><deviceType>` + rendererType + `</deviceType><friendlyName>` + name + `</friendlyName><manufacturer>TinyPlay</manufacturer><modelDescription>TinyPlay DLNA Media Renderer</modelDescription><modelName>TinyPlay DLNA Receiver</modelName><modelNumber>1.0</modelNumber><UDN>uuid:` + id + `</UDN><dlna:X_DLNADOC>DMR-1.50</dlna:X_DLNADOC><serviceList><service><serviceType>` + avTransportType + `</serviceType><serviceId>urn:upnp-org:serviceId:AVTransport</serviceId><SCPDURL>/dlna/AVTransport.xml</SCPDURL><controlURL>/dlna/AVTransport/control</controlURL><eventSubURL>/dlna/AVTransport/event</eventSubURL></service><service><serviceType>` + renderingControlType + `</serviceType><serviceId>urn:upnp-org:serviceId:RenderingControl</serviceId><SCPDURL>/dlna/RenderingControl.xml</SCPDURL><controlURL>/dlna/RenderingControl/control</controlURL><eventSubURL>/dlna/RenderingControl/event</eventSubURL></service><service><serviceType>` + connectionManagerType + `</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/dlna/ConnectionManager.xml</SCPDURL><controlURL>/dlna/ConnectionManager/control</controlURL><eventSubURL>/dlna/ConnectionManager/event</eventSubURL></service></serviceList></device></root>`
}

func (r *Receiver) connectionManager(w http.ResponseWriter, req *http.Request) {
	switch soapAction(req) {
	case "GetProtocolInfo":
		soapOK(w, "GetProtocolInfo", "ConnectionManager", map[string]string{"Source": "", "Sink": "http-get:*:video/mp4:*,http-get:*:video/*:*"})
	case "GetCurrentConnectionIDs":
		soapOK(w, "GetCurrentConnectionIDs", "ConnectionManager", map[string]string{"ConnectionIDs": "0"})
	case "GetCurrentConnectionInfo":
		soapOK(w, "GetCurrentConnectionInfo", "ConnectionManager", map[string]string{"RcsID": "0", "AVTransportID": "0", "ProtocolInfo": "http-get:*:video/*:*", "PeerConnectionManager": "", "PeerConnectionID": "-1", "Direction": "Input", "Status": "OK"})
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

func (r *Receiver) avTransport(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	action := soapAction(req)
	switch action {
	case "SetAVTransportURI":
		raw := xmlTag(string(body), "CurrentURI")
		u, err := neturl(raw)
		if err != nil {
			soapFault(w, 714, "Illegal MIME-Type")
			return
		}
		title := xmlTag(string(body), "title")
		if title == "" {
			title = u.Path[strings.LastIndex(u.Path, "/")+1:]
		}
		result := r.p.Play(raw, player.PlayOptions{ItemID: raw, Title: title, SourceType: "dlna"})
		if ok, _ := result["ok"].(bool); !ok {
			soapFault(w, 714, "Illegal MIME-Type")
			return
		}
		soapOK(w, action, "AVTransport", nil)
	case "Play":
		props := r.p.Props()
		if paused, _ := props["pause"].(bool); paused {
			r.p.Command([]any{"cycle", "pause"})
		}
		soapOK(w, action, "AVTransport", nil)
	case "Pause":
		r.p.Command([]any{"set_property", "pause", true})
		soapOK(w, action, "AVTransport", nil)
	case "Stop":
		r.p.Stop()
		soapOK(w, action, "AVTransport", nil)
	case "Seek":
		r.p.Command([]any{"seek", parseTime(xmlTag(string(body), "Target")), "absolute"})
		soapOK(w, action, "AVTransport", nil)
	case "GetTransportInfo":
		state := r.transportState()
		soapOK(w, action, "AVTransport", map[string]string{"CurrentTransportState": state, "CurrentTransportStatus": "OK", "CurrentSpeed": "1"})
	case "GetPositionInfo":
		p := r.p.Props()
		soapOK(w, action, "AVTransport", map[string]string{"Track": "1", "TrackDuration": dlnaTime(number(p["duration"])), "TrackMetaData": "", "TrackURI": r.stateString("item_id"), "RelTime": dlnaTime(number(p["time-pos"])), "AbsTime": dlnaTime(number(p["time-pos"])), "RelCount": "2147483647", "AbsCount": "2147483647"})
	case "GetMediaInfo":
		p := r.p.Props()
		soapOK(w, action, "AVTransport", map[string]string{"NrTracks": "1", "MediaDuration": dlnaTime(number(p["duration"])), "CurrentURI": r.stateString("item_id"), "CurrentURIMetaData": "", "NextURI": "", "NextURIMetaData": "", "PlayMedium": "NETWORK", "RecordMedium": "NOT_IMPLEMENTED", "WriteStatus": "NOT_IMPLEMENTED"})
	case "GetDeviceCapabilities":
		soapOK(w, action, "AVTransport", map[string]string{"PlayMedia": "NETWORK", "RecMedia": "NOT_IMPLEMENTED", "RecQualityModes": "NOT_IMPLEMENTED"})
	case "GetTransportSettings":
		soapOK(w, action, "AVTransport", map[string]string{"PlayMode": "NORMAL", "RecQualityMode": "NOT_IMPLEMENTED"})
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

func (r *Receiver) renderingControl(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	action := soapAction(req)
	switch action {
	case "GetVolume":
		volume := 100
		if value, ok := r.p.Props()["volume"]; ok {
			volume = max(0, min(100, int(number(value))))
		}
		soapOK(w, action, "RenderingControl", map[string]string{"CurrentVolume": strconv.Itoa(volume)})
	case "SetVolume":
		volume, err := strconv.Atoi(xmlTag(string(body), "DesiredVolume"))
		if err != nil || volume < 0 || volume > 100 {
			soapFault(w, 402, "Invalid Args")
			return
		}
		r.p.Command([]any{"set_property", "volume", volume})
		soapOK(w, action, "RenderingControl", nil)
	case "GetMute":
		muted, _ := r.p.Props()["mute"].(bool)
		value := "0"
		if muted {
			value = "1"
		}
		soapOK(w, action, "RenderingControl", map[string]string{"CurrentMute": value})
	case "SetMute":
		value := xmlTag(string(body), "DesiredMute")
		if value != "0" && value != "1" && !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
			soapFault(w, 402, "Invalid Args")
			return
		}
		muted := value == "1" || strings.EqualFold(value, "true")
		r.p.Command([]any{"set_property", "mute", muted})
		soapOK(w, action, "RenderingControl", nil)
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

func (r *Receiver) transportState() string {
	s := r.p.State()
	if s["source_type"] != "dlna" || s["running"] != true {
		return "STOPPED"
	}
	if paused, _ := r.p.Props()["pause"].(bool); paused {
		return "PAUSED_PLAYBACK"
	}
	return "PLAYING"
}
func (r *Receiver) stateString(key string) string { v, _ := r.p.State()[key].(string); return v }

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
func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"").Replace(s)
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
func soapAction(req *http.Request) string {
	return strings.Trim(strings.TrimSpace(strings.Split(req.Header.Get("SOAPAction"), "#")[len(strings.Split(req.Header.Get("SOAPAction"), "#"))-1]), "\"'")
}
func writeXML(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(value))
}
func soapOK(w http.ResponseWriter, action, service string, values map[string]string) {
	var body strings.Builder
	for k, v := range values {
		fmt.Fprintf(&body, "<%s>%s</%s>", k, xmlEscape(v), k)
	}
	writeXML(w, http.StatusOK, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+action+`Response xmlns:u="urn:schemas-upnp-org:service:`+service+`:1">`+body.String()+`</u:`+action+`Response></s:Body></s:Envelope>`)
}
func soapFault(w http.ResponseWriter, code int, description string) {
	writeXML(w, http.StatusInternalServerError, fmt.Sprintf(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code, xmlEscape(description)))
}
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}

const connectionManagerSCPD = `<?xml version="1.0" encoding="UTF-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetProtocolInfo</name></action><action><name>GetCurrentConnectionIDs</name></action><action><name>GetCurrentConnectionInfo</name></action></actionList><serviceStateTable><stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="yes"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable></serviceStateTable></scpd>`

const avTransportSCPD = `<?xml version="1.0" encoding="UTF-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>SetAVTransportURI</name></action><action><name>Play</name></action><action><name>Pause</name></action><action><name>Stop</name></action><action><name>Seek</name></action><action><name>GetTransportInfo</name></action><action><name>GetPositionInfo</name></action><action><name>GetMediaInfo</name></action><action><name>GetDeviceCapabilities</name></action><action><name>GetTransportSettings</name></action></actionList><serviceStateTable><stateVariable sendEvents="yes"><name>TransportState</name><dataType>string</dataType><allowedValueList><allowedValue>STOPPED</allowedValue><allowedValue>PLAYING</allowedValue><allowedValue>PAUSED_PLAYBACK</allowedValue><allowedValue>TRANSITIONING</allowedValue><allowedValue>NO_MEDIA_PRESENT</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="no"><name>AVTransportURI</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>RelativeTimePosition</name><dataType>string</dataType></stateVariable><stateVariable sendEvents="no"><name>CurrentTrackDuration</name><dataType>string</dataType></stateVariable></serviceStateTable></scpd>`

const renderingControlSCPD = `<?xml version="1.0" encoding="UTF-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetMute</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>CurrentMute</name><direction>out</direction><relatedStateVariable>Mute</relatedStateVariable></argument></argumentList></action><action><name>SetMute</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>DesiredMute</name><direction>in</direction><relatedStateVariable>Mute</relatedStateVariable></argument></argumentList></action><action><name>GetVolume</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>CurrentVolume</name><direction>out</direction><relatedStateVariable>Volume</relatedStateVariable></argument></argumentList></action><action><name>SetVolume</name><argumentList><argument><name>InstanceID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_InstanceID</relatedStateVariable></argument><argument><name>Channel</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Channel</relatedStateVariable></argument><argument><name>DesiredVolume</name><direction>in</direction><relatedStateVariable>Volume</relatedStateVariable></argument></argumentList></action></actionList><serviceStateTable><stateVariable sendEvents="no"><name>A_ARG_TYPE_InstanceID</name><dataType>ui4</dataType></stateVariable><stateVariable sendEvents="no"><name>A_ARG_TYPE_Channel</name><dataType>string</dataType><allowedValueList><allowedValue>Master</allowedValue></allowedValueList></stateVariable><stateVariable sendEvents="yes"><name>Mute</name><dataType>boolean</dataType><defaultValue>0</defaultValue></stateVariable><stateVariable sendEvents="yes"><name>Volume</name><dataType>ui2</dataType><defaultValue>100</defaultValue><allowedValueRange><minimum>0</minimum><maximum>100</maximum><step>1</step></allowedValueRange></stateVariable></serviceStateTable></scpd>`
