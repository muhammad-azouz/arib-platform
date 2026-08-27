package tenant

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// fakeMailer is InviteMember's test double for tenant.Mailer — real SMTP
// delivery is unreachable from these tests, and T124's "failure is logged,
// not rolled back" bullet needs a Mailer that can be told to fail on demand,
// which mail.Sender's real net/smtp client cannot simulate.
type fakeMailer struct {
	failWith error
	calls    []fakeMailerCall
}

type fakeMailerCall struct{ to, tenantName string }

func (f *fakeMailer) SendInvite(_ context.Context, to, tenantName string) error {
	f.calls = append(f.calls, fakeMailerCall{to, tenantName})
	return f.failWith
}

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
			m, err := s.InviteMember(ctx, tc.caller, tn.ID, tc.email, "", nil)
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
	if _, err := s.InviteMember(ctx, invitedMemberAccountID, tn.ID, "second.hire@example.com", "", nil); !errors.Is(err, ErrOwnerOnly) {
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

	m, err := s.InviteMember(ctx, owner, tn.ID, "shared@example.com", "", nil)
	if err != nil {
		t.Fatalf("invite existing account: %v", err)
	}
	if m.AccountID != existingAccountID {
		t.Fatalf("invite created a new account instead of reusing: %+v", m)
	}
}

// TestInviteMember_SendsInviteEmail covers T124's first acceptance bullet:
// a successful invite sends exactly one email, naming the tenant and
// addressed to the invitee.
func TestInviteMember_SendsInviteEmail(t *testing.T) {
	mailer := &fakeMailer{}
	s, ctx := testServiceWithMailer(t, mailer)
	tn, err := s.Register(ctx, owner, "منشأة البريد")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := s.InviteMember(ctx, owner, tn.ID, "invitee@example.com", "", nil); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("invite emails sent: want 1, got %d (%+v)", len(mailer.calls), mailer.calls)
	}
	if mailer.calls[0].to != "invitee@example.com" || mailer.calls[0].tenantName != tn.Name {
		t.Fatalf("invite email: %+v, want to=invitee@example.com tenantName=%s", mailer.calls[0], tn.Name)
	}
}

// TestInviteMember_MailFailureDoesNotRollBack covers T124's first acceptance
// bullet's other half: a send failure (SMTP outage, bad mailbox) is logged
// but the membership still stands — matching the behaviour from before this
// email existed at all.
func TestInviteMember_MailFailureDoesNotRollBack(t *testing.T) {
	var logBuf bytes.Buffer
	mailer := &fakeMailer{failWith: errors.New("smtp: connection refused")}
	s, ctx := testServiceWithMailer(t, mailer)
	s.log = slog.New(slog.NewTextHandler(&logBuf, nil))
	tn, err := s.Register(ctx, owner, "منشأة الفشل")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	m, err := s.InviteMember(ctx, owner, tn.ID, "unreachable@example.com", "", nil)
	if err != nil {
		t.Fatalf("invite should succeed despite mail failure: %v", err)
	}

	// The membership is real: the invitee can reach the tenant.
	if _, err := s.GetBundle(ctx, m.AccountID, tn.ID); err != nil {
		t.Fatalf("invited member cannot reach tenant after mail failure: %v", err)
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("mail attempts: want 1, got %d", len(mailer.calls))
	}
	if !strings.Contains(logBuf.String(), "send invite email failed") || !strings.Contains(logBuf.String(), "unreachable@example.com") {
		t.Fatalf("mail failure not logged: %s", logBuf.String())
	}
}

// TestInviteMember_ReinviteSendsNothing covers T124's third acceptance
// bullet: re-inviting an existing member still refuses with ErrAlreadyMember
// before ever reaching the mailer.
func TestInviteMember_ReinviteSendsNothing(t *testing.T) {
	mailer := &fakeMailer{}
	s, ctx := testServiceWithMailer(t, mailer)
	tn, err := s.Register(ctx, owner, "منشأة إعادة الدعوة")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := s.InviteMember(ctx, owner, tn.ID, "twice@example.com", "", nil); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("first invite emails: want 1, got %d", len(mailer.calls))
	}
	if _, err := s.InviteMember(ctx, owner, tn.ID, "twice@example.com", "", nil); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("re-invite: want ErrAlreadyMember, got %v", err)
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("re-invite sent an email: want still 1, got %d", len(mailer.calls))
	}
}

