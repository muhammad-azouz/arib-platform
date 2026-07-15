// Package hq serves the console's reads of tenant business data. The console
// never talks to a sync gateway directly (and never learns shards exist): a
// session-authed request lands here, we resolve tenant → shard → gateway, mint
// a short-lived HQ token (scope "hq" + db_name — server-side only, never sent
// to the browser), call the gateway's /hq endpoint, and wrap the answer in the
// freshness envelope the console renders. Every later console slice copies
// this read chain.
package hq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/aribpos/license-api/internal/model"
)

// Service errors surfaced to the HTTP layer.
var (
	ErrForbidden            = errors.New("resource does not belong to this account")
	ErrNotSubscribed        = errors.New("tenant has no sync subscription (no central DB provisioned)")
	ErrGatewayUnreachable   = errors.New("sync gateway unreachable")
	ErrNotFound             = errors.New("not found")
	ErrInvalidUnits         = errors.New("one or more unit_id values do not belong to this product")
	ErrInvalidGroup         = errors.New("group not found")
	ErrTenantNotProvisioned = errors.New("tenant has not completed its first sync yet")
)

// DuplicateBarcodeError is returned by CreateProduct when a requested
// barcode is already used elsewhere in the tenant (barcodes are unique
// tenant-wide, not per-product).
type DuplicateBarcodeError struct{ Barcode string }

func (e *DuplicateBarcodeError) Error() string {
	return fmt.Sprintf("barcode %q is already used by another product", e.Barcode)
}

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

// resolveGateway loads the owned, sync-subscribed tenant and its shard — the
// first step of every HQ-gateway call (T6's chain, reused by every later
// slice: session ownership check, subscription check, shard lookup).
func (s *Service) resolveGateway(ctx context.Context, accountID, tenantID string) (*model.Tenant, *model.Shard, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	if t.AccountID != accountID {
		return nil, nil, ErrForbidden
	}
	if t.DBName == "" || t.ShardID == "" {
		return nil, nil, ErrNotSubscribed
	}
	shard, err := s.store.ShardByID(ctx, t.ShardID)
	if err != nil {
		return nil, nil, err
	}
	return t, shard, nil
}

