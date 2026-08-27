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
	subs map[string]map[chan Event][]string // tenantID -> channel -> branch allowlist (nil/empty = unscoped, sees every branch)
}

// NewEventBus builds an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subs: map[string]map[chan Event][]string{}}
}

// Subscribe registers for a tenant's events, filtered by branchIDs (T122,
// spec D5d) — the same "empty means unscoped" contract perm.Scope uses. The
// allowlist is fixed for the lifetime of the subscription: it is decided
// once here from the caller's Scope at connect time, not re-evaluated per
// event or left to the client, so a scoped member's stream never carries an
// out-of-scope branch id onto the wire in the first place. The returned
// cancel func unsubscribes and closes the channel; it is safe to call more
// than once.
func (b *EventBus) Subscribe(tenantID string, branchIDs []string) (<-chan Event, func()) {
	ch := make(chan Event, 8)
	b.mu.Lock()
	set, ok := b.subs[tenantID]
	if !ok {
		set = map[chan Event][]string{}
		b.subs[tenantID] = set
	}
	set[ch] = branchIDs
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

// Publish delivers to every subscriber of the tenant whose allowlist admits
// e.BranchID (branchAllowed, shared with service.go's applyScope/T119 —
// empty means unscoped, sees everything), without ever blocking: a
// subscriber whose buffer is full simply misses the event (the next one, or
// its own periodic refetch, reconciles it), and a subscriber whose allowlist
// excludes every event it will ever see just sits on an open, idle channel —
// the heartbeat in the SSE handler is what keeps that connection alive, not
// traffic on this channel. An event with no BranchID (none exist yet, but
// the type allows it) carries no branch to filter against, so it reaches
// every subscriber regardless of allowlist.
func (b *EventBus) Publish(tenantID string, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch, allowed := range b.subs[tenantID] {
		if e.BranchID != "" && !branchAllowed(allowed, e.BranchID) {
			continue
		}
		select {
		case ch <- e:
		default:
		}
	}
}
