package hq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// fakeStore serves one tenant, one shard and its branches from memory.
type fakeStore struct {
	tenant   model.Tenant
	shard    model.Shard
	branches []model.Branch
}

func (f *fakeStore) BranchesByTenant(_ context.Context, tenantID string) ([]model.Branch, error) {
	if tenantID != f.tenant.ID {
		return nil, nil
	}
	return f.branches, nil
}

func (f *fakeStore) TenantByID(_ context.Context, id string) (*model.Tenant, error) {
	if id != f.tenant.ID {
		return nil, mongostore.ErrNotFound
	}
	t := f.tenant
	return &t, nil
}

func (f *fakeStore) ShardByID(_ context.Context, id string) (*model.Shard, error) {
	if id != f.shard.ID {
		return nil, mongostore.ErrNotFound
	}
	s := f.shard
	return &s, nil
}

type fakeTokens struct{ minted string }

func (f *fakeTokens) IssueHQToken(dbName string) (string, error) {
	f.minted = "hq-token-for-" + dbName
	return f.minted, nil
}

func testStore(gatewayURL string) *fakeStore {
	return &fakeStore{
		tenant: model.Tenant{ID: "tnt_1", AccountID: "acc_owner", DBName: "arib_1", ShardID: "shd_1"},
		shard:  model.Shard{ID: "shd_1", GatewayURL: gatewayURL},
	}
}

func TestBranchActivity_EnvelopeAndAuth(t *testing.T) {
	recent := time.Now().UTC().Add(-2 * time.Minute)
	old := time.Now().UTC().Add(-2 * time.Hour)
	var gotAuth string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/hq/branch-activity" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"branches":[` +
			`{"branch_id":"11111111-1111-1111-1111-111111111111","last_sync_at":"` + recent.Format(time.RFC3339Nano) + `"},` +
			`{"branch_id":"22222222-2222-2222-2222-222222222222","last_sync_at":"` + old.Format(time.RFC3339Nano) + `"}]}`))
	}))
	defer gw.Close()

	tokens := &fakeTokens{}
	s := New(testStore(gw.URL), tokens, nil)

	got, err := s.BranchActivity(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("branch activity: %v", err)
	}
	if gotAuth != "Bearer "+tokens.minted || tokens.minted != "hq-token-for-arib_1" {
		t.Fatalf("gateway saw auth %q (minted %q)", gotAuth, tokens.minted)
	}
	if len(got) != 2 {
		t.Fatalf("got %d envelopes, want 2", len(got))
	}
	if got[0].Source != "synced" || got[0].AsOf == nil || !got[0].AsOf.Equal(recent.Truncate(time.Millisecond)) && !got[0].AsOf.Equal(recent) {
		t.Fatalf("recent branch envelope: %+v", got[0])
	}
	if got[1].Source != "offline" {
		t.Fatalf("stale branch source = %q, want offline", got[1].Source)
	}
}

func TestBranchActivity_Ownership(t *testing.T) {
	s := New(testStore("http://127.0.0.1:1"), &fakeTokens{}, nil)
	if _, err := s.BranchActivity(context.Background(), "acc_intruder", "tnt_1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestBranchActivity_NotSubscribed(t *testing.T) {
	st := testStore("http://127.0.0.1:1")
	st.tenant.DBName = ""
	s := New(st, &fakeTokens{}, nil)
	if _, err := s.BranchActivity(context.Background(), "acc_owner", "tnt_1"); !errors.Is(err, ErrNotSubscribed) {
		t.Fatalf("expected ErrNotSubscribed, got %v", err)
	}
}

func TestBranchActivity_GatewayUnreachable(t *testing.T) {
	s := New(testStore("http://127.0.0.1:1"), &fakeTokens{}, &http.Client{Timeout: 500 * time.Millisecond})
	if _, err := s.BranchActivity(context.Background(), "acc_owner", "tnt_1"); !errors.Is(err, ErrGatewayUnreachable) {
		t.Fatalf("expected ErrGatewayUnreachable, got %v", err)
	}
}

// syncFreshness must report the newest branch sync, never request time — a
// tenant whose last sync was hours ago must not read "synced 0 seconds ago".
func TestSyncFreshness(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-3 * time.Minute)
	older := now.Add(-20 * time.Minute)
	stale := now.Add(-2 * time.Hour)

	branch := func(id string, ls *time.Time) model.Branch {
		return model.Branch{ID: id, TenantID: "tnt_1", Status: model.BranchActive, LastSyncAt: ls}
	}

	if src, asOf := syncFreshness([]model.Branch{branch("b1", &older), branch("b2", &fresh)}, now); src != "synced" || asOf == nil || !asOf.Equal(fresh) {
		t.Fatalf("fresh tenant: source=%q as_of=%v, want synced/%v (newest branch)", src, asOf, fresh)
	}
	if src, asOf := syncFreshness([]model.Branch{branch("b1", &stale)}, now); src != "offline" || asOf == nil || !asOf.Equal(stale) {
		t.Fatalf("stale tenant: source=%q as_of=%v, want offline/%v", src, asOf, stale)
	}
	if src, asOf := syncFreshness([]model.Branch{branch("b1", nil)}, now); src != "offline" || asOf != nil {
		t.Fatalf("never-synced tenant: source=%q as_of=%v, want offline/nil", src, asOf)
	}
	if src, asOf := syncFreshness(nil, now); src != "offline" || asOf != nil {
		t.Fatalf("no branches: source=%q as_of=%v, want offline/nil", src, asOf)
	}
}

