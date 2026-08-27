package mongostore

import (
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
)

// TestBackfillRolesAndMembers_PreservesAccess is the test that stops this
// deploy from locking people out: a member that existed before roles did
// must resolve to every permission and no branch restriction afterwards,
// not fewer than it had before the migration.
func TestBackfillRolesAndMembers_PreservesAccess(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_owner", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	owner := &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tn.ID, AccountID: "acc_owner",
		Role: model.RoleOwner, CreatedAt: at,
	}
	if err := s.InsertMember(ctx, owner); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	memberCreatedAt := at.Add(-24 * time.Hour) // a day before the migration runs
	member := &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tn.ID, AccountID: "acc_member",
		Role: model.RoleMember, InvitedBy: "acc_owner", CreatedAt: memberCreatedAt,
	}
	if err := s.InsertMember(ctx, member); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	rolesSeeded, membersUpdated, err := s.BackfillRolesAndMembers(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if rolesSeeded != 2 {
		t.Fatalf("rolesSeeded = %d, want 2 (full access + read only)", rolesSeeded)
	}
	if membersUpdated != 1 {
		t.Fatalf("membersUpdated = %d, want 1 (the member row only)", membersUpdated)
	}

	got, err := s.TenantByID(ctx, tn.ID)
	if err != nil {
		t.Fatalf("tenant by id: %v", err)
	}
	if len(got.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(got.Roles))
	}
	var full model.TenantRole
	for _, r := range got.Roles {
		if r.Name == backfillFullAccessRoleName {
			full = r
		}
	}
	if full.ID == "" {
		t.Fatalf("no %q role seeded: %+v", backfillFullAccessRoleName, got.Roles)
	}
	if len(full.Permissions) != len(perm.All) {
		t.Fatalf("full access role has %d permissions, want all %d", len(full.Permissions), len(perm.All))
	}
	for _, code := range perm.All {
		if !perm.Can(full.Permissions, code) {
			t.Fatalf("full access role does not grant %q", code)
		}
	}

	gotMember, err := s.MemberByAccount(ctx, tn.ID, "acc_member")
	if err != nil {
		t.Fatalf("member by account: %v", err)
	}
	if gotMember.RoleID != full.ID {
		t.Fatalf("member RoleID = %q, want %q (full access)", gotMember.RoleID, full.ID)
	}
	if len(gotMember.BranchIDs) != 0 {
		t.Fatalf("member BranchIDs = %v, want empty (unscoped)", gotMember.BranchIDs)
	}
	if gotMember.AcceptedAt == nil || !gotMember.AcceptedAt.Equal(memberCreatedAt) {
		t.Fatalf("member AcceptedAt = %v, want %v (its own CreatedAt, not pending)", gotMember.AcceptedAt, memberCreatedAt)
	}

	gotOwner, err := s.MemberByAccount(ctx, tn.ID, "acc_owner")
	if err != nil {
		t.Fatalf("owner by account: %v", err)
	}
	if gotOwner.RoleID != "" {
		t.Fatalf("owner RoleID = %q, want empty — owner is never assigned a seeded role", gotOwner.RoleID)
	}
}

// TestBackfillRolesAndMembers_SecondRunIsNoop covers "two consecutive
// startups leave identical state and the second logs zero changes".
func TestBackfillRolesAndMembers_SecondRunIsNoop(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_owner", Name: "منشأة الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if err := s.InsertMember(ctx, &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tn.ID, AccountID: "acc_owner",
		Role: model.RoleOwner, CreatedAt: at,
	}); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := s.InsertMember(ctx, &model.TenantMember{
		ID: idgen.New("mem"), TenantID: tn.ID, AccountID: "acc_member",
		Role: model.RoleMember, InvitedBy: "acc_owner", CreatedAt: at,
	}); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	if _, _, err := s.BackfillRolesAndMembers(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first, err := s.TenantByID(ctx, tn.ID)
	if err != nil {
		t.Fatalf("tenant by id: %v", err)
	}

	rolesSeeded, membersUpdated, err := s.BackfillRolesAndMembers(ctx)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if rolesSeeded != 0 || membersUpdated != 0 {
		t.Fatalf("second run changed state: rolesSeeded=%d membersUpdated=%d, want 0/0", rolesSeeded, membersUpdated)
	}

	second, err := s.TenantByID(ctx, tn.ID)
	if err != nil {
		t.Fatalf("tenant by id: %v", err)
	}
	if len(second.Roles) != len(first.Roles) {
		t.Fatalf("roles changed across runs: %d -> %d", len(first.Roles), len(second.Roles))
	}
}

// TestBackfillRolesAndMembers_TenantWithNoMembers covers a tenant with no
// members still getting both seeded roles.
func TestBackfillRolesAndMembers_TenantWithNoMembers(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_owner", Name: "منشأة بلا أعضاء",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	rolesSeeded, membersUpdated, err := s.BackfillRolesAndMembers(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if rolesSeeded != 2 {
		t.Fatalf("rolesSeeded = %d, want 2", rolesSeeded)
	}
	if membersUpdated != 0 {
		t.Fatalf("membersUpdated = %d, want 0 (no members)", membersUpdated)
	}
	got, err := s.TenantByID(ctx, tn.ID)
	if err != nil {
		t.Fatalf("tenant by id: %v", err)
	}
	if len(got.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(got.Roles))
	}
}
