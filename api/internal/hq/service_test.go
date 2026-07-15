package hq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.CatalogGroups(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("catalog groups: %v", err)
	}
	if env.Source != "synced" || len(env.Data) != 1 || env.Data[0].Name != "مشروبات" || env.Data[0].ProductCount != 5 {
		t.Fatalf("catalog groups envelope wrong: %+v", env)
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

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
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

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
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

	st := testStore(gw.URL)
	st.branches = []model.Branch{{ID: "b1", TenantID: "tnt_1", Name: "وسط البلد", Status: model.BranchActive}}
	s := New(st, &fakeTokens{}, nil)

	params := url.Values{"all": {"1"}, "page": {"1"}}
	env, err := s.Conflicts(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if gotQuery.Get("all") != "1" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Source != "synced" || env.AsOf.IsZero() {
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
