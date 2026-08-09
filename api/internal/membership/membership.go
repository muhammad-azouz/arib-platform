// Package membership is the single authorization checkpoint for "does this
// account have access to this tenant" (T13, roadmap Phase D). Every
// /tenants/{id}/** route used to compare Tenant.AccountID directly; each
// owning package's ownership helper (tenant.owned, hq.resolveGateway,
// hq.Branches, hq.CheckOwnership) now calls Require instead, so a missed
// call site still shows up in `grep -rn "AccountID ==" internal/`.
package membership

import (
	"context"
	"errors"

	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// ErrForbidden means the account has no membership row for the tenant.
var ErrForbidden = errors.New("resource does not belong to this account")

// Store is the one read a membership check needs; satisfied by
// *mongostore.Store directly, or by any narrower per-package store interface
// that embeds the same method.
type Store interface {
	MemberRole(ctx context.Context, tenantID, accountID string) (model.MemberRole, error)
}

// Require returns the account's role on the tenant, or ErrForbidden if it
// has none. Callers translate ErrForbidden into their own package's
// exported forbidden error so the HTTP-layer error mapping (and its
// user-facing message) is unchanged.
func Require(ctx context.Context, store Store, tenantID, accountID string) (model.MemberRole, error) {
	role, err := store.MemberRole(ctx, tenantID, accountID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", err
	}
	return role, nil
}
