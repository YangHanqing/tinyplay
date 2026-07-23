package website

import (
	"context"
	"sync"
	"time"
)

// Command is a single shell-consumed instruction. Phone clients never invent
// free-form script; only these typed actions are queued.
type Command struct {
	ID     uint64 `json:"id"`
	Action string `json:"action"`
	SiteID string `json:"site_id,omitempty"`
	URL    string `json:"url,omitempty"`
	Text   string `json:"text,omitempty"`
	Label  string `json:"label,omitempty"`
}

// Snapshot is the process-global website state returned to phone clients.
// Workspace choice (Media/Website) is a phone-local UI preference and is never
// part of this snapshot. Full current URLs are never exposed here.
type Snapshot struct {
	CurrentSiteID string       `json:"current_site_id"`
	DesiredOpen   bool         `json:"desired_open"`
	ReportedOpen  bool         `json:"reported_open"`
	HintActive    bool         `json:"hint_active"`
	HintLabels    []string     `json:"hint_labels"`
	MoreActions   []MoreAction `json:"more_actions"`
	LastStatus    string       `json:"last_status"`
	LastError     string       `json:"last_error,omitempty"`
	LastAction    string       `json:"last_action,omitempty"`
	Catalog       []Site       `json:"catalog"`
}

// Report is what a native shell posts after applying a command, navigating, or
// when the window lifecycle changes. CurrentURL is shell-private transport used
// only to derive CurrentSiteID; it is never returned on the phone snapshot.
type Report struct {
	Open        *bool    `json:"open,omitempty"`
	HintActive  *bool    `json:"hint_active,omitempty"`
	HintLabels  []string `json:"hint_labels,omitempty"`
	MoreActions []string `json:"more_actions,omitempty"`
	Status      string   `json:"status,omitempty"`
	Error       string   `json:"error,omitempty"`
	Action      string   `json:"action,omitempty"`
	CommandID   uint64   `json:"command_id,omitempty"`
	CurrentURL  string   `json:"current_url,omitempty"`
}

// Broker is the process-global website control plane.
type Broker struct {
	mu             sync.Mutex
	currentSite    string
	desiredOpen    bool
	reportedOpen   bool
	hintActive     bool
	hintLabels     []string
	moreActions    []MoreAction
	moreProbeCmdID uint64
	moreProbeSite  string
	lastStatus     string
	lastError      string
	lastAction     string
	nextCmdID      uint64
	lastReportID   uint64
	// latestLifecycleCmdID is the newest open/close command requested by the
	// phone. A response from an older lifecycle command must not undo it (for
	// example Close #2 arriving after Open #3 was already requested).
	latestLifecycleCmdID uint64
	// closeGen increments on every phone/reset close request so a late shell
	// navigation/open report cannot resurrect the window state.
	closeGen uint64
	// openGen is the closeGen value that an accepted open is allowed against.
	// Reports with open=true are ignored when closeGen has advanced past the
	// generation captured at the matching open (or when desiredOpen is false).
	openGen uint64
	pending []Command
	waiters []chan struct{}
	stopMPV func()
}

// Default is the process-wide broker used by the HTTP server and shells.
var Default = NewBroker(nil)

// NewBroker constructs a broker. stopMPV may be nil (tests).
func NewBroker(stopMPV func()) *Broker {
	return &Broker{
		lastStatus: "idle",
		stopMPV:    stopMPV,
	}
}

// Configure wires runtime hooks after construction (used by the server on start).
func (b *Broker) Configure(stopMPV func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if stopMPV != nil {
		b.stopMPV = stopMPV
	}
}

// Reset returns website state to a fresh install and closes any active native
// window. There is no persisted selected-site preference to clear.
func (b *Broker) Reset() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentSite = ""
	b.hintActive = false
	b.hintLabels = nil
	b.moreActions = nil
	b.moreProbeCmdID = 0
	b.moreProbeSite = ""
	b.lastError = ""
	if b.desiredOpen || b.reportedOpen {
		b.closeGen++
		b.desiredOpen = false
		b.reportedOpen = false
		b.lastAction = ActionClose
		b.lastStatus = "closing"
		cmd := b.enqueueLocked(Command{Action: ActionClose})
		b.latestLifecycleCmdID = cmd.ID
	} else {
		b.lastAction = ""
		b.lastStatus = "idle"
	}
	return b.snapshotLocked()
}

