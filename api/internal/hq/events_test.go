package hq

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event")
		return Event{}
	}
}

func TestEventBus_PublishReachesTenantSubscribersOnly(t *testing.T) {
	bus := NewEventBus()
	chA, cancelA := bus.Subscribe("tnt_a")
	defer cancelA()
	chB, cancelB := bus.Subscribe("tnt_b")
	defer cancelB()

	bus.Publish("tnt_a", Event{Type: "branch-synced", BranchID: "b1"})

	if e := recv(t, chA); e.BranchID != "b1" {
		t.Fatalf("subscriber A got %+v", e)
	}
	select {
	case e := <-chB:
		t.Fatalf("subscriber B leaked event %+v", e)
	default:
	}
}

func TestEventBus_CancelClosesChannel(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.Subscribe("tnt_a")
	cancel()
	if _, open := <-ch; open {
		t.Fatalf("channel still open after cancel")
	}
	// Publishing after cancel must not panic or block.
	bus.Publish("tnt_a", Event{Type: "branch-synced"})
}

func TestEventBus_NeverBlocksPublisher(t *testing.T) {
	bus := NewEventBus()
	// No subscribers at all.
	bus.Publish("tnt_ghost", Event{Type: "branch-synced"})

	// A subscriber that never reads: publisher must not block past the buffer.
	_, cancel := bus.Subscribe("tnt_a")
	defer cancel()
	done := make(chan struct{})
	go func() {
		for range 100 {
			bus.Publish("tnt_a", Event{Type: "branch-synced"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("publisher blocked on a slow subscriber")
	}
}
