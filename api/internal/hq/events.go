package hq

import (
	"sync"
	"time"
)

// Event is one tenant-scoped console notification. Today only the sync
// cadence emits them (branch-synced, published by the gateway's
// sync-completed callback); the console treats any event as "refetch what
// you're showing", so losing one under pressure is harmless.
type Event struct {
	Type     string    `json:"type"`
	BranchID string    `json:"branch_id,omitempty"`
	At       time.Time `json:"at"`
}

// EventBus is the in-memory per-tenant pub/sub feeding the console's SSE
// streams. In-memory means single API instance — if the API ever runs more
// than one replica this must move to a shared broker (callbacks would land on
// one instance while SSE connections live on another).
type EventBus struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewEventBus builds an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subs: map[string]map[chan Event]struct{}{}}
}

// Subscribe registers for a tenant's events. The returned cancel func
// unsubscribes and closes the channel; it is safe to call more than once.
func (b *EventBus) Subscribe(tenantID string) (<-chan Event, func()) {
	ch := make(chan Event, 8)
	b.mu.Lock()
	set, ok := b.subs[tenantID]
	if !ok {
		set = map[chan Event]struct{}{}
		b.subs[tenantID] = set
	}
	set[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs[tenantID], ch)
			if len(b.subs[tenantID]) == 0 {
				delete(b.subs, tenantID)
			}
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish delivers to every subscriber of the tenant without ever blocking:
// a subscriber whose buffer is full simply misses the event (the next one,
// or its own periodic refetch, reconciles it).
func (b *EventBus) Publish(tenantID string, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[tenantID] {
		select {
		case ch <- e:
		default:
		}
	}
}
