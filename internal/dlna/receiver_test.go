package dlna

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"tvremote/internal/player"
)

func TestSearchTargetsAreCaseInsensitiveAndComplete(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := &Receiver{}

	direct := r.searchTargets("URN:SCHEMAS-UPNP-ORG:DEVICE:MEDIARENDERER:1")
	if len(direct) != 1 || direct[0] != rendererType {
		t.Fatalf("MediaRenderer search returned %q", direct)
	}

	all := r.searchTargets("ssdp:all")
	if len(all) != 6 {
		t.Fatalf("ssdp:all returned %d targets, want 6: %q", len(all), all)
	}
	for _, wanted := range []string{rendererType, avTransportType, connectionManagerType, renderingControlType} {
		if !contains(all, wanted) {
			t.Errorf("ssdp:all is missing %q", wanted)
		}
	}
}

func TestDeviceDescriptionAdvertisesDLNARendererServices(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	xml := (&Receiver{}).deviceXML()
	for _, wanted := range []string{"<friendlyName>" + FriendlyName() + "</friendlyName>", "DMR-1.50", avTransportType, connectionManagerType, renderingControlType} {
		if !strings.Contains(xml, wanted) {
			t.Errorf("device description is missing %q", wanted)
		}
	}
	assertWellFormedXML(t, xml)
	for _, scpd := range []string{avTransportSCPD, connectionManagerSCPD, renderingControlSCPD} {
		assertWellFormedXML(t, scpd)
	}
}

func TestSubscribeSendsInitialGENAEvent(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())

	var (
		mu      sync.Mutex
		got     []genaCapture
		gotSID  string
		arrived = make(chan struct{}, 1)
	)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "NOTIFY" {
			t.Errorf("callback method = %s, want NOTIFY", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, genaCapture{
			sid:  r.Header.Get("SID"),
			seq:  r.Header.Get("SEQ"),
			nt:   r.Header.Get("NT"),
			nts:  r.Header.Get("NTS"),
			body: string(body),
		})
		if len(got) == 1 {
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()

	r := New(nil, func() int { return 1980 })
	req := httptest.NewRequest("SUBSCRIBE", "/dlna/AVTransport/event", nil)
	req.Header.Set("CALLBACK", "<"+callback.URL+">")
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("TIMEOUT", "Second-300")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SUBSCRIBE status = %d, want 200", rec.Code)
	}
	gotSID = rec.Header().Get("SID")
	if !strings.HasPrefix(gotSID, "uuid:") {
		t.Fatalf("SID = %q, want uuid:…", gotSID)
	}
	if rec.Header().Get("TIMEOUT") != "Second-300" {
		t.Fatalf("TIMEOUT = %q", rec.Header().Get("TIMEOUT"))
	}

	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial GENA NOTIFY")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 1 {
		t.Fatal("expected at least one NOTIFY")
	}
	ev := got[0]
	if ev.sid != gotSID {
		t.Errorf("NOTIFY SID = %q, want %q", ev.sid, gotSID)
	}
	if ev.seq != "0" {
		t.Errorf("initial SEQ = %q, want 0", ev.seq)
	}
	if ev.nt != "upnp:event" || ev.nts != "upnp:propchange" {
		t.Errorf("NT/NTS = %q/%q", ev.nt, ev.nts)
	}
	if !strings.Contains(ev.body, "LastChange") {
		t.Errorf("body missing LastChange: %s", ev.body)
	}
	// LastChange content is XML-escaped inside the propertyset.
	if !strings.Contains(ev.body, "TransportState") && !strings.Contains(ev.body, "TransportState val=") {
		// Escaped form uses &quot; around attributes; look for TransportState token.
		if !strings.Contains(ev.body, "TransportState") {
			t.Errorf("body missing TransportState: %s", ev.body)
		}
	}
	if !strings.Contains(ev.body, "NO_MEDIA_PRESENT") && !strings.Contains(ev.body, "STOPPED") {
		t.Errorf("body missing idle transport state: %s", ev.body)
	}
}

func TestParseCallbackURLs(t *testing.T) {
	got := parseCallbackURLs(`<http://a/1><http://b/2>`)
	if len(got) != 2 || got[0] != "http://a/1" || got[1] != "http://b/2" {
		t.Fatalf("parseCallbackURLs = %#v", got)
	}
}

func TestDeviceDescriptionAdvertisesLastChange(t *testing.T) {
	for _, scpd := range []string{avTransportSCPD, renderingControlSCPD} {
		if !strings.Contains(scpd, `<stateVariable sendEvents="yes"><name>LastChange</name>`) {
			t.Errorf("SCPD missing evented LastChange: %s", scpd[:min(120, len(scpd))])
		}
	}
}

