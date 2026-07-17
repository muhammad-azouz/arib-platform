// Package billing derives a tenant's sync-subscription state from its paid
// bills. State is never stored — it is recomputed from Bill documents on
// every read, so there is no background job and nothing to drift.
package billing

import (
	"time"

	"github.com/aribpos/license-api/internal/model"
)

// State is where a tenant sits relative to its paid coverage.
type State string

const (
	StateNone     State = "none"     // no paid bill has ever covered this tenant
	StateActive   State = "active"   // covered, more than warnBefore from expiry
	StateExpiring State = "expiring" // covered, within warnBefore of expiry — warn
	StateGrace    State = "grace"    // coverage lapsed, within graceAfter — sync still allowed
	StateExpired  State = "expired"  // past grace — sync tokens refused
)

const (
	// warnBefore is how long before coverage ends the tenant starts seeing
	// renewal warnings.
	warnBefore = 30 * 24 * time.Hour
	// graceAfter is how long sync keeps working past coverage end before
	// IssueSyncToken starts refusing.
	graceAfter = 7 * 24 * time.Hour
)

// Summary is the derived subscription state as of a point in time.
type Summary struct {
	State      State     `json:"state"`
	EndsAt     time.Time `json:"ends_at"`     // coverage end (max EndsAt over paid bills); zero if State == StateNone
	GraceUntil time.Time `json:"grace_until"` // EndsAt + graceAfter; zero if State == StateNone
	DaysLeft   int       `json:"days_left"`   // days until EndsAt, floor; negative once past EndsAt
}

// Derive computes the subscription Summary for a tenant from its bills as of
// now. Only BillPaid bills count toward coverage — void bills are ignored.
// Coverage end is the max EndsAt across paid bills, so an early-renewal bill
// (starting before the current coverage ends) simply extends it.
func Derive(bills []model.Bill, now time.Time) Summary {
	var end time.Time
	for _, b := range bills {
		if b.Status != model.BillPaid {
			continue
		}
		if b.EndsAt.After(end) {
			end = b.EndsAt
		}
	}

	if end.IsZero() {
		return Summary{State: StateNone}
	}

	graceUntil := end.Add(graceAfter)
	daysLeft := int(end.Sub(now).Hours() / 24)

	var state State
	switch {
	case !now.After(end.Add(-warnBefore)):
		state = StateActive
	case !now.After(end):
		state = StateExpiring
	case !now.After(graceUntil):
		state = StateGrace
	default:
		state = StateExpired
	}

	return Summary{State: state, EndsAt: end, GraceUntil: graceUntil, DaysLeft: daysLeft}
}

// SyncAllowed reports whether the tenant's state permits issuing sync tokens.
// Every state short of StateExpired keeps sync working, including the grace
// week — enforcement only bites once grace has fully elapsed.
func SyncAllowed(s Summary) bool {
	return s.State != StateNone && s.State != StateExpired
}
