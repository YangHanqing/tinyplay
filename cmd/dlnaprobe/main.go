// Command dlnaprobe is a throwaway diagnostic renderer. It mirrors what
// internal/dlna advertises, but logs every SSDP packet it receives, every
// response it sends, and every HTTP request a sender makes — so we can tell
// where a picky control point (腾讯视频 / 乐播 HpplaySDK) stops talking to us.
//
// Run it with TinyPlay quit, so nothing else holds UDP 1900:
//
//	go run ./cmd/dlnaprobe
//
// Then open the app's cast picker and watch the log.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/ipv4"
)

var (
	httpPort = flag.Int("port", 1980, "HTTP port for the device description")
	uuid     = flag.String("uuid", "probe-0000-0000-0000-tinyplaydiag", "UPnP device UUID")
	name     = flag.String("name", "TinyPlay (PROBE)", "friendlyName shown in pickers")
	verbose  = flag.Bool("all", false, "log every SSDP packet, including NOTIFY from other devices")
)

var groupUDP = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

const (
	rendererType          = "urn:schemas-upnp-org:device:MediaRenderer:1"
	avTransportType       = "urn:schemas-upnp-org:service:AVTransport:1"
	connectionManagerType = "urn:schemas-upnp-org:service:ConnectionManager:1"
	renderingControlType  = "urn:schemas-upnp-org:service:RenderingControl:1"
)

func targets() []string {
	return []string{
		"upnp:rootdevice",
		"uuid:" + *uuid,
		rendererType,
		renderingControlType,
		connectionManagerType,
		avTransportType,
	}
}

func usn(target string) string {
	base := "uuid:" + *uuid
	switch {
	case strings.EqualFold(target, base):
		return base
	case target == "upnp:rootdevice":
		return base + "::upnp:rootdevice"
	default:
		return base + "::" + target
	}
}

func main() {
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	go serveHTTP()

	send, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		log.Fatalf("send socket: %v", err)
	}
	pc := ipv4.NewPacketConn(send)
	_ = pc.SetMulticastTTL(4)

	var socks []*sock
	for _, ifi := range suitableInterfaces() {
		ip := firstIPv4(ifi)
		if ip == nil {
			continue
		}
		ifi := ifi
		conn, err := net.ListenMulticastUDP("udp4", &ifi, groupUDP)
		if err != nil {
			log.Printf("interface %s not joined: %v", ifi.Name, err)
			continue
		}
		s := &sock{ifi: ifi, ip: ip, conn: conn, send: pc}
		socks = append(socks, s)
		log.Printf("listening on %s (%s)", ifi.Name, ip)
		go s.serve()
	}
	if len(socks) == 0 {
		log.Fatal("no interface joined the SSDP group")
	}

	advertise(socks, pc, "ssdp:alive")
	log.Printf("probe ready — friendlyName=%q device.xml on port %d", *name, *httpPort)
	log.Printf("open the sender's cast picker now; press Ctrl-C to stop")

	tick := time.NewTicker(30 * time.Second)
	for range tick.C {
		advertise(socks, pc, "ssdp:alive")
	}
}

type sock struct {
	ifi  net.Interface
	ip   net.IP
	conn *net.UDPConn
	send *ipv4.PacketConn
}

func (s *sock) serve() {
	buf := make([]byte, 8192)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("%s read error: %v", s.ifi.Name, err)
			return
		}
		s.handle(addr, string(buf[:n]))
	}
}

func (s *sock) handle(addr *net.UDPAddr, raw string) {
	upper := strings.ToUpper(raw)
	if !strings.HasPrefix(upper, "M-SEARCH") {
		if *verbose {
			log.Printf("[%s] non-search from %s: %s", s.ifi.Name, addr, firstLine(raw))
		}
		return
	}

	st := header(raw, "st")
	log.Printf("── M-SEARCH from %s on %s", addr, s.ifi.Name)
	log.Printf("   ST=%q MX=%q MAN=%q HOST=%q UA=%q",
		st, header(raw, "mx"), header(raw, "man"), header(raw, "host"), header(raw, "user-agent"))
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key := strings.ToUpper(strings.SplitN(line, ":", 2)[0])
		switch key {
		case "ST", "MX", "MAN", "HOST", "USER-AGENT", "M-SEARCH * HTTP/1.1":
		default:
			log.Printf("   extra header: %s", line) // Hpplay adds private ones
		}
	}

	matched := matchTargets(st)
	if len(matched) == 0 {
		log.Printf("   !! NOT ANSWERED — ST does not match any advertised target")
		return
	}

	delayLimit := searchDelay(header(raw, "mx"))
	cm := &ipv4.ControlMessage{IfIndex: s.ifi.Index}
	for _, target := range matched {
		resp := response(target, s.ip, *httpPort)
		delay := time.Duration(0)
		if delayLimit > 0 {
			delay = time.Duration(rand.Int63n(int64(delayLimit) + 1))
		}
		target := target
		time.AfterFunc(delay, func() {
			if _, err := s.send.WriteTo([]byte(resp), cm, addr); err != nil {
				log.Printf("   reply ST=%s FAILED: %v", target, err)
				return
			}
			log.Printf("   replied ST=%s after %v", target, delay)
		})
	}
}