func TestAVTransportSCPDHasFullActionSet(t *testing.T) {
	// Standard UPnP AVTransport action set we advertise.
	for _, name := range []string{
		"SetAVTransportURI", "Play", "Pause", "Stop", "Seek",
		"GetTransportInfo", "GetPositionInfo", "GetMediaInfo",
		"GetDeviceCapabilities", "GetTransportSettings",
		"GetCurrentTransportActions", "Next", "Previous", "SetPlayMode",
	} {
		if !strings.Contains(avTransportSCPD, "<name>"+name+"</name>") {
			t.Errorf("AVTransport SCPD missing action %s", name)
		}
	}
	if !strings.Contains(avTransportSCPD, "argumentList") {
		t.Fatal("AVTransport SCPD must declare argumentList (Cling/iQIYI)")
	}
}

func TestDidlTitle(t *testing.T) {
	meta := `<?xml version="1.0"?><DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/"><item><dc:title>测试影片</dc:title></item></DIDL-Lite>`
	if got := didlTitle(meta); got != "测试影片" {
		t.Fatalf("didlTitle = %q", got)
	}
	if didlTitle("") != "" || didlTitle("not-xml") != "" {
		t.Fatal("empty/bad meta should return empty title")
	}
}

func TestLifecycleUpdatesStateAndGENA(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	var (
		mu      sync.Mutex
		bodies  []string
		arrived = make(chan struct{}, 2)
	)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		if len(bodies) >= 2 {
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()

	r := New(nil, func() int { return 1980 })
	// Start event loop without SSDP sockets so pending marks flush.
	r.mu.Lock()
	r.stop = make(chan struct{})
	r.running = true
	r.mu.Unlock()
	r.wg.Add(1)
	go r.eventLoop(r.stop)
	defer func() {
		close(r.stop)
		r.wg.Wait()
	}()

	req := httptest.NewRequest("SUBSCRIBE", "/dlna/AVTransport/event", nil)
	req.Header.Set("CALLBACK", "<"+callback.URL+">")
	req.Header.Set("NT", "upnp:event")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("subscribe %d", rec.Code)
	}

	// Wait for initial event, then player lifecycle → second GENA.
	time.Sleep(100 * time.Millisecond)
	r.onPlayerLifecycle(player.LifecyclePlaying, player.PlayContext{SourceType: "dlna", Title: "x"}, 12, "")
	// Flush pending sooner than 250ms by calling publish directly after mark.
	// eventLoop may take up to 250ms; wait a bit.
	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for lifecycle GENA")
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "PLAYING") {
		t.Fatalf("expected PLAYING in GENA bodies, got %s", joined)
	}
	if r.snapState().TransportState != "PLAYING" {
		t.Fatalf("state = %s", r.snapState().TransportState)
	}
}

func TestSinkProtocolInfoIsSubstantial(t *testing.T) {
	// Must stay well above the old 4-entry stub.
	if strings.Count(sinkProtocolInfo, "http-get:") < 20 {
		t.Fatalf("sinkProtocolInfo too thin: %d entries", strings.Count(sinkProtocolInfo, "http-get:"))
	}
	for _, need := range []string{"video/m3u8", "video/mp4", "video/*", "mpegurl"} {
		if !strings.Contains(strings.ToLower(sinkProtocolInfo), strings.ToLower(need)) {
			t.Errorf("sink missing %s", need)
		}
	}
}

// A cast that plays on screen while the sender reports failure is almost always
// an event the sender could not decode: the transport never appears to leave
// TRANSITIONING, so the control point gives up and disables its buttons. These
// pin the property set as a decodable document end to end — propertyset, the
// LastChange it wraps, and the DIDL inside that — for the metadata senders
// really send: kilobytes long, with CJK titles.
func TestLastChangeStaysDecodableWithLargeCJKMetadata(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })

	// A signed CDN URL plus a Chinese title: the realistic shape, and long
	// enough that any fixed-length cut lands mid-URL or mid-rune.
	longURL := "https://cache.video.example.com/videos/" + strings.Repeat("a1b2c3d4", 1500) + "/master.m3u8?tk=1"
	meta := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		`<item id="1" parentID="0" restricted="1"><dc:title>庆余年 第二季 第01集</dc:title>` +
		`<upnp:class>object.item.videoItem</upnp:class>` +
		`<res protocolInfo="http-get:*:video/mp4:*">` + longURL + `</res></item></DIDL-Lite>`
	if len(meta) <= maxEventMetadataBytes {
		t.Fatalf("fixture must exceed the event metadata cap (%d bytes)", len(meta))
	}

	r.state.setURI(longURL, meta, "庆余年 第二季 第01集")
	r.state.setTransport("PLAYING", "OK")
	assertDecodableAVTEvent(t, r.eventPropertySet(serviceAVT), "PLAYING")

	// Metadata that fits must travel intact, CJK and all.
	small := `<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/"><item><dc:title>测试影片 &amp; 续集</dc:title></item></DIDL-Lite>`
	r.state.setURI("https://example.test/a.mp4", small, "测试影片")
	didl := assertDecodableAVTEvent(t, r.eventPropertySet(serviceAVT), "")
	if didl != small {
		t.Fatalf("metadata within the cap was altered:\n got %q\nwant %q", didl, small)
	}
}

