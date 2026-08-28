package hq

// T136: two matrices over service_promotions.go.
//
//  1. The validation table (T131's acceptance), where every rule gets a case
//     that FAILS and a neighbouring case that PASSES — a rule asserted only in
//     the failing direction cannot tell "the rule works" from "everything is
//     rejected".
//  2. Spec D14's branch-scoping rules, including the two that are the actual
//     security surface: company-scope-requires-unscoped, and out-of-allowlist
//     reads being 404 rather than 403.
//
// Reuses testStore/fakeTokens/scopedCtx from service_test.go, same as
// service_orders_test.go.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }
func str(v string) *string   { return &v }

// validPromotion is the baseline every validation case mutates one field of,
// so a case's diff from valid IS the rule under test.
func validPromotion() PromotionInput {
	return PromotionInput{
		Name:         "خصم رمضان",
		Level:        promotionLevelItem,
		Scope:        promotionScopeAllProducts,
		DiscountType: discountTypePercentage,
		Value:        15,
		StartsOn:     mustTime("2026-09-01T00:00:00Z"),
		EndsOn:       mustTime("2026-09-30T00:00:00Z"),
		IsActive:     true,
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestValidatePromotion_Matrix(t *testing.T) {
	prod := PromotionTargetInput{Kind: promotionTargetProduct, RefID: "p1"}
	grp := PromotionTargetInput{Kind: promotionTargetGroup, RefID: "g1"}

	cases := []struct {
		name      string
		mutate    func(*PromotionInput)
		wantField string // "" means the input must be accepted
	}{
		// --- baseline and the neighbouring-pass cases ---
		{"valid storewide item promotion", func(p *PromotionInput) {}, ""},
		{"valid bill promotion", func(p *PromotionInput) {
			p.Level = promotionLevelBill
			p.MinBillTotal = f64(200)
		}, ""},
		{"valid product-targeted promotion", func(p *PromotionInput) {
			p.Scope = promotionScopeProducts
			p.Targets = []PromotionTargetInput{prod}
		}, ""},
		{"valid group-targeted promotion", func(p *PromotionInput) {
			p.Scope = promotionScopeGroups
			p.Targets = []PromotionTargetInput{grp}
		}, ""},

		// --- name ---
		{"empty name", func(p *PromotionInput) { p.Name = "   " }, "name"},
		{"name over 100 runes", func(p *PromotionInput) { p.Name = repeat("ش", 101) }, "name"},
		{"name of exactly 100 runes passes", func(p *PromotionInput) { p.Name = repeat("ش", 100) }, ""},

		// --- value ---
		{"zero value", func(p *PromotionInput) { p.Value = 0 }, "value"},
		{"negative value", func(p *PromotionInput) { p.Value = -5 }, "value"},
		{"percentage over 100", func(p *PromotionInput) { p.Value = 150 }, "value"},
		{"percentage of exactly 100 passes", func(p *PromotionInput) { p.Value = 100 }, ""},
		{"fixed amount over 100 passes (no ceiling)", func(p *PromotionInput) {
			p.DiscountType = discountTypeFixed
			p.Value = 5000
		}, ""},

		// --- dates ---
		{"ends before starts", func(p *PromotionInput) {
			p.StartsOn, p.EndsOn = p.EndsOn, p.StartsOn
		}, "ends_on"},
		{"same-day window passes", func(p *PromotionInput) { p.EndsOn = p.StartsOn }, ""},

		// --- targets vs scope ---
		{"targeted scope with no targets", func(p *PromotionInput) {
			p.Scope = promotionScopeProducts
		}, "targets"},
		{"group scope with no targets", func(p *PromotionInput) {
			p.Scope = promotionScopeGroups
		}, "targets"},
		{"storewide carrying targets", func(p *PromotionInput) {
			p.Targets = []PromotionTargetInput{prod}
		}, "targets"},
		{"product scope given a group target", func(p *PromotionInput) {
			p.Scope = promotionScopeProducts
			p.Targets = []PromotionTargetInput{grp}
		}, "targets"},
		{"group scope given a product target", func(p *PromotionInput) {
			p.Scope = promotionScopeGroups
			p.Targets = []PromotionTargetInput{prod}
		}, "targets"},
		{"blank target ref", func(p *PromotionInput) {
			p.Scope = promotionScopeProducts
			p.Targets = []PromotionTargetInput{{Kind: promotionTargetProduct, RefID: " "}}
		}, "targets"},

		// --- level cross-checks ---
		{"bill promotion carrying targets", func(p *PromotionInput) {
			p.Level = promotionLevelBill
			p.Targets = []PromotionTargetInput{prod}
		}, "targets"},
		{"bill promotion carrying min_qty", func(p *PromotionInput) {
			p.Level = promotionLevelBill
			p.MinQty = f64(3)
		}, "min_qty"},
		{"bill promotion with a non-storewide scope", func(p *PromotionInput) {
			p.Level = promotionLevelBill
			p.Scope = promotionScopeProducts
		}, "scope"},
		{"item promotion carrying min_bill_total", func(p *PromotionInput) {
			p.MinBillTotal = f64(200)
		}, "min_bill_total"},
		{"item promotion with min_qty passes", func(p *PromotionInput) { p.MinQty = f64(3) }, ""},

		// --- thresholds ---
		{"zero min_qty", func(p *PromotionInput) { p.MinQty = f64(0) }, "min_qty"},
		{"zero min_bill_total", func(p *PromotionInput) {
			p.Level = promotionLevelBill
			p.MinBillTotal = f64(0)
		}, "min_bill_total"},

		// --- enums ---
		{"undefined level", func(p *PromotionInput) { p.Level = 9 }, "level"},
		{"undefined scope", func(p *PromotionInput) { p.Scope = 9 }, "scope"},
		{"undefined discount type", func(p *PromotionInput) { p.DiscountType = 9 }, "discount_type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validPromotion()
			tc.mutate(&in)
			err := validatePromotion(in)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("expected acceptance, got %s: %s", err.Field, err.Message)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected refusal on %q, got acceptance", tc.wantField)
			}
			if err.Field != tc.wantField {
				t.Fatalf("refused on field %q, want %q (message: %s)", err.Field, tc.wantField, err.Message)
			}
			if err.Message == "" {
				t.Fatalf("refusal on %q carries no message — the console would show a blank error", tc.wantField)
			}
		})
	}
}