// TestInviteMember_RoleAndBranchValidation covers T124's second acceptance
// bullet: role_id/branch_ids are validated exactly as AssignMemberRole (T109)
// validates them, an unknown role or an out-of-tenant branch is rejected
// before the account is even created (and sends no mail), and a valid
// role+allowlist takes effect immediately — no separate AssignMemberRole
// call needed, closing the gap T124 exists to close.
func TestInviteMember_RoleAndBranchValidation(t *testing.T) {
	mailer := &fakeMailer{}
	s, ctx := testServiceWithMailer(t, mailer)
	tenantID, _, branchID := setupTenant(t, s, ctx)
	_, _, otherBranchID := setupTenant(t, s, ctx) // a second tenant, for the "branch from elsewhere" case
	role, err := s.CreateRole(ctx, owner, tenantID, "مدير فرع", []string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	if _, err := s.InviteMember(ctx, owner, tenantID, "unknown.role@example.com", "rol_missing", nil); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("unknown role: want ErrUnknownRole, got %v", err)
	}
	if _, err := s.store.AccountByEmail(ctx, "unknown.role@example.com"); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("rejected invite must not create an account: err=%v", err)
	}

	if _, err := s.InviteMember(ctx, owner, tenantID, "unknown.branch@example.com", role.ID, []string{otherBranchID}); !errors.Is(err, ErrUnknownBranch) {
		t.Fatalf("branch from another tenant: want ErrUnknownBranch, got %v", err)
	}
	if _, err := s.store.AccountByEmail(ctx, "unknown.branch@example.com"); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("rejected invite must not create an account: err=%v", err)
	}
	if len(mailer.calls) != 0 {
		t.Fatalf("rejected invites must not send mail: got %d", len(mailer.calls))
	}

	mv, err := s.InviteMember(ctx, owner, tenantID, "assigned.on.invite@example.com", role.ID, []string{branchID})
	if err != nil {
		t.Fatalf("invite with role: %v", err)
	}
	if mv.RoleID != role.ID || mv.RoleName != "مدير فرع" || len(mv.BranchIDs) != 1 || mv.BranchIDs[0] != branchID {
		t.Fatalf("invited member view: %+v", mv)
	}

	// Effective immediately — no separate AssignMemberRole call needed.
	bundle, err := s.GetBundle(ctx, mv.AccountID, tenantID)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	wantPerms, err := perm.Normalize([]string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if bundle.Me.RoleID != role.ID || !equalStrings(bundle.Me.Permissions, wantPerms) {
		t.Fatalf("bundle me: %+v", bundle.Me)
	}
	if len(bundle.Me.BranchIDs) != 1 || bundle.Me.BranchIDs[0] != branchID {
		t.Fatalf("bundle me branch_ids: %+v", bundle.Me.BranchIDs)
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
	m, err := s.InviteMember(ctx, owner, tn.ID, "revoke.me@example.com", "", nil)
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
	m, err := s.InviteMember(ctx, owner, tn.ID, "member.to.revoke@example.com", "", nil)
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
	m, err := s.InviteMember(ctx, owner, tn.ID, "active.member@example.com", "", nil)
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
	m, err := s.InviteMember(ctx, owner, tn.ID, "legacy.member@example.com", "", nil)
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

// assignRole sets role_id/branch_ids directly on a member row, standing in
// for T109's not-yet-built PATCH — every other write path leaves a freshly
// invited member unassigned.
func assignRole(t *testing.T, s *Service, ctx context.Context, memberID, roleID string, branchIDs []string) {
	t.Helper()
	if _, err := s.store.TenantMembers.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: memberID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "role_id", Value: roleID},
			{Key: "branch_ids", Value: branchIDs},
		}}},
	); err != nil {
		t.Fatalf("assign role: %v", err)
	}
}

