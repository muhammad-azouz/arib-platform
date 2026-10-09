package hq

// Branch staff (service_staff.go): the validation table and the branch-scoping
// rules — the security surface. Every rule gets a failing case and a
// neighbouring passing one, because a rule asserted only in the failing
// direction cannot tell "the rule works" from "everything is rejected".
//
// Reuses testStore/fakeTokens/scopedCtx from service_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func validStaff() StaffInput {
	return StaffInput{
		Name:      "سارة أحمد",
		LoginName: "sara",
		Password:  "pass1234",
		BranchID:  "b1",
		IsActive:  true,
		RoleIDs:   []string{"r1"},
	}
}

func TestValidateStaff_Matrix(t *testing.T) {
	cases := []struct {
		name      string
		create    bool
		mutate    func(*StaffInput)
		wantField string // "" means the input must be accepted
	}{
		{"valid create", true, func(p *StaffInput) {}, ""},
		{"valid update without password", false, func(p *StaffInput) { p.Password = "" }, ""},
		{"valid PIN", true, func(p *StaffInput) { p.Pin = "4821" }, ""},
		{"valid clear_pin", false, func(p *StaffInput) { p.ClearPin = true }, ""},
		{"no roles is fine", true, func(p *StaffInput) { p.RoleIDs = nil }, ""},

		{"blank name", true, func(p *StaffInput) { p.Name = "  " }, "name"},
		{"name over 50 runes", true, func(p *StaffInput) { p.Name = repeat("ش", 51) }, "name"},
		{"name of exactly 50 runes passes", true, func(p *StaffInput) { p.Name = repeat("ش", 50) }, ""},

		{"login too short", true, func(p *StaffInput) { p.LoginName = "ab" }, "login_name"},
		{"login of exactly 3 passes", true, func(p *StaffInput) { p.LoginName = "abc" }, ""},
		{"login over 50", true, func(p *StaffInput) { p.LoginName = repeat("a", 51) }, "login_name"},
		{"login with a space", true, func(p *StaffInput) { p.LoginName = "sa ra" }, "login_name"},

		{"missing password on create", true, func(p *StaffInput) { p.Password = "" }, "password"},
		{"short password on create", true, func(p *StaffInput) { p.Password = "12345" }, "password"},
		{"6-char password passes", true, func(p *StaffInput) { p.Password = "123456" }, ""},
		{"short password on update", false, func(p *StaffInput) { p.Password = "123" }, "password"},

		{"missing branch", true, func(p *StaffInput) { p.BranchID = " " }, "branch_id"},

		{"PIN too short", true, func(p *StaffInput) { p.Pin = "123" }, "pin"},
		{"PIN with a letter", true, func(p *StaffInput) { p.Pin = "12a4" }, "pin"},
		{"PIN over 12 digits", true, func(p *StaffInput) { p.Pin = "1234567890123" }, "pin"},
		{"PIN of 12 digits passes", true, func(p *StaffInput) { p.Pin = "123456789012" }, ""},
		{"PIN and clear_pin together", false, func(p *StaffInput) { p.Pin = "1234"; p.ClearPin = true }, "pin"},

		{"blank role id", true, func(p *StaffInput) { p.RoleIDs = []string{"r1", " "} }, "role_ids"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validStaff()
			tc.mutate(&in)
			err := validateStaff(in, tc.create)
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

// staffGateway serves detail reads for the ids in branchOf (id -> branch) and
// the list at /hq/staff, accepts any write, and records every request so tests
// can assert what reached the gateway.
type staffGW struct {
	*httptest.Server
	mu     sync.Mutex
	writes []string          // "METHOD path"
	bodies []string          // raw write bodies
	query  url.Values        // last list query
	status int               // forced write status (0 = 200)
	extra  map[string]string // extra raw JSON fields injected into every row, to prove they are dropped
}

func newStaffGW(branchOf map[string]string) *staffGW {
	g := &staffGW{extra: map[string]string{}}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			b, _ := io.ReadAll(r.Body)
			g.writes = append(g.writes, r.Method+" "+r.URL.Path)
			g.bodies = append(g.bodies, string(b))
			if g.status != 0 {
				w.WriteHeader(g.status)
				_, _ = w.Write([]byte(`{"error":"x","field":"login_name"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"s1","written_at":"2026-10-09T10:00:00Z"}`))
			return
		}
		row := func(id, branch string) string {
			extra := ""
			for k, v := range g.extra {
				extra += `,"` + k + `":` + v
			}
			return `{"id":"` + id + `","name":"سارة","login_name":"sara","branch_id":"` + branch +
				`","is_active":true,"role_ids":["r1"],"has_pin":true,"pin_locked":false,` +
				`"uses_default_password":false,"created_at":"2026-08-01T00:00:00Z"` + extra + `}`
		}
		switch {
		case r.URL.Path == "/hq/staff":
			g.query = r.URL.Query()
			parts := []string{}
			for id, b := range branchOf {
				parts = append(parts, row(id, b))
			}
			_, _ = w.Write([]byte(`{"staff":[` + strings.Join(parts, ",") + `]}`))
		case strings.HasPrefix(r.URL.Path, "/hq/staff/"):
			id := strings.TrimPrefix(r.URL.Path, "/hq/staff/")
			b, ok := branchOf[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(row(id, b)))
		case r.URL.Path == "/hq/pos-roles":
			_, _ = w.Write([]byte(`{"roles":[{"id":"r1","name":"كاشير","description":"d","permission_ids":null,"staff_count":2}],"permissions":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return g
}

func (g *staffGW) wrote() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.writes) > 0
}

func TestCreateStaff_TargetBranchMustBeInAllowlist(t *testing.T) {
	gw := newStaffGW(nil)
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	in := validStaff()
	in.BranchID = "b2"
	if _, err := s.CreateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, in); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope for an out-of-allowlist branch, got %v", err)
	}
	if gw.wrote() {
		t.Fatalf("gateway must never be reached for a refused create")
	}

	in.BranchID = "b1"
	if _, err := s.CreateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, in); err != nil {
		t.Fatalf("in-allowlist create should succeed, got %v", err)
	}
	if !gw.wrote() {
		t.Fatalf("expected the gateway to be reached for an in-allowlist create")
	}

	// An unscoped member may create at any branch.
	if _, err := s.CreateStaff(context.Background(), fs.tenant.AccountID, fs.tenant.ID, func() StaffInput { i := validStaff(); i.BranchID = "b9"; return i }()); err != nil {
		t.Fatalf("unscoped create should succeed, got %v", err)
	}
}

func TestCreateStaff_ValidationRunsBeforeTheGateway(t *testing.T) {
	gw := newStaffGW(nil)
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)

	in := validStaff()
	in.Password = ""
	_, err := s.CreateStaff(context.Background(), fs.tenant.AccountID, fs.tenant.ID, in)
	var bad *StaffInvalidError
	if !errors.As(err, &bad) || bad.Field != "password" {
		t.Fatalf("expected a password validation error, got %v", err)
	}
	if gw.wrote() {
		t.Fatalf("gateway must not be reached for invalid input")
	}
}

func TestStaffMember_OutOfAllowlistIs404Not403(t *testing.T) {
	gw := newStaffGW(map[string]string{"mine": "b1", "theirs": "b2"})
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	if _, err := s.StaffMember(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine"); err != nil {
		t.Fatalf("in-allowlist staff should be readable, got %v", err)
	}
	_, err := s.StaffMember(ctx, fs.tenant.AccountID, fs.tenant.ID, "theirs")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for out-of-allowlist staff, got %v", err)
	}
	if errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("out-of-allowlist detail must not be a 403 — it confirms the id exists")
	}
	// An id that truly does not exist is indistinguishable.
	if _, err := s.StaffMember(ctx, fs.tenant.AccountID, fs.tenant.ID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id should be ErrNotFound, got %v", err)
	}
}

func TestUpdateStaff_BranchMoveChecksBothEnds(t *testing.T) {
	gw := newStaffGW(map[string]string{"mine": "b1", "theirs": "b2"})
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1", "b3"})

	// Editing a row at a branch outside the allowlist: 404, gateway untouched.
	in := validStaff()
	if _, err := s.UpdateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, "theirs", in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound editing an out-of-allowlist row, got %v", err)
	}
	// Moving my own row to a branch I do not hold: 403.
	in.BranchID = "b2"
	if _, err := s.UpdateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine", in); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope moving to an out-of-allowlist branch, got %v", err)
	}
	if gw.wrote() {
		t.Fatalf("gateway must not be reached for refused updates")
	}
	// Moving between two branches I hold works.
	in.BranchID = "b3"
	if _, err := s.UpdateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine", in); err != nil {
		t.Fatalf("move between held branches should succeed, got %v", err)
	}
	if !gw.wrote() {
		t.Fatalf("expected the gateway to be reached")
	}
}

func TestClearStaffLockout_ScopedToExistingRow(t *testing.T) {
	gw := newStaffGW(map[string]string{"mine": "b1", "theirs": "b2"})
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	if _, err := s.ClearStaffLockout(ctx, fs.tenant.AccountID, fs.tenant.ID, "theirs"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if gw.wrote() {
		t.Fatalf("gateway must not be reached for an out-of-allowlist lockout clear")
	}
	if _, err := s.ClearStaffLockout(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine"); err != nil {
		t.Fatalf("in-allowlist clear should succeed, got %v", err)
	}
	if got := gw.writes; len(got) != 1 || got[0] != "POST /hq/staff/mine/clear-lockout" {
		t.Fatalf("unexpected gateway writes: %v", got)
	}
}

func TestStaff_ScopedListInjectsAllowlist(t *testing.T) {
	gw := newStaffGW(map[string]string{"a": "b1", "b": "b3"})
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1", "b3"})

	if _, err := s.Staff(ctx, fs.tenant.AccountID, fs.tenant.ID, url.Values{}); err != nil {
		t.Fatalf("scoped list failed: %v", err)
	}
	if !sameSet(gw.query["branch_id"], "b1", "b3") {
		t.Fatalf("allowlist not injected, gateway saw %v", gw.query["branch_id"])
	}

	// A caller-supplied branch outside the allowlist is refused, not narrowed.
	if _, err := s.Staff(ctx, fs.tenant.AccountID, fs.tenant.ID, url.Values{"branch_id": {"b2"}}); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope for a foreign branch filter, got %v", err)
	}

	// An unscoped member sends no branch filter at all.
	if _, err := s.Staff(context.Background(), fs.tenant.AccountID, fs.tenant.ID, url.Values{}); err != nil {
		t.Fatalf("unscoped list failed: %v", err)
	}
	if len(gw.query["branch_id"]) != 0 {
		t.Fatalf("unscoped list must not filter, gateway saw %v", gw.query["branch_id"])
	}
}

// TestStaff_ResponsesNeverCarrySecrets: the console-facing types have no
// credential fields, so a gateway that leaked one would have it dropped on
// decode. Prove that for the list, the detail, and the write path's body.
func TestStaff_ResponsesNeverCarrySecrets(t *testing.T) {
	gw := newStaffGW(map[string]string{"s1": "b1"})
	defer gw.Close()
	gw.extra["password_hash"] = `"LEAK-PW"`
	gw.extra["pin_hash"] = `"LEAK-PIN"`
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := context.Background()

	list, err := s.Staff(ctx, fs.tenant.AccountID, fs.tenant.ID, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.StaffMember(ctx, fs.tenant.AccountID, fs.tenant.ID, "s1")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{list, detail} {
		b, _ := json.Marshal(v)
		out := string(b)
		for _, bad := range []string{"LEAK-PW", "LEAK-PIN", "password_hash", "pin_hash", `"password"`, `"pin"`} {
			if strings.Contains(out, bad) {
				t.Fatalf("response leaked %q: %s", bad, out)
			}
		}
	}
}

// TestStaffWrite_ForwardsSecretsOnlyToTheGatewayAndNeverEchoesThem: the
// plaintext password/PIN reach the gateway (it hashes them), PIN has its three
// states on the wire, and an error built from a gateway refusal contains none
// of the input.
func TestStaffWrite_ForwardsSecretsOnlyToTheGatewayAndNeverEchoesThem(t *testing.T) {
	gw := newStaffGW(nil)
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := context.Background()

	in := validStaff()
	in.Pin = "4821"
	if _, err := s.CreateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, in); err != nil {
		t.Fatal(err)
	}
	if body := gw.bodies[0]; !strings.Contains(body, `"password":"pass1234"`) || !strings.Contains(body, `"pin":"4821"`) {
		t.Fatalf("gateway body missing secrets: %s", body)
	}

	// Update with no password and no PIN change must not send either key.
	up := validStaff()
	up.Password = ""
	if _, err := s.UpdateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, "s1", up); err != nil {
		t.Fatal(err)
	}
	if body := gw.bodies[1]; strings.Contains(body, `"password"`) || strings.Contains(body, `"pin"`) || strings.Contains(body, `"clear_pin"`) {
		t.Fatalf("an update that keeps password and PIN must omit them entirely: %s", body)
	}

	// clear_pin is forwarded as such.
	up.ClearPin = true
	if _, err := s.UpdateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, "s1", up); err != nil {
		t.Fatal(err)
	}
	if body := gw.bodies[2]; !strings.Contains(body, `"clear_pin":true`) {
		t.Fatalf("clear_pin not forwarded: %s", body)
	}

	// A gateway refusal surfaces as a coded error that contains none of the input.
	gw.status = http.StatusConflict
	_, err := s.CreateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, in)
	if !errors.Is(err, ErrDuplicateLoginName) {
		t.Fatalf("gateway 409 should map to ErrDuplicateLoginName, got %v", err)
	}
	gw.status = http.StatusBadRequest
	_, err = s.CreateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, in)
	var bad *StaffInvalidError
	if !errors.As(err, &bad) || bad.Field != "login_name" {
		t.Fatalf("gateway 400 should map to a field error, got %v", err)
	}
	for _, secret := range []string{"pass1234", "4821"} {
		if strings.Contains(err.Error(), secret) || strings.Contains(bad.Message, secret) {
			t.Fatalf("error text echoed a secret: %v", err)
		}
	}
	gw.status = http.StatusServiceUnavailable
	if _, err := s.CreateStaff(ctx, fs.tenant.AccountID, fs.tenant.ID, in); !errors.Is(err, ErrTenantNotProvisioned) {
		t.Fatalf("gateway 503 should map to ErrTenantNotProvisioned, got %v", err)
	}
}

func TestPosRoles_EmptySlicesNeverNil(t *testing.T) {
	gw := newStaffGW(nil)
	defer gw.Close()
	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	// A scoped member can read POS roles: they are company-wide, not branch data.
	env, err := s.PosRoles(scopedCtx(fs, []string{"b1"}), fs.tenant.AccountID, fs.tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(env)
	if !strings.Contains(string(b), `"permission_ids":[]`) || !strings.Contains(string(b), `"permissions":[]`) {
		t.Fatalf("nil slices must serialize as [], got %s", b)
	}
}
