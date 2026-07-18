package player

import (
	"fmt"
	"net/url"
	"time"
)

// playbackAttempt is an in-memory, per-title diagnostic record. It contains
// only structured state and fixed English event names; request URLs, headers,
// credentials, and user-visible media titles are deliberately excluded.
type playbackAttempt struct {
	ID                string
	StartedAt         time.Time
	CapturedAt        time.Time
	SourceType        string
	MediaEndpoint     map[string]any
	TerminationReason string
	FailureStage      string
	MPVEndReason      string
	MPVEndError       string
	ProcessExit       string
	Events            []map[string]any
}

const diagnosticEventLimit = 100

func (p *Player) beginDiagnostic(mediaURL string, opt PlayOptions) {
	p.finalizeDiagnostic("replaced_by_new_item", "user_action")
	p.diagMu.Lock()
	p.currentDiagnostic = &playbackAttempt{
		ID:            randHex(),
		StartedAt:     time.Now().UTC(),
		SourceType:    opt.SourceType,
		MediaEndpoint: diagnosticEndpoint(mediaURL),
	}
	p.appendDiagnosticEventLocked("play_requested", map[string]any{
		"source_type":    opt.SourceType,
		"resume_seconds": opt.StartSeconds,
	})
	p.diagMu.Unlock()
}

// RecordExternalFailure covers errors that happen before an mpv process or an
// IPC load request exists, such as source negotiation or file URL resolution.
func (p *Player) RecordExternalFailure(sourceType, stage string) {
	p.beginDiagnostic("", PlayOptions{SourceType: sourceType})
	p.appendDiagnosticEvent("playback_request_failed", map[string]any{"failure_stage": stage})
	p.finalizeDiagnostic("negotiation_failed", stage)
}

func diagnosticEndpoint(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return map[string]any{"parseable": false}
	}
	host := u.Hostname()
	return map[string]any{
		"scheme":      u.Scheme,
		"host":        host,
		"port":        u.Port(),
		"is_loopback": host == "127.0.0.1" || host == "::1" || host == "localhost",
	}
}

func (p *Player) appendDiagnosticEvent(event string, fields map[string]any) {
	p.diagMu.Lock()
	defer p.diagMu.Unlock()
	p.appendDiagnosticEventLocked(event, fields)
}

func (p *Player) appendDiagnosticEventLocked(event string, fields map[string]any) {
	if p.currentDiagnostic == nil {
		return
	}
	entry := map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"event": event,
	}
	for k, v := range fields {
		entry[k] = v
	}
	p.currentDiagnostic.Events = append(p.currentDiagnostic.Events, entry)
	if len(p.currentDiagnostic.Events) > diagnosticEventLimit {
		p.currentDiagnostic.Events = p.currentDiagnostic.Events[len(p.currentDiagnostic.Events)-diagnosticEventLimit:]
	}
}

// recordMPVEvent retains only the stable, non-sensitive part of mpv's JSON IPC
// events. In particular, it never copies raw event maps which may include a
// URL or a local file path.
func (p *Player) recordMPVEvent(event string, data map[string]any) {
	fields := map[string]any{}
	for _, key := range []string{"reason", "error", "playlist_entry_id"} {
		if value, ok := data[key]; ok {
			fields[key] = value
		}
	}
	p.diagMu.Lock()
	defer p.diagMu.Unlock()
	if p.currentDiagnostic == nil {
		return
	}
	if event == "end-file" {
		p.currentDiagnostic.MPVEndReason, _ = fields["reason"].(string)
		if value, ok := fields["error"]; ok {
			p.currentDiagnostic.MPVEndError = fmt.Sprint(value)
		}
	}
	p.appendDiagnosticEventLocked("mpv_"+event, fields)
}

// finalizeDiagnostic freezes the current attempt before player state is
// cleared. A later playback replaces this one; nothing is persisted to disk.
func (p *Player) finalizeDiagnostic(reason, stage string) {
	props := p.Props()
	p.diagMu.Lock()
	defer p.diagMu.Unlock()
	current := p.currentDiagnostic
	if current == nil {
		return
	}
	if reason == "replaced_by_new_item" && len(current.Events) <= 1 {
		// No previous title has actually started; avoid manufacturing a report
		// while the first request is merely being initialized.
		return
	}
	if reason == "engine_process_exit" && current.MPVEndReason == "eof" {
		reason = "completed"
		stage = ""
	}
	if reason == "engine_process_exit" && current.MPVEndReason == "error" {
		reason = "backend_error"
		stage = "playback"
	}
	current.TerminationReason = reason
	current.FailureStage = stage
	current.CapturedAt = time.Now().UTC()
	p.appendDiagnosticEventLocked("attempt_finalized", map[string]any{
		"termination_reason": reason,
		"failure_stage":      stage,
	})
	p.lastDiagnostic = p.diagnosticMapLocked(current, props, "last")
	p.currentDiagnostic = nil
}

func (p *Player) Diagnostics() (map[string]any, bool) {
	props := p.Props()
	p.diagMu.Lock()
	defer p.diagMu.Unlock()
	if p.currentDiagnostic != nil {
		return p.diagnosticMapLocked(p.currentDiagnostic, props, "current"), true
	}
	if p.lastDiagnostic == nil {
		return nil, false
	}
	return cloneDiagnosticMap(p.lastDiagnostic), true
}

func (p *Player) DiagnosticStatus() (available bool, scope string) {
	p.diagMu.Lock()
	defer p.diagMu.Unlock()
	if p.currentDiagnostic != nil {
		return true, "current"
	}
	if p.lastDiagnostic != nil {
		return true, "last"
	}
	return false, ""
}

func (p *Player) diagnosticMapLocked(attempt *playbackAttempt, props map[string]any, scope string) map[string]any {
	events := make([]map[string]any, len(attempt.Events))
	for i, event := range attempt.Events {
		events[i] = cloneDiagnosticMap(event)
	}
	outcome := "playing"
	if scope == "last" {
		switch attempt.TerminationReason {
		case "completed":
			outcome = "completed"
		case "user_stop", "replaced_by_new_item", "app_shutdown":
			outcome = "stopped"
		default:
			outcome = "failed"
		}
	}
	capturedAt := ""
	if !attempt.CapturedAt.IsZero() {
		capturedAt = attempt.CapturedAt.Format(time.RFC3339Nano)
	}
	return map[string]any{
		"report_scope": scope,
		"playback_attempt": map[string]any{
			"id":                 attempt.ID,
			"started_at":         attempt.StartedAt.Format(time.RFC3339Nano),
			"captured_at":        capturedAt,
			"outcome":            outcome,
			"termination_reason": attempt.TerminationReason,
			"failure_stage":      attempt.FailureStage,
			"source_type":        attempt.SourceType,
			"media_endpoint":     cloneDiagnosticMap(attempt.MediaEndpoint),
		},
		"engine": map[string]any{
			"kind":            "mpv",
			"end_file_reason": attempt.MPVEndReason,
			"end_file_error":  attempt.MPVEndError,
			"process_exit":    attempt.ProcessExit,
		},
		"engine_events": events,
		"playback":      cloneDiagnosticMap(props),
	}
}

func cloneDiagnosticMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
