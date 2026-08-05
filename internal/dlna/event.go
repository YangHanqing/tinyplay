package dlna

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"tvremote/internal/player"
)

// service keys used as subscription map buckets.
const (
	serviceAVT = "AVTransport"
	serviceRCS = "RenderingControl"
	serviceCM  = "ConnectionManager"
)

// subscription is one GENA session: a control point's CALLBACK list + SEQ.
// UPnP Device Architecture requires an initial event (SEQ 0) as soon as the
// SUBSCRIBE response is accepted; iQIYI waits on that before SetAVTransportURI.
type subscription struct {
	sid       string // "uuid:…"
	service   string
	callbacks []string
	seq       uint32
	expires   time.Time
	errors    int // consecutive NOTIFY failures; drop after 10
	// Keep callback requests in strict on-the-wire SEQ order. Reserving numbers
	// under Receiver.mu alone does not stop concurrent HTTP requests arriving as
	// 1,0 or 2,1 at the control point.
	initialDone chan struct{}
	notifyMu    sync.Mutex
}

// eventLoop batches GENA publishes roughly once per 250ms.
// Callers mark pendingAVT/pendingRCS under r.mu; this loop drains them.
func (r *Receiver) eventLoop(stop <-chan struct{}) {
	defer r.wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			// Drop expired subscriptions even without a pending publish.
			r.mu.Lock()
			now := time.Now()
			for sid, sub := range r.subs {
				if now.After(sub.expires) {
					delete(r.subs, sid)
				}
			}
			avt, rcs := r.pendingAVT, r.pendingRCS
			r.pendingAVT, r.pendingRCS = false, false
			r.mu.Unlock()
			if avt {
				r.publishServiceEvent(serviceAVT)
			}
			if rcs {
				r.publishServiceEvent(serviceRCS)
			}
		}
	}
}

// markServicePending queues a GENA flush for service (deduped until eventLoop runs).
func (r *Receiver) markServicePending(service string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch service {
	case serviceAVT:
		r.pendingAVT = true
	case serviceRCS:
		r.pendingRCS = true
	}
}

// notifyTransport publishes AVTransport LastChange after an observed change.
func (r *Receiver) notifyTransport() { r.markServicePending(serviceAVT) }
func (r *Receiver) notifyRendering() { r.markServicePending(serviceRCS) }

// onPlayerLifecycle is wired as player.LifecycleReporter. Only DLNA sessions update
// the renderer state table (player engine → set state → GENA queue).
func (r *Receiver) onPlayerLifecycle(event player.LifecycleEvent, ctx player.PlayContext, durationSeconds float64, reason string) {
	if ctx.SourceType != "dlna" {
		return
	}
	if r.state == nil {
		return
	}
	switch event {
	case player.LifecyclePlaying, player.LifecycleResumed:
		if r.state.setTransport("PLAYING", "OK") {
			r.notifyTransport()
		}
	case player.LifecyclePaused:
		if r.state.setTransport("PAUSED_PLAYBACK", "OK") {
			r.notifyTransport()
		}
	case player.LifecycleEOF:
		if r.state.setTransport("NO_MEDIA_PRESENT", "OK") {
			r.notifyTransport()
		}
	case player.LifecycleError:
		if r.state.setTransport("STOPPED", "ERROR_OCCURRED") {
			r.notifyTransport()
		}
	case player.LifecycleStopped:
		if r.state.setTransport("STOPPED", "OK") {
			r.notifyTransport()
		}
	case player.LifecycleDuration:
		if r.state.setDuration(dlnaTime(durationSeconds)) {
			r.notifyTransport()
		}
	}
	_ = reason
}

func (r *Receiver) initSubsLocked() {
	if r.subs == nil {
		r.subs = map[string]*subscription{}
	}
}

func (r *Receiver) clearSubscriptions() {
	r.mu.Lock()
	r.subs = map[string]*subscription{}
	r.mu.Unlock()
}

