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
	BranchesByTenant(ctx context.Context, tenantID string) ([]model.Branch, error)
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

// OpenShift is an open cashier shift as reported by the gateway.
type OpenShift struct {
	Num      int       `json:"num"`
	OpenedBy string    `json:"opened_by"`
	OpenedAt time.Time `json:"opened_at"`
}

// BranchSnapshot is one branch's day-so-far from the gateway.
type BranchSnapshot struct {
	BranchID          string     `json:"branch_id"`
	TodaySalesTotal   float64    `json:"today_sales_total"`
	TodaySalesCount   int        `json:"today_sales_count"`
	TodayRefundsTotal float64    `json:"today_refunds_total"`
	OpenShift         *OpenShift `json:"open_shift"`
	OpenShiftCount    int        `json:"open_shift_count"`
}

// BranchView is everything the Branches page needs for one branch: the
// control-plane identity, the sync-health tier, and the freshness-enveloped
// snapshot. The snapshot degrades to {data: null, source: "offline"} when the
// tenant has no sync subscription or the gateway is unreachable — the page
// still renders from control-plane data.
type BranchView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Health     string     `json:"health"` // ok | lagging | stale | never
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
	Snapshot   struct {
		Data   *BranchSnapshot `json:"data"`
		Source string          `json:"source"`
		AsOf   *time.Time      `json:"as_of,omitempty"`
	} `json:"snapshot"`
}

// healthTier buckets a branch's last completed sync round into the console's
// health dots: ok 🟢 <10 min, lagging 🟡 10–30 min, stale 🔴 older, never = no
// round recorded yet. Derived from the control plane's last_sync_at copy so it
// works even when the gateway is unreachable.
func healthTier(lastSync *time.Time, now time.Time) string {
	if lastSync == nil {
		return "never"
	}
	age := now.Sub(*lastSync)
	switch {
	case age < 10*time.Minute:
		return "ok"
	case age < 30*time.Minute:
		return "lagging"
	default:
		return "stale"
	}
}

// Totals is the company-wide day-so-far, summed over the branch snapshots
// (slice 2's KPI tiles — no extra gateway call, no aggregate endpoint). Stale
// data stays visible on the cards, so it is summed too; honesty comes from
// the offline count and AsOf, the oldest sync among contributing branches.
type Totals struct {
	SalesTotal      float64    `json:"sales_total"`
	SalesCount      int        `json:"sales_count"`
	RefundsTotal    float64    `json:"refunds_total"`
	OpenShiftCount  int        `json:"open_shift_count"`
	SyncedBranches  int        `json:"synced_branches"`
	OfflineBranches int        `json:"offline_branches"`
	AsOf            *time.Time `json:"as_of,omitempty"`
}

// BranchesResult is the full /hq/branches payload: per-branch views plus the
// company totals the Overview's KPI tiles render.
type BranchesResult struct {
	Branches []BranchView `json:"branches"`
	Totals   Totals       `json:"totals"`
}

// Branches returns the tenant's branches merged with the gateway snapshot,
// plus company-wide totals. Unlike BranchActivity it never fails on
// subscription or gateway problems: those only downgrade the snapshot
// envelopes to offline and zero the totals.
func (s *Service) Branches(ctx context.Context, accountID, tenantID string) (*BranchesResult, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if t.AccountID != accountID {
		return nil, ErrForbidden
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Best-effort snapshot: a tenant without sync or a dead gateway means the
	// branch cards render control-plane data with offline envelopes.
	snapshots := map[string]*BranchSnapshot{}
	gatewayOK := false
	if t.DBName != "" && t.ShardID != "" {
		if shard, err := s.store.ShardByID(ctx, t.ShardID); err == nil {
			var resp struct {
				Branches []BranchSnapshot `json:"branches"`
			}
			if err := s.getJSON(ctx, shard.GatewayURL+"/hq/branch-snapshot", t.DBName, &resp); err == nil {
				gatewayOK = true
				for i := range resp.Branches {
					snapshots[resp.Branches[i].BranchID] = &resp.Branches[i]
				}
			}
		}
	}

	now := time.Now().UTC()
	res := &BranchesResult{Branches: make([]BranchView, 0, len(branches))}
	for i := range branches {
		b := &branches[i]
		health := healthTier(b.LastSyncAt, now)
		v := BranchView{
			ID:         b.ID,
			Name:       b.Name,
			Status:     string(b.Status),
			Health:     health,
			LastSyncAt: b.LastSyncAt,
		}
		v.Snapshot.Data = snapshots[b.ID]
		v.Snapshot.AsOf = b.LastSyncAt
		if v.Snapshot.Data == nil && gatewayOK && b.LastSyncAt != nil {
			// The gateway answered but had no rows for this branch — no bills
			// or shifts today. A synced zero, not a gap.
			v.Snapshot.Data = &BranchSnapshot{BranchID: b.ID}
		}
		// Stale data stays visible (Data set) but is labeled honestly: the
		// envelope says "synced" only while the branch's cadence is healthy.
		if gatewayOK && (health == "ok" || health == "lagging") {
			v.Snapshot.Source = "synced"
			res.Totals.SyncedBranches++
		} else {
			v.Snapshot.Source = "offline"
			res.Totals.OfflineBranches++
		}
		if d := v.Snapshot.Data; d != nil {
			res.Totals.SalesTotal += d.TodaySalesTotal
			res.Totals.SalesCount += d.TodaySalesCount
			res.Totals.RefundsTotal += d.TodayRefundsTotal
			res.Totals.OpenShiftCount += d.OpenShiftCount
			if b.LastSyncAt != nil && (res.Totals.AsOf == nil || b.LastSyncAt.Before(*res.Totals.AsOf)) {
				res.Totals.AsOf = b.LastSyncAt
			}
		}
		res.Branches = append(res.Branches, v)
	}
	return res, nil
}

// CheckOwnership verifies the tenant belongs to the account (the SSE endpoint
// authorises once at subscribe time, so it needs the bare check).
func (s *Service) CheckOwnership(ctx context.Context, accountID, tenantID string) error {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return err
	}
	if t.AccountID != accountID {
		return ErrForbidden
	}
	return nil
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
