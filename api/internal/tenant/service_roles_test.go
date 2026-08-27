package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
)

// seedMemberWithRole inserts a plain member row assigned roleID — bypassing
// InviteMember (which never sets RoleID; PATCH …/members is T124, not this
// task) so DeleteRole's assigned-count guard has something to refuse
// against.
func seedMemberWithRole(t *testing.T, s *Service, ctx context.Context, tenantID, accountID, roleID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := s.store.InsertMember(ctx, &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tenantID, AccountID: accountID,
		Role: model.RoleMember, RoleID: roleID, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

// TestRoles_NonOwnerListsButCannotWrite covers the acceptance line "a
// non-owner member gets ErrOwnerOnly on create/update/delete and a 200 on
// list" — any member may read, only the owner may write.
func TestRoles_NonOwnerListsButCannotWrite(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)
	const member = "acc_member"
	seedAccount(t, s, ctx, member, "member@arib.test")
	seedMemberWithRole(t, s, ctx, tenantID, member, "")

	if _, err := s.Roles(ctx, member, tenantID); err != nil {
		t.Fatalf("member list: want 200/nil error, got %v", err)
	}
	if _, err := s.CreateRole(ctx, member, tenantID, "دور جديد", []string{perm.OrdersView}); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("member create: want ErrOwnerOnly, got %v", err)
	}
	if _, err := s.UpdateRole(ctx, member, tenantID, "rol_whatever", "اسم", []string{perm.OrdersView}); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("member update: want ErrOwnerOnly, got %v", err)
	}
	if err := s.DeleteRole(ctx, member, tenantID, "rol_whatever"); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("member delete: want ErrOwnerOnly, got %v", err)
	}

	// The owner passes the same gate cleanly.
	if _, err := s.CreateRole(ctx, owner, tenantID, "دور المالك", []string{perm.OrdersView}); err != nil {
		t.Fatalf("owner create: %v", err)
	}
}

// TestCreateRole_ValidationErrors covers "creating with an unknown code, an
// empty permission set, or a duplicate name is rejected with a distinct
// error each".
func TestCreateRole_ValidationErrors(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)

	if _, err := s.CreateRole(ctx, owner, tenantID, "دور", []string{"no.such.code"}); !errors.Is(err, perm.ErrUnknownCode) {
		t.Fatalf("unknown code: want perm.ErrUnknownCode, got %v", err)
	}
	if _, err := s.CreateRole(ctx, owner, tenantID, "دور", nil); !errors.Is(err, perm.ErrEmptyPermissions) {
		t.Fatalf("empty permissions: want perm.ErrEmptyPermissions, got %v", err)
	}
	if _, err := s.CreateRole(ctx, owner, tenantID, "", []string{perm.OrdersView}); !errors.Is(err, ErrInvalidRoleName) {
		t.Fatalf("empty name: want ErrInvalidRoleName, got %v", err)
	}

	if _, err := s.CreateRole(ctx, owner, tenantID, "مكرر", []string{perm.OrdersView}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s.CreateRole(ctx, owner, tenantID, "مكرر", []string{perm.ReportsView}); !errors.Is(err, ErrDuplicateRoleName) {
		t.Fatalf("duplicate name: want ErrDuplicateRoleName, got %v", err)
	}
}

// TestCreateRole_StoresNormalizedPermissions covers "a created role's
// stored permissions are the normalized set — ticking only catalog.manage
// stores catalog.view too" (D3's implication rule, applied at write time).
func TestCreateRole_StoresNormalizedPermissions(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)

	role, err := s.CreateRole(ctx, owner, tenantID, "مدير الكتالوج", []string{perm.CatalogManage})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !perm.Can(role.Permissions, perm.CatalogView) || !perm.Can(role.Permissions, perm.CatalogManage) {
		t.Fatalf("stored permissions = %v, want catalog.manage to imply catalog.view", role.Permissions)
	}
	var found bool
	for _, p := range role.Permissions {
		if p == perm.CatalogView {
			found = true
		}
	}
	if !found {
		t.Fatalf("catalog.view not explicitly stored: %v (D3 wants it materialized, not just implied)", role.Permissions)
	}
	if role.AssignedCount != 0 {
		t.Fatalf("assigned count on a brand-new role = %d, want 0", role.AssignedCount)
	}
}

// TestDeleteRole_RefusedWhileAssignedSucceedsWhenUnheld covers "deleting a
// role held by >=1 member returns 409 naming the count; deleting an unheld
// role succeeds" (spec D8).
func TestDeleteRole_RefusedWhileAssignedSucceedsWhenUnheld(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)

	held, err := s.CreateRole(ctx, owner, tenantID, "دور مشغول", []string{perm.OrdersView})
	if err != nil {
		t.Fatalf("create held role: %v", err)
	}
	free, err := s.CreateRole(ctx, owner, tenantID, "دور فارغ", []string{perm.OrdersView})
	if err != nil {
		t.Fatalf("create free role: %v", err)
	}

	seedAccount(t, s, ctx, "acc_holder1", "holder1@arib.test")
	seedAccount(t, s, ctx, "acc_holder2", "holder2@arib.test")
	seedMemberWithRole(t, s, ctx, tenantID, "acc_holder1", held.ID)
	seedMemberWithRole(t, s, ctx, tenantID, "acc_holder2", held.ID)

	err = s.DeleteRole(ctx, owner, tenantID, held.ID)
	var roleErr *RoleAssignedError
	if !errors.As(err, &roleErr) {
		t.Fatalf("delete held role: want *RoleAssignedError, got %v", err)
	}
	if roleErr.Count != 2 {
		t.Fatalf("assigned count = %d, want 2", roleErr.Count)
	}

	if err := s.DeleteRole(ctx, owner, tenantID, free.ID); err != nil {
		t.Fatalf("delete unheld role: %v", err)
	}
	roles, err := s.Roles(ctx, owner, tenantID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, r := range roles {
		if r.ID == free.ID {
			t.Fatalf("deleted role %q still present: %+v", free.ID, roles)
		}
	}
}

// TestRoles_ListReportsAssignedCounts covers the API surface note ("list
// roles + assigned counts") beyond the bare delete-guard acceptance line.
func TestRoles_ListReportsAssignedCounts(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)

	role, err := s.CreateRole(ctx, owner, tenantID, "دور", []string{perm.OrdersView})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	seedAccount(t, s, ctx, "acc_holder", "holder@arib.test")
	seedMemberWithRole(t, s, ctx, tenantID, "acc_holder", role.ID)

	roles, err := s.Roles(ctx, owner, tenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got *RoleView
	for i := range roles {
		if roles[i].ID == role.ID {
			got = &roles[i]
		}
	}
	if got == nil || got.AssignedCount != 1 {
		t.Fatalf("role %+v, want AssignedCount 1", got)
	}
}

// TestPermissions_ReturnsFullCatalogToAnyMember covers the ".../permissions"
// route: gated the same as any other tenant read, returns the whole D3
// catalog untouched by the tenant's own roles.
func TestPermissions_ReturnsFullCatalogToAnyMember(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)
	const member = "acc_member"
	seedAccount(t, s, ctx, member, "member@arib.test")
	seedMemberWithRole(t, s, ctx, tenantID, member, "")

	codes, err := s.Permissions(ctx, member, tenantID)
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}
	if len(codes) != len(perm.All) {
		t.Fatalf("permissions = %v, want the full %d-code catalog", codes, len(perm.All))
	}

	if _, err := s.Permissions(ctx, "acc_stranger", tenantID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger: want ErrForbidden, got %v", err)
	}
}