func TestHealthTier(t *testing.T) {
	now := time.Now().UTC()
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	cases := []struct {
		name string
		last *time.Time
		want string
	}{
		{"never synced", nil, "never"},
		{"just synced", ago(1 * time.Minute), "ok"},
		{"nine minutes", ago(9 * time.Minute), "ok"},
		{"eleven minutes", ago(11 * time.Minute), "lagging"},
		{"twentynine minutes", ago(29 * time.Minute), "lagging"},
		{"thirtyone minutes", ago(31 * time.Minute), "stale"},
		{"two hours", ago(2 * time.Hour), "stale"},
	}
	for _, tc := range cases {
		if got := healthTier(tc.last, now); got != tc.want {
			t.Errorf("%s: healthTier = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBranches_TotalsSumSnapshotsHonestly(t *testing.T) {
	freshSync := time.Now().UTC().Add(-3 * time.Minute)
	staleSync := time.Now().UTC().Add(-2 * time.Hour)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"branches":[` +
			`{"branch_id":"11111111-1111-1111-1111-111111111111","today_sales_total":150.5,"today_sales_count":2,"today_refunds_total":20,"open_shift_count":1},` +
			`{"branch_id":"22222222-2222-2222-2222-222222222222","today_sales_total":100,"today_sales_count":1,"today_refunds_total":0,"open_shift_count":0}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{
		{ID: "11111111-1111-1111-1111-111111111111", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &freshSync},
		{ID: "22222222-2222-2222-2222-222222222222", TenantID: "tnt_1", Name: "المعادي", Status: model.BranchActive, LastSyncAt: &staleSync},
		{ID: "33333333-3333-3333-3333-333333333333", TenantID: "tnt_1", Name: "مدينة نصر", Status: model.BranchActive},
	}
	s := New(st, &fakeTokens{}, nil)

	got, err := s.Branches(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	if len(got.Branches) != 3 {
		t.Fatalf("got %d branches, want 3", len(got.Branches))
	}
	tot := got.Totals
	// Stale data stays visible on cards, so it must be summed too — honesty
	// comes from the offline count and the oldest contributing as_of.
	if tot.SalesTotal != 250.5 || tot.SalesCount != 3 || tot.RefundsTotal != 20 || tot.OpenShiftCount != 1 {
		t.Fatalf("totals sums wrong: %+v", tot)
	}
	if tot.SyncedBranches != 1 || tot.OfflineBranches != 2 {
		t.Fatalf("totals branch counts wrong: %+v", tot)
	}
	if tot.AsOf == nil || !tot.AsOf.Equal(staleSync) {
		t.Fatalf("totals as_of = %v, want oldest contributing sync %v", tot.AsOf, staleSync)
	}

	t.Run("gateway down zeros the totals, all branches offline", func(t *testing.T) {
		st2 := testStore("http://127.0.0.1:1")
		st2.branches = st.branches
		s2 := New(st2, &fakeTokens{}, &http.Client{Timeout: 300 * time.Millisecond})
		got, err := s2.Branches(context.Background(), "acc_owner", "tnt_1")
		if err != nil {
			t.Fatalf("branches with dead gateway: %v", err)
		}
		tot := got.Totals
		if tot.SalesTotal != 0 || tot.SalesCount != 0 || tot.SyncedBranches != 0 || tot.OfflineBranches != 3 {
			t.Fatalf("dead-gateway totals wrong: %+v", tot)
		}
		if tot.AsOf != nil {
			t.Fatalf("dead-gateway as_of = %v, want nil (no contributing data)", tot.AsOf)
		}
	})
}

func TestBranches_MergesSnapshotAndDegradesOffline(t *testing.T) {
	syncedAt := time.Now().UTC().Add(-3 * time.Minute)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/branch-snapshot" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"branches":[{"branch_id":"11111111-1111-1111-1111-111111111111",` +
			`"today_sales_total":150.5,"today_sales_count":2,"today_refunds_total":20,` +
			`"open_shift":{"num":7,"opened_by":"admin","opened_at":"2026-07-14T08:00:00Z"},"open_shift_count":1}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{
		{ID: "11111111-1111-1111-1111-111111111111", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &syncedAt},
		{ID: "22222222-2222-2222-2222-222222222222", TenantID: "tnt_1", Name: "المعادي", Status: model.BranchActive},
	}
	s := New(st, &fakeTokens{}, nil)

	res, err := s.Branches(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	got := res.Branches
	if len(got) != 2 {
		t.Fatalf("got %d branches, want 2", len(got))
	}
	b1 := got[0]
	if b1.Health != "ok" || b1.Snapshot.Source != "synced" || b1.Snapshot.Data == nil ||
		b1.Snapshot.Data.TodaySalesTotal != 150.5 || b1.Snapshot.Data.OpenShift == nil {
		t.Fatalf("synced branch view wrong: %+v", b1)
	}
	b2 := got[1]
	if b2.Health != "never" || b2.Snapshot.Data != nil || b2.Snapshot.Source != "offline" {
		t.Fatalf("never-synced branch view wrong: %+v", b2)
	}

	t.Run("gateway down degrades to control-plane data", func(t *testing.T) {
		st2 := testStore("http://127.0.0.1:1")
		st2.branches = st.branches
		s2 := New(st2, &fakeTokens{}, &http.Client{Timeout: 300 * time.Millisecond})
		res, err := s2.Branches(context.Background(), "acc_owner", "tnt_1")
		if err != nil {
			t.Fatalf("branches with dead gateway: %v", err)
		}
		got := res.Branches
		if len(got) != 2 || got[0].Snapshot.Data != nil || got[0].Snapshot.Source != "offline" {
			t.Fatalf("expected offline degrade, got %+v", got[0])
		}
		if got[0].Health != "ok" {
			t.Fatalf("health should come from control plane even offline, got %q", got[0].Health)
		}
	})

	t.Run("unsubscribed tenant still lists branches", func(t *testing.T) {
		st3 := testStore(gw.URL)
		st3.tenant.DBName = ""
		st3.branches = st.branches
		s3 := New(st3, &fakeTokens{}, nil)
		res, err := s3.Branches(context.Background(), "acc_owner", "tnt_1")
		if err != nil {
			t.Fatalf("branches without subscription: %v", err)
		}
		got := res.Branches
		if len(got) != 2 || got[0].Snapshot.Source != "offline" {
			t.Fatalf("expected offline snapshots, got %+v", got[0])
		}
	})
}

func TestCatalogGroups(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/groups" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groups":[{"id":"g1","parent_id":"00000000-0000-0000-0000-000000000000","name":"مشروبات","is_active":true,"num":1,"product_count":5}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	env, err := s.CatalogGroups(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("catalog groups: %v", err)
	}
	if env.Source != "synced" || len(env.Data) != 1 || env.Data[0].Name != "مشروبات" || env.Data[0].ProductCount != 5 {
		t.Fatalf("catalog groups envelope wrong: %+v", env)
	}
	if env.AsOf == nil || !env.AsOf.Equal(fresh) {
		t.Fatalf("as_of = %v, want the branch's last sync %v (never request time)", env.AsOf, fresh)
	}

	if _, err := s.CatalogGroups(context.Background(), "acc_intruder", "tnt_1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCatalogProducts_PassesQueryParams(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"page":2,"page_size":10,"items":[` +
			`{"id":"p1","code":100,"name":"كولا","kind":1,"is_active":true,"unit":"علبة","sale":5,"buy":3,"barcodes":["123"],"total_qty":42}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	params := url.Values{"search": {"كولا"}, "page": {"2"}, "page_size": {"10"}}
	env, err := s.CatalogProducts(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("catalog products: %v", err)
	}
	if gotQuery.Get("search") != "كولا" || gotQuery.Get("page") != "2" || gotQuery.Get("page_size") != "10" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Source != "synced" || env.Data.Total != 1 || len(env.Data.Items) != 1 || env.Data.Items[0].TotalQty != 42 {
		t.Fatalf("catalog products envelope wrong: %+v", env)
	}
	if env.AsOf == nil || !env.AsOf.Equal(fresh) {
		t.Fatalf("as_of = %v, want the branch's last sync %v", env.AsOf, fresh)
	}
}

func TestCatalogProductDetail_DecoratesAvailabilityWithBranchHealth(t *testing.T) {
	fresh := time.Now().UTC().Add(-2 * time.Minute)
	stale := time.Now().UTC().Add(-2 * time.Hour)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/products/p1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p1","code":100,"name":"كولا","kind":1,"is_active":true,"re_order":0,"is_expire":false,` +
			`"created_at":"2026-01-01T00:00:00Z","units":[{"id":"u1","name":"علبة","val_sub":1,"level":0,"buy":3,"sale":5,"prices":[5,5,5,5,5,5,5,5,5],"barcodes":["123"]}],` +
			`"availability":[` +
			`{"branch_id":"b1","warehouse_id":"w1","warehouse_name":"المخزن الرئيسي","total_qty":10,"unit_cost":2.5,"updated_at":"2026-07-14T00:00:00Z"},` +
			`{"branch_id":"b2","warehouse_id":"w2","warehouse_name":"مخزن الفرع","total_qty":4,"unit_cost":2.5,"updated_at":null}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{
		{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh},
		{ID: "b2", TenantID: "tnt_1", Name: "المعادي", Status: model.BranchActive, LastSyncAt: &stale},
	}
	s := New(st, &fakeTokens{}, nil)

	env, err := s.CatalogProductDetail(context.Background(), "acc_owner", "tnt_1", "p1")
	if err != nil {
		t.Fatalf("catalog product detail: %v", err)
	}
	if env.Source != "synced" || env.Data.Name != "كولا" || len(env.Data.Units) != 1 {
		t.Fatalf("product detail wrong: %+v", env)
	}
	if len(env.Data.Availability) != 2 {
		t.Fatalf("got %d availability rows, want 2", len(env.Data.Availability))
	}
	a1, a2 := env.Data.Availability[0], env.Data.Availability[1]
	if a1.BranchName != "وسط البلد" || a1.Health != "ok" {
		t.Fatalf("fresh branch availability wrong: %+v", a1)
	}
	if a2.BranchName != "المعادي" || a2.Health != "stale" {
		t.Fatalf("stale branch availability wrong: %+v", a2)
	}
}

func TestCatalogProductDetail_NotFound(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	if _, err := s.CatalogProductDetail(context.Background(), "acc_owner", "tnt_1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestChangeProductPrices_ForwardsChangesAndReturnsWrittenAt(t *testing.T) {
	var gotMethod string
	var gotBody struct {
		Changes []PriceChange `json:"changes"`
	}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.URL.Path != "/hq/products/p1/prices" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"written_at":"2026-07-14T12:00:00Z"}`))
	}))
	defer gw.Close()

	sale := 12.5
	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	result, err := s.ChangeProductPrices(context.Background(), "acc_owner", "tnt_1", "p1",
		[]PriceChange{{UnitID: "u1", Sale: &sale}})
	if err != nil {
		t.Fatalf("change product prices: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT, gateway saw %s", gotMethod)
	}
	if len(gotBody.Changes) != 1 || gotBody.Changes[0].UnitID != "u1" || *gotBody.Changes[0].Sale != 12.5 {
		t.Fatalf("gateway did not receive the forwarded change: %+v", gotBody)
	}
	want := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	if !result.WrittenAt.Equal(want) {
		t.Fatalf("written_at = %v, want %v", result.WrittenAt, want)
	}

	if _, err := s.ChangeProductPrices(context.Background(), "acc_intruder", "tnt_1", "p1",
		[]PriceChange{{UnitID: "u1", Sale: &sale}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestChangeProductPrices_InvalidUnits(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer gw.Close()

	sale := 5.0
	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.ChangeProductPrices(context.Background(), "acc_owner", "tnt_1", "p1",
		[]PriceChange{{UnitID: "not-mine", Sale: &sale}})
	if !errors.Is(err, ErrInvalidUnits) {
		t.Fatalf("expected ErrInvalidUnits, got %v", err)
	}
}

func TestChangeProductPrices_ProductNotFound(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gw.Close()

	sale := 5.0
	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.ChangeProductPrices(context.Background(), "acc_owner", "tnt_1", "missing",
		[]PriceChange{{UnitID: "u1", Sale: &sale}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateProduct_ForwardsAndReturnsResult(t *testing.T) {
	var gotMethod string
	var gotBody NewProduct
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.URL.Path != "/hq/products" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"p1","code":101,"written_at":"2026-07-14T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	input := NewProduct{
		Name: "كولا", Kind: 0,
		Units: []NewProductUnit{{Name: "قطعة", ValSub: 1, Buy: 3, Sale: 5, Barcodes: []string{"123"}}},
	}
	result, err := s.CreateProduct(context.Background(), "acc_owner", "tnt_1", input)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, gateway saw %s", gotMethod)
	}
	if gotBody.Name != "كولا" || len(gotBody.Units) != 1 || gotBody.Units[0].Barcodes[0] != "123" {
		t.Fatalf("gateway did not receive the forwarded product: %+v", gotBody)
	}
	if result.ID != "p1" || result.Code != 101 {
		t.Fatalf("create product result wrong: %+v", result)
	}
	want := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	if !result.WrittenAt.Equal(want) {
		t.Fatalf("written_at = %v, want %v", result.WrittenAt, want)
	}

	if _, err := s.CreateProduct(context.Background(), "acc_intruder", "tnt_1", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCreateProduct_InvalidGroup(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateProduct(context.Background(), "acc_owner", "tnt_1", NewProduct{
		Name: "كولا", Units: []NewProductUnit{{Name: "قطعة", ValSub: 1}},
	})
	if !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("expected ErrInvalidGroup, got %v", err)
	}
}

func TestCreateProduct_DuplicateBarcode(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"duplicate barcode","barcode":"123"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateProduct(context.Background(), "acc_owner", "tnt_1", NewProduct{
		Name: "كولا", Units: []NewProductUnit{{Name: "قطعة", ValSub: 1, Barcodes: []string{"123"}}},
	})
	var dup *DuplicateBarcodeError
	if !errors.As(err, &dup) || dup.Barcode != "123" {
		t.Fatalf("expected DuplicateBarcodeError{123}, got %v", err)
	}
}

func TestInventoryByBranch_MergesRegistryAndSumsTotals(t *testing.T) {
	fresh := time.Now().UTC().Add(-2 * time.Minute)
	stale := time.Now().UTC().Add(-2 * time.Hour)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/inventory/branch-summary" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"branches":[` +
			`{"branch_id":"11111111-1111-1111-1111-111111111111","sku_count":10,"stock_value":500,` +
			`"negative_count":1,"out_count":2,"low_count":3,"warehouses":[]}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{
		{ID: "11111111-1111-1111-1111-111111111111", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh},
		{ID: "22222222-2222-2222-2222-222222222222", TenantID: "tnt_1", Name: "المعادي", Status: model.BranchActive, LastSyncAt: &stale},
	}
	s := New(st, &fakeTokens{}, nil)

	env, err := s.InventoryByBranch(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("inventory by branch: %v", err)
	}
	if len(env.Data.Branches) != 2 {
		t.Fatalf("got %d branches, want 2", len(env.Data.Branches))
	}
	b1, b2 := env.Data.Branches[0], env.Data.Branches[1]
	if b1.SkuCount != 10 || b1.StockValue != 500 || b1.Health != "ok" {
		t.Fatalf("gateway-reported branch wrong: %+v", b1)
	}
	if b2.SkuCount != 0 || b2.StockValue != 0 || b2.Health != "stale" || b2.Warehouses == nil {
		t.Fatalf("missing-from-gateway branch should zero out (not nil warehouses): %+v", b2)
	}
	tot := env.Data.Totals
	if tot.StockValue != 500 || tot.NegativeCount != 1 || tot.OutCount != 2 || tot.LowCount != 3 {
		t.Fatalf("totals wrong: %+v", tot)
	}

	if _, err := s.InventoryByBranch(context.Background(), "acc_intruder", "tnt_1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestInventoryProducts_PassesQueryParams(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"items":[` +
			`{"id":"p1","code":100,"name":"كولا","is_active":true,"unit":"علبة","re_order":10,` +
			`"total_qty":-3,"stock_value":-45,"branches_with_stock":0,"status":"negative"}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	params := url.Values{"status": {"negative"}, "branch_id": {"b1"}}
	env, err := s.InventoryProducts(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("inventory products: %v", err)
	}
	if gotQuery.Get("status") != "negative" || gotQuery.Get("branch_id") != "b1" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Source != "synced" || len(env.Data.Items) != 1 || env.Data.Items[0].Status != "negative" {
		t.Fatalf("inventory products envelope wrong: %+v", env)
	}
	if env.AsOf == nil || !env.AsOf.Equal(fresh) {
		t.Fatalf("as_of = %v, want the branch's last sync %v", env.AsOf, fresh)
	}
}

func TestInventoryAttention_MergesStaleBranchesAndDecoratesItems(t *testing.T) {
	fresh := time.Now().UTC().Add(-2 * time.Minute)
	stale := time.Now().UTC().Add(-2 * time.Hour)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/inventory/attention" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"counts":{"negative":1,"out":0,"low":0},"items":[` +
			`{"status":"negative","product_id":"p1","product_code":100,"product_name":"كولا","re_order":10,` +
			`"branch_id":"11111111-1111-1111-1111-111111111111","warehouse_id":"w1","warehouse_name":"الرئيسي","total_qty":-3,"unit_cost":2.5}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{
		{ID: "11111111-1111-1111-1111-111111111111", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh},
		{ID: "22222222-2222-2222-2222-222222222222", TenantID: "tnt_1", Name: "المعادي", Status: model.BranchActive, LastSyncAt: &stale},
		{ID: "33333333-3333-3333-3333-333333333333", TenantID: "tnt_1", Name: "مدينة نصر", Status: model.BranchActive}, // never synced
	}
	s := New(st, &fakeTokens{}, nil)

	env, err := s.InventoryAttention(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("inventory attention: %v", err)
	}
	if len(env.Data.Items) != 1 || env.Data.Items[0].BranchName != "وسط البلد" || env.Data.Items[0].Health != "ok" {
		t.Fatalf("item decoration wrong: %+v", env.Data.Items)
	}
	if len(env.Data.StaleBranches) != 1 || env.Data.StaleBranches[0].BranchID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected exactly the stale branch (never-synced excluded), got %+v", env.Data.StaleBranches)
	}
	if env.Data.Counts.Negative != 1 {
		t.Fatalf("counts wrong: %+v", env.Data.Counts)
	}

	t.Run("branch_id scopes the stale merge too", func(t *testing.T) {
		env, err := s.InventoryAttention(context.Background(), "acc_owner", "tnt_1",
			url.Values{"branch_id": {"33333333-3333-3333-3333-333333333333"}})
		if err != nil {
			t.Fatalf("inventory attention: %v", err)
		}
		if len(env.Data.StaleBranches) != 0 {
			t.Fatalf("never-synced branch should never appear in stale_branches, got %+v", env.Data.StaleBranches)
		}
	})
}

func TestProductMovements_PassesParamsAndDecoratesBranchName(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.URL.Path != "/hq/products/p1/movements" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"opening_qty":120,"total":1,"page":1,"page_size":50,"items":[` +
			`{"id":"m1","issue_date":"2026-07-01T10:30:00Z","dealing":100,"branch_id":"b1",` +
			`"warehouse_id":"w1","warehouse_name":"الرئيسي","in_qty":0,"in_price":0,"out_qty":2,` +
			`"out_price":15,"cost":10,"unit":"قطعة","reg_num":"r1","running_qty":118}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive}}
	s := New(st, &fakeTokens{}, nil)

	params := url.Values{"from": {"2026-06-01"}, "to": {"2026-07-01"}}
	env, err := s.ProductMovements(context.Background(), "acc_owner", "tnt_1", "p1", params)
	if err != nil {
		t.Fatalf("product movements: %v", err)
	}
	if gotQuery.Get("from") != "2026-06-01" || gotQuery.Get("to") != "2026-07-01" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Data.OpeningQty != 120 || len(env.Data.Items) != 1 || env.Data.Items[0].BranchName != "وسط البلد" || env.Data.Items[0].RunningQty != 118 {
		t.Fatalf("movements envelope wrong: %+v", env.Data)
	}
}

func TestProductMovements_NotFound(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	if _, err := s.ProductMovements(context.Background(), "acc_owner", "tnt_1", "missing", url.Values{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateProduct_TenantNotProvisioned(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateProduct(context.Background(), "acc_owner", "tnt_1", NewProduct{
		Name: "كولا", Units: []NewProductUnit{{Name: "قطعة", ValSub: 1}},
	})
	if !errors.Is(err, ErrTenantNotProvisioned) {
		t.Fatalf("expected ErrTenantNotProvisioned, got %v", err)
	}
}

func TestConflicts_PassesParamsAndDecoratesBranchNames(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.URL.Path != "/hq/conflicts" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unacked":2,"total":3,"page":1,"page_size":50,"items":[` +
			`{"id":9,"occurred_at":"2026-07-15T09:00:00Z","branch_id":"b1","table_name":"UnitOfMeasures",` +
			`"row_pk":"u1","conflict_type":"RemoteExistsLocalExists","resolution":"ServerWins",` +
			`"local_row":"{\"Sale\":12}","remote_row":"{\"Sale\":10}",` +
			`"product_id":"p1","product_name":"كولا"},` +
			`{"id":8,"occurred_at":"2026-07-15T08:00:00Z","branch_id":"b-unknown","table_name":"Customers",` +
			`"conflict_type":"RemoteExistsLocalExists","resolution":"ServerWins"}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)

	params := url.Values{"all": {"1"}, "page": {"1"}}
	env, err := s.Conflicts(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if gotQuery.Get("all") != "1" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Source != "synced" || env.AsOf == nil || !env.AsOf.Equal(fresh) {
		t.Fatalf("envelope not freshness-shaped: %+v", env)
	}
	d := env.Data
	if d.Unacked != 2 || d.Total != 3 || len(d.Items) != 2 {
		t.Fatalf("conflicts page wrong: %+v", d)
	}
	if d.Items[0].BranchName != "وسط البلد" || d.Items[0].ProductID == nil || *d.Items[0].ProductID != "p1" {
		t.Fatalf("known-branch row not decorated: %+v", d.Items[0])
	}
	if d.Items[1].BranchName != "" || d.Items[1].ProductID != nil {
		t.Fatalf("unknown-branch row should stay undecorated: %+v", d.Items[1])
	}
}

func TestConflicts_EmptyItemsNeverNil(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unacked":0,"total":0,"page":1,"page_size":50,"items":[]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.Conflicts(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if env.Data.Items == nil || len(env.Data.Items) != 0 {
		t.Fatalf("items should be an empty slice, got %#v", env.Data.Items)
	}
}

func TestAckConflicts_ForwardsBodyAndReturnsCount(t *testing.T) {
	var gotBody map[string]any
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/conflicts/ack" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"acked":3}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	up := int64(9)
	result, err := s.AckConflicts(context.Background(), "acc_owner", "tnt_1", []int64{4, 5}, &up)
	if err != nil {
		t.Fatalf("ack conflicts: %v", err)
	}
	if result.Acked != 3 {
		t.Fatalf("acked = %d, want 3", result.Acked)
	}
	ids, _ := gotBody["ids"].([]any)
	if len(ids) != 2 || gotBody["up_to_id"] != float64(9) {
		t.Fatalf("gateway saw body %v", gotBody)
	}
}

func TestAckConflicts_Ownership(t *testing.T) {
	s := New(testStore("http://127.0.0.1:1"), &fakeTokens{}, nil)
	if _, err := s.AckConflicts(context.Background(), "acc_intruder", "tnt_1", []int64{1}, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestReportSales_PassesParamsAndWrapsEnvelope(t *testing.T) {
	var gotPath, gotQuery string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"from":"2026-07-09","to":"2026-07-15",` +
			`"sales_total":1500,"sales_count":12,"refunds_total":100,"refunds_count":1,` +
			`"tender":{"cash":900,"bank":400,"wallet":100,"credit":100},` +
			`"days":[{"day":"2026-07-09","sales_total":1500,"sales_count":12,"refunds_total":100}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	params := url.Values{}
	params.Set("from", "2026-07-09")
	params.Set("to", "2026-07-15")
	params.Set("branch_id", "b-1")
	env, err := s.ReportSales(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("report sales: %v", err)
	}
	if gotPath != "/hq/reports/sales" {
		t.Fatalf("gateway saw path %q", gotPath)
	}
	q, _ := url.ParseQuery(gotQuery)
	if q.Get("from") != "2026-07-09" || q.Get("to") != "2026-07-15" || q.Get("branch_id") != "b-1" {
		t.Fatalf("gateway saw query %q", gotQuery)
	}
	if env.Source != "synced" || env.AsOf == nil || !env.AsOf.Equal(fresh) {
		t.Fatalf("envelope: source=%q as_of=%v, want as_of = last sync %v", env.Source, env.AsOf, fresh)
	}
	if env.Data.SalesTotal != 1500 || env.Data.Tender.Bank != 400 || len(env.Data.Days) != 1 {
		t.Fatalf("data round-trip: %+v", env.Data)
	}
}

func TestReportSales_EmptyDaysNeverNil(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"from":"2026-07-15","to":"2026-07-15","sales_total":0,"sales_count":0,` +
			`"refunds_total":0,"refunds_count":0,"tender":{"cash":0,"bank":0,"wallet":0,"credit":0},"days":null}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.ReportSales(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("report sales: %v", err)
	}
	if env.Data.Days == nil || len(env.Data.Days) != 0 {
		t.Fatalf("days should be an empty slice, got %#v", env.Data.Days)
	}
}

func TestReportProducts_PassesParamsAndEmptyItemsNeverNil(t *testing.T) {
	var gotQuery string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/reports/products" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":0,"page":2,"page_size":25,"items":null}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	params := url.Values{}
	params.Set("sort", "profit")
	params.Set("page", "2")
	params.Set("page_size", "25")
	params.Set("group_id", "g-1")
	env, err := s.ReportProducts(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("report products: %v", err)
	}
	q, _ := url.ParseQuery(gotQuery)
	if q.Get("sort") != "profit" || q.Get("page") != "2" || q.Get("page_size") != "25" || q.Get("group_id") != "g-1" {
		t.Fatalf("gateway saw query %q", gotQuery)
	}
	if env.Data.Items == nil || len(env.Data.Items) != 0 {
		t.Fatalf("items should be an empty slice, got %#v", env.Data.Items)
	}
	if env.Data.Page != 2 || env.Data.PageSize != 25 {
		t.Fatalf("paging round-trip: %+v", env.Data)
	}
}

func TestReportBranches_MergesRegistryAndZeroFills(t *testing.T) {
	recent := time.Now().UTC().Add(-2 * time.Minute)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/reports/branches" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Reports rows for br_1 and an id the registry doesn't know (dropped).
		_, _ = w.Write([]byte(`{"branches":[` +
			`{"branch_id":"br_1","sales_total":900,"sales_count":9,"refunds_total":50,"refunds_count":1,"profit":300},` +
			`{"branch_id":"br_ghost","sales_total":1,"sales_count":1,"refunds_total":0,"refunds_count":0,"profit":1}]}`))
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{
		{ID: "br_1", TenantID: "tnt_1", Name: "الفرع الأول", LastSyncAt: &recent},
		{ID: "br_2", TenantID: "tnt_1", Name: "الفرع الثاني"}, // never synced, no report rows
	}
	s := New(st, &fakeTokens{}, nil)

	env, err := s.ReportBranches(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("report branches: %v", err)
	}
	if len(env.Data.Branches) != 2 {
		t.Fatalf("got %d branches, want 2 (registry-driven)", len(env.Data.Branches))
	}
	b1, b2 := env.Data.Branches[0], env.Data.Branches[1]
	if b1.BranchID != "br_1" || b1.BranchName != "الفرع الأول" || b1.Health != "ok" {
		t.Fatalf("br_1 decoration: %+v", b1)
	}
	if b1.SalesTotal != 900 || b1.Profit != 300 || b1.RefundsCount != 1 {
		t.Fatalf("br_1 numbers: %+v", b1)
	}
	if b2.BranchID != "br_2" || b2.Health != "never" {
		t.Fatalf("br_2 decoration: %+v", b2)
	}
	if b2.SalesTotal != 0 || b2.SalesCount != 0 || b2.Profit != 0 {
		t.Fatalf("br_2 should be zero-filled: %+v", b2)
	}
}

func TestReportStaff_PassthroughAndEmptyNeverNil(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/reports/staff" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"staff":[{"user_id":"u-1","user_name":"أحمد","sales_total":700,"sales_count":7,"refunds_total":0,"refunds_count":0}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.ReportStaff(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("report staff: %v", err)
	}
	if len(env.Data.Staff) != 1 || env.Data.Staff[0].UserName != "أحمد" || env.Data.Staff[0].SalesTotal != 700 {
		t.Fatalf("staff round-trip: %+v", env.Data.Staff)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"staff":null}`))
	}))
	defer empty.Close()
	s2 := New(testStore(empty.URL), &fakeTokens{}, nil)
	env2, err := s2.ReportStaff(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("report staff (empty): %v", err)
	}
	if env2.Data.Staff == nil || len(env2.Data.Staff) != 0 {
		t.Fatalf("staff should be an empty slice, got %#v", env2.Data.Staff)
	}
}

