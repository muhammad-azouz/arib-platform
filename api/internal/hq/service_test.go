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

// fakeStore serves one tenant and one shard from memory.
type fakeStore struct {
	tenant model.Tenant
	shard  model.Shard
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