// BranchActivity returns every branch's last completed sync round for an
// owned, sync-subscribed tenant, wrapped in freshness envelopes.
func (s *Service) BranchActivity(ctx context.Context, accountID, tenantID string) ([]BranchActivityEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
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
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// putJSON performs one HQ-token-authed gateway PUT with a JSON body and
// decodes the response. Maps the gateway's 404 (no such product) to
// ErrNotFound and 400 (invalid unit_id) to ErrInvalidUnits.
func (s *Service) putJSON(ctx context.Context, url, dbName string, body, into any) error {
	tok, err := s.tokens.IssueHQToken(dbName)
	if err != nil {
		return fmt.Errorf("mint hq token: %w", err)
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusBadRequest:
		return ErrInvalidUnits
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(into)
	default:
		return fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}

// --- Catalog (slice 3): the master product tables, proxied read-only. Unlike
// the branch envelopes above (which grade by sync cadence), catalog data is
// read live off the central DB on every call, so the envelope always reads
// "synced" — the freshness pill is honestly reporting "just read", not
// grading staleness. Availability rows are the exception: each one is
// decorated with its owning branch's health/last_sync_at (from the registry
// this service already loads), so the console needs no second call to know
// whether a branch's stock number can be trusted. ---

// CatalogGroup is one product group; the console builds the parent/child tree
// client-side from ParentID.
type CatalogGroup struct {
	ID           string `json:"id"`
	ParentID     string `json:"parent_id"`
	Name         string `json:"name"`
	IsActive     bool   `json:"is_active"`
	Num          int    `json:"num"`
	ProductCount int    `json:"product_count"`
}

// CatalogGroupsEnvelope wraps the full group list in the freshness envelope.
type CatalogGroupsEnvelope struct {
	Data   []CatalogGroup `json:"data"`
	Source string         `json:"source"`
	AsOf   time.Time      `json:"as_of"`
}

// CatalogGroups returns every product group for an owned, sync-subscribed
// tenant.
func (s *Service) CatalogGroups(ctx context.Context, accountID, tenantID string) (*CatalogGroupsEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Groups []CatalogGroup `json:"groups"`
	}
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/groups", t.DBName, &resp); err != nil {
		return nil, err
	}
	return &CatalogGroupsEnvelope{Data: resp.Groups, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// CatalogProduct is one row of the paged product list.
type CatalogProduct struct {
	ID        string   `json:"id"`
	Code      int      `json:"code"`
	Name      string   `json:"name"`
	Kind      int      `json:"kind"`
	GroupID   *string  `json:"group_id,omitempty"`
	GroupName *string  `json:"group_name,omitempty"`
	IsActive  bool     `json:"is_active"`
	Unit      *string  `json:"unit,omitempty"`
	Sale      float64  `json:"sale"`
	Buy       float64  `json:"buy"`
	Barcodes  []string `json:"barcodes"`
	TotalQty  float64  `json:"total_qty"`
}

// CatalogProductsPage is the paged products payload.
type CatalogProductsPage struct {
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Items    []CatalogProduct `json:"items"`
}

// CatalogProductsEnvelope wraps a page of products in the freshness envelope.
type CatalogProductsEnvelope struct {
	Data   CatalogProductsPage `json:"data"`
	Source string              `json:"source"`
	AsOf   time.Time           `json:"as_of"`
}

// CatalogProducts returns one page of the master product list. params carries
// the console's search/group_id/page/page_size straight through to the
// gateway, which owns defaulting and clamping.
func (s *Service) CatalogProducts(ctx context.Context, accountID, tenantID string, params url.Values) (*CatalogProductsEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	u := shard.GatewayURL + "/hq/products"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp CatalogProductsPage
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	return &CatalogProductsEnvelope{Data: resp, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// ProductUnit is one unit of measure with its full price ladder and barcodes.
type ProductUnit struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	ValSub   float64   `json:"val_sub"`
	Level    int       `json:"level"`
	Buy      float64   `json:"buy"`
	Sale     float64   `json:"sale"`
	Prices   []float64 `json:"prices"`
	Barcodes []string  `json:"barcodes"`
}

// ProductAvailability is one branch warehouse's stock of the product,
// decorated with that branch's sync health so the console needs no second
// call to judge whether the number can be trusted.
type ProductAvailability struct {
	BranchID      string     `json:"branch_id"`
	BranchName    string     `json:"branch_name"`
	Health        string     `json:"health"`
	WarehouseID   string     `json:"warehouse_id"`
	WarehouseName string     `json:"warehouse_name"`
	TotalQty      float64    `json:"total_qty"`
	UnitCost      float64    `json:"unit_cost"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
}

// ProductDetail is the full detail for one product.
type ProductDetail struct {
	ID           string                `json:"id"`
	Code         int                   `json:"code"`
	Name         string                `json:"name"`
	Kind         int                   `json:"kind"`
	GroupID      *string               `json:"group_id,omitempty"`
	GroupName    *string               `json:"group_name,omitempty"`
	IsActive     bool                  `json:"is_active"`
	ReOrder      float64               `json:"re_order"`
	IsExpire     bool                  `json:"is_expire"`
	CreatedAt    time.Time             `json:"created_at"`
	Units        []ProductUnit         `json:"units"`
	Availability []ProductAvailability `json:"availability"`
}

// ProductDetailEnvelope wraps one product's detail in the freshness envelope.
type ProductDetailEnvelope struct {
	Data   *ProductDetail `json:"data"`
	Source string         `json:"source"`
	AsOf   time.Time      `json:"as_of"`
}

// CatalogProductDetail fetches one product's full detail, decorating each
// availability row with its branch's name and current health tier. Returns
// ErrNotFound when the gateway has no such product (including the
// never-synced-tenant case).
func (s *Service) CatalogProductDetail(ctx context.Context, accountID, tenantID, productID string) (*ProductDetailEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ID           string        `json:"id"`
		Code         int           `json:"code"`
		Name         string        `json:"name"`
		Kind         int           `json:"kind"`
		GroupID      *string       `json:"group_id"`
		GroupName    *string       `json:"group_name"`
		IsActive     bool          `json:"is_active"`
		ReOrder      float64       `json:"re_order"`
		IsExpire     bool          `json:"is_expire"`
		CreatedAt    time.Time     `json:"created_at"`
		Units        []ProductUnit `json:"units"`
		Availability []struct {
			BranchID      string     `json:"branch_id"`
			WarehouseID   string     `json:"warehouse_id"`
			WarehouseName string     `json:"warehouse_name"`
			TotalQty      float64    `json:"total_qty"`
			UnitCost      float64    `json:"unit_cost"`
			UpdatedAt     *time.Time `json:"updated_at"`
		} `json:"availability"`
	}
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/products/"+productID, t.DBName, &raw); err != nil {
		return nil, err
	}

	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*model.Branch, len(branches))
	for i := range branches {
		byID[branches[i].ID] = &branches[i]
	}

	now := time.Now().UTC()
	availability := make([]ProductAvailability, 0, len(raw.Availability))
	for _, a := range raw.Availability {
		row := ProductAvailability{
			BranchID: a.BranchID, Health: "never",
			WarehouseID: a.WarehouseID, WarehouseName: a.WarehouseName,
			TotalQty: a.TotalQty, UnitCost: a.UnitCost, UpdatedAt: a.UpdatedAt,
		}
		if b, ok := byID[a.BranchID]; ok {
			row.BranchName = b.Name
			row.Health = healthTier(b.LastSyncAt, now)
			row.LastSyncAt = b.LastSyncAt
		}
		availability = append(availability, row)
	}

	detail := &ProductDetail{
		ID: raw.ID, Code: raw.Code, Name: raw.Name, Kind: raw.Kind,
		GroupID: raw.GroupID, GroupName: raw.GroupName, IsActive: raw.IsActive,
		ReOrder: raw.ReOrder, IsExpire: raw.IsExpire, CreatedAt: raw.CreatedAt,
		Units: raw.Units, Availability: availability,
	}
	return &ProductDetailEnvelope{Data: detail, Source: "synced", AsOf: now}, nil
}

// --- Catalog write (slice 3): the first HQ write. Prices live on the UoM
// row, so a "price change" is a batch of per-unit updates; the gateway
// rejects the whole batch (no partial writes) if any unit_id doesn't belong
// to the product. Propagation to branches needs no extra step here — DMS's
// own tracking triggers on the central DB pick up this EF write like any
// other Tier-A change and carry it down on the branch's next sync round. ---

// PriceChange is one unit's requested price update; nil fields are left
// unchanged by the gateway.
type PriceChange struct {
	UnitID string   `json:"unit_id"`
	Sale   *float64 `json:"sale,omitempty"`
	Buy    *float64 `json:"buy,omitempty"`
	Price1 *float64 `json:"price1,omitempty"`
	Price2 *float64 `json:"price2,omitempty"`
	Price3 *float64 `json:"price3,omitempty"`
	Price4 *float64 `json:"price4,omitempty"`
	Price5 *float64 `json:"price5,omitempty"`
	Price6 *float64 `json:"price6,omitempty"`
	Price7 *float64 `json:"price7,omitempty"`
	Price8 *float64 `json:"price8,omitempty"`
	Price9 *float64 `json:"price9,omitempty"`
}

// PriceChangeResult is the gateway's write receipt: the UTC instant the
// change committed to central. The console compares this to a branch's live
// last_sync_at (already streamed via SSE) to flip a propagation chip — no new
// wiring needed.
type PriceChangeResult struct {
	WrittenAt time.Time `json:"written_at"`
}

// ChangeProductPrices forwards a batch of unit price changes to the gateway
// for an owned, sync-subscribed tenant. Returns ErrNotFound if the product
// doesn't exist and ErrInvalidUnits if any unit_id isn't one of its units.
func (s *Service) ChangeProductPrices(ctx context.Context, accountID, tenantID, productID string, changes []PriceChange) (*PriceChangeResult, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	var result PriceChangeResult
	body := map[string]any{"changes": changes}
	if err := s.putJSON(ctx, shard.GatewayURL+"/hq/products/"+productID+"/prices", t.DBName, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// --- HQ product create (slice 3, T26): the second HQ write. ---

// NewProductUnit is one unit to create alongside a new product.
type NewProductUnit struct {
	Name     string   `json:"name"`
	ValSub   float64  `json:"val_sub"`
	Buy      float64  `json:"buy"`
	Sale     float64  `json:"sale"`
	Barcodes []string `json:"barcodes,omitempty"`
}

// NewProduct is a product to create, with at least one unit.
type NewProduct struct {
	Name     string           `json:"name"`
	Kind     int              `json:"kind"`
	GroupID  *string          `json:"group_id,omitempty"`
	ReOrder  float64          `json:"re_order,omitempty"`
	IsExpire bool             `json:"is_expire,omitempty"`
	Units    []NewProductUnit `json:"units"`
}

// NewProductResult is the gateway's create receipt: the new product's id and
// code, plus the same written_at propagation timestamp price changes carry.
type NewProductResult struct {
	ID        string    `json:"id"`
	Code      int       `json:"code"`
	WrittenAt time.Time `json:"written_at"`
}

// CreateProduct forwards a product-create request to the gateway for an
// owned, sync-subscribed tenant. Returns ErrInvalidGroup, a
// *DuplicateBarcodeError, or ErrTenantNotProvisioned (subscribed but no
// central DB yet — the tenant has never completed a first sync round).
func (s *Service) CreateProduct(ctx context.Context, accountID, tenantID string, input NewProduct) (*NewProductResult, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}

	tok, err := s.tokens.IssueHQToken(t.DBName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	buf, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, shard.GatewayURL+"/hq/products", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return nil, ErrInvalidGroup
	case http.StatusConflict:
		var body struct {
			Barcode string `json:"barcode"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, &DuplicateBarcodeError{Barcode: body.Barcode}
	case http.StatusServiceUnavailable:
		return nil, ErrTenantNotProvisioned
	case http.StatusOK, http.StatusCreated:
		var result NewProductResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}

// --- Inventory (slice 4): one dataset (WarehousesProductInventories +
// InventoryMovements), three perspectives. Unlike Branches (which degrades to
// control-plane data so the page always renders), these fail hard on gateway
// problems like the rest of the catalog reads — there is no honest "offline"
// render for a stock number; the freshness pill already tells that story per
// branch on the by-branch/attention views. ---

// WarehouseStock is one warehouse's slice of a branch's stock summary.
type WarehouseStock struct {
	WarehouseID   string  `json:"warehouse_id"`
	WarehouseName string  `json:"warehouse_name"`
	IsActive      bool    `json:"is_active"`
	SkuCount      int     `json:"sku_count"`
	StockValue    float64 `json:"stock_value"`
	NegativeCount int     `json:"negative_count"`
	OutCount      int     `json:"out_count"`
	LowCount      int     `json:"low_count"`
}

// InventoryBranchView is one branch's stock summary, decorated with sync
// health the same way BranchView is — the console needs no second call to
// judge whether a branch's numbers can be trusted. A registry branch the
// gateway has no stock rows for still renders, zeroed.
type InventoryBranchView struct {
	BranchID      string           `json:"branch_id"`
	BranchName    string           `json:"branch_name"`
	Health        string           `json:"health"`
	LastSyncAt    *time.Time       `json:"last_sync_at,omitempty"`
	SkuCount      int              `json:"sku_count"`
	StockValue    float64          `json:"stock_value"`
	NegativeCount int              `json:"negative_count"`
	OutCount      int              `json:"out_count"`
	LowCount      int              `json:"low_count"`
	Warehouses    []WarehouseStock `json:"warehouses"`
}

// InventoryTotals is the company-wide roll-up over every InventoryBranchView.
// There is no company-wide sku_count: a product stocked at two branches would
// double-count.
type InventoryTotals struct {
	StockValue    float64 `json:"stock_value"`
	NegativeCount int     `json:"negative_count"`
	OutCount      int     `json:"out_count"`
	LowCount      int     `json:"low_count"`
}

// InventoryBranchesData is the full "by branch" inventory payload.
type InventoryBranchesData struct {
	Branches []InventoryBranchView `json:"branches"`
	Totals   InventoryTotals       `json:"totals"`
}

// InventoryByBranchEnvelope wraps the by-branch inventory view in the
// freshness envelope.
type InventoryByBranchEnvelope struct {
	Data   InventoryBranchesData `json:"data"`
	Source string                `json:"source"`
	AsOf   time.Time             `json:"as_of"`
}

// InventoryByBranch returns the "by branch" inventory view: every registry
// branch (zeroed if the gateway reports no stock rows for it) decorated with
// sync health, plus company-wide totals summed over them.
func (s *Service) InventoryByBranch(ctx context.Context, accountID, tenantID string) (*InventoryByBranchEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Branches []struct {
			BranchID      string           `json:"branch_id"`
			SkuCount      int              `json:"sku_count"`
			StockValue    float64          `json:"stock_value"`
			NegativeCount int              `json:"negative_count"`
			OutCount      int              `json:"out_count"`
			LowCount      int              `json:"low_count"`
			Warehouses    []WarehouseStock `json:"warehouses"`
		} `json:"branches"`
	}
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/inventory/branch-summary", t.DBName, &resp); err != nil {
		return nil, err
	}
	byID := make(map[string]int, len(resp.Branches))
	for i, b := range resp.Branches {
		byID[b.BranchID] = i
	}

	now := time.Now().UTC()
	data := InventoryBranchesData{Branches: make([]InventoryBranchView, 0, len(branches))}
	for i := range branches {
		b := &branches[i]
		v := InventoryBranchView{
			BranchID: b.ID, BranchName: b.Name,
			Health: healthTier(b.LastSyncAt, now), LastSyncAt: b.LastSyncAt,
			Warehouses: []WarehouseStock{},
		}
		if idx, ok := byID[b.ID]; ok {
			g := resp.Branches[idx]
			v.SkuCount, v.StockValue = g.SkuCount, g.StockValue
			v.NegativeCount, v.OutCount, v.LowCount = g.NegativeCount, g.OutCount, g.LowCount
			v.Warehouses = g.Warehouses
		}
		data.Totals.StockValue += v.StockValue
		data.Totals.NegativeCount += v.NegativeCount
		data.Totals.OutCount += v.OutCount
		data.Totals.LowCount += v.LowCount
		data.Branches = append(data.Branches, v)
	}
	return &InventoryByBranchEnvelope{Data: data, Source: "synced", AsOf: now}, nil
}

// InventoryProduct is one row of the "by product" inventory view. Qty/value
// are company-wide, or scoped to one branch when the caller's branch_id
// param is set — the gateway owns that scoping.
type InventoryProduct struct {
	ID                string     `json:"id"`
	Code              int        `json:"code"`
	Name              string     `json:"name"`
	GroupID           *string    `json:"group_id,omitempty"`
	GroupName         *string    `json:"group_name,omitempty"`
	IsActive          bool       `json:"is_active"`
	Unit              *string    `json:"unit,omitempty"`
	ReOrder           float64    `json:"re_order"`
	TotalQty          float64    `json:"total_qty"`
	StockValue        float64    `json:"stock_value"`
	BranchesWithStock int        `json:"branches_with_stock"`
	LastActivityAt    *time.Time `json:"last_activity_at,omitempty"`
	Status            string     `json:"status"`
}

// InventoryProductsPage is the paged by-product inventory payload.
type InventoryProductsPage struct {
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Items    []InventoryProduct `json:"items"`
}

// InventoryProductsEnvelope wraps a page of the by-product view in the
// freshness envelope.
type InventoryProductsEnvelope struct {
	Data   InventoryProductsPage `json:"data"`
	Source string                `json:"source"`
	AsOf   time.Time             `json:"as_of"`
}

// InventoryProducts returns one page of the "by product" inventory view.
// params carries search/group_id/branch_id/status/page/page_size straight
// through to the gateway, which owns filtering, defaulting and clamping.
func (s *Service) InventoryProducts(ctx context.Context, accountID, tenantID string, params url.Values) (*InventoryProductsEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	u := shard.GatewayURL + "/hq/inventory/products"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp InventoryProductsPage
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	return &InventoryProductsEnvelope{Data: resp, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// AttentionCounts is the unpaged per-severity totals for the needs-attention
// view.
type AttentionCounts struct {
	Negative int `json:"negative"`
	Out      int `json:"out"`
	Low      int `json:"low"`
}

// AttentionItem is one WPI row needing attention, decorated with its
// branch's name and current health tier.
type AttentionItem struct {
	Status        string     `json:"status"`
	ProductID     string     `json:"product_id"`
	ProductCode   int        `json:"product_code"`
	ProductName   string     `json:"product_name"`
	Unit          *string    `json:"unit,omitempty"`
	ReOrder       float64    `json:"re_order"`
	BranchID      string     `json:"branch_id"`
	BranchName    string     `json:"branch_name"`
	Health        string     `json:"health"`
	WarehouseID   string     `json:"warehouse_id"`
	WarehouseName string     `json:"warehouse_name"`
	TotalQty      float64    `json:"total_qty"`
	UnitCost      float64    `json:"unit_cost"`
	LastInDate    *time.Time `json:"last_in_date,omitempty"`
	LastOutDate   *time.Time `json:"last_out_date,omitempty"`
}

// StaleBranch is a branch whose data is too old to trust, surfaced as a
// separate list from the paged stock items so injecting it never breaks
// paging math. "never" branches are excluded — the Overview alerts already
// own "never connected".
type StaleBranch struct {
	BranchID   string     `json:"branch_id"`
	BranchName string     `json:"branch_name"`
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
}

// AttentionData is the full needs-attention payload.
type AttentionData struct {
	StaleBranches []StaleBranch   `json:"stale_branches"`
	Counts        AttentionCounts `json:"counts"`
	Total         int             `json:"total"`
	Page          int             `json:"page"`
	PageSize      int             `json:"page_size"`
	Items         []AttentionItem `json:"items"`
}

// AttentionEnvelope wraps the needs-attention view in the freshness envelope.
type AttentionEnvelope struct {
	Data   AttentionData `json:"data"`
	Source string        `json:"source"`
	AsOf   time.Time     `json:"as_of"`
}

// InventoryAttention returns the needs-attention view: WPI rows failing the
// low/out/negative rule (decorated with branch name/health), plus a
// stale_branches list merged in from the registry — the fourth attention
// condition (stale branch data) needs no gateway work since branch health is
// already computed API-side. params' branch_id (if any) scopes both the
// gateway query and the stale-branch merge.
func (s *Service) InventoryAttention(ctx context.Context, accountID, tenantID string, params url.Values) (*AttentionEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*model.Branch, len(branches))
	for i := range branches {
		byID[branches[i].ID] = &branches[i]
	}

	u := shard.GatewayURL + "/hq/inventory/attention"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var raw struct {
		Total    int             `json:"total"`
		Page     int             `json:"page"`
		PageSize int             `json:"page_size"`
		Counts   AttentionCounts `json:"counts"`
		Items    []struct {
			Status        string     `json:"status"`
			ProductID     string     `json:"product_id"`
			ProductCode   int        `json:"product_code"`
			ProductName   string     `json:"product_name"`
			Unit          *string    `json:"unit"`
			ReOrder       float64    `json:"re_order"`
			BranchID      string     `json:"branch_id"`
			WarehouseID   string     `json:"warehouse_id"`
			WarehouseName string     `json:"warehouse_name"`
			TotalQty      float64    `json:"total_qty"`
			UnitCost      float64    `json:"unit_cost"`
			LastInDate    *time.Time `json:"last_in_date"`
			LastOutDate   *time.Time `json:"last_out_date"`
		} `json:"items"`
	}
	if err := s.getJSON(ctx, u, t.DBName, &raw); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	items := make([]AttentionItem, 0, len(raw.Items))
	for _, r := range raw.Items {
		item := AttentionItem{
			Status: r.Status, ProductID: r.ProductID, ProductCode: r.ProductCode, ProductName: r.ProductName,
			Unit: r.Unit, ReOrder: r.ReOrder, BranchID: r.BranchID, Health: "never",
			WarehouseID: r.WarehouseID, WarehouseName: r.WarehouseName,
			TotalQty: r.TotalQty, UnitCost: r.UnitCost, LastInDate: r.LastInDate, LastOutDate: r.LastOutDate,
		}
		if b, ok := byID[r.BranchID]; ok {
			item.BranchName = b.Name
			item.Health = healthTier(b.LastSyncAt, now)
		}
		items = append(items, item)
	}

	branchFilter := params.Get("branch_id")
	stale := []StaleBranch{}
	for i := range branches {
		b := &branches[i]
		if branchFilter != "" && b.ID != branchFilter {
			continue
		}
		if healthTier(b.LastSyncAt, now) == "stale" {
			stale = append(stale, StaleBranch{BranchID: b.ID, BranchName: b.Name, LastSyncAt: b.LastSyncAt})
		}
	}

	data := AttentionData{
		StaleBranches: stale, Counts: raw.Counts, Total: raw.Total,
		Page: raw.Page, PageSize: raw.PageSize, Items: items,
	}
	return &AttentionEnvelope{Data: data, Source: "synced", AsOf: now}, nil
}

// --- Conflicts (slice 5): the gateway's ConflictLog, proxied for review.
// ServerWins (D12) already resolved these rows at sync time — the console
// lists the losing branch writes as alerts, and acknowledging them (a write
// that lives on the same central-only table) is what clears the alert. ---

// ConflictItem is one logged sync conflict, decorated with its branch's name.
// LocalRow is the central row that was kept; RemoteRow is the branch's losing
// write (nil when the branch had deleted the row). ProductID/ProductName are
// the gateway's best-effort link to the product the row belongs to.
type ConflictItem struct {
	ID             int64      `json:"id"`
	OccurredAt     time.Time  `json:"occurred_at"`
	BranchID       *string    `json:"branch_id,omitempty"`
	BranchName     string     `json:"branch_name,omitempty"`
	TableName      string     `json:"table_name"`
	RowPk          *string    `json:"row_pk,omitempty"`
	ConflictType   string     `json:"conflict_type"`
	Resolution     string     `json:"resolution"`
	LocalRow       *string    `json:"local_row,omitempty"`
	RemoteRow      *string    `json:"remote_row,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ProductID      *string    `json:"product_id,omitempty"`
	ProductName    *string    `json:"product_name,omitempty"`
}

// ConflictsData is one newest-first page of the ConflictLog plus the unpaged
// unacknowledged count (the alert badge's number).
type ConflictsData struct {
	Unacked  int            `json:"unacked"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Items    []ConflictItem `json:"items"`
}

// ConflictsEnvelope wraps a conflicts page in the freshness envelope.
type ConflictsEnvelope struct {
	Data   ConflictsData `json:"data"`
	Source string        `json:"source"`
	AsOf   time.Time     `json:"as_of"`
}

// Conflicts returns one page of the tenant's sync-conflict log, each row
// decorated with its branch's registry name. params carries page/page_size/all
// straight through to the gateway, which owns defaulting and clamping.
func (s *Service) Conflicts(ctx context.Context, accountID, tenantID string, params url.Values) (*ConflictsEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	nameByID := make(map[string]string, len(branches))
	for i := range branches {
		nameByID[branches[i].ID] = branches[i].Name
	}

	u := shard.GatewayURL + "/hq/conflicts"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var raw ConflictsData
	if err := s.getJSON(ctx, u, t.DBName, &raw); err != nil {
		return nil, err
	}

	for i := range raw.Items {
		if id := raw.Items[i].BranchID; id != nil {
			raw.Items[i].BranchName = nameByID[*id]
		}
	}
	if raw.Items == nil {
		raw.Items = []ConflictItem{}
	}
	return &ConflictsEnvelope{Data: raw, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// AckConflictsResult is the gateway's receipt: how many rows actually flipped
// (already-acknowledged rows don't count, so a repeat call reports 0).
type AckConflictsResult struct {
	Acked int `json:"acked"`
}

// AckConflicts acknowledges conflicts by explicit ids and/or everything up to
// an id (inclusive). The handler guarantees at least one of the two is set.
func (s *Service) AckConflicts(ctx context.Context, accountID, tenantID string, ids []int64, upToID *int64) (*AckConflictsResult, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}

	tok, err := s.tokens.IssueHQToken(t.DBName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	body := map[string]any{}
	if len(ids) > 0 {
		body["ids"] = ids
	}
	if upToID != nil {
		body["up_to_id"] = *upToID
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, shard.GatewayURL+"/hq/conflicts/ack", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
	var result AckConflictsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MovementRow is one inventory movement, decorated with its branch's name (a
// health grade would be redundant here — the ProductDetail page's per-branch
// availability section already carries it for this same product).
type MovementRow struct {
	ID            string    `json:"id"`
	IssueDate     time.Time `json:"issue_date"`
	Dealing       int       `json:"dealing"`
	BranchID      string    `json:"branch_id"`
	BranchName    string    `json:"branch_name"`
	WarehouseID   string    `json:"warehouse_id"`
	WarehouseName string    `json:"warehouse_name"`
	CustomerName  *string   `json:"customer_name,omitempty"`
	InQty         float64   `json:"in_qty"`
	InPrice       float64   `json:"in_price"`
	OutQty        float64   `json:"out_qty"`
	OutPrice      float64   `json:"out_price"`
	Cost          float64   `json:"cost"`
	Unit          string    `json:"unit"`
	RegNum        string    `json:"reg_num"`
	RunningQty    float64   `json:"running_qty"`
}

// MovementsPage is one page of a product's movement history, with the
// opening balance for the period.
type MovementsPage struct {
	OpeningQty float64        `json:"opening_qty"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	Items      []MovementRow `json:"items"`
}

// MovementsEnvelope wraps a page of movement history in the freshness
// envelope.
type MovementsEnvelope struct {
	Data   MovementsPage `json:"data"`
	Source string        `json:"source"`
	AsOf   time.Time     `json:"as_of"`
}

// ProductMovements returns one page of a product's movement history. params
// carries branch_id/from/to/page/page_size straight through to the gateway.
// Returns ErrNotFound when the product doesn't exist (including a
// never-synced tenant, same as CatalogProductDetail).
func (s *Service) ProductMovements(ctx context.Context, accountID, tenantID, productID string, params url.Values) (*MovementsEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*model.Branch, len(branches))
	for i := range branches {
		byID[branches[i].ID] = &branches[i]
	}

	u := shard.GatewayURL + "/hq/products/" + productID + "/movements"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var raw struct {
		OpeningQty float64 `json:"opening_qty"`
		Total      int     `json:"total"`
		Page       int     `json:"page"`
		PageSize   int     `json:"page_size"`
		Items      []struct {
			ID            string    `json:"id"`
			IssueDate     time.Time `json:"issue_date"`
			Dealing       int       `json:"dealing"`
			BranchID      string    `json:"branch_id"`
			WarehouseID   string    `json:"warehouse_id"`
			WarehouseName string    `json:"warehouse_name"`
			CustomerName  *string   `json:"customer_name"`
			InQty         float64   `json:"in_qty"`
			InPrice       float64   `json:"in_price"`
			OutQty        float64   `json:"out_qty"`
			OutPrice      float64   `json:"out_price"`
			Cost          float64   `json:"cost"`
			Unit          string    `json:"unit"`
			RegNum        string    `json:"reg_num"`
			RunningQty    float64   `json:"running_qty"`
		} `json:"items"`
	}
	if err := s.getJSON(ctx, u, t.DBName, &raw); err != nil {
		return nil, err
	}

	items := make([]MovementRow, 0, len(raw.Items))
	for _, r := range raw.Items {
		row := MovementRow{
			ID: r.ID, IssueDate: r.IssueDate, Dealing: r.Dealing,
			BranchID: r.BranchID, WarehouseID: r.WarehouseID, WarehouseName: r.WarehouseName,
			CustomerName: r.CustomerName, InQty: r.InQty, InPrice: r.InPrice,
			OutQty: r.OutQty, OutPrice: r.OutPrice, Cost: r.Cost, Unit: r.Unit,
			RegNum: r.RegNum, RunningQty: r.RunningQty,
		}
		if b, ok := byID[r.BranchID]; ok {
			row.BranchName = b.Name
		}
		items = append(items, row)
	}

	data := MovementsPage{OpeningQty: raw.OpeningQty, Total: raw.Total, Page: raw.Page, PageSize: raw.PageSize, Items: items}
	return &MovementsEnvelope{Data: data, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// --- Reports (slice 6): question-organized, date-bounded aggregates read
// straight off the tenant DB via the gateway (open question 2's v1 answer —
// direct SQL, no rollups). Like the catalog reads, every payload is computed
// live at request time, so the envelope always reads "synced" with as_of =
// now; the branches report is the one view that merges the registry (every
// branch renders, zeroed when the gateway reports no rows for it — the same
// philosophy as InventoryByBranch). ---

// TenderSplit is how the period's sales were paid: cash in drawer, bank/card,
// e-wallet, and credit = the on-account remainder.
type TenderSplit struct {
	Cash   float64 `json:"cash"`
	Bank   float64 `json:"bank"`
	Wallet float64 `json:"wallet"`
	Credit float64 `json:"credit"`
}

// SalesDay is one local calendar day of the sales series. Day is a plain
// YYYY-MM-DD string in the tenant's day-scope, not an instant.
type SalesDay struct {
	Day          string  `json:"day"`
	SalesTotal   float64 `json:"sales_total"`
	SalesCount   int     `json:"sales_count"`
	RefundsTotal float64 `json:"refunds_total"`
}

// SalesReport is the period's totals, tender split and gap-filled day series.
// From/To echo the gateway's resolved period (it owns defaulting/clamping).
type SalesReport struct {
	From         string      `json:"from"`
	To           string      `json:"to"`
	SalesTotal   float64     `json:"sales_total"`
	SalesCount   int         `json:"sales_count"`
	RefundsTotal float64     `json:"refunds_total"`
	RefundsCount int         `json:"refunds_count"`
	Tender       TenderSplit `json:"tender"`
	Days         []SalesDay  `json:"days"`
}

// SalesReportEnvelope wraps the sales report in the freshness envelope.
type SalesReportEnvelope struct {
	Data   SalesReport `json:"data"`
	Source string      `json:"source"`
	AsOf   time.Time   `json:"as_of"`
}

// ReportSales returns the period sales report. params carries
// from/to/branch_id straight through to the gateway, which owns period
// defaulting and clamping.
func (s *Service) ReportSales(ctx context.Context, accountID, tenantID string, params url.Values) (*SalesReportEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	u := shard.GatewayURL + "/hq/reports/sales"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp SalesReport
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	if resp.Days == nil {
		resp.Days = []SalesDay{}
	}
	return &SalesReportEnvelope{Data: resp, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// ProductReportRow is one product's period performance: quantity sold (base
// units, labeled with the master-unit name), revenue and profit
// (revenue − COGS, the desktop's own profit formula).
type ProductReportRow struct {
	ID        string  `json:"id"`
	Code      int     `json:"code"`
	Name      string  `json:"name"`
	GroupName *string `json:"group_name,omitempty"`
	Unit      *string `json:"unit,omitempty"`
	QtySold   float64 `json:"qty_sold"`
	Revenue   float64 `json:"revenue"`
	Profit    float64 `json:"profit"`
}

// ProductsReportPage is the paged products report payload.
type ProductsReportPage struct {
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Items    []ProductReportRow `json:"items"`
}

// ProductsReportEnvelope wraps a page of the products report in the freshness
// envelope.
type ProductsReportEnvelope struct {
	Data   ProductsReportPage `json:"data"`
	Source string             `json:"source"`
	AsOf   time.Time          `json:"as_of"`
}

// ReportProducts returns one page of the period products report. params
// carries from/to/branch_id/group_id/sort/page/page_size straight through to
// the gateway.
func (s *Service) ReportProducts(ctx context.Context, accountID, tenantID string, params url.Values) (*ProductsReportEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	u := shard.GatewayURL + "/hq/reports/products"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp ProductsReportPage
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	if resp.Items == nil {
		resp.Items = []ProductReportRow{}
	}
	return &ProductsReportEnvelope{Data: resp, Source: "synced", AsOf: time.Now().UTC()}, nil
}

// BranchReportRow is one branch's period performance, decorated with the
// registry identity and sync health the same way every branch view is.
type BranchReportRow struct {
	BranchID     string     `json:"branch_id"`
	BranchName   string     `json:"branch_name"`
	Health       string     `json:"health"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	SalesTotal   float64    `json:"sales_total"`
	SalesCount   int        `json:"sales_count"`
	RefundsTotal float64    `json:"refunds_total"`
	RefundsCount int        `json:"refunds_count"`
	Profit       float64    `json:"profit"`
}

// BranchesReportData is the full branches-comparison payload.
type BranchesReportData struct {
	Branches []BranchReportRow `json:"branches"`
}

// BranchesReportEnvelope wraps the branches report in the freshness envelope.
type BranchesReportEnvelope struct {
	Data   BranchesReportData `json:"data"`
	Source string             `json:"source"`
	AsOf   time.Time          `json:"as_of"`
}

// ReportBranches returns the period branches comparison: every registry
// branch (zeroed when the gateway reports no rows for it in the period),
// decorated with name/health/last_sync_at. params carries from/to straight
// through to the gateway.
func (s *Service) ReportBranches(ctx context.Context, accountID, tenantID string, params url.Values) (*BranchesReportEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	u := shard.GatewayURL + "/hq/reports/branches"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp struct {
		Branches []struct {
			BranchID     string  `json:"branch_id"`
			SalesTotal   float64 `json:"sales_total"`
			SalesCount   int     `json:"sales_count"`
			RefundsTotal float64 `json:"refunds_total"`
			RefundsCount int     `json:"refunds_count"`
			Profit       float64 `json:"profit"`
		} `json:"branches"`
	}
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	byID := make(map[string]int, len(resp.Branches))
	for i, b := range resp.Branches {
		byID[b.BranchID] = i
	}

	now := time.Now().UTC()
	data := BranchesReportData{Branches: make([]BranchReportRow, 0, len(branches))}
	for i := range branches {
		b := &branches[i]
		row := BranchReportRow{
			BranchID: b.ID, BranchName: b.Name,
			Health: healthTier(b.LastSyncAt, now), LastSyncAt: b.LastSyncAt,
		}
		if idx, ok := byID[b.ID]; ok {
			g := resp.Branches[idx]
			row.SalesTotal, row.SalesCount = g.SalesTotal, g.SalesCount
			row.RefundsTotal, row.RefundsCount = g.RefundsTotal, g.RefundsCount
			row.Profit = g.Profit
		}
		data.Branches = append(data.Branches, row)
	}
	return &BranchesReportEnvelope{Data: data, Source: "synced", AsOf: now}, nil
}

// StaffReportRow is one user's period performance. UserName comes from the
// tenant DB's Tier-A Users table (replicated in full), not the registry.
type StaffReportRow struct {
	UserID       string  `json:"user_id"`
	UserName     string  `json:"user_name"`
	SalesTotal   float64 `json:"sales_total"`
	SalesCount   int     `json:"sales_count"`
	RefundsTotal float64 `json:"refunds_total"`
	RefundsCount int     `json:"refunds_count"`
}

// StaffReportData is the full staff report payload.
type StaffReportData struct {
	Staff []StaffReportRow `json:"staff"`
}

// StaffReportEnvelope wraps the staff report in the freshness envelope.
type StaffReportEnvelope struct {
	Data   StaffReportData `json:"data"`
	Source string          `json:"source"`
	AsOf   time.Time       `json:"as_of"`
}

// ReportStaff returns the period per-cashier report. params carries
// from/to/branch_id straight through to the gateway.
func (s *Service) ReportStaff(ctx context.Context, accountID, tenantID string, params url.Values) (*StaffReportEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	u := shard.GatewayURL + "/hq/reports/staff"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp StaffReportData
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	if resp.Staff == nil {
		resp.Staff = []StaffReportRow{}
	}
	return &StaffReportEnvelope{Data: resp, Source: "synced", AsOf: time.Now().UTC()}, nil
}