func TestReportSales_Ownership(t *testing.T) {
	s := New(testStore("http://127.0.0.1:1"), &fakeTokens{}, nil)
	if _, err := s.ReportSales(context.Background(), "acc_intruder", "tnt_1", url.Values{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

// --- Customers (slice 7) ---

func TestCustomerGroups(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/customer-groups" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groups":[{"id":"g1","parent_id":"00000000-0000-0000-0000-000000000000","name":"جملة","is_active":true,"num":1}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	env, err := s.CustomerGroups(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("customer groups: %v", err)
	}
	if env.Source != "synced" || len(env.Data) != 1 || env.Data[0].Name != "جملة" {
		t.Fatalf("customer groups envelope wrong: %+v", env)
	}

	if _, err := s.CustomerGroups(context.Background(), "acc_intruder", "tnt_1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCustomers_PassesParamsAndDecoratesBranch(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"items":[` +
			`{"id":"c1","num":1,"name":"محمد","branch_id":"b1","phone1":"0100","is_active":true,"balance":150.5,"credit_limit":500,"is_credit":false}]}`))
	}))
	defer gw.Close()

	fresh := time.Now().UTC().Add(-3 * time.Minute)
	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)
	params := url.Values{"search": {"محمد"}, "debt": {"has_debt"}}
	env, err := s.Customers(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("customers: %v", err)
	}
	if gotQuery.Get("search") != "محمد" || gotQuery.Get("debt") != "has_debt" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Source != "synced" || env.Data.Total != 1 || len(env.Data.Items) != 1 {
		t.Fatalf("customers envelope wrong: %+v", env)
	}
	row := env.Data.Items[0]
	if row.Balance != 150.5 || row.BranchName != "وسط البلد" || row.Health != "ok" {
		t.Fatalf("customer row not decorated with branch name/health: %+v", row)
	}

	t.Run("row for an unregistered branch degrades to never/empty name", func(t *testing.T) {
		gw2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"items":[` +
				`{"id":"c2","num":2,"name":"سارة","branch_id":"ghost","phone1":"0101","is_active":true,"balance":0,"credit_limit":0,"is_credit":false}]}`))
		}))
		defer gw2.Close()
		st2 := testStore(gw2.URL)
		st2.branches = st.branches
		s2 := New(st2, &fakeTokens{}, nil)
		env2, err := s2.Customers(context.Background(), "acc_owner", "tnt_1", url.Values{})
		if err != nil {
			t.Fatalf("customers: %v", err)
		}
		if env2.Data.Items[0].Health != "never" || env2.Data.Items[0].BranchName != "" {
			t.Fatalf("ghost-branch row should degrade cleanly: %+v", env2.Data.Items[0])
		}
	})
}

func TestCustomerDetail_DecoratesBranchAndNotFound(t *testing.T) {
	fresh := time.Now().UTC().Add(-2 * time.Minute)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hq/customers/c1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c1","num":1,"name":"محمد","branch_id":"b1","phone1":"0100",` +
				`"credit_limit":500,"is_credit":false,"is_active":true,"balance":150.5,` +
				`"stats":{"number_of_orders":3,"total_spent":900,"average_order_value":300,"last_purchase_date":"2026-07-01T00:00:00Z"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gw.Close()

	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive, LastSyncAt: &fresh}}
	s := New(st, &fakeTokens{}, nil)

	env, err := s.CustomerDetail(context.Background(), "acc_owner", "tnt_1", "c1")
	if err != nil {
		t.Fatalf("customer detail: %v", err)
	}
	if env.Data.BranchName != "وسط البلد" || env.Data.Health != "ok" || env.Data.Stats.NumberOfOrders != 3 || env.Data.Stats.TotalSpent != 900 {
		t.Fatalf("customer detail wrong: %+v", env.Data)
	}

	if _, err := s.CustomerDetail(context.Background(), "acc_owner", "tnt_1", "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCustomerPurchases_ParamsAndNotFound(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hq/customers/c1/purchases":
			gotQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"items":[` +
				`{"id":"bl1","num":"S-1","issued_at":"2026-07-01T00:00:00Z","total":300,"item_count":2,"is_paid":true,"type":100}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.CustomerPurchases(context.Background(), "acc_owner", "tnt_1", "c1", url.Values{"page": {"1"}})
	if err != nil {
		t.Fatalf("customer purchases: %v", err)
	}
	if gotQuery.Get("page") != "1" || len(env.Data.Items) != 1 || env.Data.Items[0].Total != 300 {
		t.Fatalf("customer purchases wrong: %+v", env.Data)
	}

	if _, err := s.CustomerPurchases(context.Background(), "acc_owner", "tnt_1", "ghost", url.Values{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCustomerLedger_RunningBalancePassesThroughAndNotFound(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hq/customers/c1/ledger":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total":2,"page":1,"page_size":50,"items":[` +
				`{"id":"t1","created_at":"2026-07-01T00:00:00Z","dealing":100,"total":300,"debit":300,"credit":0,"running_balance":300,"user_id":"u1"},` +
				`{"id":"t2","created_at":"2026-07-02T00:00:00Z","dealing":400,"total":100,"debit":0,"credit":100,"running_balance":200,"user_id":"u1"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.CustomerLedger(context.Background(), "acc_owner", "tnt_1", "c1", url.Values{})
	if err != nil {
		t.Fatalf("customer ledger: %v", err)
	}
	if len(env.Data.Items) != 2 || env.Data.Items[0].RunningBalance != 300 || env.Data.Items[1].RunningBalance != 200 {
		t.Fatalf("ledger running balance not passed through verbatim: %+v", env.Data.Items)
	}

	if _, err := s.CustomerLedger(context.Background(), "acc_owner", "tnt_1", "ghost", url.Values{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCustomerInsights_EnvelopeAndEmptyDegrade(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hq/customers/insights" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"top_customers":[{"id":"c1","num":1,"name":"محمد","branch_id":"b1","amount":900}],` +
			`"new_this_month":{"count":2,"items":[{"id":"c2","num":2,"name":"سارة","branch_id":"b1"}]},` +
			`"inactive":{"count":0,"items":[]},` +
			`"credit_limit_warnings":[{"id":"c3","num":3,"name":"علي","branch_id":"b1","balance":480,"credit_limit":500,"level":"approaching"}],` +
			`"highest_spenders":[{"id":"c1","num":1,"name":"محمد","branch_id":"b1","amount":900}],` +
			`"growth_over_time":[{"day":"2026-07-01","new_customers":1}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.CustomerInsights(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("customer insights: %v", err)
	}
	if len(env.Data.TopCustomers) != 1 || env.Data.NewThisMonth.Count != 2 || len(env.Data.CreditLimitWarnings) != 1 ||
		env.Data.CreditLimitWarnings[0].Level != "approaching" || len(env.Data.GrowthOverTime) != 1 {
		t.Fatalf("customer insights envelope wrong: %+v", env.Data)
	}

	t.Run("empty/never-synced tenant degrades to empty slices, not nil", func(t *testing.T) {
		empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"top_customers":null,"new_this_month":{"count":0,"items":null},"inactive":{"count":0,"items":null},` +
				`"credit_limit_warnings":null,"highest_spenders":null,"growth_over_time":null}`))
		}))
		defer empty.Close()
		s2 := New(testStore(empty.URL), &fakeTokens{}, nil)
		env2, err := s2.CustomerInsights(context.Background(), "acc_owner", "tnt_1", url.Values{})
		if err != nil {
			t.Fatalf("customer insights (empty): %v", err)
		}
		if env2.Data.TopCustomers == nil || env2.Data.NewThisMonth.Items == nil || env2.Data.Inactive.Items == nil ||
			env2.Data.CreditLimitWarnings == nil || env2.Data.HighestSpenders == nil || env2.Data.GrowthOverTime == nil {
			t.Fatalf("customer insights should degrade to empty slices, got %+v", env2.Data)
		}
	})
}

