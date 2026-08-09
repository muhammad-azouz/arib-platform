package rollout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
)

// The rollout's "is this tenant behind?" test compares Tenant.SchemaVersion
// against the gateway's /healthz schema_version. Until 2026-08-08 the two were
// on different scales — /admin/migrate reported applied_version, the tenant DB's
// EF __EFMigrationsHistory count (37 and climbing), which the orchestrator stored
// as the tenant's version and compared with >= against SyncScope.SchemaVersion
// (13). Always true, so every already-provisioned tenant was skipped in every
// rollout after its first, and the v13 flag day would have reached nobody.
//
// These tests pin the fixed contract against a fake gateway: no SQL Server, no
// real sync-gateway process (unlike TestE3FleetRollout, which needs both).

// fakeGateway serves the two endpoints the orchestrator calls and counts the
// migrates it received, so a test can assert a tenant was (or was not) selected.
type fakeGateway struct {
	version  int      // what /healthz and /admin/migrate report
	omitVer  bool     // simulate a gateway too old to report schema_version
	migrated []string // db_names asked to migrate, in order
}

func (g *fakeGateway) start(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "schema_version": g.version})
	})
	mux.HandleFunc("/admin/migrate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DBName string `json:"db_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.migrated = append(g.migrated, body.DBName)
		out := map[string]any{
			"ok":                 true,
			"db_name":            body.DBName,
			"applied_migrations": 37, // the old applied_version value, now informational
		}
		if !g.omitVer {
			out["schema_version"] = g.version
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

type fakeIssuer struct{}

func (fakeIssuer) IssueOpsToken() (string, error) { return "ops-token", nil }

func newSvc(t *testing.T, g *fakeGateway, tenants ...*tenantFixture) (*Service, *fakeStore) {
	t.Helper()
	url := g.start(t)
	var ts []*model.Tenant
	for _, f := range tenants {
		tn := syncTenant(f.id, f.dbName)
		tn.SchemaVersion = f.version
		ts = append(ts, tn)
	}
	store := newFakeStore(url, ts...)
	return New(store, fakeIssuer{}, &http.Client{Timeout: 5 * time.Second}), store
}

type tenantFixture struct {
	id      string
	dbName  string
	version int
}

// A tenant carrying a stale EF-migration-count version (37) against a gateway at
// 13 must be re-migrated and end up recorded at 13. Under the old >= comparison
// it was skipped, because 37 >= 13 — the exact bug.
func TestRolloutMigratesTenantRecordedAboveTarget(t *testing.T) {
	g := &fakeGateway{version: 13}
	svc, store := newSvc(t, g, &tenantFixture{"tnt_stale", "arib_stale", 37})

	rep, err := svc.Rollout(context.Background())
	if err != nil {
		t.Fatalf("rollout: %v", err)
	}
	if len(g.migrated) != 1 || g.migrated[0] != "arib_stale" {
		t.Fatalf("gateway migrates = %v, want [arib_stale] — a tenant recorded above "+
			"target must be re-migrated, not skipped", g.migrated)
	}
	if got := store.tenants["tnt_stale"].SchemaVersion; got != 13 {
		t.Fatalf("recorded version = %d, want 13 (the /healthz scale, not the EF count)", got)
	}
	if rep.ByVersion[13] != 1 {
		t.Fatalf("by_version = %v, want one tenant at 13", rep.ByVersion)
	}
	if len(rep.Failed) != 0 {
		t.Fatalf("unexpected failures %v", rep.Failed)
	}
}

// The idempotence the rollout's doc comment promises: a tenant already at the
// target is not touched. Guards against "fix the skip by never skipping".
func TestRolloutSkipsTenantAtTarget(t *testing.T) {
	g := &fakeGateway{version: 13}
	svc, _ := newSvc(t, g, &tenantFixture{"tnt_current", "arib_current", 13})

	if _, err := svc.Rollout(context.Background()); err != nil {
		t.Fatalf("rollout: %v", err)
	}
	if len(g.migrated) != 0 {
		t.Fatalf("gateway migrates = %v, want none — tenant is already at target", g.migrated)
	}
}

// A never-provisioned tenant (version 0) is behind and gets migrated.
func TestRolloutMigratesNeverProvisionedTenant(t *testing.T) {
	g := &fakeGateway{version: 13}
	svc, store := newSvc(t, g, &tenantFixture{"tnt_new", "arib_new", 0})

	if _, err := svc.Rollout(context.Background()); err != nil {
		t.Fatalf("rollout: %v", err)
	}
	if len(g.migrated) != 1 {
		t.Fatalf("gateway migrates = %v, want [arib_new]", g.migrated)
	}
	if got := store.tenants["tnt_new"].SchemaVersion; got != 13 {
		t.Fatalf("recorded version = %d, want 13", got)
	}
}

// A gateway too old to report schema_version must fail the tenant loudly rather
// than record 0 and silently re-migrate it on every future rollout.
func TestRolloutFailsWhenGatewayOmitsSchemaVersion(t *testing.T) {
	g := &fakeGateway{version: 13, omitVer: true}
	svc, store := newSvc(t, g, &tenantFixture{"tnt_old", "arib_old", 0})

	rep, err := svc.Rollout(context.Background())
	if err != nil {
		t.Fatalf("rollout itself must succeed: %v", err)
	}
	if len(rep.Failed) != 1 || rep.Failed[0] != "tnt_old" {
		t.Fatalf("failed = %v, want [tnt_old]", rep.Failed)
	}
	if got := store.tenants["tnt_old"]; got.SchemaVersion != 0 || got.RolloutError == "" {
		t.Fatalf("tenant = v%d/%q, want v0 with an error explaining the missing schema_version",
			got.SchemaVersion, got.RolloutError)
	}
}
