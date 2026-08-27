package membership

import (
	"context"
	"errors"
	"testing"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// fakeStore is a minimal counting Store fake — RequireRole/RequireScope's
// context short-circuit (T105) is only provable by asserting this stays at
// zero.
type fakeStore struct {
	calls   int
	members map[string]*model.TenantMember // keyed "tenantID|accountID"
}

func (f *fakeStore) MemberByAccount(_ context.Context, tenantID, accountID string) (*model.TenantMember, error) {
	f.calls++
	if m, ok := f.members[tenantID+"|"+accountID]; ok {
		return m, nil
	}
	return nil, mongostore.ErrNotFound
}

func TestRequireRole_UsesContextScopeWithoutStoreRead(t *testing.T) {
	fs := &fakeStore{}
	ctx := perm.WithScope(context.Background(), &perm.Scope{
		AccountID: "acc_1", TenantID: "tnt_1", Role: string(model.RoleOwner),
	})

	role, err := RequireRole(ctx, fs, "tnt_1", "acc_1")
	if err != nil {
		t.Fatalf("RequireRole: %v", err)
	}
	if role != model.RoleOwner {
		t.Fatalf("role = %q, want owner", role)
	}
	if fs.calls != 0 {
		t.Fatalf("MemberByAccount called %d times, want 0", fs.calls)
	}
}

func TestRequireRole_EmptyContextFallsBackToStore(t *testing.T) {
	fs := &fakeStore{members: map[string]*model.TenantMember{
		"tnt_1|acc_1": {Role: model.RoleMember},
	}}

	role, err := RequireRole(context.Background(), fs, "tnt_1", "acc_1")
	if err != nil {
		t.Fatalf("RequireRole: %v", err)
	}
	if role != model.RoleMember {
		t.Fatalf("role = %q, want member", role)
	}
	if fs.calls != 1 {
		t.Fatalf("MemberByAccount called %d times, want exactly 1", fs.calls)
	}
}

func TestRequireRole_MismatchedScopeFallsBackToStore(t *testing.T) {
	fs := &fakeStore{members: map[string]*model.TenantMember{
		"tnt_1|acc_1": {Role: model.RoleMember},
	}}
	// A scope resolved for a different tenant must not be trusted.
	ctx := perm.WithScope(context.Background(), &perm.Scope{
		AccountID: "acc_1", TenantID: "tnt_other", Role: string(model.RoleOwner),
	})

	role, err := RequireRole(ctx, fs, "tnt_1", "acc_1")
	if err != nil {
		t.Fatalf("RequireRole: %v", err)
	}
	if role != model.RoleMember {
		t.Fatalf("role = %q, want member (the store's answer, not the mismatched scope's)", role)
	}
	if fs.calls != 1 {
		t.Fatalf("MemberByAccount called %d times, want exactly 1 — mismatched scope must fall back", fs.calls)
	}
}

func TestRequireRole_NonMemberReturnsErrForbidden(t *testing.T) {
	fs := &fakeStore{}
	if _, err := RequireRole(context.Background(), fs, "tnt_1", "acc_stranger"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestRequireScope_UsesContextScopeWithoutStoreRead(t *testing.T) {
	fs := &fakeStore{}
	want := &perm.Scope{AccountID: "acc_1", TenantID: "tnt_1", Role: string(model.RoleMember), RoleID: "rol_1", Permissions: []string{perm.OrdersView}}
	ctx := perm.WithScope(context.Background(), want)
	tenant := &model.Tenant{ID: "tnt_1"}

	got, err := RequireScope(ctx, fs, tenant, "acc_1")
	if err != nil {
		t.Fatalf("RequireScope: %v", err)
	}
	if got != want {
		t.Fatalf("RequireScope returned a different Scope than the one in context")
	}
	if fs.calls != 0 {
		t.Fatalf("MemberByAccount called %d times, want 0", fs.calls)
	}
}

func TestRequireScope_EmptyContextResolvesFromTenantRoles(t *testing.T) {
	fs := &fakeStore{members: map[string]*model.TenantMember{
		"tnt_1|acc_1": {Role: model.RoleMember, RoleID: "rol_1", BranchIDs: []string{"br_1"}},
	}}
	tenant := &model.Tenant{ID: "tnt_1", Roles: []model.TenantRole{
		{ID: "rol_1", Permissions: []string{perm.OrdersView}},
	}}

	got, err := RequireScope(context.Background(), fs, tenant, "acc_1")
	if err != nil {
		t.Fatalf("RequireScope: %v", err)
	}
	if fs.calls != 1 {
		t.Fatalf("MemberByAccount called %d times, want exactly 1 — no second TenantByID needed, t is already in hand", fs.calls)
	}
	if got.RoleID != "rol_1" || len(got.BranchIDs) != 1 || got.BranchIDs[0] != "br_1" || !got.Has(perm.OrdersView) {
		t.Fatalf("resolved scope = %+v, want RoleID=rol_1 BranchIDs=[br_1] holding orders.view", got)
	}
}

func TestRequireScope_OwnerShortCircuitsToAllPermissions(t *testing.T) {
	fs := &fakeStore{members: map[string]*model.TenantMember{
		"tnt_1|acc_owner": {Role: model.RoleOwner},
	}}
	tenant := &model.Tenant{ID: "tnt_1"} // no Roles entry for the owner (D1)

	got, err := RequireScope(context.Background(), fs, tenant, "acc_owner")
	if err != nil {
		t.Fatalf("RequireScope: %v", err)
	}
	if got.Role != string(model.RoleOwner) || !got.IsUnscoped() {
		t.Fatalf("owner scope = %+v, want role=owner unscoped", got)
	}
	for _, code := range perm.All {
		if !got.Has(code) {
			t.Fatalf("owner scope missing %q", code)
		}
	}
}

func TestRequireScope_NonMemberReturnsErrForbidden(t *testing.T) {
	fs := &fakeStore{}
	tenant := &model.Tenant{ID: "tnt_1"}
	if _, err := RequireScope(context.Background(), fs, tenant, "acc_stranger"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}
