package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// seedAccount inserts a bare Account for id — the other tenant tests use
// accountID purely as an opaque session subject and never need a backing
// Account document, but Members() enriches each row with the account's
// email, so tests exercising it need a real one to join against.
func seedAccount(t *testing.T, s *Service, ctx context.Context, id, email string) {
	t.Helper()
	now := time.Now().UTC()
	if err := s.store.InsertAccount(ctx, &model.Account{
		ID: id, Email: email, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed account %s: %v", id, err)
	}
}

// TestMembers_List covers T14's GET route: the owner's row is present from
// Register (T13), and only a tenant member — not a stranger — may list.
func TestMembers_List(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الأعضاء")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	list, err := s.Members(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(list) != 1 || list[0].Role != model.RoleOwner || list[0].AccountID != owner {
		t.Fatalf("owner-only member list: %+v", list)
	}

	if _, err := s.Members(ctx, "acc_stranger", tn.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger list: want ErrForbidden, got %v", err)
	}
}

// TestInviteMember covers the POST route: owner-only, find-or-creates the
// invitee's Account, rejects a second invite of the same email, and rejects
// a malformed email.
func TestInviteMember(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الدعوات")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	cases := []struct {
		name    string
		caller  string
		email   string
		wantErr error
	}{
		{name: "owner invites a fresh email", caller: owner, email: "new.hire@example.com"},
		{name: "owner re-invites the same email", caller: owner, email: "new.hire@example.com", wantErr: ErrAlreadyMember},
		{name: "owner sends a malformed email", caller: owner, email: "not-an-email", wantErr: ErrInvalidEmail},
		{name: "a stranger cannot invite", caller: "acc_stranger", email: "third.hire@example.com", wantErr: ErrForbidden},
	}

	var invitedMemberAccountID string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := s.InviteMember(ctx, tc.caller, tn.ID, tc.email)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("invite: %v", err)
			}
			if m.Role != model.RoleMember || m.InvitedBy != owner || m.Email != tc.email {
				t.Fatalf("member view: %+v", m)
			}
			invitedMemberAccountID = m.AccountID

			// The find-or-create Account is real and reachable by email.
			acc, err := s.store.AccountByEmail(ctx, tc.email)
			if err != nil || acc.ID != m.AccountID {
				t.Fatalf("account: %+v err=%v", acc, err)
			}

			// The invited account can now reach the tenant.
			if _, err := s.GetBundle(ctx, m.AccountID, tn.ID); err != nil {
				t.Fatalf("invited member cannot reach tenant: %v", err)
			}
		})
	}

	// A member (not owner) — the account invited above — cannot invite others.
	if invitedMemberAccountID == "" {
		t.Fatal("no member was invited; cannot test non-owner invite rejection")
	}
	if _, err := s.InviteMember(ctx, invitedMemberAccountID, tn.ID, "second.hire@example.com"); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("non-owner invite: want ErrOwnerOnly, got %v", err)
	}
}

// TestInviteMember_ExistingAccount covers inviting an email that already has
// an Account (e.g. it signed up independently, or owns a different tenant) —
// InviteMember must reuse that Account, not create a duplicate.
func TestInviteMember_ExistingAccount(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة أولى")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	existingAccountID := "acc_pre_existing"
	seedAccount(t, s, ctx, existingAccountID, "shared@example.com")

	m, err := s.InviteMember(ctx, owner, tn.ID, "shared@example.com")
	if err != nil {
		t.Fatalf("invite existing account: %v", err)
	}
	if m.AccountID != existingAccountID {
		t.Fatalf("invite created a new account instead of reusing: %+v", m)
	}
}

// TestRevokeMember covers the DELETE route: owner-only, the owner row is
// never removable, and revocation is effective on the very next request.
func TestRevokeMember(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الإلغاء")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	m, err := s.InviteMember(ctx, owner, tn.ID, "revoke.me@example.com")
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	list, err := s.Members(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var ownerMemberID string
	for _, mv := range list {
		if mv.Role == model.RoleOwner {
			ownerMemberID = mv.ID
		}
	}

	if err := s.RevokeMember(ctx, m.AccountID, tn.ID, m.ID); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("non-owner revoke: want ErrOwnerOnly, got %v", err)
	}
	if err := s.RevokeMember(ctx, owner, tn.ID, ownerMemberID); !errors.Is(err, ErrCannotRemoveOwner) {
		t.Fatalf("revoke owner: want ErrCannotRemoveOwner, got %v", err)
	}
	if err := s.RevokeMember(ctx, owner, tn.ID, "mem_does_not_exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown member: want ErrNotFound, got %v", err)
	}

	// The revoked account can still reach the tenant right up until revoke.
	if _, err := s.GetBundle(ctx, m.AccountID, tn.ID); err != nil {
		t.Fatalf("member should still have access before revoke: %v", err)
	}
	if err := s.RevokeMember(ctx, owner, tn.ID, m.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// Effective immediately — the very next request fails.
	if _, err := s.GetBundle(ctx, m.AccountID, tn.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked member's next request: want ErrForbidden, got %v", err)
	}
}

