package mongostore

import (
	"testing"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
)

// TestMarkMemberAccepted_StampsOnceAndIsIdempotent covers the store-layer
// half of T125 (spec-console-rbac D6): the accepted_at:{$exists:false}
// filter is what makes a second (or concurrent) call a genuine no-op rather
// than relying on the caller (httpapi.requirePerm) to serialize on its own
// in-memory read.
func TestMarkMemberAccepted_StampsOnceAndIsIdempotent(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	m := &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tn.ID, AccountID: "acc_member",
		Role: model.RoleMember, RoleID: "rol_full", CreatedAt: at, InvitedAt: at,
	}
	if err := s.InsertMember(ctx, m); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	if err := s.MarkMemberAccepted(ctx, tn.ID, m.ID); err != nil {
		t.Fatalf("first MarkMemberAccepted: %v", err)
	}
	got, err := s.MemberByAccount(ctx, tn.ID, "acc_member")
	if err != nil {
		t.Fatalf("MemberByAccount: %v", err)
	}
	if got.AcceptedAt == nil {
		t.Fatal("AcceptedAt still nil after MarkMemberAccepted")
	}
	first := *got.AcceptedAt

	// A second call must not move the timestamp — the $exists:false filter
	// matches nothing once accepted_at is already set.
	if err := s.MarkMemberAccepted(ctx, tn.ID, m.ID); err != nil {
		t.Fatalf("second MarkMemberAccepted: %v", err)
	}
	got, err = s.MemberByAccount(ctx, tn.ID, "acc_member")
	if err != nil {
		t.Fatalf("MemberByAccount after second call: %v", err)
	}
	if got.AcceptedAt == nil || !got.AcceptedAt.Equal(first) {
		t.Fatalf("AcceptedAt changed on a second call: first=%v now=%v", first, got.AcceptedAt)
	}

	// An unknown member id matches no document — reported as success (a
	// bare no-op), not an error; requirePerm never needs to distinguish
	// "already accepted" from "nothing to accept" for its best-effort stamp.
	if err := s.MarkMemberAccepted(ctx, tn.ID, "mem_does_not_exist"); err != nil {
		t.Fatalf("MarkMemberAccepted on unknown member: %v", err)
	}
}
