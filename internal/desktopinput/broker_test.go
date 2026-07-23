package desktopinput

import (
	"context"
	"testing"
	"time"
)

func TestBrokerOnlyAcceptsBoundedTypedInput(t *testing.T) {
	b := NewBroker()
	if _, ok := b.Enqueue(ActionMove, 12, -8, ""); !ok {
		t.Fatal("valid move rejected")
	}
	if _, ok := b.Enqueue(ActionKey, 0, 0, "escape"); !ok {
		t.Fatal("valid key rejected")
	}
	if _, ok := b.Enqueue(ActionType, 0, 0, "hello"); !ok {
		t.Fatal("valid text rejected")
	}
	if _, ok := b.Enqueue("javascript", 0, 0, "alert(1)"); ok {
		t.Fatal("unknown action accepted")
	}
	if _, ok := b.Enqueue(ActionMove, 501, 0, ""); ok {
		t.Fatal("unbounded move accepted")
	}
	if _, ok := b.Enqueue(ActionKey, 0, 0, "cmd-q"); ok {
		t.Fatal("arbitrary key accepted")
	}
}

func TestBrokerWaitsForNewerCommand(t *testing.T) {
	b := NewBroker()
	first, _ := b.Enqueue(ActionLeftClick, 0, 0, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { time.Sleep(10 * time.Millisecond); _, _ = b.Enqueue(ActionRightClick, 0, 0, "") }()
	cmd, ok := b.WaitCommand(ctx, first.ID)
	if !ok || cmd.Action != ActionRightClick {
		t.Fatalf("got %#v, ok=%v", cmd, ok)
	}
}