func repeat(s string, n int) string {
	out := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, []rune(s)[0])
	}
	return string(out)
}

// --- D14 scoping ---

// promotionGateway serves a detail read for each id in branches (nil value =
// company-wide) and accepts any write, recording whether one arrived.
func promotionGateway(branches map[string]*string, wrote *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			*wrote = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"pm1","written_at":"2026-08-27T10:00:00Z"}`))
			return
		}
		id := r.URL.Path[len("/hq/promotions/"):]
		branch, ok := branches[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := `{"id":"` + id + `","name":"عرض","level":0,"scope":0,"discount_type":0,"value":10,` +
			`"starts_on":"2026-09-01T00:00:00Z","ends_on":"2026-09-30T00:00:00Z","is_active":true,` +
			`"created_at":"2026-08-01T00:00:00Z","status":"scheduled","target_count":0,"targets":[],`
		if branch == nil {
			body += `"branch_id":null}`
		} else {
			body += `"branch_id":"` + *branch + `"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// TestCreatePromotion_CompanyScopeRequiresUnscoped is the scope-escape hole:
// a company-wide promotion has no branch identity, so a branch allowlist has
// nothing to check it against — it lands at every branch, exactly like a
// Tier-A catalog write. The direct analogue of Phase 13's price-change case.
func TestCreatePromotion_CompanyScopeRequiresUnscoped(t *testing.T) {
	wrote := false
	gw := promotionGateway(map[string]*string{}, &wrote)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	in := validPromotion() // BranchID nil == company-wide
	_, err := s.CreatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, in)
	if !errors.Is(err, ErrForbiddenUnscoped) {
		t.Fatalf("expected ErrForbiddenUnscoped for a company-wide create by a scoped member, got %v", err)
	}
	if wrote {
		t.Fatalf("gateway must never be reached for a refused company-wide create")
	}

	// The same member CAN create one scoped to a branch they hold.
	in.BranchID = str("b1")
	if _, err := s.CreatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, in); err != nil {
		t.Fatalf("in-allowlist branch create should succeed, got %v", err)
	}
	if !wrote {
		t.Fatalf("expected the gateway to be reached for an in-allowlist create")
	}

	// And an unscoped member can create the company-wide one.
	wrote = false
	if _, err := s.CreatePromotion(context.Background(), fs.tenant.AccountID, fs.tenant.ID, validPromotion()); err != nil {
		t.Fatalf("unscoped company-wide create should succeed, got %v", err)
	}
	if !wrote {
		t.Fatalf("expected the gateway to be reached for the unscoped create")
	}
}