func TestCreateCustomer_ForwardsAndReturnsResult(t *testing.T) {
	var gotMethod string
	var gotBody NewCustomer
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.URL.Path != "/hq/customers" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","num":42,"written_at":"2026-07-16T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	input := NewCustomer{Name: "محمد", Phone1: "0100", BranchID: "b1"}
	result, err := s.CreateCustomer(context.Background(), "acc_owner", "tnt_1", input)
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, gateway saw %s", gotMethod)
	}
	if gotBody.Name != "محمد" || gotBody.BranchID != "b1" {
		t.Fatalf("gateway did not receive the forwarded customer: %+v", gotBody)
	}
	if result.ID != "c1" || result.Num != 42 {
		t.Fatalf("create customer result wrong: %+v", result)
	}

	if _, err := s.CreateCustomer(context.Background(), "acc_intruder", "tnt_1", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCreateCustomer_InvalidInputForwardsGatewayMessage(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"branch not found"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateCustomer(context.Background(), "acc_owner", "tnt_1", NewCustomer{Name: "محمد", Phone1: "0100", BranchID: "ghost"})
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) || badInput.Error() != "branch not found" {
		t.Fatalf("expected InvalidCustomerInputError(\"branch not found\"), got %v", err)
	}
}

func TestCreateCustomer_MissingAccountOperand(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateCustomer(context.Background(), "acc_owner", "tnt_1", NewCustomer{Name: "محمد", Phone1: "0100", BranchID: "b1"})
	if !errors.Is(err, ErrMissingAccountOperand) {
		t.Fatalf("expected ErrMissingAccountOperand, got %v", err)
	}
}

