package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aribpos/license-api/internal/auth"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"
)

type ctxKey int

const claimsKey ctxKey = iota

// requireAuth validates the bearer access token and stores claims in context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := s.auth.TokenManager().Parse(token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin rejects non-admin callers (must run after requireAuth).
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r.Context())
		if c == nil || !c.Admin {
			writeErr(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func claimsFrom(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(claimsKey).(*auth.Claims)
	return c
}

// scopeFrom returns the perm.Scope requirePerm resolved for this request,
// or nil if requirePerm never ran (a route outside /v1/tenants/{id}, or a
// non-HTTP caller in a test). T105's service call sites fall back to a
// fresh lookup when this is nil. A thin wrapper over perm.ScopeFrom: the
// storage itself lives in perm (T105) so tenant/hq can read it back without
// importing httpapi.
func scopeFrom(ctx context.Context) *perm.Scope {
	return perm.ScopeFrom(ctx)
}

// --- console RBAC: requirePerm (spec-console-rbac D9) ---

// scopeResolver is the one store dependency requirePerm needs — narrow and
// declared here (rather than reusing tenant.Service, which only exposes
// tenant-scoped business operations, never a raw membership+role read) so
// a test can satisfy it with a counting fake instead of a real Mongo
// connection. *mongostore.Store already implements it.
type scopeResolver interface {
	MemberByAccount(ctx context.Context, tenantID, accountID string) (*model.TenantMember, error)
	TenantByID(ctx context.Context, id string) (*model.Tenant, error)
	MarkMemberAccepted(ctx context.Context, tenantID, memberID string) error
}

// accessRule is one entry in permTable. Exactly one of its three modes
// applies to a matched route:
//   - len(codes) > 0: the caller's Scope must hold at least one of them —
//     more than one only for the single route two D3 sections legitimately
//     share (see permTable's comment on hq/customer-groups).
//   - ownerOnly: only the tenant owner may call this route — for actions
//     D2 deliberately keeps out of the permission catalog entirely
//     (member/role management, billing).
//   - anyMember: no permission code applies — every member, owner or not,
//     may call this route (D3's "always visible" surfaces: the bundle,
//     the member list, issuing a sync token to activate the app).
type accessRule struct {
	method    string
	segments  []string // path segments after "/v1/tenants/{id}"; a "{...}" segment matches anything
	codes     []string
	ownerOnly bool
	anyMember bool
}

func rule(method, pattern string, codes ...string) accessRule {
	return accessRule{method: method, segments: splitSegments(pattern), codes: codes}
}

func ownerRule(method, pattern string) accessRule {
	return accessRule{method: method, segments: splitSegments(pattern), ownerOnly: true}
}

func memberRule(method, pattern string) accessRule {
	return accessRule{method: method, segments: splitSegments(pattern), anyMember: true}
}

func splitSegments(pattern string) []string {
	pattern = strings.Trim(pattern, "/")
	if pattern == "" {
		return nil
	}
	return strings.Split(pattern, "/")
}

// permTable is the complete route→permission map for every tenant-scoped
// route (spec-console-rbac D9): "the route→permission table lives beside
// the router ... so a new route and its permission are one diff." Mirrors
// the registrations under r.Route("/{id}", ...) in Router() exactly — a
// route present there with no matching entry here is denied for everyone,
// owner included, by requirePerm's fail-closed default. The SSE endpoint
// (/v1/tenants/{id}/events) is registered outside this subtree entirely
// (its own token mechanism, D9's hq.CheckOwnership) and is deliberately
// absent from this table.
var permTable = []accessRule{
	memberRule(http.MethodGet, ""), // GET /v1/tenants/{id} — the bundle; every member needs it to load the console at all

	rule(http.MethodPut, "company", perm.CompanyManage),

	rule(http.MethodPost, "branches", perm.BranchesManage),
	rule(http.MethodPatch, "branches/{branchId}", perm.BranchesManage),
	rule(http.MethodPost, "branches/{branchId}/bind", perm.BranchesManage),
	rule(http.MethodPost, "devices/{deviceId}/release", perm.BranchesManage),

	memberRule(http.MethodPost, "sync-token"), // activating the app (D3's ungated "Download") needs no section permission
	ownerRule(http.MethodGet, "subscription"), // billing is owner-only (D2)

	memberRule(http.MethodGet, "members"), // any member may see who else is on the tenant
	ownerRule(http.MethodPost, "members"),
	ownerRule(http.MethodDelete, "members/{memberId}"),
	ownerRule(http.MethodPatch, "members/{memberId}"), // T109: reassign role + branches

	// Roles (T107): reads are any member (the members table labels each row
	// with its role name); writes are owner-only, same shape as members above.
	memberRule(http.MethodGet, "roles"),
	ownerRule(http.MethodPost, "roles"),
	ownerRule(http.MethodPut, "roles/{roleId}"),
	ownerRule(http.MethodDelete, "roles/{roleId}"),
	memberRule(http.MethodGet, "permissions"), // the code catalog for the role editor's grid

	rule(http.MethodGet, "hq/branch-activity", perm.BranchesView),
	rule(http.MethodGet, "hq/branches", perm.BranchesView),

	rule(http.MethodGet, "hq/catalog/groups", perm.CatalogView),
	rule(http.MethodGet, "hq/catalog/products", perm.CatalogView),
	rule(http.MethodGet, "hq/catalog/products/{productId}", perm.CatalogView),
	rule(http.MethodPut, "hq/catalog/products/{productId}/prices", perm.CatalogManage),
	rule(http.MethodPost, "hq/catalog/products", perm.CatalogManage),
	rule(http.MethodGet, "hq/catalog/products/{productId}/movements", perm.CatalogView),

	rule(http.MethodGet, "hq/inventory/branches", perm.InventoryView),
	rule(http.MethodGet, "hq/inventory/products", perm.InventoryView),
	rule(http.MethodGet, "hq/inventory/attention", perm.InventoryView),

	rule(http.MethodGet, "hq/conflicts", perm.ConflictsView),
	rule(http.MethodPost, "hq/conflicts/ack", perm.ConflictsManage),

	rule(http.MethodGet, "hq/reports/sales", perm.ReportsView),
	rule(http.MethodGet, "hq/reports/products", perm.ReportsView),
	rule(http.MethodGet, "hq/reports/branches", perm.ReportsView),
	rule(http.MethodGet, "hq/reports/staff", perm.ReportsView),

	// Shared by Customers and Suppliers — there is no /hq/supplier-groups,
	// groups aren't type-scoped in the schema (console/lib/api.ts reuses
	// this one route from both pages) — so either view permission admits
	// the request; a suppliers-only member must not 403 loading Suppliers.
	rule(http.MethodGet, "hq/customer-groups", perm.CustomersView, perm.SuppliersView),

	rule(http.MethodGet, "hq/customers", perm.CustomersView),
	rule(http.MethodPost, "hq/customers", perm.CustomersManage),
	rule(http.MethodPut, "hq/customers/bulk", perm.CustomersManage),
	rule(http.MethodGet, "hq/customers/export", perm.CustomersView),
	rule(http.MethodPost, "hq/customers/import", perm.CustomersManage),
	rule(http.MethodGet, "hq/customers/insights", perm.CustomersView),
	rule(http.MethodGet, "hq/customers/{customerId}", perm.CustomersView),
	rule(http.MethodPut, "hq/customers/{customerId}", perm.CustomersManage),
	rule(http.MethodGet, "hq/customers/{customerId}/purchases", perm.CustomersView),
	rule(http.MethodGet, "hq/customers/{customerId}/ledger", perm.CustomersView),

	rule(http.MethodGet, "hq/suppliers", perm.SuppliersView),
	rule(http.MethodPost, "hq/suppliers", perm.SuppliersManage),
	rule(http.MethodPut, "hq/suppliers/bulk", perm.SuppliersManage),
	rule(http.MethodGet, "hq/suppliers/export", perm.SuppliersView),
	rule(http.MethodPost, "hq/suppliers/import", perm.SuppliersManage),
	rule(http.MethodGet, "hq/suppliers/insights", perm.SuppliersView),
	rule(http.MethodGet, "hq/suppliers/{supplierId}", perm.SuppliersView),
	rule(http.MethodPut, "hq/suppliers/{supplierId}", perm.SuppliersManage),
	rule(http.MethodGet, "hq/suppliers/{supplierId}/purchases", perm.SuppliersView),
	rule(http.MethodGet, "hq/suppliers/{supplierId}/ledger", perm.SuppliersView),

	rule(http.MethodGet, "hq/orders", perm.OrdersView),
	rule(http.MethodPost, "hq/orders", perm.OrdersManage),
	// availability and delivery-fee are read-only, but both exist only to
	// support the New Order form (D3's orders.manage "create" bucket) — a
	// member who can merely view orders never reaches this form.
	rule(http.MethodGet, "hq/orders/availability", perm.OrdersManage),
	rule(http.MethodGet, "hq/orders/delivery-fee", perm.OrdersManage),
	rule(http.MethodGet, "hq/orders/{orderId}", perm.OrdersView),
	rule(http.MethodPost, "hq/orders/{orderId}/cancel", perm.OrdersManage),
	rule(http.MethodPost, "hq/orders/{orderId}/transfer", perm.OrdersManage),
}

// findAccessRule returns the table entry matching method and the request
// path's suffix after "/v1/tenants/{id}" (e.g. "/hq/orders" or "" for the
// bundle itself), and whether one was found.
func findAccessRule(method, suffix string) (accessRule, bool) {
	segs := splitSegments(suffix)
	for _, r := range permTable {
		if r.method != method || len(r.segments) != len(segs) {
			continue
		}
		match := true
		for i, want := range r.segments {
			if strings.HasPrefix(want, "{") {
				continue
			}
			if want != segs[i] {
				match = false
				break
			}
		}
		if match {
			return r, true
		}
	}
	return accessRule{}, false
}

// requirePerm resolves the caller's perm.Scope on the tenant named by the
// {id} URL param exactly once — one MemberByAccount read, plus a
// TenantByID read to turn a member's RoleID into a permission set (skipped
// entirely for an owner, who always holds perm.All) — enforces the
// matched route's permission, and stashes the Scope in the request context
// for T105's service call sites to read back instead of re-querying.
//
// A route absent from permTable is denied for everyone, owner included:
// "we cannot know what an undeclared route requires, and a loud 403 in dev
// is the point" (D9). Must run after requireAuth (needs claims) and inside
// the r.Route("/{id}", ...) subrouter (needs the {id} URL param already
// resolved, which chi guarantees at this level before the leaf route
// itself is matched).
func (s *Server) requirePerm(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := chi.URLParam(r, "id")
		suffix := strings.TrimPrefix(r.URL.Path, "/v1/tenants/"+tenantID)
		matched, ok := findAccessRule(r.Method, suffix)
		if !ok {
			writeForbiddenPermission(w, "")
			return
		}

		c := claimsFrom(r.Context())
		scope, err := s.resolveScope(r.Context(), tenantID, c.Subject)
		if errors.Is(err, mongostore.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "resource does not belong to this account")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "request failed")
			return
		}

		switch {
		case matched.anyMember:
			// every member passes; no code to check
		case matched.ownerOnly:
			if scope.Role != string(model.RoleOwner) {
				writeForbiddenPermission(w, "")
				return
			}
		default:
			allowed := false
			for _, code := range matched.codes {
				if scope.Has(code) {
					allowed = true
					break
				}
			}
			if !allowed {
				writeForbiddenPermission(w, matched.codes[0])
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(perm.WithScope(r.Context(), scope)))
	})
}

