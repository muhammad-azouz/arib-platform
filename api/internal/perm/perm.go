// Package perm is the permission catalog for console RBAC (spec
// tasks/spec-console-rbac.md, D3). It is a pure leaf package — no store or
// Mongo dependency — so it can be imported by both the httpapi middleware
// (T104) that resolves a Scope once per request and the services (T105)
// that read it back out of the request context. It does carry a
// context.Context helper pair (WithScope/ScopeFrom): the request-scoped
// value has to live somewhere both httpapi (the writer) and tenant/hq (the
// readers) can import without a cycle, and perm — imported by all three
// already for the Scope type itself — is that place.
//
// There is deliberately no dotted-prefix hierarchy: r1 of the spec
// specified one and it was arithmetically wrong (a prefix walk from
// "catalog.price.write" yields "catalog.price" then "catalog" — it never
// reaches "catalog.manage"). The whole rule is one sentence: Can is exact
// set membership, plus "X.manage" implies "X.view".
package perm

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// The permission catalog (spec D3). Every code the console can gate on.
const (
	BranchesView    = "branches.view"
	BranchesManage  = "branches.manage"
	CatalogView     = "catalog.view"
	CatalogManage   = "catalog.manage"
	InventoryView   = "inventory.view"
	CustomersView   = "customers.view"
	CustomersManage = "customers.manage"
	SuppliersView   = "suppliers.view"
	SuppliersManage = "suppliers.manage"
	OrdersView      = "orders.view"
	OrdersManage    = "orders.manage"
	ReportsView     = "reports.view"
	ConflictsView   = "conflicts.view"
	ConflictsManage = "conflicts.manage"
	CompanyManage   = "company.manage"
)

// All is the full permission catalog (spec D3), in table order. Adding or
// dropping a code is a deliberate edit here, not an incidental one.
var All = []string{
	BranchesView, BranchesManage,
	CatalogView, CatalogManage,
	InventoryView,
	CustomersView, CustomersManage,
	SuppliersView, SuppliersManage,
	OrdersView, OrdersManage,
	ReportsView,
	ConflictsView, ConflictsManage,
	CompanyManage,
}

var catalogSet = func() map[string]bool {
	m := make(map[string]bool, len(All))
	for _, c := range All {
		m[c] = true
	}
	return m
}()

// managePairs lists every section that has both a view and a manage code
// (D3's table): inventory and reports have view only, company has manage
// only, so those three are absent here on purpose.
var managePairs = []struct{ manage, view string }{
	{BranchesManage, BranchesView},
	{CatalogManage, CatalogView},
	{CustomersManage, CustomersView},
	{SuppliersManage, SuppliersView},
	{OrdersManage, OrdersView},
	{ConflictsManage, ConflictsView},
}

// ErrUnknownCode means Normalize was given a code outside the catalog.
var ErrUnknownCode = errors.New("perm: unknown permission code")

// ErrEmptyPermissions means Normalize was given no codes at all — every
// role must hold at least one permission (spec API surface: ">= 1").
var ErrEmptyPermissions = errors.New("perm: at least one permission is required")

// Can reports whether perms grants code: exact set membership, plus the
// rule that holding "<section>.manage" also grants "<section>.view".
func Can(perms []string, code string) bool {
	held := make(map[string]bool, len(perms))
	for _, p := range perms {
		held[p] = true
	}
	if held[code] {
		return true
	}
	for _, pair := range managePairs {
		if pair.view == code && held[pair.manage] {
			return true
		}
	}
	return false
}

// Normalize validates codes against the catalog, rejects an unknown code or
// an empty result, dedupes, and expands every "X.manage" to also store
// "X.view" explicitly — so a stored role's permission set already reflects
// D3's implication rule rather than resolving it again on every read.
func Normalize(codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, ErrEmptyPermissions
	}
	result := make(map[string]bool, len(codes))
	for _, c := range codes {
		if !catalogSet[c] {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCode, c)
		}
		result[c] = true
		for _, pair := range managePairs {
			if pair.manage == c {
				result[pair.view] = true
			}
		}
	}
	out := make([]string, 0, len(result))
	for c := range result {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// Scope is what one request may do: the resolved permission set and branch
// allowlist for one member on one tenant. T104's middleware builds exactly
// one Scope per request and stashes it in the request context; T105's
// service call sites read it back instead of re-querying membership.
type Scope struct {
	AccountID   string
	TenantID    string // the tenant this Scope was resolved for — T105's fallback helpers check this against the tenantID they were asked about before trusting a context-cached Scope
	Role        string // "owner" or "member" (model.MemberRole, kept as a string here to stay dependency-free)
	RoleID      string
	Permissions []string
	BranchIDs   []string // empty means every branch (spec D4)
}

// scopeCtxKey is unexported so only WithScope/ScopeFrom can set or read the
// value — an unexported type prevents any other package's context key from
// colliding with it, per the standard library's context-key convention.
type scopeCtxKey struct{}

// WithScope returns a context carrying scope, for httpapi's requirePerm
// middleware (T104) to stash the Scope it resolves once per request.
func WithScope(ctx context.Context, scope *Scope) context.Context {
	return context.WithValue(ctx, scopeCtxKey{}, scope)
}

// ScopeFrom returns the Scope a prior WithScope call stashed in ctx, or nil
// if none was — a non-HTTP caller, a test using context.Background(), or an
// HTTP route requirePerm never ran for. T105's service call sites treat nil
// as "resolve it myself", the same fallback every pre-T105 caller already
// took.
func ScopeFrom(ctx context.Context) *Scope {
	s, _ := ctx.Value(scopeCtxKey{}).(*Scope)
	return s
}

// Has reports whether the scope grants code.
func (s Scope) Has(code string) bool {
	return Can(s.Permissions, code)
}

// AllowsBranch reports whether the scope may act on branchID. An empty
// allowlist means unscoped — every branch is allowed (spec D4).
func (s Scope) AllowsBranch(branchID string) bool {
	if len(s.BranchIDs) == 0 {
		return true
	}
	for _, id := range s.BranchIDs {
		if id == branchID {
			return true
		}
	}
	return false
}

// IsUnscoped reports whether the scope has no branch allowlist — true for
// the owner always, and for a member whose BranchIDs is empty (spec D4).
func (s Scope) IsUnscoped() bool {
	return len(s.BranchIDs) == 0
}