func TestUpdateCustomer_ForwardsPartialBodyAndNotFound(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody CustomerEdit
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.URL.Path != "/hq/customers/c1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"written_at":"2026-07-16T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	active := false
	result, err := s.UpdateCustomer(context.Background(), "acc_owner", "tnt_1", "c1", CustomerEdit{IsActive: &active})
	if err != nil {
		t.Fatalf("update customer: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/hq/customers/c1" {
		t.Fatalf("expected PUT /hq/customers/c1, gateway saw %s %s", gotMethod, gotPath)
	}
	if gotBody.Name != nil || gotBody.IsActive == nil || *gotBody.IsActive != false {
		t.Fatalf("gateway did not receive the partial body verbatim: %+v", gotBody)
	}
	want := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if !result.WrittenAt.Equal(want) {
		t.Fatalf("written_at = %v, want %v", result.WrittenAt, want)
	}

	if _, err := s.UpdateCustomer(context.Background(), "acc_owner", "tnt_1", "ghost", CustomerEdit{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateCustomer_InvalidInputForwardsGatewayMessage(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"invalid field value"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	neg := -5.0
	_, err := s.UpdateCustomer(context.Background(), "acc_owner", "tnt_1", "c1", CustomerEdit{CreditLimit: &neg})
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) || badInput.Error() != "invalid field value" {
		t.Fatalf("expected InvalidCustomerInputError(\"invalid field value\"), got %v", err)
	}
}

func TestBulkUpdateCustomers_ForwardsBodyAndInvalidInput(t *testing.T) {
	var gotMethod string
	var gotBody map[string]any
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.URL.Path != "/hq/customers/bulk" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"updated":2,"written_at":"2026-07-16T12:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	group := "g1"
	result, err := s.BulkUpdateCustomers(context.Background(), "acc_owner", "tnt_1", []string{"c1", "c2"}, &group, nil)
	if err != nil {
		t.Fatalf("bulk update customers: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT, gateway saw %s", gotMethod)
	}
	if gotBody["group_id"] != "g1" {
		t.Fatalf("gateway did not receive group_id: %+v", gotBody)
	}
	if result.Updated != 2 {
		t.Fatalf("bulk update result wrong: %+v", result)
	}

	badGW := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"one or more ids do not belong to this tenant, or group_id not found"}`))
	}))
	defer badGW.Close()
	s2 := New(testStore(badGW.URL), &fakeTokens{}, nil)
	_, err = s2.BulkUpdateCustomers(context.Background(), "acc_owner", "tnt_1", []string{"ghost"}, &group, nil)
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) {
		t.Fatalf("expected InvalidCustomerInputError, got %v", err)
	}
}

func TestExportCustomers_StreamsCsvWithHeaders(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.URL.Path != "/hq/customers/export" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		_, _ = w.Write([]byte("code,name\n1,\xd9\x85\xd8\xad\xd9\x85\xd8\xaf\n"))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	rec := httptest.NewRecorder()
	err := s.ExportCustomers(context.Background(), "acc_owner", "tnt_1", url.Values{"debt": {"has_debt"}}, rec)
	if err != nil {
		t.Fatalf("export customers: %v", err)
	}
	if gotQuery.Get("debt") != "has_debt" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/csv", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Fatalf("expected a Content-Disposition header, got none")
	}
	if !strings.Contains(rec.Body.String(), "code,name") {
		t.Fatalf("csv body not streamed through: %q", rec.Body.String())
	}
}

func TestExportCustomers_GatewayErrorWritesNothing(t *testing.T) {
	s := New(testStore("http://127.0.0.1:1"), &fakeTokens{}, &http.Client{Timeout: 300 * time.Millisecond})
	rec := httptest.NewRecorder()
	err := s.ExportCustomers(context.Background(), "acc_owner", "tnt_1", url.Values{}, rec)
	if !errors.Is(err, ErrGatewayUnreachable) {
		t.Fatalf("expected ErrGatewayUnreachable, got %v", err)
	}
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("expected nothing written to the recorder on a setup failure, got code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestImportCustomers_ForwardsBodyAndDecodesResult(t *testing.T) {
	var gotContentType, gotBody string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"errors":[{"row":3,"message":"branch not found"}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	result, err := s.ImportCustomers(context.Background(), "acc_owner", "tnt_1",
		`multipart/form-data; boundary=X`, strings.NewReader("--X--"))
	if err != nil {
		t.Fatalf("import customers: %v", err)
	}
	if gotContentType != "multipart/form-data; boundary=X" || !strings.Contains(gotBody, "--X--") {
		t.Fatalf("gateway did not see the forwarded multipart body: ct=%q body=%q", gotContentType, gotBody)
	}
	if result.Created != 1 || len(result.Errors) != 1 || result.Errors[0].Message != "branch not found" {
		t.Fatalf("import result wrong: %+v", result)
	}
}
