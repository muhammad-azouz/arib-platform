// Package membership is the single authorization checkpoint for "does this
// account have access to this tenant" (T13, roadmap Phase D). Every
// /tenants/{id}/** route used to compare Tenant.AccountID directly; each
// owning package's ownership helper (tenant.owned, hq.resolveGateway,
// hq.Branches, hq.CheckOwnership) now calls Require instead, so a missed
// call site still shows up in `grep -rn "AccountID ==" internal/`.
//
// RequireRole and RequireScope (spec-console-rbac T105) are Require's
// context-aware siblings: they check for a perm.Scope httpapi's
// requirePerm middleware (T104) already resolved for this exact request
// before falling back to Require's own store read, so a handler chain that
// runs through requirePerm and then into a service method pays for the
// membership lookup once, not twice.
package membership

import (
	"context"
	"errors"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// ErrForbidden means the account has no membership row for the tenant.
var ErrForbidden = errors.New("resource does not belong to this account")

// Store is the one read a membership check needs; satisfied by
// *mongostore.Store directly, or by any narrower per-package store interface
// that embeds the same method.
type Store interface {
	MemberByAccount(ctx context.Context, tenantID, accountID string) (*model.TenantMember, error)
}

// Require returns the account's role on the tenant, or ErrForbidden if it
// has none. Callers translate ErrForbidden into their own package's
// exported forbidden error so the HTTP-layer error mapping (and its
// user-facing message) is unchanged. Require's own contract is unchanged by
// the console-RBAC widening of the underlying store read (spec-console-rbac
// D9) — callers wanting the full row (role_id, branch_ids) go through
// perm.Scope instead once that's wired (T104/T105).
func Require(ctx context.Context, store Store, tenantID, accountID string) (model.MemberRole, error) {
	m, err := store.MemberByAccount(ctx, tenantID, accountID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", err
	}
	return m.Role, nil
}

// RequireRole is Require, but checks the request context first (T105):
// httpapi's requirePerm middleware (T104) already resolves and stashes a
// perm.Scope once per request, so a call whose ctx carries one for this
// exact (tenantID, accountID) pair costs zero further reads. Any other
// caller — a non-HTTP path, or a test using context.Background() — falls
// through to Require unmodified, so this is a drop-in replacement wherever
// only the role (not the full permission set) is needed.
func RequireRole(ctx context.Context, store Store, tenantID, accountID string) (model.MemberRole, error) {
	if s := perm.ScopeFrom(ctx); s != nil && s.TenantID == tenantID && s.AccountID == accountID {
		return model.MemberRole(s.Role), nil
	}
	return Require(ctx, store, tenantID, accountID)
}

// RequireScope is RequireRole's full-Scope counterpart, for callers that
// need the permission set and branch allowlist, not just the role (T105 —
// hq.resolveGateway, so T119/T120's branch scoping gets the allowlist
// without a second plumbing pass). t is the tenant the caller already has
// in hand (every call site fetches it before checking membership), so the
// fallback path never re-reads it just to resolve permissions from
// t.Roles.
func RequireScope(ctx context.Context, store Store, t *model.Tenant, accountID string) (*perm.Scope, error) {
	if s := perm.ScopeFrom(ctx); s != nil && s.TenantID == t.ID && s.AccountID == accountID {
		return s, nil
	}
	m, err := store.MemberByAccount(ctx, t.ID, accountID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	if m.Role == model.RoleOwner {
		return &perm.Scope{AccountID: accountID, TenantID: t.ID, Role: string(model.RoleOwner), Permissions: perm.All}, nil
	}
	var permissions []string
	for _, role := range t.Roles {
		if role.ID == m.RoleID {
			permissions = role.Permissions
			break
		}
	}
	return &perm.Scope{
		AccountID:   accountID,
		TenantID:    t.ID,
		Role:        string(m.Role),
		RoleID:      m.RoleID,
		Permissions: permissions,
		BranchIDs:   m.BranchIDs,
	}, nil
}