func TestCreatePromotion_OutOfAllowlistBranchRefused(t *testing.T) {
	wrote := false
	gw := promotionGateway(map[string]*string{}, &wrote)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	in := validPromotion()
	in.BranchID = str("b2")
	if _, err := s.CreatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, in); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope for an out-of-allowlist target branch, got %v", err)
	}
	if wrote {
		t.Fatalf("gateway must never be reached for an out-of-allowlist create")
	}
}

// TestPromotionDetail_OutOfAllowlistIs404Not403 is the probe case: a scoped
// member must not be able to tell "no such promotion" from "exists at a branch
// you cannot see" by walking ids. 403 would answer the question 404 refuses to.
func TestPromotionDetail_OutOfAllowlistIs404Not403(t *testing.T) {
	wrote := false
	gw := promotionGateway(map[string]*string{
		"mine":    str("b1"),
		"theirs":  str("b2"),
		"company": nil,
	}, &wrote)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	if _, err := s.PromotionDetail(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine"); err != nil {
		t.Fatalf("in-allowlist promotion should be readable, got %v", err)
	}
	// A company-wide promotion applies at this member's branch too, so they can
	// see it — hiding it would misreport what their own branch is giving away.
	if _, err := s.PromotionDetail(ctx, fs.tenant.AccountID, fs.tenant.ID, "company"); err != nil {
		t.Fatalf("company-wide promotion should be readable by a scoped member, got %v", err)
	}
	_, err := s.PromotionDetail(ctx, fs.tenant.AccountID, fs.tenant.ID, "theirs")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an out-of-allowlist promotion, got %v", err)
	}
	if errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("out-of-allowlist detail must not be a 403 — it confirms the promotion exists")
	}
}

// TestUpdatePromotion_RescopeChecksBothEnds covers D14's transfer-shaped rule:
// moving a promotion from branch A to branch B needs BOTH in the allowlist.
func TestUpdatePromotion_RescopeChecksBothEnds(t *testing.T) {
	wrote := false
	gw := promotionGateway(map[string]*string{
		"atB1":    str("b1"),
		"atB3":    str("b3"),
		"company": nil,
	}, &wrote)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1", "b2"})

	// old b1 (held) -> new b2 (held): allowed.
	in := validPromotion()
	in.BranchID = str("b2")
	if _, err := s.UpdatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "atB1", in); err != nil {
		t.Fatalf("re-scope between two held branches should succeed, got %v", err)
	}

	// old b1 (held) -> new b9 (not held): refused on the destination.
	wrote = false
	in.BranchID = str("b9")
	if _, err := s.UpdatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "atB1", in); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope for an out-of-allowlist destination, got %v", err)
	}
	if wrote {
		t.Fatalf("gateway must never be written for a refused re-scope")
	}

	// old b3 (not held) -> new b1 (held): refused on the ORIGIN, and as a 404 —
	// the member must not learn that atB3 exists.
	in.BranchID = str("b1")
	if _, err := s.UpdatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "atB3", in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an out-of-allowlist origin, got %v", err)
	}

	// Editing an existing COMPANY-WIDE promotion is 403, not 404: the member can
	// already see it in their list, so pretending it does not exist would be a
	// lie about a row that is visibly right there.
	if _, err := s.UpdatePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "company", in); !errors.Is(err, ErrForbiddenUnscoped) {
		t.Fatalf("expected ErrForbiddenUnscoped editing a company-wide promotion, got %v", err)
	}
}

func TestDeletePromotion_ScopedToExistingRow(t *testing.T) {
	wrote := false
	gw := promotionGateway(map[string]*string{
		"mine":    str("b1"),
		"theirs":  str("b2"),
		"company": nil,
	}, &wrote)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	if _, err := s.DeletePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine"); err != nil {
		t.Fatalf("deleting an in-allowlist promotion should succeed, got %v", err)
	}
	if _, err := s.DeletePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "theirs"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting an out-of-allowlist promotion, got %v", err)
	}
	if _, err := s.DeletePromotion(ctx, fs.tenant.AccountID, fs.tenant.ID, "company"); !errors.Is(err, ErrForbiddenUnscoped) {
		t.Fatalf("expected ErrForbiddenUnscoped deleting a company-wide promotion, got %v", err)
	}
}

