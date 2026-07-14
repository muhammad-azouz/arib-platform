package hq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

	got, err := s.Branches(context.Background(), "acc_owner", "tnt_1")
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
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
		got, err := s2.Branches(context.Background(), "acc_owner", "tnt_1")
		if err != nil {
			t.Fatalf("branches with dead gateway: %v", err)
		}
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
		got, err := s3.Branches(context.Background(), "acc_owner", "tnt_1")
		if err != nil {
			t.Fatalf("branches without subscription: %v", err)
		}
		if len(got) != 2 || got[0].Snapshot.Source != "offline" {
			t.Fatalf("expected offline snapshots, got %+v", got[0])
		}
	})
}
