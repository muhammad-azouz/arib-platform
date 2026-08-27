package tenant

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// Errors specific to membership management (T14, T109).
var (
	ErrOwnerOnly         = errors.New("only the tenant owner can manage members")
	ErrAlreadyMember     = errors.New("this email is already a member of the tenant")
	ErrCannotRemoveOwner = errors.New("the tenant owner cannot be removed")
	ErrInvalidEmail      = errors.New("invalid email")
	// ErrCannotModifyOwner is ErrCannotRemoveOwner's sibling for
	// AssignMemberRole (T109): the owner row is always every permission,
	// unscoped — there is no role or allowlist to assign it.
	ErrCannotModifyOwner = errors.New("the tenant owner's role and branches cannot be changed")
	ErrUnknownRole       = errors.New("no such role on this tenant")
	ErrUnknownBranch     = errors.New("branch does not belong to this tenant")
)

// MemberView is a TenantMember enriched with the account's display info —
// the console identifies people by email, not by opaque account/member ids.
// RoleID/RoleName/BranchIDs/AcceptedAt support console RBAC (spec-console-rbac
// T108): empty RoleID/RoleName and a nil-turned-empty BranchIDs describe the
// owner row, or a member not yet assigned a role (T109's job).
type MemberView struct {
	ID         string           `json:"id"`
	AccountID  string           `json:"account_id"`
	Email      string           `json:"email"`
	FirstName  string           `json:"first_name,omitempty"`
	LastName   string           `json:"last_name,omitempty"`
	Role       model.MemberRole `json:"role"`
	RoleID     string           `json:"role_id,omitempty"`
	RoleName   string           `json:"role_name,omitempty"`
	BranchIDs  []string         `json:"branch_ids"`
	InvitedBy  string           `json:"invited_by,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	AcceptedAt *time.Time       `json:"accepted_at,omitempty"`
}

// Members lists a tenant's members. Any member may view the list —
// InviteMember/RevokeMember are the owner-only operations. Each row's role
// name is resolved against the already-fetched Tenant.Roles — no extra query
// per row (spec-console-rbac T108).
func (s *Service) Members(ctx context.Context, accountID, tenantID string) ([]MemberView, error) {
	t, err := s.owned(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.MembersByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	views := make([]MemberView, 0, len(rows))
	for _, m := range rows {
		acc, err := s.store.AccountByID(ctx, m.AccountID)
		if err != nil {
			return nil, err
		}
		views = append(views, memberView(&m, acc, roleName(t, m.RoleID)))
	}
	return views, nil
}

// InviteMember adds a new member to the tenant by email — owner only. As of
// T124 it also takes the role and branch allowlist to invite them into,
// rather than leaving them assigned nothing until a separate
// AssignMemberRole call — an unassigned member has zero permissions in the
// meantime, which is exactly the silent gap this closes. Both stay
// optional (empty roleID, nil/empty branchIDs): the console's invite dialog
// (T125) always sends a role, but the API itself doesn't require one, so
// every pre-T124 caller inviting bare and assigning later still works
// unchanged.
//
// An email with no Account yet gets a bare one now, the same find-or-create
// admin.Service.FindOrCreateClient already does for admin-assigned licenses:
// no trial, no password, just an anchor for the TenantMember row and for
// AccountByEmail to find on first OTP sign-in. The existing OTP/OAuth
// findOrCreateAccount path (auth/service.go) is what actually lets the
// invitee sign in — unchanged by this feature, so "gets one on first sign-in"
// holds for the account itself even when this creates it early.
//
// role/branch validation (identical to AssignMemberRole's, T109) happens
// before the account is created or anything else is written — an unknown
// role or a branch from another tenant is rejected clean. The invite email
// (T124, spec D6) is the last step, after the membership row is already
// durable: a send failure (bad mailbox, SMTP outage) is logged and
// swallowed, not rolled back — the person is a real member either way,
// exactly today's behaviour from before this email existed at all.
func (s *Service) InviteMember(ctx context.Context, accountID, tenantID, email, roleID string, branchIDs []string) (*MemberView, error) {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if role != model.RoleOwner {
		return nil, ErrOwnerOnly
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, ErrInvalidEmail
	}

	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if roleID != "" && !hasRole(t, roleID) {
		return nil, ErrUnknownRole
	}
	if len(branchIDs) > 0 {
		branches, err := s.store.BranchesByTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, id := range branchIDs {
			if !hasBranch(branches, id) {
				return nil, ErrUnknownBranch
			}
		}
	}

	acc, err := s.store.AccountByEmail(ctx, email)
	if errors.Is(err, mongostore.ErrNotFound) {
		now := time.Now().UTC()
		acc = &model.Account{
			ID:          idgen.New("acc"),
			Email:       email,
			Providers:   []model.Provider{},
			ProviderIDs: map[string]string{},
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.store.InsertAccount(ctx, acc); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if _, err := s.store.MemberByAccount(ctx, tenantID, acc.ID); err == nil {
		return nil, ErrAlreadyMember
	} else if !errors.Is(err, mongostore.ErrNotFound) {
		return nil, err
	}

	now := time.Now().UTC()
	m := &model.TenantMember{
		ID:        idgen.New("mem"),
		TenantID:  tenantID,
		AccountID: acc.ID,
		Role:      model.RoleMember,
		RoleID:    roleID,
		BranchIDs: branchIDs,
		InvitedBy: accountID,
		CreatedAt: now,
		InvitedAt: now,
	}
	if err := s.store.InsertMember(ctx, m); err != nil {
		return nil, err
	}
	// Durable, one-directional: this account is now (and remains, even past
	// a future revoke) ineligible for Register's self-serve tenant creation.
	if err := s.store.MarkHasBeenMember(ctx, acc.ID); err != nil {
		return nil, err
	}

	if s.mailer != nil {
		if err := s.mailer.SendInvite(ctx, email, t.Name); err != nil {
			s.log.Warn("send invite email failed", "tenant_id", tenantID, "email", email, "err", err)
		}
	}

	view := memberView(m, acc, roleName(t, roleID))
	return &view, nil
}

// RevokeMember removes a member — owner only, and the owner row itself can
// never be removed (a tenant must always have exactly one owner). Effective
// immediately: the next request from the revoked account hits owned()'s
// membership lookup and gets ErrForbidden. Control-plane only — no sync
// round involved, since console access has nothing to do with a branch DB.
func (s *Service) RevokeMember(ctx context.Context, accountID, tenantID, memberID string) error {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if role != model.RoleOwner {
		return ErrOwnerOnly
	}
	target, err := s.store.MemberByID(ctx, tenantID, memberID)
	if err != nil {
		return err
	}
	if target.Role == model.RoleOwner {
		return ErrCannotRemoveOwner
	}
	deleted, err := s.store.DeleteMember(ctx, tenantID, memberID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

// AssignMemberRole sets an existing member's role and branch allowlist —
// owner only (spec-console-rbac T109), closing the gap that used to make a
// promotion mean revoking and re-inviting. Rejected before any write: an
// unknown roleID, any branchID not on this tenant, or an attempt to touch
// the owner row (which is always every permission, unscoped — there is
// nothing to assign it). Effective on the member's very next request with
// no session action (spec D7) — requirePerm resolves a fresh perm.Scope
// from Mongo on every request; nothing about this write needs to touch a
// session or a cache.
func (s *Service) AssignMemberRole(ctx context.Context, accountID, tenantID, memberID, roleID string, branchIDs []string) (*MemberView, error) {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if role != model.RoleOwner {
		return nil, ErrOwnerOnly
	}
	target, err := s.store.MemberByID(ctx, tenantID, memberID)
	if err != nil {
		return nil, err
	}
	if target.Role == model.RoleOwner {
		return nil, ErrCannotModifyOwner
	}
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !hasRole(t, roleID) {
		return nil, ErrUnknownRole
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, id := range branchIDs {
		if !hasBranch(branches, id) {
			return nil, ErrUnknownBranch
		}
	}
	found, err := s.store.UpdateMemberRole(ctx, tenantID, memberID, roleID, branchIDs)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	acc, err := s.store.AccountByID(ctx, target.AccountID)
	if err != nil {
		return nil, err
	}
	target.RoleID, target.BranchIDs = roleID, branchIDs
	view := memberView(target, acc, roleName(t, roleID))
	return &view, nil
}

func hasRole(t *model.Tenant, roleID string) bool {
	for _, r := range t.Roles {
		if r.ID == roleID {
			return true
		}
	}
	return false
}

func hasBranch(branches []model.Branch, branchID string) bool {
	for _, b := range branches {
		if b.ID == branchID {
			return true
		}
	}
	return false
}

func memberView(m *model.TenantMember, acc *model.Account, roleName string) MemberView {
	return MemberView{
		ID: m.ID, AccountID: m.AccountID, Email: acc.Email,
		FirstName: acc.FirstName, LastName: acc.LastName,
		Role: m.Role, RoleID: m.RoleID, RoleName: roleName,
		BranchIDs: orEmpty(m.BranchIDs),
		InvitedBy: m.InvitedBy, CreatedAt: m.CreatedAt, AcceptedAt: m.AcceptedAt,
	}
}

// roleName resolves roleID against t.Roles (a handful of rows, embedded on
// the tenant document and already in hand — no extra query). Empty for the
// owner row and for a member not yet assigned a role (T109).
func roleName(t *model.Tenant, roleID string) string {
	if roleID == "" {
		return ""
	}
	for _, r := range t.Roles {
		if r.ID == roleID {
			return r.Name
		}
	}
	return ""
}

// orEmpty turns a nil branch allowlist into [] rather than JSON null — the
// spec's wire contract for "unscoped" is an explicit empty array.
func orEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// MeView is the requesting member's own role, permissions, and branch
// allowlist — the tenant bundle's `me` block (spec-console-rbac T108), so
// the console renders nav, routes, tiles, and buttons from a payload it
// already fetches, with no extra request.
type MeView struct {
	Role        model.MemberRole `json:"role"`
	RoleID      string           `json:"role_id,omitempty"`
	RoleName    string           `json:"role_name,omitempty"`
	Permissions []string         `json:"permissions"`
	BranchIDs   []string         `json:"branch_ids"`
}

// meView builds the bundle's `me` block from the perm.Scope GetBundle
// resolved for this request — computed from that same Scope, never
// recomputed, so the UI can never disagree with the enforcement middleware.
func meView(t *model.Tenant, scope *perm.Scope) MeView {
	permissions := scope.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return MeView{
		Role:        model.MemberRole(scope.Role),
		RoleID:      scope.RoleID,
		RoleName:    roleName(t, scope.RoleID),
		Permissions: permissions,
		BranchIDs:   orEmpty(scope.BranchIDs),
	}
}