// TestPromotions_ScopedListInjectsAllowlist proves the branch list reaching the
// gateway is computed server-side from the member's allowlist, never taken from
// the client alone — and that a client-supplied branch outside it is refused
// rather than silently narrowed.
func TestPromotions_ScopedListInjectsAllowlist(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":0,"page":1,"page_size":50,"items":null}`))
	}))
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1", "b2"})

	env, err := s.Promotions(ctx, fs.tenant.AccountID, fs.tenant.ID, url.Values{})
	if err != nil {
		t.Fatalf("promotions: %v", err)
	}
	if !sameSet(gotQuery["branch_id"], "b1", "b2") {
		t.Fatalf("gateway saw branch_id %v, want the allowlist injected", gotQuery["branch_id"])
	}
	if env.Data.Items == nil {
		t.Fatalf("expected a non-nil empty slice, got nil")
	}

	if _, err := s.Promotions(ctx, fs.tenant.AccountID, fs.tenant.ID,
		url.Values{"branch_id": {"b9"}}); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope for an out-of-allowlist filter, got %v", err)
	}
}

// TestPromotionWrite_GatewayFieldErrorSurfaces proves a rule this layer's own
// table happens to miss still reaches the console as a field-level error
// rather than a generic 500 — the gateway's copy is the authoritative one.
func TestPromotionWrite_GatewayFieldErrorSurfaces(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid promotion","field":"value"}`))
	}))
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)

	_, err := s.CreatePromotion(context.Background(), fs.tenant.AccountID, fs.tenant.ID, validPromotion())
	var invalid *PromotionInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected *PromotionInvalidError from a gateway 400, got %v", err)
	}
	if invalid.Field != "value" || invalid.Message != "invalid promotion" {
		t.Fatalf("gateway field error not carried through: %+v", invalid)
	}
}

// --- performance (T151/T152) ---

// promotionPerformanceGateway extends promotionGateway's branch-lookup fake
// with the /performance sub-route, so PromotionPerformance's two gateway
// calls (branch-existence check, then the aggregate) both have somewhere to
// land. gotQuery/hit are optional out-params recording the aggregate call
// specifically — nil to ignore either.
func promotionPerformanceGateway(branches map[string]*string, gotQuery *url.Values, hit *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/performance") {
			if hit != nil {
				*hit = true
			}
			if gotQuery != nil {
				*gotQuery = r.URL.Query()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"from":"2026-08-01","to":"2026-08-27",` +
				`"bills_count":2,"items_count":3,"total_amount":45.5,"bills_discount_total":72,` +
				`"by_branch":null,"by_day":null}`))
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/hq/promotions/")
		branch, ok := branches[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := `{"id":"` + id + `","name":"عرض","level":0,"scope":0,"discount_type":0,"value":10,` +
			`"starts_on":"2026-09-01T00:00:00Z","ends_on":"2026-09-30T00:00:00Z","is_active":true,` +
			`"created_at":"2026-08-01T00:00:00Z","status":"scheduled","target_count":0,"targets":[],`
		if branch == nil {
			body += `"branch_id":null}`
		} else {
			body += `"branch_id":"` + *branch + `"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// TestPromotionPerformance_OutOfAllowlistIs404Not403 is PromotionDetail's own
// probe case, replayed here: the acceptance criterion is that performance is
// gated identically. An out-of-allowlist promotion must read as "doesn't
// exist", never as "exists, forbidden" — and the aggregate route must not
// even be reached once the branch check has already refused the call.
func TestPromotionPerformance_OutOfAllowlistIs404Not403(t *testing.T) {
	var gotQuery url.Values
	hit := false
	gw := promotionPerformanceGateway(map[string]*string{
		"mine":    str("b1"),
		"theirs":  str("b2"),
		"company": nil,
	}, &gotQuery, &hit)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1"})

	if _, err := s.PromotionPerformance(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine", url.Values{}); err != nil {
		t.Fatalf("in-allowlist performance should be readable, got %v", err)
	}
	if !hit {
		t.Fatalf("expected the aggregate route to be reached for an in-allowlist promotion")
	}

	// A company-wide promotion applies at this member's branch too, same
	// reasoning as the detail read.
	hit = false
	if _, err := s.PromotionPerformance(ctx, fs.tenant.AccountID, fs.tenant.ID, "company", url.Values{}); err != nil {
		t.Fatalf("company-wide performance should be readable by a scoped member, got %v", err)
	}
	if !hit {
		t.Fatalf("expected the aggregate route to be reached for a company-wide promotion")
	}

	hit = false
	_, err := s.PromotionPerformance(ctx, fs.tenant.AccountID, fs.tenant.ID, "theirs", url.Values{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an out-of-allowlist promotion, got %v", err)
	}
	if errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("out-of-allowlist performance must not be a 403 — it confirms the promotion exists")
	}
	if hit {
		t.Fatalf("aggregate route must never be reached once the branch check has refused the call")
	}
}

// TestPromotionPerformance_UnknownIDIs404ForEveryCaller is the point of
// running the existence check unconditionally rather than only for a scoped
// caller (the write paths' optimization): the gateway aggregate itself
// deliberately returns a zeroed 200 for a bogus id (T151), so without this
// check an UNSCOPED caller would get 200 zeros instead of 404 for a
// promotion that was never created.
func TestPromotionPerformance_UnknownIDIs404ForEveryCaller(t *testing.T) {
	hit := false
	gw := promotionPerformanceGateway(map[string]*string{}, nil, &hit)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)

	for _, ctx := range []context.Context{context.Background(), scopedCtx(fs, []string{"b1"})} {
		hit = false
		_, err := s.PromotionPerformance(ctx, fs.tenant.AccountID, fs.tenant.ID, "ghost", url.Values{})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for an unknown promotion id, got %v", err)
		}
		if hit {
			t.Fatalf("aggregate route must never be reached for a promotion that does not exist")
		}
	}
}

