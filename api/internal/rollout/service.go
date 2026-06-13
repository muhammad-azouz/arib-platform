// Package rollout drives operator-triggered fleet schema rollouts (roadmap E3).
//
// When the schema changes, every tenant's central DB needs the new shape. Only
// the shard gateway can reach the shard's localhost-only SQL Server (D11), so
// this orchestrator does not touch DBs directly: it reads a shard gateway's
// current schema version, walks the shard's tenants from the registry, and asks
// the gateway to migrate the ones that are behind — recording each result back
// into the tenant doc (the per-tenant schema-version registry).
package rollout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/model"
)

// maxAttempts is the per-tenant immediate retry budget for transient errors
// (a network blip to the gateway). A deterministic failure (e.g. a broken
// migration) is left in `failed` for the operator to fix; re-running the
// rollout is idempotent and retries only the behind/failed tenants.
const maxAttempts = 2

// Store is the slice of the registry the orchestrator needs.
type Store interface {
	ShardByID(ctx context.Context, id string) (*model.Shard, error)
	TenantsOnShard(ctx context.Context, shardID string) ([]model.Tenant, error)
	UpdateTenantSchema(ctx context.Context, id string, version int, status model.RolloutStatus, errMsg string, attempts int, at time.Time) error
}

// TokenIssuer mints the ops token the gateway's /admin endpoints require.
type TokenIssuer interface {
	IssueOpsToken() (string, error)
}

// Service orchestrates fleet rollouts against shard gateways.
type Service struct {
	store  Store
	tokens TokenIssuer
	http   *http.Client
}

// New builds a rollout Service. A nil client gets a default with a generous
// timeout (a cold tenant DB's first baseline can take a while).
func New(store Store, tokens TokenIssuer, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Service{store: store, tokens: tokens, http: client}
}

// TenantState is one tenant's line in a rollout/report.
type TenantState struct {
	TenantID string `json:"tenant_id"`
	DBName   string `json:"db_name"`
	Version  int    `json:"schema_version"`
	Status   string `json:"rollout_status"`
	Error    string `json:"rollout_error,omitempty"`
}

// Report is the mixed-version view returned by a rollout or a schema-report.
type Report struct {
	ShardID   string        `json:"shard_id"`
	Target    int           `json:"target_version"`
	Tenants   []TenantState `json:"tenants"`
	ByVersion map[int]int   `json:"by_version"`
	Failed    []string      `json:"failed"`
}

// RolloutShard migrates every tenant on the shard that is behind the gateway's
// current schema version (or previously failed), then returns a mixed-version
// report. Idempotent: tenants already at the target are skipped.
func (s *Service) RolloutShard(ctx context.Context, shardID string) (*Report, error) {
	sh, err := s.store.ShardByID(ctx, shardID)
	if err != nil {
		return nil, err
	}
	gateway := strings.TrimRight(sh.GatewayURL, "/")
	target, err := s.gatewayVersion(ctx, gateway)
	if err != nil {
		return nil, fmt.Errorf("gateway %s healthz: %w", gateway, err)
	}
	tenants, err := s.store.TenantsOnShard(ctx, shardID)
	if err != nil {
		return nil, err
	}

	for i := range tenants {
		t := &tenants[i]
		if t.DBName == "" {
			continue // not placed on a shard yet
		}
		if t.SchemaVersion >= target && t.RolloutStatus != model.RolloutFailed {
			continue // already current
		}
		s.migrateTenant(ctx, gateway, t, target)
	}
	return s.report(shardID, target, tenants), nil
}

// ShardReport returns the current mixed-version view without migrating anything.
func (s *Service) ShardReport(ctx context.Context, shardID string) (*Report, error) {
	sh, err := s.store.ShardByID(ctx, shardID)
	if err != nil {
		return nil, err
	}
	target, err := s.gatewayVersion(ctx, strings.TrimRight(sh.GatewayURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("gateway healthz: %w", err)
	}
	tenants, err := s.store.TenantsOnShard(ctx, shardID)
	if err != nil {
		return nil, err
	}
	return s.report(shardID, target, tenants), nil
}

// migrateTenant runs (and records) one tenant's migrate, mutating t in place so
// the caller's report reflects the outcome.
func (s *Service) migrateTenant(ctx context.Context, gateway string, t *model.Tenant, target int) {
	// Mark in-flight so a concurrent report shows the migrating state.
	now := time.Now().UTC()
	_ = s.store.UpdateTenantSchema(ctx, t.ID, t.SchemaVersion, model.RolloutMigrating, "", t.RolloutAttempts, now)

	var version int
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if version, err = s.migrate(ctx, gateway, t.DBName); err == nil {
			break
		}
	}

	now = time.Now().UTC()
	if err != nil {
		t.RolloutStatus, t.RolloutError, t.RolloutAttempts, t.RolloutAt = model.RolloutFailed, err.Error(), t.RolloutAttempts+1, now
		_ = s.store.UpdateTenantSchema(ctx, t.ID, t.SchemaVersion, model.RolloutFailed, err.Error(), t.RolloutAttempts, now)
		return
	}
	t.SchemaVersion, t.RolloutStatus, t.RolloutError, t.RolloutAt = version, model.RolloutIdle, "", now
	_ = s.store.UpdateTenantSchema(ctx, t.ID, version, model.RolloutIdle, "", t.RolloutAttempts, now)
}

func (s *Service) report(shardID string, target int, tenants []model.Tenant) *Report {
	rep := &Report{ShardID: shardID, Target: target, ByVersion: map[int]int{}}
	for i := range tenants {
		t := &tenants[i]
		if t.DBName == "" {
			continue
		}
		status := string(t.RolloutStatus)
		if status == "" {
			status = string(model.RolloutIdle)
		}
		rep.Tenants = append(rep.Tenants, TenantState{
			TenantID: t.ID, DBName: t.DBName, Version: t.SchemaVersion,
			Status: status, Error: t.RolloutError,
		})
		rep.ByVersion[t.SchemaVersion]++
		if t.RolloutStatus == model.RolloutFailed {
			rep.Failed = append(rep.Failed, t.ID)
		}
	}
	sort.Strings(rep.Failed)
	return rep
}

// gatewayVersion reads the shard gateway's current schema version from /healthz.
func (s *Service) gatewayVersion(ctx context.Context, gateway string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway+"/healthz", nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	return body.SchemaVersion, nil
}

// migrate asks the gateway to create+migrate one tenant DB and returns the
// version it stamped.
func (s *Service) migrate(ctx context.Context, gateway, dbName string) (int, error) {
	tok, err := s.tokens.IssueOpsToken()
	if err != nil {
		return 0, fmt.Errorf("mint ops token: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"db_name": dbName})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway+"/admin/migrate", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var body struct {
		OK             bool   `json:"ok"`
		AppliedVersion int    `json:"applied_version"`
		Error          string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || !body.OK {
		if body.Error != "" {
			return 0, fmt.Errorf("%s", body.Error)
		}
		return 0, fmt.Errorf("gateway status %d", resp.StatusCode)
	}
	return body.AppliedVersion, nil
}