// handleSubscribe implements GENA SUBSCRIBE (new + renew).
// It always replies first, then fires the mandatory initial event for new
// subscriptions asynchronously so a slow CALLBACK cannot stall the response.
func (r *Receiver) handleSubscribe(w http.ResponseWriter, req *http.Request) {
	service := serviceFromEventPath(req.URL.Path)
	if service == "" {
		http.NotFound(w, req)
		return
	}

	existingSID := strings.TrimSpace(req.Header.Get("SID"))
	timeout := parseSubscribeTimeout(req.Header.Get("TIMEOUT"))
	expires := time.Now().Add(timeout)

	// Renew path: SID present, no CALLBACK required.
	if existingSID != "" {
		r.mu.Lock()
		r.initSubsLocked()
		sub, ok := r.subs[existingSID]
		if !ok || sub.service != service {
			r.mu.Unlock()
			w.WriteHeader(http.StatusPreconditionFailed)
			log.Printf("DLNA SUBSCRIBE renew rejected unknown sid=%s path=%s", existingSID, req.URL.Path)
			return
		}
		sub.expires = expires
		sid := sub.sid
		r.mu.Unlock()
		w.Header().Set("SID", sid)
		w.Header().Set("TIMEOUT", formatSubscribeTimeout(timeout))
		w.WriteHeader(http.StatusOK)
		log.Printf("DLNA SUBSCRIBE renew ok sid=%s service=%s timeout=%s", sid, service, formatSubscribeTimeout(timeout))
		return
	}

	callbacks := parseCallbackURLs(req.Header.Get("CALLBACK"))
	if len(callbacks) == 0 || !strings.EqualFold(strings.TrimSpace(req.Header.Get("NT")), "upnp:event") {
		w.WriteHeader(http.StatusPreconditionFailed)
		log.Printf("DLNA SUBSCRIBE rejected: missing CALLBACK/NT path=%s callback=%q nt=%q",
			req.URL.Path, req.Header.Get("CALLBACK"), req.Header.Get("NT"))
		return
	}

	sid := "uuid:" + newEventSID()
	sub := &subscription{
		sid:         sid,
		service:     service,
		callbacks:   callbacks,
		seq:         1, // SEQ 0 is reserved for the mandatory initial event.
		expires:     expires,
		initialDone: make(chan struct{}),
	}
	r.mu.Lock()
	r.initSubsLocked()
	r.subs[sid] = sub
	r.mu.Unlock()

	w.Header().Set("SID", sid)
	w.Header().Set("TIMEOUT", formatSubscribeTimeout(timeout))
	w.WriteHeader(http.StatusOK)
	// Push the SID to the wire before the first NOTIFY leaves, so a control
	// point cannot be handed an event for a subscription it has not been told
	// about yet. Without the flush the response body is still buffered while
	// the goroutine below is already dialling back.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	log.Printf("DLNA SUBSCRIBE ok path=%s sid=%s timeout=%s callbacks=%v",
		req.URL.Path, sid, formatSubscribeTimeout(timeout), callbacks)

	// Initial event is mandatory; deliver off the request path.
	go r.deliverInitialEvent(sub, r.eventPropertySet(service))
}