// Snapshot returns a copy of the public state.
func (b *Broker) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshotLocked()
}

func (b *Broker) snapshotLocked() Snapshot {
	cat := make([]Site, len(Catalog))
	copy(cat, Catalog)
	return Snapshot{
		CurrentSiteID: b.currentSite,
		DesiredOpen:   b.desiredOpen,
		ReportedOpen:  b.reportedOpen,
		HintActive:    b.hintActive,
		HintLabels:    append([]string(nil), b.hintLabels...),
		MoreActions:   append([]MoreAction(nil), b.moreActions...),
		LastStatus:    b.lastStatus,
		LastError:     b.lastError,
		LastAction:    b.lastAction,
		Catalog:       cat,
	}
}

// RequestOpen opens (or navigates) the singleton native WebView to an
// allowlisted site URL after stopping mpv. Current site is NOT set from the
// request — only from native navigation reports of the real document URL.
func (b *Broker) RequestOpen(siteID string) (Snapshot, error) {
	site, ok := SiteByID(siteID)
	if !ok {
		return Snapshot{}, errInvalid("unknown_site")
	}
	b.mu.Lock()
	stop := b.stopMPV
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.desiredOpen = true
	b.openGen = b.closeGen
	// Clearing current site until a real navigation report arrives keeps the
	// phone UI honest during the brief opening window.
	b.currentSite = ""
	b.hintActive = false
	b.hintLabels = nil
	b.moreActions = nil
	b.moreProbeCmdID = 0
	b.moreProbeSite = ""
	b.lastAction = ActionOpen
	b.lastStatus = "opening"
	b.lastError = ""
	cmd := b.enqueueLocked(Command{Action: ActionOpen, SiteID: site.ID, URL: site.URL})
	b.latestLifecycleCmdID = cmd.ID
	return b.snapshotLocked(), nil
}

// RequestClose tears down the website window. Cookies/session persist via the
// platform WebView profile.
func (b *Broker) RequestClose() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeGen++
	b.desiredOpen = false
	b.reportedOpen = false
	b.currentSite = ""
	b.hintActive = false
	b.hintLabels = nil
	b.moreActions = nil
	b.moreProbeCmdID = 0
	b.moreProbeSite = ""
	b.lastAction = ActionClose
	b.lastStatus = "closing"
	b.lastError = ""
	cmd := b.enqueueLocked(Command{Action: ActionClose})
	b.latestLifecycleCmdID = cmd.ID
	return b.snapshotLocked()
}

