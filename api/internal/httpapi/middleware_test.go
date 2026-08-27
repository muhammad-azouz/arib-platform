package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/auth"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

// fakeScopeStore is the counting scopeResolver fake T104's acceptance
// requires ("exactly one membership lookup per request"). Keyed by
// "tenantID|accountID" for members, tenantID for tenants.
type fakeScopeStore struct {
	memberCalls int
	tenantCalls int
	acceptCalls int
	acceptErr   error
	members     map[string]*model.TenantMember
	tenants     map[string]*model.Tenant
}

func (f *fakeScopeStore) MemberByAccount(_ context.Context, tenantID, accountID string) (*model.TenantMember, error) {
	f.memberCalls++
	if m, ok := f.members[tenantID+"|"+accountID]; ok {
		return m, nil
	}
	return nil, mongostore.ErrNotFound
}

func (f *fakeScopeStore) TenantByID(_ context.Context, id string) (*model.Tenant, error) {
	f.tenantCalls++
	if t, ok := f.tenants[id]; ok {
		return t, nil
	}
	return nil, mongostore.ErrNotFound
}

// MarkMemberAccepted stands in for the real store's $exists-gated update
// (T125): it counts calls (the "exactly once" acceptance) and, on success,
// mutates the fixture's AcceptedAt in place — the map holds pointers, so a
// second requirePerm pass over the same fixture sees the field already set
// and skips the call, exactly like a real second read would after the
// filtered Mongo write actually persisted.
func (f *fakeScopeStore) MarkMemberAccepted(_ context.Context, _, memberID string) error {
	f.acceptCalls++
	if f.acceptErr != nil {
		return f.acceptErr
	}
	for _, m := range f.members {
		if m.ID == memberID {
			now := time.Now()
			m.AcceptedAt = &now
		}
	}
	return nil
}

// reqWithScope builds a request as requirePerm sees it once requireAuth and
// chi's own {id} resolution have already run: claims in context, and the
// "id" URL param already resolved (T104's empirical premise — see
// requirePerm's doc comment — is that chi resolves the *ancestor* {id}
// param before this level's own middleware runs, even though the leaf
// route's own pattern is not yet known).
func reqWithScope(method, path, tenantID, accountID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", tenantID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, claimsKey, &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: accountID}})
	return req.WithContext(ctx)
}

func TestRequirePerm_OwnerPassesMemberGatedByCode(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_owner":  {Role: model.RoleOwner},
			"tnt_1|acc_member": {Role: model.RoleMember, RoleID: "rol_orders_view"},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_orders_view", Permissions: []string{perm.OrdersView}},
			}},
		},
	}
	s := &Server{store: fake}

	var handlerCalled bool
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name       string
		method     string
		path       string
		account    string
		wantStatus int
		wantCalled bool
	}{
		{"owner passes an orders.manage route", "POST", "/v1/tenants/tnt_1/hq/orders", "acc_owner", http.StatusOK, true},
		{"owner passes an orders.view route", "GET", "/v1/tenants/tnt_1/hq/orders", "acc_owner", http.StatusOK, true},
		{"member with only orders.view fails orders.manage", "POST", "/v1/tenants/tnt_1/hq/orders", "acc_member", http.StatusForbidden, false},
		{"member with orders.view passes orders.view", "GET", "/v1/tenants/tnt_1/hq/orders", "acc_member", http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalled = false
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, reqWithScope(tc.method, tc.path, "tnt_1", tc.account))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if handlerCalled != tc.wantCalled {
				t.Fatalf("handlerCalled = %v, want %v", handlerCalled, tc.wantCalled)
			}
		})
	}
}

