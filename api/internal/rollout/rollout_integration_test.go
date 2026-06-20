package rollout

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/tenant"
)

// TestE3FleetRollout drives the real fleet-rollout flow against a live local
// gateway (roadmap E3 verify: "simulated 3-tenant rollout with one induced
// failure recovers cleanly"). It uses an in-memory registry (the Store
// interface) so the rollout/retry/report logic is exercised end-to-end without
// host access to Mongo; the gateway it calls is real, so SQL provisioning,
// version stamping and ops-token verification are all genuine.
//
// Requires a gateway started with the public key matching E3_SYNC_KEY:
//
//	E3_GATEWAY_URL=http://127.0.0.1:5310 \
//	E3_SYNC_KEY=/tmp/arib-d3-key.pem \
//	go test ./internal/rollout -run TestE3FleetRollout -v
func TestE3FleetRollout(t *testing.T) {
	gateway := os.Getenv("E3_GATEWAY_URL")
	keyPath := os.Getenv("E3_SYNC_KEY")
	if gateway == "" || keyPath == "" {
		t.Skip("E3 integration test needs E3_GATEWAY_URL and E3_SYNC_KEY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	const tA, tB, tC = "tnt_E3SMOKEA", "tnt_E3SMOKEB", "tnt_E3SMOKEC"
	store := newFakeStore(gateway,
		syncTenant(tA, "arib_e3smokea"),
		syncTenant(tB, "arib_e3smokeb"),
		syncTenant(tC, "arib_e3smokec"))

	// Real signer → real ops tokens the gateway verifies with its public key.
	signer := tenant.New(nil, parseRSAKey(t, keyPath), time.Hour, nil)
	svc := New(store, signer, &http.Client{Timeout: 3 * time.Minute})

	// --- Phase 1: clean rollout brings all three DBs to the gateway version ---
	rep, err := svc.Rollout(ctx)
	if err != nil {
		t.Fatalf("phase 1 rollout: %v", err)
	}
	target := rep.Target
	if target < 1 {
		t.Fatalf("phase 1: gateway target version = %d, want >= 1", target)
	}
	if len(rep.Failed) != 0 {
		t.Fatalf("phase 1: unexpected failures %v", rep.Failed)
	}
	if rep.ByVersion[target] != 3 {
		t.Fatalf("phase 1: want 3 tenants at v%d, got by_version=%v", target, rep.ByVersion)
	}
	for _, id := range []string{tA, tB, tC} {
		if got := store.tenants[id]; got.SchemaVersion != target || got.RolloutStatus != model.RolloutIdle {
			t.Fatalf("phase 1: tenant %s = v%d/%s, want v%d/idle", id, got.SchemaVersion, got.RolloutStatus, target)
		}
	}
	t.Logf("phase 1 OK: 3 tenants at v%d", target)

	// --- Phase 2: induce one failure (a bad db name the gateway rejects) ---
	store.tenants[tC].DBName = "e3-bad-name" // hyphen → gateway 400 (invalid identifier)
	store.tenants[tC].SchemaVersion = 0      // mark behind so the rollout retries it
	store.tenants[tC].RolloutStatus = model.RolloutIdle

	rep, err = svc.Rollout(ctx)
	if err != nil {
		t.Fatalf("phase 2 rollout (the rollout itself must succeed): %v", err)
	}
	if len(rep.Failed) != 1 || rep.Failed[0] != tC {
		t.Fatalf("phase 2: want failed=[%s], got %v", tC, rep.Failed)
	}
	if c := store.tenants[tC]; c.RolloutStatus != model.RolloutFailed || c.RolloutError == "" {
		t.Fatalf("phase 2: tenant C = %s/%q, want failed with an error", c.RolloutStatus, c.RolloutError)
	}
	if rep.ByVersion[target] != 2 {
		t.Fatalf("phase 2: want 2 tenants still at v%d, got by_version=%v", target, rep.ByVersion)
	}
	t.Logf("phase 2 OK: mixed version (2 at v%d, 1 failed: %q)", target, store.tenants[tC].RolloutError)

	// --- Phase 3: fix the root cause and re-run; only the failed tenant retries ---
	store.tenants[tC].DBName = "arib_e3smokec"
	rep, err = svc.Rollout(ctx)
	if err != nil {
		t.Fatalf("phase 3 rollout: %v", err)
	}
	if len(rep.Failed) != 0 {
		t.Fatalf("phase 3: still failing %v", rep.Failed)
	}
	if rep.ByVersion[target] != 3 {
		t.Fatalf("phase 3: want all 3 at v%d, got by_version=%v", target, rep.ByVersion)
	}
	if c := store.tenants[tC]; c.SchemaVersion != target || c.RolloutStatus != model.RolloutIdle {
		t.Fatalf("phase 3: tenant C = v%d/%s, want v%d/idle", c.SchemaVersion, c.RolloutStatus, target)
	}
	t.Logf("phase 3 OK: recovered — all 3 tenants at v%d", target)
}

// --- in-memory registry implementing rollout.Store ---

type fakeStore struct {
	tenants map[string]*model.Tenant
	order   []string
	gateway string
}

func newFakeStore(gateway string, ts ...*model.Tenant) *fakeStore {
	f := &fakeStore{tenants: map[string]*model.Tenant{}, gateway: gateway}
	for _, t := range ts {
		f.tenants[t.ID] = t
		f.order = append(f.order, t.ID)
	}
	return f
}

func syncTenant(id, dbName string) *model.Tenant {
	return &model.Tenant{
		ID: id, Name: id, Status: model.TenantActive,
		DBName: dbName, ShardID: "shd_e3", RolloutStatus: model.RolloutIdle,
	}
}

func (f *fakeStore) TenantsWithSync(_ context.Context) ([]model.Tenant, error) {
	var out []model.Tenant
	for _, id := range f.order {
		if t := f.tenants[id]; t.DBName != "" {
			out = append(out, *t) // copies, like the Mongo store
		}
	}
	return out, nil
}

func (f *fakeStore) UpdateTenantSchema(_ context.Context, id string, version int, status model.RolloutStatus, errMsg string, attempts int, _ time.Time) error {
	t := f.tenants[id]
	t.SchemaVersion, t.RolloutStatus, t.RolloutError, t.RolloutAttempts = version, status, errMsg, attempts
	return nil
}

func (f *fakeStore) ListActiveShards(_ context.Context) ([]model.Shard, error) {
	return []model.Shard{{
		ID:         "shd_e3",
		GatewayURL: f.gateway,
		Status:     model.ShardActive,
	}}, nil
}

func parseRSAKey(t *testing.T, path string) *rsa.PrivateKey {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read key %s: %v", path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("no PEM block in %s", path)
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k
	}
	anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse key %s: %v", path, err)
	}
	k, ok := anyKey.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("key %s is not RSA", path)
	}
	return k
}