// resolveScope is requirePerm's one-membership-read resolution. An owner
// row short-circuits to perm.All without a second read — Roles never
// contains an entry for the owner (D1: "owner is not one of these roles").
func (s *Server) resolveScope(ctx context.Context, tenantID, accountID string) (*perm.Scope, error) {
	m, err := s.store.MemberByAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	// T125 (D6): a non-owner member's first authenticated request on this
	// tenant stamps AcceptedAt so the console can stop showing them as
	// pending. Best-effort — a write failure here must never fail the read
	// that triggered it, same shape as InviteMember's mail-send failure
	// (T124) — logged and swallowed, guarded for the nil-log test fakes in
	// middleware_test.go. Subsequent requests see AcceptedAt already set and
	// skip the write entirely; MarkMemberAccepted's own $exists filter
	// covers the race between two concurrent first requests.
	if m.Role != model.RoleOwner && m.AcceptedAt == nil {
		if err := s.store.MarkMemberAccepted(ctx, tenantID, m.ID); err != nil && s.log != nil {
			s.log.Warn("mark member accepted", "tenant", tenantID, "member", m.ID, "err", err)
		}
	}
	if m.Role == model.RoleOwner {
		return &perm.Scope{AccountID: accountID, TenantID: tenantID, Role: string(model.RoleOwner), Permissions: perm.All}, nil
	}
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
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
		TenantID:    tenantID,
		Role:        string(m.Role),
		RoleID:      m.RoleID,
		Permissions: permissions,
		BranchIDs:   m.BranchIDs,
	}, nil
}