// EnqueueAction validates and queues a phone-originated action.
// ActionOpen is not accepted here — open requires an explicit site_id via
// RequestOpen / POST /api/website/open.
func (b *Broker) EnqueueAction(action, text, label string) (Snapshot, error) {
	if !IsKnownAction(action) {
		return Snapshot{}, errInvalid("unknown_action")
	}
	switch action {
	case ActionOpen:
		return Snapshot{}, errInvalid("site_required")
	case ActionClose:
		return b.RequestClose(), nil
	}

	cmd := Command{Action: action}
	switch action {
	case ActionSearch, ActionType:
		max := MaxSearchText
		if action == ActionType {
			max = MaxTypeText
		}
		clean, ok := ValidateText(text, max)
		if !ok {
			return Snapshot{}, errInvalid("text_too_long")
		}
		if action == ActionSearch && clean == "" {
			return Snapshot{}, errInvalid("text_required")
		}
		cmd.Text = clean
	case ActionSeek:
		clean, ok := ValidateNumber(text, MinSeekSeconds, MaxSeekSeconds)
		if !ok {
			return Snapshot{}, errInvalid("invalid_number")
		}
		cmd.Text = clean
	case ActionSpeed:
		clean, ok := ValidateNumber(text, MinPlaybackRate, MaxPlaybackRate)
		if !ok {
			return Snapshot{}, errInvalid("invalid_number")
		}
		cmd.Text = clean
	case ActionVolume:
		clean, ok := ValidateNumber(text, MinWebsiteVolumeDelta, MaxWebsiteVolumeDelta)
		if !ok || clean == "0" {
			return Snapshot{}, errInvalid("invalid_number")
		}
		cmd.Text = clean
	case ActionHintLabel:
		clean, ok := ValidateHintLabel(label)
		if !ok {
			return Snapshot{}, errInvalid("invalid_label")
		}
		cmd.Label = clean
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.desiredOpen {
		return b.snapshotLocked(), errInvalid("window_not_open")
	}
	if isSiteMoreAction(action) && !hasMoreAction(b.moreActions, action) {
		return b.snapshotLocked(), errInvalid("action_unavailable")
	}
	if action == ActionHome {
		// Root URLs come only from the fixed catalog, never from a phone request.
		site, ok := SiteByID(b.currentSite)
		if !ok {
			return b.snapshotLocked(), errInvalid("home_unavailable")
		}
		cmd.SiteID = site.ID
		cmd.URL = site.URL
	}
	if action == ActionLogin {
		// Prefer a fixed route where we have verified one. A catalog site with
		// no fixed route (currently Douyin) is still allowed to invoke the
		// controller's generic visible-login control; neither path accepts a
		// phone-provided URL.
		if _, ok := SiteByID(b.currentSite); !ok {
			return b.snapshotLocked(), errInvalid("login_unavailable")
		}
		cmd.SiteID = b.currentSite
		if loginURL, ok := LoginURL(b.currentSite); ok {
			cmd.URL = loginURL
		}
	}
	if action == ActionHintEnter {
		b.hintActive = true
		// A fresh overlay can contain a different set of targets. Keep the
		// phone keypad inert until the shell returns its new label list.
		b.hintLabels = nil
	}
	if action == ActionHintExit {
		b.hintActive = false
		b.hintLabels = nil
	}
	if action == ActionCapabilities {
		// An old page capability list must not remain tappable while the new probe
		// is in flight.
		b.moreActions = nil
	}
	b.lastAction = action
	b.lastStatus = "pending"
	b.lastError = ""
	queued := b.enqueueLocked(cmd)
	if action == ActionCapabilities {
		b.moreProbeCmdID = queued.ID
		b.moreProbeSite = b.currentSite
	}
	return b.snapshotLocked(), nil
}

// ApplyReport updates reported open/hint/status/current-site from the shell.
func (b *Broker) ApplyReport(r Report) Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.CommandID > 0 {
		if r.CommandID < b.latestLifecycleCmdID {
			return b.snapshotLocked()
		}
		if r.CommandID < b.lastReportID {
			return b.snapshotLocked()
		}
		b.lastReportID = r.CommandID
	}

	// Stale open/navigation after a close must not resurrect window state.
	// Native close and phone close both clear desiredOpen; only a newer open
	// (openGen == closeGen) may accept open=true / current_url updates.
	acceptOpenState := b.desiredOpen && b.openGen == b.closeGen

	if r.Open != nil {
		if *r.Open {
			if !acceptOpenState {
				// Ignore resurrecting open reports (including delayed navigations).
				// Still fall through for non-state fields only when this was not
				// an open resurrection attempt with URL — drop the whole report
				// so status cannot imply the window is live.
				return b.snapshotLocked()
			}
			b.reportedOpen = true
		} else {
			b.reportedOpen = false
			b.currentSite = ""
			b.hintActive = false
			b.hintLabels = nil
			b.moreActions = nil
			b.moreProbeCmdID = 0
			b.moreProbeSite = ""
			// User closed the window natively — align desired so UI stays honest.
			if r.Action == "window_closed" || r.Action == ActionClose {
				if b.desiredOpen {
					b.closeGen++
					b.desiredOpen = false
				}
			}
			acceptOpenState = false
		}
	}

	if r.CurrentURL != "" {
		if acceptOpenState {
			b.currentSite = SiteIDFromURL(r.CurrentURL)
			// Every main-document navigation invalidates page-level capabilities.
			b.moreActions = nil
			b.moreProbeCmdID = 0
			b.moreProbeSite = ""
			// A navigation report implies the singleton window is still up.
			if r.Open == nil {
				b.reportedOpen = true
			}
		}
		// Stale navigation after close: ignore URL only; close reports above
		// already cleared site/open state.
	}

	if r.HintActive != nil && acceptOpenState {
		b.hintActive = *r.HintActive
		if !b.hintActive {
			b.hintLabels = nil
		}
	}
	if r.HintLabels != nil && acceptOpenState && b.hintActive {
		b.hintLabels = validHintLabels(r.HintLabels)
	}
	if r.MoreActions != nil && acceptOpenState && r.Action == ActionCapabilities &&
		r.CommandID != 0 && r.CommandID == b.moreProbeCmdID && b.currentSite == b.moreProbeSite {
		b.moreActions = FilterMoreActions(b.currentSite, r.MoreActions)
		b.moreProbeCmdID = 0
		b.moreProbeSite = ""
	}
	if r.Status != "" {
		b.lastStatus = r.Status
	}
	if r.Error != "" {
		b.lastError = r.Error
		if b.lastStatus == "" || b.lastStatus == "pending" || b.lastStatus == "opening" {
			b.lastStatus = "error"
		}
	} else if r.Status != "" && r.Status != "error" {
		b.lastError = ""
	}
	if r.Action != "" {
		b.lastAction = r.Action
	}
	return b.snapshotLocked()
}