// TestRevokedMemberCannotSelfServeCreateTenant is the T23 sweep regression:
// a member, once revoked, used to land on the console's "no tenant" empty
// state with an open invitation to create one — indistinguishable from a
// brand-new signup, even though members are never meant to reach that
// owner-only path. HasBeenMember is the durable signal (TenantMember rows
// are hard-deleted on revoke, so nothing else survives) that closes it, both
// before and after the revoke.
func TestRevokedMemberCannotSelfServeCreateTenant(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الأعضاء المُلغاة")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	m, err := s.InviteMember(ctx, owner, tn.ID, "member.to.revoke@example.com")
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	// Blocked from self-serve creation immediately on invite — being a
	// member at all disqualifies the account, not just a revoked one.
	if _, err := s.Register(ctx, m.AccountID, "منشأة يحاول العضو إنشاءها"); !errors.Is(err, ErrMembersCannotCreateTenant) {
		t.Fatalf("member self-serve create: want ErrMembersCannotCreateTenant, got %v", err)
	}

	list, err := s.Members(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var memberID string
	for _, mv := range list {
		if mv.AccountID == m.AccountID {
			memberID = mv.ID
		}
	}
	if err := s.RevokeMember(ctx, owner, tn.ID, memberID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Still blocked after revoke, even though the TenantMember row (the only
	// other record of the membership) is now gone.
	if _, err := s.Register(ctx, m.AccountID, "محاولة أخرى بعد الإلغاء"); !errors.Is(err, ErrMembersCannotCreateTenant) {
		t.Fatalf("revoked member self-serve create: want ErrMembersCannotCreateTenant, got %v", err)
	}

	// And Tenants() — the console's GET /v1/tenants — correctly shows zero,
	// same as a stranger, rather than erroring or leaking the old tenant.
	tenants, err := s.Tenants(ctx, m.AccountID)
	if err != nil {
		t.Fatalf("tenants: %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("revoked member's tenant list: want empty, got %+v", tenants)
	}
}

// TestTenants_IncludesMemberTenants covers the other half of the T23
// finding: before this fix, GET /v1/tenants was owner-only, so an *active*
// (non-revoked) member's own resolver page showed the same false "no
// tenant" empty state as a revoked one. An invited member must see the
// tenant they belong to, not just its owner.
func TestTenants_IncludesMemberTenants(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الأعضاء النشطين")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	m, err := s.InviteMember(ctx, owner, tn.ID, "active.member@example.com")
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	tenants, err := s.Tenants(ctx, m.AccountID)
	if err != nil {
		t.Fatalf("tenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].ID != tn.ID {
		t.Fatalf("active member's tenant list: want [%s], got %+v", tn.ID, tenants)
	}
}

// TestBackfillHasBeenMember covers the migration path for members invited
// before HasBeenMember existed: an account with a member-role TenantMember
// row but no flag set gets one backfilled, and an account that was never a
// member (the owner itself) is left alone.
func TestBackfillHasBeenMember(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الترحيل")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	m, err := s.InviteMember(ctx, owner, tn.ID, "legacy.member@example.com")
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	// Simulate a pre-fix invite: clear the flag InviteMember just set, as if
	// this member row predates HasBeenMember.
	if _, err := s.store.Accounts.UpdateByID(ctx, m.AccountID, bson.D{
		{Key: "$set", Value: bson.D{{Key: "has_been_member", Value: false}}},
	}); err != nil {
		t.Fatalf("clear flag: %v", err)
	}
	if _, err := s.Register(ctx, m.AccountID, "يجب أن ينجح مؤقتًا"); err != nil {
		t.Fatalf("register with cleared flag should succeed pre-backfill: %v", err)
	}

	n, err := s.BackfillHasBeenMember(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n != 1 {
		t.Fatalf("backfill count = %d, want 1", n)
	}
	if _, err := s.Register(ctx, m.AccountID, "يجب أن يفشل بعد الترحيل"); !errors.Is(err, ErrMembersCannotCreateTenant) {
		t.Fatalf("register after backfill: want ErrMembersCannotCreateTenant, got %v", err)
	}

	// Re-running is a no-op count-wise (owner was never a member; the one
	// member account is already flagged).
	if n, err := s.BackfillHasBeenMember(ctx); err != nil {
		t.Fatalf("second backfill: %v", err)
	} else if n != 1 {
		t.Fatalf("second backfill count = %d, want 1 (re-marking is idempotent, not a no-op count)", n)
	}
}