func (r *Receiver) handleUnsubscribe(w http.ResponseWriter, req *http.Request) {
	sid := strings.TrimSpace(req.Header.Get("SID"))
	if sid == "" {
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	r.mu.Lock()
	if r.subs != nil {
		delete(r.subs, sid)
	}
	r.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	log.Printf("DLNA UNSUBSCRIBE ok sid=%s", sid)
}

// publishServiceEvent pushes a state change to every live subscriber of service.
func (r *Receiver) publishServiceEvent(service string) {
	body := r.eventPropertySet(service)
	r.mu.Lock()
	r.initSubsLocked()
	now := time.Now()
	var targets []*subscription
	for sid, sub := range r.subs {
		if sub.service != service {
			continue
		}
		if now.After(sub.expires) {
			delete(r.subs, sid)
			continue
		}
		targets = append(targets, sub)
	}
	r.mu.Unlock()

	for _, sub := range targets {
		go r.deliverEvent(sub, body)
	}
}

func (r *Receiver) deliverEvent(sub *subscription, propertySet string) {
	if sub == nil || propertySet == "" {
		return
	}
	if sub.initialDone != nil {
		<-sub.initialDone
	}
	sub.notifyMu.Lock()
	defer sub.notifyMu.Unlock()

	r.mu.Lock()
	// Always resolve through the live map so concurrent publishes share one SEQ
	// counter and a deleted SID is skipped.
	live := (*subscription)(nil)
	if r.subs != nil {
		live = r.subs[sub.sid]
	}
	if live == nil {
		r.mu.Unlock()
		return
	}
	seq := live.seq
	callbacks := append([]string(nil), live.callbacks...)
	sid := live.sid
	service := live.service
	// Claim this sequence number before unlocking so two NOTIFYs never share it.
	live.seq = seq + 1
	r.mu.Unlock()
	r.deliverClaimedEvent(callbacks, sid, service, seq, propertySet)
}

func (r *Receiver) deliverInitialEvent(sub *subscription, propertySet string) {
	if sub == nil {
		return
	}
	if sub.initialDone != nil {
		defer close(sub.initialDone)
	}
	r.mu.Lock()
	live := r.subs[sub.sid]
	if live == nil {
		r.mu.Unlock()
		return
	}
	callbacks := append([]string(nil), live.callbacks...)
	sid, service := live.sid, live.service
	r.mu.Unlock()
	r.deliverClaimedEvent(callbacks, sid, service, 0, propertySet)
}

func (r *Receiver) deliverClaimedEvent(callbacks []string, sid, service string, seq uint32, propertySet string) {

	var lastErr error
	delivered := false
	for _, cb := range callbacks {
		if err := postGENANotify(cb, sid, seq, propertySet); err != nil {
			lastErr = err
			log.Printf("DLNA GENA NOTIFY failed sid=%s service=%s seq=%d cb=%s err=%v", sid, service, seq, cb, err)
			continue
		}
		delivered = true
		// Reset error counter on success.
		r.mu.Lock()
		if live := r.subs[sid]; live != nil {
			live.errors = 0
		}
		r.mu.Unlock()
		break
	}
	if !delivered {
		r.mu.Lock()
		if live := r.subs[sid]; live != nil {
			live.errors++
			if live.errors > 10 {
				delete(r.subs, sid)
				log.Printf("DLNA GENA subscription dropped sid=%s service=%s after errors: %v", sid, service, lastErr)
			}
		}
		r.mu.Unlock()
	}
}

func postGENANotify(callbackURL, sid string, seq uint32, propertySet string) error {
	u, err := url.Parse(callbackURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("bad callback %q", callbackURL)
	}
	req, err := http.NewRequest("NOTIFY", callbackURL, bytes.NewBufferString(propertySet))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("NTS", "upnp:propchange")
	req.Header.Set("SID", sid)
	req.Header.Set("SEQ", strconv.FormatUint(uint64(seq), 10))
	req.Header.Set("SERVER", "TinyPlay/1.0 UPnP/1.1")
	// HOST is set by the transport from the URL.

	resp, err := genaHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	// 2xx is success; some stacks return 200 only.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("callback HTTP %d", resp.StatusCode)
	}
	return nil
}

var genaHTTPClient = &http.Client{Timeout: 3 * time.Second}

func serviceFromEventPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/dlna/AVTransport/"):
		return serviceAVT
	case strings.HasPrefix(path, "/dlna/RenderingControl/"):
		return serviceRCS
	case strings.HasPrefix(path, "/dlna/ConnectionManager/"):
		return serviceCM
	default:
		return ""
	}
}

