package billing

import (
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
)

func paidBill(startsAt, endsAt time.Time) model.Bill {
	return model.Bill{StartsAt: startsAt, EndsAt: endsAt, Status: model.BillPaid}
}

func voidBill(startsAt, endsAt time.Time) model.Bill {
	return model.Bill{StartsAt: startsAt, EndsAt: endsAt, Status: model.BillVoid}
}

func TestDeriveBoundaries(t *testing.T) {
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, -1, 0)
	oneBill := []model.Bill{paidBill(start, end)}

	tests := []struct {
		name string
		now  time.Time
		want State
	}{
		{"well within coverage", end.Add(-60 * 24 * time.Hour), StateActive},
		{"exactly 30d before end — still active", end.Add(-warnBefore), StateActive},
		{"one second into the warn window — expiring", end.Add(-warnBefore + time.Second), StateExpiring},
		{"exactly at end — still expiring", end, StateExpiring},
		{"one second past end — grace", end.Add(time.Second), StateGrace},
		{"exactly at grace boundary — still grace", end.Add(graceAfter), StateGrace},
		{"one second past grace — expired", end.Add(graceAfter + time.Second), StateExpired},
		{"well past grace — expired", end.Add(60 * 24 * time.Hour), StateExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Derive(oneBill, tt.now)
			if got.State != tt.want {
				t.Fatalf("state = %s, want %s", got.State, tt.want)
			}
			if !got.EndsAt.Equal(end) {
				t.Fatalf("EndsAt = %v, want %v", got.EndsAt, end)
			}
			if !got.GraceUntil.Equal(end.Add(graceAfter)) {
				t.Fatalf("GraceUntil = %v, want %v", got.GraceUntil, end.Add(graceAfter))
			}
		})
	}
}

func TestDeriveNoBills(t *testing.T) {
	got := Derive(nil, time.Now())
	if got.State != StateNone {
		t.Fatalf("state = %s, want %s", got.State, StateNone)
	}
	if !got.EndsAt.IsZero() || !got.GraceUntil.IsZero() {
		t.Fatalf("expected zero EndsAt/GraceUntil for no bills, got %+v", got)
	}
	if SyncAllowed(got) {
		t.Fatalf("sync must not be allowed with no bills")
	}
}

func TestDeriveOnlyVoidBills(t *testing.T) {
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	bills := []model.Bill{voidBill(end.AddDate(0, -1, 0), end)}
	got := Derive(bills, end.Add(-time.Hour))
	if got.State != StateNone {
		t.Fatalf("state = %s, want %s (only-void bills must not count)", got.State, StateNone)
	}
}

func TestDeriveVoidedCoveringBillDowngrades(t *testing.T) {
	earlier := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	bills := []model.Bill{
		paidBill(earlier.AddDate(0, -1, 0), earlier),
		voidBill(earlier, later), // would have been the covering bill, but voided
	}
	now := earlier.Add(-time.Hour) // still within the first bill's coverage
	got := Derive(bills, now)
	if !got.EndsAt.Equal(earlier) {
		t.Fatalf("EndsAt = %v, want the surviving paid bill's end %v (void bill must not count)", got.EndsAt, earlier)
	}
}

func TestDeriveOverlappingBillsUseMaxEnd(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bills := []model.Bill{
		paidBill(base, base.AddDate(0, 6, 0)),
		paidBill(base.AddDate(0, 3, 0), base.AddDate(0, 9, 0)), // overlaps, ends later
	}
	got := Derive(bills, base.AddDate(0, 4, 0))
	want := base.AddDate(0, 9, 0)
	if !got.EndsAt.Equal(want) {
		t.Fatalf("EndsAt = %v, want max(EndsAt) = %v", got.EndsAt, want)
	}
	if got.State != StateActive {
		t.Fatalf("state = %s, want %s", got.State, StateActive)
	}
}

func TestDeriveEarlyRenewalExtendsCoverage(t *testing.T) {
	currentEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	renewalEnd := currentEnd.AddDate(1, 0, 0)
	bills := []model.Bill{
		paidBill(currentEnd.AddDate(0, -1, 0), currentEnd),
		// Paid well before the current period ends — coverage should extend
		// to the new bill's end regardless of when it was purchased.
		paidBill(currentEnd, renewalEnd),
	}
	now := currentEnd.Add(-15 * 24 * time.Hour) // still mid-current-period
	got := Derive(bills, now)
	if !got.EndsAt.Equal(renewalEnd) {
		t.Fatalf("EndsAt = %v, want extended coverage %v", got.EndsAt, renewalEnd)
	}
	if got.State != StateActive {
		t.Fatalf("state = %s, want %s (early renewal should read as active, not expiring)", got.State, StateActive)
	}
}

func TestSyncAllowed(t *testing.T) {
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	bills := []model.Bill{paidBill(end.AddDate(0, -1, 0), end)}

	cases := []struct {
		now  time.Time
		want bool
	}{
		{end.Add(-60 * 24 * time.Hour), true},      // active
		{end.Add(-time.Hour), true},                // expiring
		{end.Add(time.Hour), true},                 // grace
		{end.Add(graceAfter + time.Second), false}, // expired
	}
	for _, c := range cases {
		got := SyncAllowed(Derive(bills, c.now))
		if got != c.want {
			t.Fatalf("SyncAllowed at now=%v = %v, want %v", c.now, got, c.want)
		}
	}
}
