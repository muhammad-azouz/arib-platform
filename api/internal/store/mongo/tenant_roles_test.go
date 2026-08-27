package mongostore

import (
	"errors"
	"sync"
	"testing"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
)

func TestMemberByAccount_ReturnsFullRowAndNotFound(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	accepted := at
	m := &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tn.ID, AccountID: "acc_member",
		Role: model.RoleMember, RoleID: "rol_full", BranchIDs: []string{"br_2", "br_7"},
		InvitedBy: "acc_1", CreatedAt: at, InvitedAt: at, AcceptedAt: &accepted,
	}
	if err := s.InsertMember(ctx, m); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	got, err := s.MemberByAccount(ctx, tn.ID, "acc_member")
	if err != nil {
		t.Fatalf("MemberByAccount: %v", err)
	}
	if got.RoleID != "rol_full" || len(got.BranchIDs) != 2 || got.BranchIDs[0] != "br_2" || got.BranchIDs[1] != "br_7" {
		t.Fatalf("MemberByAccount did not return the full row: %+v", got)
	}
	if got.AcceptedAt == nil || !got.AcceptedAt.Equal(accepted) {
		t.Fatalf("MemberByAccount lost AcceptedAt: %+v", got)
	}

	if _, err := s.MemberByAccount(ctx, tn.ID, "acc_stranger"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member lookup: want ErrNotFound, got %v", err)
	}
}

func TestTenantRole_ConcurrentEditsDoNotClobber(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	roleA := model.TenantRole{ID: "rol_a", Name: "دور أ", Permissions: []string{"reports.view"}, CreatedAt: at, UpdatedAt: at}
	roleB := model.TenantRole{ID: "rol_b", Name: "دور ب", Permissions: []string{"orders.view"}, CreatedAt: at, UpdatedAt: at}
	if err := s.AddTenantRole(ctx, tn.ID, roleA); err != nil {
		t.Fatalf("add role a: %v", err)
	}
	if err := s.AddTenantRole(ctx, tn.ID, roleB); err != nil {
		t.Fatalf("add role b: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- s.UpdateTenantRole(ctx, tn.ID, "rol_a", "دور أ (محدث)", []string{"reports.view", "catalog.view"}, now())
	}()
	go func() {
		defer wg.Done()
		errs <- s.UpdateTenantRole(ctx, tn.ID, "rol_b", "دور ب (محدث)", []string{"orders.view", "orders.manage"}, now())
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}

	got, err := s.TenantByID(ctx, tn.ID)
	if err != nil {
		t.Fatalf("tenant by id: %v", err)
	}
	if len(got.Roles) != 2 {
		t.Fatalf("roles = %d, want 2 (neither update should add/remove a role)", len(got.Roles))
	}
	byID := map[string]model.TenantRole{}
	for _, r := range got.Roles {
		byID[r.ID] = r
	}
	a, b := byID["rol_a"], byID["rol_b"]
	if a.Name != "دور أ (محدث)" || len(a.Permissions) != 2 {
		t.Fatalf("role a was clobbered by the concurrent edit to role b: %+v", a)
	}
	if b.Name != "دور ب (محدث)" || len(b.Permissions) != 2 {
		t.Fatalf("role b was clobbered by the concurrent edit to role a: %+v", b)
	}
}

func TestUpdateTenantRole_UnknownRoleReturnsNotFound(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	if err := s.UpdateTenantRole(ctx, tn.ID, "rol_missing", "x", []string{"reports.view"}, now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update on unknown tenant: want ErrNotFound, got %v", err)
	}

	if err := s.AddTenantRole(ctx, tn.ID, model.TenantRole{ID: "rol_a", Name: "a", Permissions: []string{"reports.view"}, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatalf("add role: %v", err)
	}
	if err := s.UpdateTenantRole(ctx, tn.ID, "rol_missing", "x", []string{"reports.view"}, now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update on unknown role id: want ErrNotFound, got %v", err)
	}
}

func TestRemoveTenantRole(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if err := s.AddTenantRole(ctx, tn.ID, model.TenantRole{ID: "rol_a", Name: "a", Permissions: []string{"reports.view"}, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatalf("add role: %v", err)
	}

	if err := s.RemoveTenantRole(ctx, tn.ID, "rol_a", now()); err != nil {
		t.Fatalf("remove role: %v", err)
	}
	got, err := s.TenantByID(ctx, tn.ID)
	if err != nil {
		t.Fatalf("tenant by id: %v", err)
	}
	if len(got.Roles) != 0 {
		t.Fatalf("roles = %d, want 0 after removal", len(got.Roles))
	}

	if err := s.RemoveTenantRole(ctx, tn.ID, "rol_a", now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing an already-removed role: want ErrNotFound, got %v", err)
	}
}

func TestCountMembersByRole(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	for i, roleID := range []string{"rol_full", "rol_full", "rol_readonly"} {
		m := &model.TenantMember{
			ID: idgen.New("mem"), TenantID: tn.ID, AccountID: idgen.New("acc"),
			Role: model.RoleMember, RoleID: roleID, CreatedAt: at, InvitedAt: at,
		}
		if err := s.InsertMember(ctx, m); err != nil {
			t.Fatalf("insert member %d: %v", i, err)
		}
	}

	n, err := s.CountMembersByRole(ctx, tn.ID, "rol_full")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}

	n, err = s.CountMembersByRole(ctx, tn.ID, "rol_unassigned")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
}
