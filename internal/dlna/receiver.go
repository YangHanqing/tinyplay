// Package dlna implements the small, interoperable core of a UPnP AV
// MediaRenderer: SSDP discovery plus ConnectionManager and AVTransport SOAP.
// It deliberately has no media-library concept; a sender supplies a URL and
// the injected player replaces whatever is currently playing.
package dlna

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tvremote/internal/config"
	"tvremote/internal/netutil"
	"tvremote/internal/player"
)

const (
	group        = "239.255.255.250:1900"
	rendererType = "urn:schemas-upnp-org:device:MediaRenderer:1"
)

// Receiver is safe to start/stop repeatedly as the Settings toggle changes.
type Receiver struct {
	p       *player.Player
	port    func() int
	mu      sync.Mutex
	conn    *net.UDPConn
	stop    chan struct{}
	stopped chan struct{}
}

func New(p *player.Player, port func() int) *Receiver { return &Receiver{p: p, port: port} }

func (r *Receiver) Start() {
	r.mu.Lock()
	if r.conn != nil {
		r.mu.Unlock()
		return
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900})
	if err != nil {
		r.mu.Unlock()
		log.Printf("DLNA receiver unavailable (UDP 1900): %v", err)
		return
	}
	r.conn, r.stop, r.stopped = conn, make(chan struct{}), make(chan struct{})
	stop, stopped := r.stop, r.stopped
	r.mu.Unlock()
	go r.serve(conn, stop, stopped)
	go r.advertiseLoop(conn, stop)
	log.Printf("DLNA receiver enabled on UDP 1900")
}

func (r *Receiver) Stop() {
	r.mu.Lock()
	conn, stop, stopped := r.conn, r.stop, r.stopped
	if conn == nil {
		r.mu.Unlock()
		return
	}
	r.conn = nil
	r.mu.Unlock()
	r.notify(conn, "ssdp:byebye")
	close(stop)
	_ = conn.Close()
	<-stopped
	log.Printf("DLNA receiver disabled")
}

func (r *Receiver) serve(conn *net.UDPConn, stop <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	buf := make([]byte, 8192)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-stop:
					return
				default:
					continue
				}
			}
			return
		}
		r.handleSearch(conn, addr, string(buf[:n]))
	}
}

func (r *Receiver) advertiseLoop(conn *net.UDPConn, stop <-chan struct{}) {
	r.notify(conn, "ssdp:alive")
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			r.notify(conn, "ssdp:alive")
		}
	}
}

func (r *Receiver) handleSearch(conn *net.UDPConn, addr *net.UDPAddr, request string) {
	upper := strings.ToUpper(request)
	if !strings.HasPrefix(upper, "M-SEARCH * HTTP/1.1") || !strings.Contains(upper, "SSDP:DISCOVER") {
		return
	}
	st := strings.ToLower(header(request, "st"))
	var targets []string
	switch st {
	case "ssdp:all":
		targets = []string{"upnp:rootdevice", rendererType}
	case "upnp:rootdevice", rendererType:
		targets = []string{st}
	default:
		return
	}
	for _, target := range targets {
		_, _ = conn.WriteToUDP([]byte(r.response(target)), addr)
	}
}

func (r *Receiver) notify(conn *net.UDPConn, nts string) {
	addr, _ := net.ResolveUDPAddr("udp4", group)
	for _, target := range []string{"upnp:rootdevice", rendererType} {
		message := strings.Join([]string{
			"NOTIFY * HTTP/1.1", "HOST: " + group, "CACHE-CONTROL: max-age=1800",
			"LOCATION: " + r.location(), "NT: " + target, "NTS: " + nts,
			"SERVER: TinyPlay/1.0 UPnP/1.1", "USN: " + r.usn(target), "", "",
		}, "\r\n")
		_, _ = conn.WriteToUDP([]byte(message), addr)
	}
}

func (r *Receiver) response(target string) string {
	return strings.Join([]string{
		"HTTP/1.1 200 OK", "CACHE-CONTROL: max-age=1800", "EXT:", "LOCATION: " + r.location(),
		"SERVER: TinyPlay/1.0 UPnP/1.1", "ST: " + target, "USN: " + r.usn(target), "", "",
	}, "\r\n")
}

func (r *Receiver) location() string {
	return fmt.Sprintf("http://%s:%d/dlna/device.xml", netutil.LocalIP(), r.port())
}
func (r *Receiver) usn(target string) string {
	base := "uuid:" + config.DLNAReceiverID()
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
	case req.Method == "SUBSCRIBE" && strings.HasPrefix(req.URL.Path, "/dlna/"):
		w.Header().Set("SID", "uuid:"+config.DLNAReceiverID())
		w.Header().Set("TIMEOUT", "Second-300")
		w.WriteHeader(http.StatusOK)
	case req.Method == http.MethodPost && req.URL.Path == "/dlna/ConnectionManager/control":
		r.connectionManager(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/dlna/AVTransport/control":
		r.avTransport(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (r *Receiver) deviceXML() string {
	id := config.DLNAReceiverID()
	suffix := id
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	// The UUID remains the protocol identity. Its short, stable suffix keeps
	// multiple TinyPlay receivers distinguishable in a sender's device picker.
	name := xmlEscape("TinyPlay (" + strings.ToUpper(suffix) + ")")
	return `<?xml version="1.0" encoding="UTF-8"?><root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><device><deviceType>` + rendererType + `</deviceType><friendlyName>` + name + `</friendlyName><manufacturer>TinyPlay</manufacturer><modelName>TinyPlay DLNA Receiver</modelName><modelNumber>1.0</modelNumber><UDN>uuid:` + id + `</UDN><serviceList><service><serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/dlna/ConnectionManager.xml</SCPDURL><controlURL>/dlna/ConnectionManager/control</controlURL><eventSubURL>/dlna/ConnectionManager/event</eventSubURL></service><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><serviceId>urn:upnp-org:serviceId:AVTransport</serviceId><SCPDURL>/dlna/AVTransport.xml</SCPDURL><controlURL>/dlna/AVTransport/control</controlURL><eventSubURL>/dlna/AVTransport/event</eventSubURL></service></serviceList></device></root>`
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

const connectionManagerSCPD = `<?xml version="1.0"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetProtocolInfo</name></action><action><name>GetCurrentConnectionIDs</name></action><action><name>GetCurrentConnectionInfo</name></action></actionList></scpd>`
const avTransportSCPD = `<?xml version="1.0"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>SetAVTransportURI</name></action><action><name>Play</name></action><action><name>Pause</name></action><action><name>Stop</name></action><action><name>Seek</name></action><action><name>GetTransportInfo</name></action><action><name>GetPositionInfo</name></action><action><name>GetMediaInfo</name></action><action><name>GetDeviceCapabilities</name></action><action><name>GetTransportSettings</name></action></actionList></scpd>`