func TestEmittedXMLDropsCharactersNoDocumentCanCarry(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })
	// A control character and a lone continuation byte have no escaped form,
	// so passing either through makes the whole document undecodable.
	r.state.setURI("https://example.test/a.mp4", "<DIDL-Lite><item>\x01\xffbad</item></DIDL-Lite>", "t\x0bitle")
	assertDecodableAVTEvent(t, r.eventPropertySet(serviceAVT), "")

	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetPositionInfo xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID></u:GetPositionInfo></s:Body></s:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/dlna/AVTransport/control", strings.NewReader(body))
	req.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:AVTransport:1#GetPositionInfo"`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetPositionInfo status %d", rec.Code)
	}
	assertWellFormedXML(t, rec.Body.String())
}

// assertDecodableAVTEvent walks propertyset → LastChange → DIDL, returning the
// metadata a control point would end up with. wantState, when set, must appear
// as the TransportState attribute.
func assertDecodableAVTEvent(t *testing.T, propertySet, wantState string) string {
	t.Helper()
	if !utf8.ValidString(propertySet) {
		t.Fatal("property set is not valid UTF-8; every event would be undecodable")
	}
	assertWellFormedXML(t, propertySet)

	lastChange := ""
	decoder := xml.NewDecoder(strings.NewReader(propertySet))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "LastChange" {
			if err := decoder.DecodeElement(&lastChange, &start); err != nil {
				t.Fatalf("LastChange value: %v", err)
			}
		}
	}
	if lastChange == "" {
		t.Fatalf("no LastChange in property set: %s", propertySet)
	}
	assertWellFormedXML(t, lastChange)

	metadata, state := "", ""
	inner := xml.NewDecoder(strings.NewReader(lastChange))
	for {
		token, err := inner.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local != "val" {
				continue
			}
			switch start.Name.Local {
			case "TransportState":
				state = attr.Value
			case "CurrentTrackMetaData":
				metadata = attr.Value
			}
		}
	}
	if wantState != "" && state != wantState {
		t.Fatalf("TransportState = %q, want %q", state, wantState)
	}
	if metadata != "" {
		assertWellFormedXML(t, metadata)
	}
	return metadata
}

func TestSOAPFaultCarriesEXT(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:SetAVTransportURI xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID><CurrentURI>ftp://nope/x.mp4</CurrentURI></u:SetAVTransportURI></s:Body></s:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/dlna/AVTransport/control", strings.NewReader(body))
	req.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:AVTransport:1#SetAVTransportURI"`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("bad URI status = %d, want 500", rec.Code)
	}
	if _, ok := rec.Header()["Ext"]; !ok {
		t.Fatalf("SOAP fault missing EXT header: %#v", rec.Header())
	}
	assertWellFormedXML(t, rec.Body.String())
}

func TestHTMLUnescapeResolvesInOnePass(t *testing.T) {
	// "&amp;lt;" is a literal "&lt;" inside DIDL; rescanning would eat it.
	if got := htmlUnescape("a&amp;lt;b"); got != "a&lt;b" {
		t.Errorf("double-unescaped: %q", got)
	}
	if got := htmlUnescape("q=1&amp;p=2"); got != "q=1&p=2" {
		t.Errorf("query decode = %q", got)
	}
	if got := htmlUnescape("it&apos;s &#39;ok&#39; &#x4e2d;"); got != "it's 'ok' 中" {
		t.Errorf("entity decode = %q", got)
	}
	if got := htmlUnescape("a & b"); got != "a & b" {
		t.Errorf("bare ampersand = %q", got)
	}
}

func TestSeekOnlyMovesTimeForTimeUnits(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })
	r.state.setRelTime("00:10:00")

	seek := func(unit, target string) {
		t.Helper()
		body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:Seek xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID><Unit>` + unit + `</Unit><Target>` + target + `</Target></u:Seek></s:Body></s:Envelope>`
		req := httptest.NewRequest(http.MethodPost, "/dlna/AVTransport/control", strings.NewReader(body))
		req.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:AVTransport:1#Seek"`)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Seek(%s) status %d", unit, rec.Code)
		}
	}

	// "1" is a track number, not a position; it must not rewind the title.
	seek("TRACK_NR", "1")
	if got := r.snapState().RelTime; got != "00:10:00" {
		t.Fatalf("TRACK_NR seek moved position to %q", got)
	}
	seek("REL_TIME", "00:20:30")
	if got := r.snapState().RelTime; got != "00:20:30" {
		t.Fatalf("REL_TIME seek = %q", got)
	}
}