func TestRequirePerm_UndeclaredRouteDeniesEvenOwner(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{"tnt_1|acc_owner": {Role: model.RoleOwner}},
	}
	s := &Server{store: fake}

	handlerCalled := false
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/no-such-route", "tnt_1", "acc_owner"))

	if handlerCalled {
		t.Fatal("handler ran for a route absent from permTable — fail-closed is broken")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "forbidden_permission" {
		t.Fatalf("code = %q, want forbidden_permission", body["code"])
	}
	// An undeclared route can't know a required code, and denying it must
	// not cost a Mongo read — the table lookup happens before any scope
	// resolution.
	if _, ok := body["required"]; ok {
		t.Fatalf("body carries a required code for an undeclared route: %+v", body)
	}
	if fake.memberCalls != 0 {
		t.Fatalf("MemberByAccount called %d times for an undeclared route, want 0", fake.memberCalls)
	}
}

func TestRequirePerm_403BodyCarriesCodeAndRequired(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_member": {Role: model.RoleMember, RoleID: "rol_readonly"},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_readonly", Permissions: []string{perm.ReportsView}},
			}},
		},
	}
	s := &Server{store: fake}
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run when the permission is missing")
	}))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_member"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "forbidden_permission" {
		t.Fatalf("code = %q, want forbidden_permission", body["code"])
	}
	if body["required"] != perm.OrdersView {
		t.Fatalf("required = %q, want %q", body["required"], perm.OrdersView)
	}
	if body["error"] == "" {
		t.Fatal("body has no error message")
	}
}

func TestRequirePerm_ExactlyOneMembershipLookupPerRequest(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{"tnt_1|acc_owner": {Role: model.RoleOwner}},
	}
	s := &Server{store: fake}
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_owner"))

	if fake.memberCalls != 1 {
		t.Fatalf("MemberByAccount called %d times, want exactly 1", fake.memberCalls)
	}
}

func TestRequirePerm_OwnerSkipsTenantReadOwnerRowsHaveNoRoleEntry(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{"tnt_1|acc_owner": {Role: model.RoleOwner}},
	}
	s := &Server{store: fake}
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_owner"))

	if fake.tenantCalls != 0 {
		t.Fatalf("TenantByID called %d times for an owner, want 0 (D1: owner is not one of the roles)", fake.tenantCalls)
	}
}

func TestRequirePerm_NonMemberForbidden(t *testing.T) {
	fake := &fakeScopeStore{}
	s := &Server{store: fake}
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run for a non-member")
	}))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_stranger"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRequirePerm_OwnerOnlyRouteRejectsMember(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_owner":  {Role: model.RoleOwner},
			"tnt_1|acc_member": {Role: model.RoleMember, RoleID: "rol_full"},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_full", Permissions: perm.All}, // even holding every catalog code doesn't buy an owner-only route (D2)
			}},
		},
	}
	s := &Server{store: fake}
	var handlerCalled bool
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }))

	handlerCalled = false
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("POST", "/v1/tenants/tnt_1/members", "tnt_1", "acc_member"))
	if rec.Code != http.StatusForbidden || handlerCalled {
		t.Fatalf("member: status=%d handlerCalled=%v, want 403/false", rec.Code, handlerCalled)
	}

	handlerCalled = false
	rec = httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("POST", "/v1/tenants/tnt_1/members", "tnt_1", "acc_owner"))
	if rec.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("owner: status=%d handlerCalled=%v, want 200/true", rec.Code, handlerCalled)
	}
}

func TestRequirePerm_AnyMemberRouteNeedsNoCode(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_member": {Role: model.RoleMember, RoleID: "rol_empty"},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_empty", Permissions: []string{perm.ReportsView}}, // holds nothing relevant — still must pass
			}},
		},
	}
	s := &Server{store: fake}
	var handlerCalled bool
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1", "tnt_1", "acc_member"))
	if rec.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("bundle: status=%d handlerCalled=%v, want 200/true", rec.Code, handlerCalled)
	}
}

func TestRequirePerm_SharedCustomerGroupsRouteAcceptsEitherCode(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_supplier_only": {Role: model.RoleMember, RoleID: "rol_suppliers"},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_suppliers", Permissions: []string{perm.SuppliersView}},
			}},
		},
	}
	s := &Server{store: fake}
	var handlerCalled bool
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/customer-groups", "tnt_1", "acc_supplier_only"))
	if rec.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("suppliers-only member on shared groups route: status=%d handlerCalled=%v, want 200/true", rec.Code, handlerCalled)
	}
}

