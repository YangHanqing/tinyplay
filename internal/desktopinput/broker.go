// Package desktopinput provides a deliberately small, typed bridge between the
// LAN phone UI and the platform shell that owns native input injection.
package desktopinput

import (
	"context"
	"sync"
	"time"
)

const (
	ActionMove              = "move"
	ActionLeftClick         = "left_click"
	ActionRightClick        = "right_click"
	ActionScroll            = "scroll"
	ActionKey               = "key"
	ActionType              = "type"
	ActionRequestPermission = "request_permission"
)

// Command is intentionally not a script or arbitrary keycode transport.
// Keeping the vocabulary small makes the emergency desktop-control feature
// inspectable on both the Go and native-shell sides.
type Command struct {
	ID     uint64 `json:"id"`
	Action string `json:"action"`
	DX     int    `json:"dx,omitempty"`
	DY     int    `json:"dy,omitempty"`
	Text   string `json:"text,omitempty"`
}

type Snapshot struct {
	Ready              bool   `json:"ready"`
	PermissionRequired bool   `json:"permission_required"`
	PermissionGranted  bool   `json:"permission_granted"`
	LastError          string `json:"last_error,omitempty"`
}

type Broker struct {
	mu       sync.Mutex
	nextID   uint64
	pending  []Command
	waiters  []chan struct{}
	snapshot Snapshot
}

var Default = NewBroker()

func NewBroker() *Broker { return &Broker{} }

func (b *Broker) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshot
}

func (b *Broker) ReportState(state Snapshot) {
	b.mu.Lock()
	b.snapshot = state
	b.mu.Unlock()
}

func (b *Broker) Enqueue(action string, dx, dy int, text string) (Command, bool) {
	if !valid(action, dx, dy, text) {
		return Command{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	cmd := Command{ID: b.nextID, Action: action, DX: dx, DY: dy, Text: text}
	b.pending = append(b.pending, cmd)
	for _, ch := range b.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return cmd, true
}

func valid(action string, dx, dy int, text string) bool {
	switch action {
	case ActionMove:
		return dx >= -500 && dx <= 500 && dy >= -500 && dy <= 500 && (dx != 0 || dy != 0)
	case ActionScroll:
		return dy >= -120 && dy <= 120 && dy != 0
	case ActionLeftClick, ActionRightClick, ActionRequestPermission:
		return dx == 0 && dy == 0 && text == ""
	case ActionKey:
		switch text {
		case "escape", "enter", "left", "right", "up", "down":
			return true
		}
	case ActionType:
		return len([]rune(text)) > 0 && len([]rune(text)) <= 256
	}
	return false
}

// WaitCommand returns the first command newer than afterID. The long poll is
// bounded so native shells can retry after restarts without retaining requests.
func (b *Broker) WaitCommand(ctx context.Context, afterID uint64) (Command, bool) {
	for {
		b.mu.Lock()
		for _, cmd := range b.pending {
			if cmd.ID > afterID {
				b.mu.Unlock()
				return cmd, true
			}
		}
		ch := make(chan struct{}, 1)
		b.waiters = append(b.waiters, ch)
		b.mu.Unlock()

		timer := time.NewTimer(25 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			b.removeWaiter(ch)
			return Command{}, false
		case <-ch:
			timer.Stop()
			b.removeWaiter(ch)
		case <-timer.C:
			b.removeWaiter(ch)
			return Command{}, false
		}
	}
}

func (b *Broker) removeWaiter(target chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, ch := range b.waiters {
		if ch == target {
			b.waiters = append(b.waiters[:i], b.waiters[i+1:]...)
			return
		}
	}
}