// parseCallbackURLs extracts the bracketed URL list from a CALLBACK header.
// Example: `<http://192.168.1.2:1234/path><http://fallback/…>`
func parseCallbackURLs(raw string) []string {
	var out []string
	for {
		start := strings.IndexByte(raw, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(raw[start:], '>')
		if end < 0 {
			break
		}
		end += start
		u := strings.TrimSpace(raw[start+1 : end])
		if u != "" {
			out = append(out, u)
		}
		raw = raw[end+1:]
	}
	return out
}

func parseSubscribeTimeout(raw string) time.Duration {
	// Default 5 minutes — long enough for a cast session setup, short enough
	// that abandoned phone sessions do not pile up forever.
	const defaultTimeout = 300 * time.Second
	const maxTimeout = 1800 * time.Second
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "Second-infinite") {
		return defaultTimeout
	}
	// TIMEOUT: Second-###
	if i := strings.IndexByte(raw, '-'); i >= 0 {
		raw = raw[i+1:]
	}
	secs, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || secs <= 0 {
		return defaultTimeout
	}
	d := time.Duration(secs) * time.Second
	if d > maxTimeout {
		return maxTimeout
	}
	return d
}

func formatSubscribeTimeout(d time.Duration) string {
	secs := int(d / time.Second)
	if secs <= 0 {
		secs = 300
	}
	return fmt.Sprintf("Second-%d", secs)
}

func newEventSID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b)
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// eventPropertySet builds the GENA propertyset body for a service.
// AVTransport and RenderingControl use the UPnP AV LastChange moderated
// variable; ConnectionManager events its three protocol-info variables.
func (r *Receiver) eventPropertySet(service string) string {
	switch service {
	case serviceAVT:
		return propertySetXML(map[string]string{"LastChange": r.avtLastChange()})
	case serviceRCS:
		return propertySetXML(map[string]string{"LastChange": r.rcsLastChange()})
	case serviceCM:
		return propertySetXML(map[string]string{
			"SourceProtocolInfo":   "",
			"SinkProtocolInfo":     sinkProtocolInfo,
			"CurrentConnectionIDs": "0",
		})
	default:
		return ""
	}
}

func propertySetXML(props map[string]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">`)
	// Stable order for tests: sort keys via fixed preferred sequence when known.
	order := []string{"LastChange", "SourceProtocolInfo", "SinkProtocolInfo", "CurrentConnectionIDs"}
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := props[k]; ok {
			fmt.Fprintf(&b, `<e:property><%s>%s</%s></e:property>`, k, xmlEscape(v), k)
			seen[k] = true
		}
	}
	for k, v := range props {
		if seen[k] {
			continue
		}
		fmt.Fprintf(&b, `<e:property><%s>%s</%s></e:property>`, k, xmlEscape(v), k)
	}
	b.WriteString(`</e:propertyset>`)
	return b.String()
}

// maxEventMetadataBytes bounds the DIDL that rides inside a LastChange value.
//
// Metadata in an event is all-or-nothing on purpose. Cutting it to a fixed
// length is the one thing a renderer must never do: the fragment stops being
// parseable DIDL, and a cut landing inside a multi-byte character (a Chinese
// title is three bytes per rune) leaves the *whole* NOTIFY body invalid UTF-8.
// The control point then cannot decode any AVTransport event — so it never
// learns the transport reached PLAYING, reports the cast as failed and greys
// out its transport controls, while the video is already on screen. Nothing in
// the sender's logs points at metadata; the symptom is a renderer that plays
// and appears dead.
const maxEventMetadataBytes = 8192