// TestPromotionPerformance_ScopedInjectsAllowlistAndForwardsPeriod proves the
// branch list reaching the gateway is server-computed from the allowlist
// (same rule as TestPromotions_ScopedListInjectsAllowlist), and that from/to
// pass through untouched — the gateway owns their defaulting/clamping, this
// layer must not re-decide it.
func TestPromotionPerformance_ScopedInjectsAllowlistAndForwardsPeriod(t *testing.T) {
	var gotQuery url.Values
	hit := false
	gw := promotionPerformanceGateway(map[string]*string{"mine": nil}, &gotQuery, &hit)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)
	ctx := scopedCtx(fs, []string{"b1", "b2"})

	_, err := s.PromotionPerformance(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine",
		url.Values{"from": {"2026-08-01"}, "to": {"2026-08-27"}})
	if err != nil {
		t.Fatalf("promotion performance: %v", err)
	}
	if !sameSet(gotQuery["branch_id"], "b1", "b2") {
		t.Fatalf("gateway saw branch_id %v, want the allowlist injected", gotQuery["branch_id"])
	}
	if gotQuery.Get("from") != "2026-08-01" || gotQuery.Get("to") != "2026-08-27" {
		t.Fatalf("from/to not forwarded to the gateway: %v", gotQuery)
	}

	if _, err := s.PromotionPerformance(ctx, fs.tenant.AccountID, fs.tenant.ID, "mine",
		url.Values{"branch_id": {"b9"}}); !errors.Is(err, ErrForbiddenScope) {
		t.Fatalf("expected ErrForbiddenScope for an out-of-allowlist filter, got %v", err)
	}
}

// TestPromotionPerformance_EmptySlicesNeverNil matches Promotions' own
// nil-safety for Items: the gateway's JSON omits by_branch/by_day when a
// promotion never fired, and the console should never have to guard against
// a null array it can't .map() over.
func TestPromotionPerformance_EmptySlicesNeverNil(t *testing.T) {
	gw := promotionPerformanceGateway(map[string]*string{"mine": nil}, nil, nil)
	defer gw.Close()

	fs := testStore(gw.URL)
	s := New(fs, &fakeTokens{}, nil)

	env, err := s.PromotionPerformance(context.Background(), fs.tenant.AccountID, fs.tenant.ID, "mine", url.Values{})
	if err != nil {
		t.Fatalf("promotion performance: %v", err)
	}
	if env.Data.ByBranch == nil || env.Data.ByDay == nil {
		t.Fatalf("expected non-nil empty slices, got by_branch=%v by_day=%v", env.Data.ByBranch, env.Data.ByDay)
	}
	// BillsDiscountTotal (T153) passes through untouched — this layer never
	// computes the promotional/manual split itself, the console does.
	if env.Data.BillsDiscountTotal != 72 {
		t.Fatalf("expected bills_discount_total to pass through as 72, got %v", env.Data.BillsDiscountTotal)
	}
}