type genaCapture struct {
	sid, seq, nt, nts, body string
}

func assertWellFormedXML(t *testing.T, value string) {
	t.Helper()
	var document struct{}
	if err := xml.Unmarshal([]byte(value), &document); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestSubscribeRenewUnknownSID(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })
	req := httptest.NewRequest("SUBSCRIBE", "/dlna/AVTransport/event", nil)
	req.Header.Set("SID", "uuid:does-not-exist")
	req.Header.Set("TIMEOUT", "Second-180")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("renew unknown SID status = %d, want 412", rec.Code)
	}
}

func TestSetPlayModeUpdatesState(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:SetPlayMode xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID><NewPlayMode>REPEAT_ONE</NewPlayMode></u:SetPlayMode></s:Body></s:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/dlna/AVTransport/control", strings.NewReader(body))
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:AVTransport:1#SetPlayMode"`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("SetPlayMode status %d body %s", rec.Code, rec.Body.String())
	}
	if r.snapState().PlayMode != "REPEAT_ONE" {
		t.Fatalf("PlayMode = %q", r.snapState().PlayMode)
	}
}

func TestDeviceDescriptionHasManufacturerURL(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	xml := (&Receiver{state: newRendererState()}).deviceXML()
	for _, want := range []string{"manufacturerURL", "modelURL", "serialNumber", "DMR-1.50"} {
		if !strings.Contains(xml, want) {
			t.Errorf("device.xml missing %s", want)
		}
	}
}

func TestSOAPResponseUsesSCPDArgumentOrderAndBodyActionFallback(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	r := New(nil, func() int { return 1980 })
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetProtocolInfo xmlns:u="urn:schemas-upnp-org:service:ConnectionManager:1"><InstanceID>0</InstanceID></u:GetProtocolInfo></s:Body></s:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/dlna/ConnectionManager/control", strings.NewReader(body))
	// Deliberately omit SOAPAction: some valid stacks rely on the body action.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetProtocolInfo status %d body %s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	if strings.Index(response, "<Source>") > strings.Index(response, "<Sink>") {
		t.Fatalf("SOAP outputs are not in SCPD order: %s", response)
	}
	if _, ok := rec.Header()["Ext"]; !ok {
		t.Fatalf("SOAP response missing EXT header: %#v", rec.Header())
	}
}

func TestSetAVTransportURIAcknowledgesBeforePlayerReady(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	release := make(chan struct{})
	started := make(chan struct{})
	r := New(nil, func() int { return 1980 })
	r.playURI = func(string, player.PlayOptions) map[string]any {
		close(started)
		<-release
		return map[string]any{"ok": true}
	}
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:SetAVTransportURI xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID><CurrentURI>https://example.test/slow.m3u8</CurrentURI><CurrentURIMetaData></CurrentURIMetaData></u:SetAVTransportURI></s:Body></s:Envelope>`
	req := httptest.NewRequest(http.MethodPost, "/dlna/AVTransport/control", strings.NewReader(body))
	req.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:AVTransport:1#SetAVTransportURI"`)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		r.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		close(release)
		t.Fatal("SetAVTransportURI held the SOAP response until player readiness")
	}
	if rec.Code != http.StatusOK {
		close(release)
		t.Fatalf("SetAVTransportURI status %d body %s", rec.Code, rec.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("background playback did not start")
	}
	close(release)
}

func TestGENAInitialEventCannotBeOvertaken(t *testing.T) {
	t.Setenv("TVREMOTE_DATA_DIR", t.TempDir())
	initialArrived := make(chan struct{})
	releaseInitial := make(chan struct{})
	secondArrived := make(chan struct{})
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Header.Get("SEQ") {
		case "0":
			select {
			case <-initialArrived:
			default:
				close(initialArrived)
			}
			<-releaseInitial
		case "1":
			close(secondArrived)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()

	r := New(nil, func() int { return 1980 })
	req := httptest.NewRequest("SUBSCRIBE", "/dlna/AVTransport/event", nil)
	req.Header.Set("CALLBACK", "<"+callback.URL+">")
	req.Header.Set("NT", "upnp:event")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	select {
	case <-initialArrived:
	case <-time.After(time.Second):
		t.Fatal("initial GENA event did not arrive")
	}
	r.publishServiceEvent(serviceAVT)
	select {
	case <-secondArrived:
		close(releaseInitial)
		t.Fatal("SEQ 1 overtook the blocked SEQ 0 event")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseInitial)
	select {
	case <-secondArrived:
	case <-time.After(time.Second):
		t.Fatal("SEQ 1 did not arrive after SEQ 0 completed")
	}
}