func matchTargets(requested string) []string {
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(requested, "ssdp:all") {
		return targets()
	}
	for _, t := range targets() {
		if strings.EqualFold(requested, t) {
			return []string{t}
		}
	}
	return nil
}

func serveHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		dump, _ := httputil.DumpRequest(req, true)
		log.Printf("<< HTTP %s %s from %s\n%s", req.Method, req.URL.Path, req.RemoteAddr, indent(string(dump)))
		switch req.URL.Path {
		case "/dlna/device.xml":
			xml := deviceXML()
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(xml))
			log.Printf(">> served device.xml (%d bytes)", len(xml))
		default:
			http.NotFound(w, req)
			log.Printf(">> 404 %s  (probe only serves device.xml)", req.URL.Path)
		}
	})
	addr := ":" + strconv.Itoa(*httpPort)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http %s: %v (is TinyPlay still running?)", addr, err)
	}
}

func deviceXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?><root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><device><deviceType>` + rendererType + `</deviceType><friendlyName>` + *name + `</friendlyName><manufacturer>TinyPlay</manufacturer><modelDescription>TinyPlay DLNA Media Renderer</modelDescription><modelName>TinyPlay DLNA Receiver</modelName><modelNumber>1.0</modelNumber><UDN>uuid:` + *uuid + `</UDN><dlna:X_DLNADOC>DMR-1.50</dlna:X_DLNADOC><serviceList><service><serviceType>` + avTransportType + `</serviceType><serviceId>urn:upnp-org:serviceId:AVTransport</serviceId><SCPDURL>/dlna/AVTransport.xml</SCPDURL><controlURL>/dlna/AVTransport/control</controlURL><eventSubURL>/dlna/AVTransport/event</eventSubURL></service><service><serviceType>` + renderingControlType + `</serviceType><serviceId>urn:upnp-org:serviceId:RenderingControl</serviceId><SCPDURL>/dlna/RenderingControl.xml</SCPDURL><controlURL>/dlna/RenderingControl/control</controlURL><eventSubURL>/dlna/RenderingControl/event</eventSubURL></service><service><serviceType>` + connectionManagerType + `</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/dlna/ConnectionManager.xml</SCPDURL><controlURL>/dlna/ConnectionManager/control</controlURL><eventSubURL>/dlna/ConnectionManager/event</eventSubURL></service></serviceList></device></root>`
}

func advertise(socks []*sock, send *ipv4.PacketConn, nts string) {
	for _, s := range socks {
		cm := &ipv4.ControlMessage{IfIndex: s.ifi.Index}
		for _, target := range targets() {
			msg := strings.Join([]string{
				"NOTIFY * HTTP/1.1", "HOST: 239.255.255.250:1900", "CACHE-CONTROL: max-age=1800",
				"LOCATION: " + location(s.ip, *httpPort), "NT: " + target, "NTS: " + nts,
				"SERVER: TinyPlay/1.0 UPnP/1.1", "USN: " + usn(target), "", "",
			}, "\r\n")
			_, _ = send.WriteTo([]byte(msg), cm, groupUDP)
		}
	}
}

func response(target string, ip net.IP, port int) string {
	return strings.Join([]string{
		"HTTP/1.1 200 OK", "CACHE-CONTROL: max-age=1800", "EXT:", "LOCATION: " + location(ip, port),
		"SERVER: TinyPlay/1.0 UPnP/1.1", "DATE: " + time.Now().UTC().Format(http.TimeFormat),
		"ST: " + target, "USN: " + usn(target), "", "",
	}, "\r\n")
}

func location(ip net.IP, port int) string {
	return fmt.Sprintf("http://%s:%d/dlna/device.xml", ip, port)
}

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
		if firstIPv4(ifi) != nil {
			out = append(out, ifi)
		}
	}
	return out
}

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

func header(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, ":"); i > 0 && strings.EqualFold(strings.TrimSpace(line[:i]), key) {
			return strings.TrimSpace(line[i+1:])
		}
	}
	return ""
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

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		return s[:i]
	}
	return s
}

func indent(s string) string {
	return "   " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n   ")
}