func TestRequirePerm_StashesScopeInContextForNextHandler(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_member": {Role: model.RoleMember, RoleID: "rol_orders", BranchIDs: []string{"br_1"}},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_orders", Permissions: []string{perm.OrdersView}},
			}},
		},
	}
	s := &Server{store: fake}
	var got *perm.Scope
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = scopeFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_member"))

	if got == nil {
		t.Fatal("scopeFrom returned nil inside the wrapped handler")
	}
	if got.RoleID != "rol_orders" || len(got.BranchIDs) != 1 || got.BranchIDs[0] != "br_1" {
		t.Fatalf("stashed scope = %+v, want RoleID=rol_orders BranchIDs=[br_1]", got)
	}
	if !got.Has(perm.OrdersView) {
		t.Fatalf("stashed scope does not grant orders.view: %+v", got)
	}
}

func TestRequirePerm_StampsAcceptedAtOnFirstRequestOnly(t *testing.T) {
	fake := &fakeScopeStore{
		members: map[string]*model.TenantMember{
			"tnt_1|acc_member": {ID: "mem_1", Role: model.RoleMember, RoleID: "rol_orders"},
			"tnt_1|acc_owner":  {ID: "mem_owner", Role: model.RoleOwner},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_orders", Permissions: []string{perm.OrdersView}},
			}},
		},
	}
	s := &Server{store: fake}
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// First request: the member's AcceptedAt is nil, so it stamps.
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_member"))
	if fake.acceptCalls != 1 {
		t.Fatalf("acceptCalls after 1st member request = %d, want 1", fake.acceptCalls)
	}
	if fake.members["tnt_1|acc_member"].AcceptedAt == nil {
		t.Fatal("member AcceptedAt still nil after the stamping request")
	}

	// Second request from the same member: AcceptedAt is now set, no write.
	rec = httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_member"))
	if fake.acceptCalls != 1 {
		t.Fatalf("acceptCalls after 2nd member request = %d, want still 1 (no re-stamp)", fake.acceptCalls)
	}

	// The owner row is never stamped — D1: the owner isn't one of these
	// roles, and AcceptedAt has no meaning for a row that was never invited.
	rec = httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_owner"))
	if fake.acceptCalls != 1 {
		t.Fatalf("acceptCalls after owner request = %d, want still 1 (owner never stamped)", fake.acceptCalls)
	}
	if fake.members["tnt_1|acc_owner"].AcceptedAt != nil {
		t.Fatal("owner row got an AcceptedAt stamp — it should never be assigned one")
	}
}

func TestRequirePerm_FailedAcceptStampDoesNotFailRequest(t *testing.T) {
	fake := &fakeScopeStore{
		acceptErr: errors.New("mongo write failed"),
		members: map[string]*model.TenantMember{
			"tnt_1|acc_member": {ID: "mem_1", Role: model.RoleMember, RoleID: "rol_orders"},
		},
		tenants: map[string]*model.Tenant{
			"tnt_1": {ID: "tnt_1", Roles: []model.TenantRole{
				{ID: "rol_orders", Permissions: []string{perm.OrdersView}},
			}},
		},
	}
	s := &Server{store: fake} // s.log is nil — must not panic on the warn path either
	var handlerCalled bool
	mw := s.requirePerm(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerCalled = true }))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, reqWithScope("GET", "/v1/tenants/tnt_1/hq/orders", "tnt_1", "acc_member"))

	if rec.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("status=%d handlerCalled=%v, want 200/true — a failed accept stamp must not fail the request", rec.Code, handlerCalled)
	}
	if fake.acceptCalls != 1 {
		t.Fatalf("acceptCalls = %d, want 1 (the stamp was attempted, just failed)", fake.acceptCalls)
	}
}