// writeForbiddenPermission writes D9's error contract for a permission
// denial. required is omitted for an owner-only route, which has no
// catalog code to report.
func writeForbiddenPermission(w http.ResponseWriter, required string) {
	body := map[string]string{
		"code":  "forbidden_permission",
		"error": "ليس لديك صلاحية للوصول إلى هذا القسم",
	}
	if required != "" {
		body["required"] = required
	}
	writeJSON(w, http.StatusForbidden, body)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// requestLogger logs method, path, status and latency.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.Info("http",
				"method", r.Method, "path", r.URL.Path,
				"status", sw.status, "dur_ms", time.Since(start).Milliseconds())
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher by delegating to the wrapped ResponseWriter.
// Embedding http.ResponseWriter only promotes its own three methods, not
// Flush — without this, every request through requestLogger (i.e. every
// request) fails the SSE handler's w.(http.Flusher) check and the event
// stream 500s on every connection attempt.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// --- OTP rate limiting (per client IP) ---

func (s *Server) rateLimitOTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.otpLimiter.Allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many requests, slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if i := strings.LastIndex(r.RemoteAddr, ":"); i >= 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

type keyedLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	r       rate.Limit
	burst   int
}

func newKeyedLimiter(r rate.Limit, burst int) *keyedLimiter {
	return &keyedLimiter{buckets: map[string]*rate.Limiter{}, r: r, burst: burst}
}

func (k *keyedLimiter) Allow(key string) bool {
	k.mu.Lock()
	lim, ok := k.buckets[key]
	if !ok {
		lim = rate.NewLimiter(k.r, k.burst)
		k.buckets[key] = lim
	}
	k.mu.Unlock()
	return lim.Allow()
}

func rateEvery(d time.Duration) rate.Limit { return rate.Every(d) }