func hasMoreAction(actions []MoreAction, id string) bool {
	for _, action := range actions {
		if action.ID == id {
			return true
		}
	}
	return false
}

func isSiteMoreAction(id string) bool {
	for _, actions := range siteMoreActions {
		for _, action := range actions {
			if action.ID == id {
				return true
			}
		}
	}
	return false
}

// WaitCommand long-polls for the next command with id > afterID.
func (b *Broker) WaitCommand(ctx context.Context, afterID uint64) (Command, bool) {
	for {
		b.mu.Lock()
		if cmd, ok := b.nextPendingLocked(afterID); ok {
			b.mu.Unlock()
			return cmd, true
		}
		ch := make(chan struct{}, 1)
		b.waiters = append(b.waiters, ch)
		b.mu.Unlock()

		timer := time.NewTimer(25 * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			b.mu.Lock()
			b.removeWaiterLocked(ch)
			b.mu.Unlock()
			return Command{}, false
		case <-ch:
			if !timer.Stop() {
				<-timer.C
			}
			// re-check
		case <-timer.C:
			b.mu.Lock()
			b.removeWaiterLocked(ch)
			// Return empty on timeout so the shell can re-poll with same afterID.
			b.mu.Unlock()
			return Command{}, false
		}
	}
}

// PendingAfter is a non-blocking peek used by tests.
func (b *Broker) PendingAfter(afterID uint64) (Command, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextPendingLocked(afterID)
}

func (b *Broker) nextPendingLocked(afterID uint64) (Command, bool) {
	for _, cmd := range b.pending {
		if cmd.ID > afterID {
			return cmd, true
		}
	}
	return Command{}, false
}

func (b *Broker) enqueueLocked(cmd Command) Command {
	b.nextCmdID++
	cmd.ID = b.nextCmdID
	// Cap queue so a dead shell cannot grow memory forever.
	const maxPending = 64
	if len(b.pending) >= maxPending {
		b.pending = b.pending[len(b.pending)-maxPending/2:]
	}
	b.pending = append(b.pending, cmd)
	for _, ch := range b.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	b.waiters = b.waiters[:0]
	return cmd
}

func (b *Broker) removeWaiterLocked(target chan struct{}) {
	out := b.waiters[:0]
	for _, ch := range b.waiters {
		if ch != target {
			out = append(out, ch)
		}
	}
	b.waiters = out
}

// public validation errors
type apiError struct {
	code string
}

func (e apiError) Error() string { return e.code }

func errInvalid(code string) error { return apiError{code: code} }

// IsInvalid reports a validation/API client error.
func IsInvalid(err error) bool {
	_, ok := err.(apiError)
	return ok
}

// ErrorCode returns the stable error token for HTTP detail.
func ErrorCode(err error) string {
	if e, ok := err.(apiError); ok {
		return e.code
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
