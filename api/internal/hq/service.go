// Package hq serves the console's reads of tenant business data. The console
// never talks to a sync gateway directly (and never learns shards exist): a
// session-authed request lands here, we resolve tenant → shard → gateway, mint
// a short-lived HQ token (scope "hq" + db_name — server-side only, never sent
// to the browser), call the gateway's /hq endpoint, and wrap the answer in the
// freshness envelope the console renders. Every later console slice copies
// this read chain.
package hq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aribpos/license-api/internal/model"
)

// Service errors surfaced to the HTTP layer.
var (
	ErrForbidden          = errors.New("resource does not belong to this account")
	ErrNotSubscribed      = errors.New("tenant has no sync subscription (no central DB provisioned)")
	ErrGatewayUnreachable = errors.New("sync gateway unreachable")
)

// offlineAfter is how stale a branch's last completed sync round may be before
// its data is presented as "offline" rather than "synced" (spec: the 🔴 tier).
const offlineAfter = 30 * time.Minute

// Store is the slice of the registry the HQ read chain needs.
type Store interface {
	TenantByID(ctx context.Context, id string) (*model.Tenant, error)
	ShardByID(ctx context.Context, id string) (*model.Shard, error)
}

// TokenIssuer mints the HQ token the gateway's /hq endpoints require.
type TokenIssuer interface {
	IssueHQToken(dbName string) (string, error)
}

// Service proxies console reads to the tenant's sync gateway.
type Service struct {
	store  Store
	tokens TokenIssuer
	http   *http.Client
}

// New builds an hq Service. A nil client gets a default whose timeout sits
// under the API's 30s request timeout, so a dead gateway surfaces as a clean
// 503 rather than a cut-off request.
func New(store Store, tokens TokenIssuer, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{store: store, tokens: tokens, http: client}
}

// BranchSync is one branch's last completed sync round.
type BranchSync struct {
	BranchID   string    `json:"branch_id"`
	LastSyncAt time.Time `json:"last_sync_at"`
}

// BranchActivityEnvelope is the freshness envelope (API⇄console contract):
// branch-derived data arrives as {data, source, as_of}. Source is "synced"
// while the branch's sync cadence is healthy, "offline" once it goes stale;
// "live" is reserved for the future SignalR tier.
type BranchActivityEnvelope struct {
	Data   BranchSync `json:"data"`
	Source string     `json:"source"`
	AsOf   *time.Time `json:"as_of,omitempty"`
}

// BranchActivity returns every branch's last completed sync round for an
// owned, sync-subscribed tenant, wrapped in freshness envelopes.
func (s *Service) BranchActivity(ctx context.Context, accountID, tenantID string) ([]BranchActivityEnvelope, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if t.AccountID != accountID {
		return nil, ErrForbidden
	}
	if t.DBName == "" || t.ShardID == "" {
		return nil, ErrNotSubscribed
	}
	shard, err := s.store.ShardByID(ctx, t.ShardID)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Branches []BranchSync `json:"branches"`
	}
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/branch-activity", t.DBName, &resp); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]BranchActivityEnvelope, 0, len(resp.Branches))
	for _, b := range resp.Branches {
		asOf := b.LastSyncAt
		source := "synced"
		if now.Sub(asOf) > offlineAfter {
			source = "offline"
		}
		out = append(out, BranchActivityEnvelope{Data: b, Source: source, AsOf: &asOf})
	}
	return out, nil
}

// getJSON performs one HQ-token-authed gateway GET and decodes the body.
func (s *Service) getJSON(ctx context.Context, url, dbName string, into any) error {
	tok, err := s.tokens.IssueHQToken(dbName)
	if err != nil {
		return fmt.Errorf("mint hq token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