// eventMetadata returns DIDL that is safe to carry inside a LastChange value:
// sanitized, and either whole or replaced by a title-only document. The full
// original is still available to the control point through GetPositionInfo /
// GetMediaInfo, which are not size-bounded the same way.
func eventMetadata(meta, title string) string {
	meta = sanitizeXMLText(strings.TrimSpace(meta))
	if len(meta) <= maxEventMetadataBytes {
		return meta
	}
	if title == "" {
		return ""
	}
	return `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">` +
		`<item id="0" parentID="-1" restricted="1"><dc:title>` + xmlEscape(title) +
		`</dc:title><upnp:class>object.item.videoItem</upnp:class></item></DIDL-Lite>`
}

func (r *Receiver) avtLastChange() string {
	snap := rendererState{TransportState: "NO_MEDIA_PRESENT", TransportStatus: "OK", Duration: "00:00:00", RelTime: "00:00:00", PlayMode: "NORMAL", Speed: "1"}
	if r != nil && r.state != nil {
		snap = r.state.snapshot()
	}
	uri := snap.URI
	track := trackCount(uri)
	meta := eventMetadata(snap.URIMeta, snap.Title)
	inner := fmt.Sprintf(
		`<Event xmlns="urn:schemas-upnp-org:metadata-1-0/AVT/">`+
			`<InstanceID val="0">`+
			`<TransportState val="%s"/>`+
			`<TransportStatus val="%s"/>`+
			`<NumberOfTracks val="%s"/>`+
			`<CurrentTrack val="%s"/>`+
			`<CurrentTrackDuration val="%s"/>`+
			`<CurrentMediaDuration val="%s"/>`+
			`<AVTransportURI val="%s"/>`+
			`<AVTransportURIMetaData val="%s"/>`+
			`<CurrentTrackURI val="%s"/>`+
			`<CurrentTrackMetaData val="%s"/>`+
			`<PlaybackStorageMedium val="NETWORK"/>`+
			`<PossiblePlaybackStorageMedia val="NONE,NETWORK"/>`+
			`<CurrentPlayMode val="%s"/>`+
			`<TransportPlaySpeed val="%s"/>`+
			`<CurrentTransportActions val="%s"/>`+
			`</InstanceID></Event>`,
		xmlAttrEscape(snap.TransportState),
		xmlAttrEscape(snap.TransportStatus),
		track, track,
		xmlAttrEscape(snap.Duration),
		xmlAttrEscape(snap.Duration),
		xmlAttrEscape(uri),
		xmlAttrEscape(meta),
		xmlAttrEscape(uri),
		xmlAttrEscape(meta),
		xmlAttrEscape(snap.PlayMode),
		xmlAttrEscape(snap.Speed),
		xmlAttrEscape(transportActions(snap.TransportState)),
	)
	return inner
}

func (r *Receiver) rcsLastChange() string {
	snap := rendererState{Volume: 100}
	if r != nil && r.state != nil {
		snap = r.state.snapshot()
	}
	mute := "0"
	if snap.Mute {
		mute = "1"
	}
	return fmt.Sprintf(
		`<Event xmlns="urn:schemas-upnp-org:metadata-1-0/RCS/">`+
			`<InstanceID val="0">`+
			`<Mute channel="Master" val="%s"/>`+
			`<Volume channel="Master" val="%d"/>`+
			`</InstanceID></Event>`,
		mute, snap.Volume,
	)
}

func trackCount(uri string) string {
	if uri == "" {
		return "0"
	}
	return "1"
}

func transportActions(state string) string {
	switch state {
	case "PLAYING":
		return "Pause,Stop,Seek"
	case "PAUSED_PLAYBACK":
		return "Play,Stop,Seek"
	case "TRANSITIONING":
		return "Stop"
	default: // STOPPED / NO_MEDIA_PRESENT
		return "Play"
	}
}

func xmlAttrEscape(s string) string {
	return strings.NewReplacer(
		`&`, `&amp;`,
		`<`, `&lt;`,
		`>`, `&gt;`,
		`"`, `&quot;`,
		`'`, `&apos;`,
	).Replace(sanitizeXMLText(s))
}
