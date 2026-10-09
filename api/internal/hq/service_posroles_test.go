package hq

// POS role writes (service_posroles.go): the unscoped-only rule, the
// validation table, and the gateway-status mapping.
//
// Reuses testStore/fakeTokens/scopedCtx from service_test.go.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type roleGW struct {
	*httptest.Server
	mu     sync.Mutex
	writes []string
	bodies []string
	status int
	body   string
}

func newRoleGW() *roleGW {
	g := &roleGW{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		buf := new(strings.Builder)
		if r.Body != nil {
			b := make([]byte, 4096)
			n, _ := r.Body.Read(b)
			buf.Write(b[:n])
		}
		g.writes = append(g.writes, r.Method+" "+r.URL.Path)
		g.bodies = append(g.bodies, buf.String())
		w.Header().Set("Content-Type", "application/json")
		if g.status != 0 {
			w.WriteHeader(g.status)
			_, _ = w.Write([]byte(g.body))
			return
		}
		_, _ = w.Write([]byte(`{"id":"r9","written_at":"2026-10-09T10:00:00Z"}`))
	}))
	return g
}

func (g *roleGW) wrote() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.writes) > 0
}

func validRole() PosRoleInput {
	return PosRoleInput{Name: "مشرف وردية", Description: "d", PermissionIDs: []string{"p1", "p2"}}
}

func TestValidatePosRole_Matrix(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*PosRoleInput)
		wantField string
	}{
		{"valid", func(p *PosRoleInput) {}, ""},
		{"no permissions is fine", func(p *PosRoleInput) { p.PermissionIDs = nil }, ""},
		{"blank description is fine", func(p *PosRoleInput) { p.Description = "" }, ""},
		{"blank name", func(p *PosRoleInput) { p.Name = "  " }, "name"},
		{"name over 50 runes", func(p *PosRoleInput) { p.Name = repeat("ش", 51) }, "name"},
		{"name of exactly 50 passes", func(p *PosRoleInput) { p.Name = repeat("ش", 50) }, ""},
		{"description over 200", func(p *PosRoleInput) { p.Description = repeat("ش", 201) }, "description"},
		{"description of exactly 200 passes", func(p *PosRoleInput) { p.Description = repeat("ش", 200) }, ""},
		{"blank permission id", func(p *PosRoleInput) { p.PermissionIDs = []string{"p1", " "} }, "permission_ids"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validRole()
			tc.mutate(&in)
			err := validatePosRole(in)
			switch {
			case tc.wantField == "" && err != nil:
				t.Fatalf("expected accepted, got %q (%s)", err.Message, err.Field)
			case tc.wantField != "" && err == nil:
				t.Fatalf("expected refusal on %q, got accepted", tc.wantField)
			case tc.wantField != "" && err.Field != tc.wantField:
				t.Fatalf("refused on %q, want %q", err.Field, tc.wantField)
			}
		})
	}
}

// A role write reaches every branch, so a branch-scoped member is refused for
// all three operations before the gateway is touched; an unscoped member goes through.
func TestPosRoleWrites_RefusedForScopedMembers(t *testing.T) {
	gw := newRoleGW()
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	scoped := scopedCtx(fs, []string{"b1"})
	acc, ten := fs.tenant.AccountID, fs.tenant.ID

	if _, err := s.CreatePosRole(scoped, acc, ten, validRole()); !errors.Is(err, ErrForbiddenUnscoped) {
		t.Fatalf("create: expected ErrForbiddenUnscoped, got %v", err)
	}
	if _, err := s.UpdatePosRole(scoped, acc, ten, "r1", validRole()); !errors.Is(err, ErrForbiddenUnscoped) {
		t.Fatalf("update: expected ErrForbiddenUnscoped, got %v", err)
	}
	if _, err := s.DeletePosRole(scoped, acc, ten, "r1"); !errors.Is(err, ErrForbiddenUnscoped) {
		t.Fatalf("delete: expected ErrForbiddenUnscoped, got %v", err)
	}
	if gw.wrote() {
		t.Fatalf("gateway must never be reached for a scoped member")
	}

	ctx := context.Background()
	if _, err := s.CreatePosRole(ctx, acc, ten, validRole()); err != nil {
		t.Fatalf("unscoped create should succeed, got %v", err)
	}
	if _, err := s.UpdatePosRole(ctx, acc, ten, "r1", validRole()); err != nil {
		t.Fatalf("unscoped update should succeed, got %v", err)
	}
	if _, err := s.DeletePosRole(ctx, acc, ten, "r1"); err != nil {
		t.Fatalf("unscoped delete should succeed, got %v", err)
	}
	want := []string{"POST /hq/pos-roles", "PUT /hq/pos-roles/r1", "DELETE /hq/pos-roles/r1"}
	if strings.Join(gw.writes, "|") != strings.Join(want, "|") {
		t.Fatalf("gateway writes = %v, want %v", gw.writes, want)
	}
}

func TestPosRoleWrites_ValidationRunsBeforeTheGateway(t *testing.T) {
	gw := newRoleGW()
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)

	in := validRole()
	in.Name = ""
	_, err := s.CreatePosRole(context.Background(), fs.tenant.AccountID, fs.tenant.ID, in)
	var bad *StaffInvalidError
	if !errors.As(err, &bad) || bad.Field != "name" {
		t.Fatalf("expected a name validation error, got %v", err)
	}
	if gw.wrote() {
		t.Fatalf("gateway must not be reached for invalid input")
	}
}

func TestPosRoleWrites_MapGatewayStatuses(t *testing.T) {
	gw := newRoleGW()
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := context.Background()
	acc, ten := fs.tenant.AccountID, fs.tenant.ID

	cases := []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusNotFound, `{"error":"not found"}`, ErrNotFound},
		{http.StatusForbidden, `{"code":"role_protected"}`, ErrRoleProtected},
		{http.StatusConflict, `{"code":"duplicate_role_name"}`, ErrDuplicateRoleName},
		{http.StatusConflict, `{"code":"role_in_use"}`, ErrRoleInUse},
		{http.StatusServiceUnavailable, `{}`, ErrTenantNotProvisioned},
	}
	for _, tc := range cases {
		gw.status, gw.body = tc.status, tc.body
		if _, err := s.UpdatePosRole(ctx, acc, ten, "r1", validRole()); !errors.Is(err, tc.want) {
			t.Fatalf("gateway %d %s: got %v, want %v", tc.status, tc.body, err, tc.want)
		}
	}

	gw.status, gw.body = http.StatusBadRequest, `{"error":"x","field":"permission_ids"}`
	_, err := s.CreatePosRole(ctx, acc, ten, validRole())
	var bad *StaffInvalidError
	if !errors.As(err, &bad) || bad.Field != "permission_ids" || bad.Message == "" {
		t.Fatalf("gateway 400 should map to a field error, got %v", err)
	}
}