// TestGetBundle_MeBlock covers T108's core acceptance: the owner's `me` is
// the full catalog, unscoped; a member's is exactly their assigned role's
// normalized permission set and branch allowlist — and the member row in
// Members() carries the same role name and allowlist, resolved from the
// tenant document already in hand (no extra query per row).
func TestGetBundle_MeBlock(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة الأدوار")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Owner: full catalog, unscoped.
	ownerBundle, err := s.GetBundle(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("owner bundle: %v", err)
	}
	if ownerBundle.Me.Role != model.RoleOwner || len(ownerBundle.Me.BranchIDs) != 0 {
		t.Fatalf("owner me: %+v", ownerBundle.Me)
	}
	if len(ownerBundle.Me.Permissions) != len(perm.All) {
		t.Fatalf("owner permissions: want the full catalog (%d codes), got %+v", len(perm.All), ownerBundle.Me.Permissions)
	}

	// A member assigned a custom role sees exactly that role's normalized
	// permissions and its own branch allowlist.
	role, err := s.CreateRole(ctx, owner, tn.ID, "مدير فرع", []string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	wantPerms, err := perm.Normalize([]string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	mv, err := s.InviteMember(ctx, owner, tn.ID, "branch.manager@example.com", "", nil)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	assignRole(t, s, ctx, mv.ID, role.ID, []string{"br_2", "br_7"})

	memberBundle, err := s.GetBundle(ctx, mv.AccountID, tn.ID)
	if err != nil {
		t.Fatalf("member bundle: %v", err)
	}
	if memberBundle.Me.Role != model.RoleMember || memberBundle.Me.RoleID != role.ID || memberBundle.Me.RoleName != "مدير فرع" {
		t.Fatalf("member me identity: %+v", memberBundle.Me)
	}
	if got := memberBundle.Me.Permissions; len(got) != len(wantPerms) || !equalStrings(got, wantPerms) {
		t.Fatalf("member permissions: want %v, got %v", wantPerms, got)
	}
	if got := memberBundle.Me.BranchIDs; len(got) != 2 || got[0] != "br_2" || got[1] != "br_7" {
		t.Fatalf("member branch_ids: %+v", got)
	}

	// Members() surfaces the same role name and allowlist on the row, without
	// a second store read: roleName resolves against the tenant document
	// Members() already fetched via owned(), never a per-row query.
	list, err := s.Members(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	var branchManagerRow *MemberView
	for i := range list {
		if list[i].AccountID == mv.AccountID {
			branchManagerRow = &list[i]
		}
	}
	if branchManagerRow == nil {
		t.Fatal("branch manager row missing from Members()")
	}
	if branchManagerRow.RoleID != role.ID || branchManagerRow.RoleName != "مدير فرع" {
		t.Fatalf("member row role fields: %+v", branchManagerRow)
	}
	if len(branchManagerRow.BranchIDs) != 2 || branchManagerRow.BranchIDs[0] != "br_2" || branchManagerRow.BranchIDs[1] != "br_7" {
		t.Fatalf("member row branch_ids: %+v", branchManagerRow.BranchIDs)
	}
	// The owner's own row has no role/allowlist, and branch_ids is [] rather
	// than null.
	for _, row := range list {
		if row.Role == model.RoleOwner {
			if row.RoleID != "" || row.RoleName != "" || row.BranchIDs == nil || len(row.BranchIDs) != 0 {
				t.Fatalf("owner row: %+v", row)
			}
		}
	}
}

// TestGetBundle_MeComputedFromContextScope covers T108's second acceptance
// line: `me` must come from the same perm.Scope the middleware already
// resolved for this request, never recomputed from a fresh store read. A
// context carrying a deliberately different Scope than what Mongo actually
// holds proves GetBundle trusts the context, not the store, once one is
// present — exactly the contract RequireScope (T105) already established for
// hq.resolveGateway.
func TestGetBundle_MeComputedFromContextScope(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة السياق")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	fake := &perm.Scope{
		AccountID:   owner,
		TenantID:    tn.ID,
		Role:        string(model.RoleMember),
		RoleID:      "rol_fake",
		Permissions: []string{perm.ReportsView},
		BranchIDs:   []string{"br_fake"},
	}
	scopedCtx := perm.WithScope(ctx, fake)

	bundle, err := s.GetBundle(scopedCtx, owner, tn.ID)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	// Mongo still says this account is the RoleOwner with the full catalog —
	// if GetBundle recomputed instead of trusting the context, Me would show
	// that, not the stashed fake.
	if bundle.Me.Role != model.RoleMember || bundle.Me.RoleID != "rol_fake" {
		t.Fatalf("me did not come from the context scope: %+v", bundle.Me)
	}
	if len(bundle.Me.Permissions) != 1 || bundle.Me.Permissions[0] != perm.ReportsView {
		t.Fatalf("me permissions did not come from the context scope: %+v", bundle.Me.Permissions)
	}
	if len(bundle.Me.BranchIDs) != 1 || bundle.Me.BranchIDs[0] != "br_fake" {
		t.Fatalf("me branch_ids did not come from the context scope: %+v", bundle.Me.BranchIDs)
	}
}

// TestAssignMemberRole_OwnerOnlyAndOwnerRowProtected covers T109's first
// acceptance line: a non-owner caller is refused regardless of who the
// target is, and the owner row itself can never be reassigned — its
// "ErrCannotRemoveOwner sibling" (ErrCannotModifyOwner) fires whether the
// attempted change is the role or just the branch allowlist.
func TestAssignMemberRole_OwnerOnlyAndOwnerRowProtected(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة التعيين")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	role, err := s.CreateRole(ctx, owner, tn.ID, "مدير فرع", []string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	mv, err := s.InviteMember(ctx, owner, tn.ID, "assignee@example.com", "", nil)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	if _, err := s.AssignMemberRole(ctx, mv.AccountID, tn.ID, mv.ID, role.ID, nil); !errors.Is(err, ErrOwnerOnly) {
		t.Fatalf("non-owner caller: want ErrOwnerOnly, got %v", err)
	}

	list, err := s.Members(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	var ownerMemberID string
	for _, row := range list {
		if row.Role == model.RoleOwner {
			ownerMemberID = row.ID
		}
	}
	if ownerMemberID == "" {
		t.Fatal("owner row missing")
	}
	if _, err := s.AssignMemberRole(ctx, owner, tn.ID, ownerMemberID, role.ID, nil); !errors.Is(err, ErrCannotModifyOwner) {
		t.Fatalf("reassign owner's role: want ErrCannotModifyOwner, got %v", err)
	}
	if _, err := s.AssignMemberRole(ctx, owner, tn.ID, ownerMemberID, "", []string{"br_x"}); !errors.Is(err, ErrCannotModifyOwner) {
		t.Fatalf("reassign owner's branches: want ErrCannotModifyOwner, got %v", err)
	}
}

// TestAssignMemberRole_ValidationRejectsBeforeWrite covers T109's second
// acceptance line: an unknown role_id, or a branch_id belonging to a
// different tenant, is rejected — and neither partially writes the member
// row first.
func TestAssignMemberRole_ValidationRejectsBeforeWrite(t *testing.T) {
	s, ctx := testService(t)
	tn, err := s.Register(ctx, owner, "منشأة التحقق")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_, _, otherBranchID := setupTenant(t, s, ctx) // a second tenant, for the "branch from elsewhere" case
	role, err := s.CreateRole(ctx, owner, tn.ID, "مدير فرع", []string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	mv, err := s.InviteMember(ctx, owner, tn.ID, "assignee2@example.com", "", nil)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	if _, err := s.AssignMemberRole(ctx, owner, tn.ID, mv.ID, "rol_missing", nil); !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("unknown role: want ErrUnknownRole, got %v", err)
	}
	if _, err := s.AssignMemberRole(ctx, owner, tn.ID, mv.ID, role.ID, []string{otherBranchID}); !errors.Is(err, ErrUnknownBranch) {
		t.Fatalf("branch from another tenant: want ErrUnknownBranch, got %v", err)
	}

	// Neither rejected call wrote anything.
	list, err := s.Members(ctx, owner, tn.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	for _, row := range list {
		if row.AccountID == mv.AccountID {
			if row.RoleID != "" || len(row.BranchIDs) != 0 {
				t.Fatalf("rejected assignment still wrote to the member row: %+v", row)
			}
		}
	}
}

// TestAssignMemberRole_TakesEffectOnNextRequest covers T109's third
// acceptance line end-to-end (spec D7): the reassigned member's very next
// GetBundle call reflects the new role's permissions and branch allowlist,
// with no session action of their own — requirePerm resolves a fresh
// perm.Scope from Mongo on every request.
func TestAssignMemberRole_TakesEffectOnNextRequest(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, branchID := setupTenant(t, s, ctx)
	role, err := s.CreateRole(ctx, owner, tenantID, "مدير فرع", []string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	mv, err := s.InviteMember(ctx, owner, tenantID, "assignee3@example.com", "", nil)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	// Before assignment: no role, so no permissions.
	before, err := s.GetBundle(ctx, mv.AccountID, tenantID)
	if err != nil {
		t.Fatalf("bundle before: %v", err)
	}
	if len(before.Me.Permissions) != 0 {
		t.Fatalf("unassigned member already has permissions: %+v", before.Me.Permissions)
	}

	updated, err := s.AssignMemberRole(ctx, owner, tenantID, mv.ID, role.ID, []string{branchID})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if updated.RoleID != role.ID || updated.RoleName != "مدير فرع" || len(updated.BranchIDs) != 1 || updated.BranchIDs[0] != branchID {
		t.Fatalf("assign result: %+v", updated)
	}

	// No session action for the member — just their next request.
	after, err := s.GetBundle(ctx, mv.AccountID, tenantID)
	if err != nil {
		t.Fatalf("bundle after: %v", err)
	}
	wantPerms, err := perm.Normalize([]string{perm.OrdersManage})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if after.Me.RoleID != role.ID || !equalStrings(after.Me.Permissions, wantPerms) {
		t.Fatalf("bundle after assign: %+v", after.Me)
	}
	if len(after.Me.BranchIDs) != 1 || after.Me.BranchIDs[0] != branchID {
		t.Fatalf("bundle after assign branch_ids: %+v", after.Me.BranchIDs)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
