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
	chA, cancelA := bus.Subscribe("tnt_a", nil)
	defer cancelA()
	chB, cancelB := bus.Subscribe("tnt_b", nil)
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
	ch, cancel := bus.Subscribe("tnt_a", nil)
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
	_, cancel := bus.Subscribe("tnt_a", nil)
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

// TestEventBus_ScopedSubscriberSeesOnlyAllowlistedBranches covers T122's
// first acceptance bullet: a subscriber with a branch allowlist receives
// events for branches on it and never receives events for branches off it,
// while an unscoped subscriber (nil allowlist) on the same tenant is
// unaffected and sees both.
func TestEventBus_ScopedSubscriberSeesOnlyAllowlistedBranches(t *testing.T) {
	bus := NewEventBus()
	scoped, cancelScoped := bus.Subscribe("tnt_a", []string{"b1"})
	defer cancelScoped()
	unscoped, cancelUnscoped := bus.Subscribe("tnt_a", nil)
	defer cancelUnscoped()

	bus.Publish("tnt_a", Event{Type: "branch-synced", BranchID: "b1"})
	bus.Publish("tnt_a", Event{Type: "branch-synced", BranchID: "b2"})

	if e := recv(t, scoped); e.BranchID != "b1" {
		t.Fatalf("scoped subscriber got %+v, want only b1", e)
	}
	select {
	case e := <-scoped:
		t.Fatalf("scoped subscriber leaked out-of-allowlist event %+v", e)
	default:
	}

	if e := recv(t, unscoped); e.BranchID != "b1" {
		t.Fatalf("unscoped subscriber's first event: got %+v", e)
	}
	if e := recv(t, unscoped); e.BranchID != "b2" {
		t.Fatalf("unscoped subscriber's second event: got %+v", e)
	}
}

// TestEventBus_ScopedSubscriberDroppingEveryEventStaysOpen covers T122's
// third acceptance bullet: a subscriber whose allowlist matches nothing
// published still has a live, open channel — Publish filtering them out of
// every event must never close or otherwise stall their stream.
func TestEventBus_ScopedSubscriberDroppingEveryEventStaysOpen(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.Subscribe("tnt_a", []string{"b1"})
	defer cancel()

	for range 10 {
		bus.Publish("tnt_a", Event{Type: "branch-synced", BranchID: "b2"})
	}

	select {
	case e, open := <-ch:
		t.Fatalf("expected no events and an open channel, got %+v (open=%v)", e, open)
	default:
	}

	// The channel is still live: an allowlisted event still arrives.
	bus.Publish("tnt_a", Event{Type: "branch-synced", BranchID: "b1"})
	if e := recv(t, ch); e.BranchID != "b1" {
		t.Fatalf("subscriber got %+v after filtered events, want b1", e)
	}
}
