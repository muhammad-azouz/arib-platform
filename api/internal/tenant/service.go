// Package tenant implements the multi-tenant registry: tenant registration,
// company/branch management, per-branch device seats and sync-token issuance.
package tenant

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Service errors surfaced to clients.
var (
	ErrForbidden       = errors.New("resource does not belong to this account")
	ErrTenantSuspended = errors.New("tenant is suspended")
	ErrBranchInactive  = errors.New("branch is deactivated")
	ErrSeatLimit       = errors.New("branch seat limit reached")
	ErrNotBound        = errors.New("no such device binding")
	ErrNotSubscribed   = errors.New("tenant has no sync subscription (no central DB provisioned)")
	ErrCompanyExists   = errors.New("tenant already has a company (one company per tenant)")
	ErrNoCompany       = errors.New("tenant has no company yet")
	ErrNotFound        = mongostore.ErrNotFound
)

// Service coordinates the registry store and the sync-token signer.
type Service struct {
	store   *mongostore.Store
	syncKey *rsa.PrivateKey
	syncTTL time.Duration
	http    *http.Client
}

// New builds a tenant Service. syncKey signs sync tokens (RS256 — the gateway
// holds only the public key, so a compromised gateway cannot mint tokens);
// syncTTL is their lifetime. The gateway URL for each tenant is resolved at
// token-issuance time from the shard registry stored in Mongo. httpClient is
// used for gateway admin calls (e.g. dropping a tenant's central DB on
// deletion); a nil value gets a default with a generous timeout.
func New(store *mongostore.Store, syncKey *rsa.PrivateKey, syncTTL time.Duration, httpClient *http.Client) *Service {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Service{store: store, syncKey: syncKey, syncTTL: syncTTL, http: httpClient}
}

// Bundle is everything the app needs at activation/login: the tenant plus its
// cloud-authoritative company (exactly one per tenant, D15) and branches.
type Bundle struct {
	Tenant   model.Tenant
	Company  *model.Company // nil until the company is registered
	Branches []model.Branch
}

// SyncClaims is the JWT a bound device presents to the DMS gateway.
type SyncClaims struct {
	TenantID string `json:"tenant_id"`
	BranchID string `json:"branch_id"`
	DeviceID string `json:"device_id"`
	DBName   string `json:"db_name"`
	ShardID  string `json:"shard_id,omitempty"` // defense-in-depth: gateway validates this matches its SHARD_ID
	jwt.RegisteredClaims
}

// --- tenants ---

// Register creates a tenant owned by an account.
func (s *Service) Register(ctx context.Context, accountID, name string) (*model.Tenant, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("tenant name required")
	}
	now := time.Now().UTC()
	t := &model.Tenant{
		ID:        idgen.New("tnt"),
		AccountID: accountID,
		Name:      strings.TrimSpace(name),
		Status:    model.TenantActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.InsertTenant(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Tenants lists the account's tenants.
func (s *Service) Tenants(ctx context.Context, accountID string) ([]model.Tenant, error) {
	return s.store.TenantsByAccount(ctx, accountID)
}

// GetBundle returns the tenant with its companies and branches, enforcing
// ownership.
func (s *Service) GetBundle(ctx context.Context, accountID, tenantID string) (*Bundle, error) {
	t, err := s.owned(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	company, err := s.store.CompanyByTenant(ctx, tenantID)
	if err != nil && !errors.Is(err, mongostore.ErrNotFound) {
		return nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// Enrich each branch with its live seat usage (active device count) so the
	// console can show used-of-limit without a separate devices endpoint.
	for i := range branches {
		n, err := s.store.CountActiveBranchDevices(ctx, branches[i].ID)
		if err != nil {
			return nil, err
		}
		branches[i].ActiveDevices = int(n)
	}
	return &Bundle{Tenant: *t, Company: company, Branches: branches}, nil
}

// --- company ---

// CompanyInput carries the client-editable company fields. ID is optional:
// empty mints a new GUID; a value adopts an existing local GUID (subscribe
// flow of a standalone install).
type CompanyInput struct {
	ID        string
	Name      string
	Phone     string
	Address   string
	TaxNumber string
}

// SetCompany creates or updates the tenant's single company (D15: one company
// per tenant). Updates always target the existing company; supplying a
// different GUID once one exists is rejected.
func (s *Service) SetCompany(ctx context.Context, accountID, tenantID string, in CompanyInput) (*model.Company, error) {
	if _, err := s.owned(ctx, accountID, tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("company name required")
	}

	now := time.Now().UTC()
	c := &model.Company{
		TenantID: tenantID,
		Name:     strings.TrimSpace(in.Name), Phone: in.Phone, Address: in.Address, TaxNumber: in.TaxNumber,
		CreatedAt: now, UpdatedAt: now,
	}

	existing, err := s.store.CompanyByTenant(ctx, tenantID)
	switch {
	case err == nil:
		// Update in place; a different explicit GUID would be a second company.
		if in.ID != "" {
			id, gerr := normalizeGUID(in.ID)
			if gerr != nil {
				return nil, gerr
			}
			if id != existing.ID {
				return nil, ErrCompanyExists
			}
		}
		c.ID, c.CreatedAt = existing.ID, existing.CreatedAt
	case errors.Is(err, mongostore.ErrNotFound):
		// First registration: mint a GUID or adopt the supplied local one.
		if c.ID, err = normalizeGUID(in.ID); err != nil {
			return nil, err
		}
		if other, oerr := s.store.CompanyByID(ctx, c.ID); oerr == nil && other.TenantID != tenantID {
			return nil, ErrForbidden
		}
	default:
		return nil, err
	}

	if err := s.store.UpsertCompany(ctx, c); err != nil {
		if mongostore.IsDuplicateKey(err) {
			return nil, ErrCompanyExists
		}
		return nil, err
	}
	return c, nil
}

// --- branches ---

// BranchInput carries the fields for creating a branch. ID is optional (same
// mint-or-adopt rule as CompanyInput). CompanyID is optional — the tenant has
// exactly one company (D15); when supplied it must match. Seats defaults to 1.
type BranchInput struct {
	ID        string
	CompanyID string
	Name      string
	Phone1    string // required on the POS branch (printed on receipts); validated client-side
	Phone2    string
	Phone3    string
	Address   string // required on the POS branch (printed on receipts)
	Seats     int
}

// AddBranch creates a branch under the tenant's company (a licensing event).
func (s *Service) AddBranch(ctx context.Context, accountID, tenantID string, in BranchInput) (*model.Branch, error) {
	if _, err := s.owned(ctx, accountID, tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("branch name required")
	}
	company, err := s.store.CompanyByTenant(ctx, tenantID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil, ErrNoCompany
	}
	if err != nil {
		return nil, err
	}
	if in.CompanyID != "" && in.CompanyID != company.ID {
		return nil, ErrForbidden
	}
	id, err := normalizeGUID(in.ID)
	if err != nil {
		return nil, err
	}
	if existing, err := s.store.BranchByID(ctx, id); err == nil && existing.TenantID != tenantID {
		return nil, ErrForbidden
	}
	seats := in.Seats
	if seats <= 0 {
		seats = 1
	}
	now := time.Now().UTC()
	b := &model.Branch{
		ID: id, TenantID: tenantID, CompanyID: company.ID,
		Name:    strings.TrimSpace(in.Name),
		Phone1:  strings.TrimSpace(in.Phone1),
		Phone2:  strings.TrimSpace(in.Phone2),
		Phone3:  strings.TrimSpace(in.Phone3),
		Address: strings.TrimSpace(in.Address),
		Seats:   seats, Status: model.BranchActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.UpsertBranch(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}

// SetBranchContact updates a branch's contact fields (address/phones). Empty
// strings are treated as "leave unchanged" so a PATCH can touch just one field.
func (s *Service) SetBranchContact(ctx context.Context, accountID, tenantID, branchID string, in BranchInput) error {
	b, err := s.ownedBranch(ctx, accountID, tenantID, branchID)
	if err != nil {
		return err
	}
	if v := strings.TrimSpace(in.Phone1); v != "" {
		b.Phone1 = v
	}
	if v := strings.TrimSpace(in.Phone2); v != "" {
		b.Phone2 = v
	}
	if v := strings.TrimSpace(in.Phone3); v != "" {
		b.Phone3 = v
	}
	if v := strings.TrimSpace(in.Address); v != "" {
		b.Address = v
	}
	b.UpdatedAt = time.Now().UTC()
	return s.store.UpsertBranch(ctx, b)
}

// RenameBranch changes a branch's display name.
func (s *Service) RenameBranch(ctx context.Context, accountID, tenantID, branchID, name string) error {
	b, err := s.ownedBranch(ctx, accountID, tenantID, branchID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("branch name required")
	}
	b.Name = strings.TrimSpace(name)
	b.UpdatedAt = time.Now().UTC()
	return s.store.UpsertBranch(ctx, b)
}

// SetBranchStatus activates/deactivates a branch (a licensing event).
func (s *Service) SetBranchStatus(ctx context.Context, accountID, tenantID, branchID string, status model.BranchStatus) error {
	if _, err := s.ownedBranch(ctx, accountID, tenantID, branchID); err != nil {
		return err
	}
	return s.store.SetBranchStatus(ctx, branchID, status, time.Now().UTC())
}

// SetBranchSeats changes a branch's seat limit (admin/billing operation; the
// HTTP layer restricts who may call it).
func (s *Service) SetBranchSeats(ctx context.Context, branchID string, seats int) error {
	if seats < 1 {
		return errors.New("seats must be >= 1")
	}
	return s.store.SetBranchSeats(ctx, branchID, seats, time.Now().UTC())
}

// --- device seats ---

// BindDevice attaches a machine to a branch seat, enforcing the seat limit.
// Rebinding the same machine is idempotent and returns the existing binding.
func (s *Service) BindDevice(ctx context.Context, accountID, tenantID, branchID, machineID, machineName, osName string) (*model.BranchDevice, error) {
	if machineID == "" {
		return nil, errors.New("machine id required")
	}
	if _, err := s.activeTenant(ctx, accountID, tenantID); err != nil {
		return nil, err
	}
	b, err := s.ownedBranch(ctx, accountID, tenantID, branchID)
	if err != nil {
		return nil, err
	}
	if b.Status != model.BranchActive {
		return nil, ErrBranchInactive
	}

	// Idempotent activation.
	if d, err := s.store.ActiveBranchDeviceForMachine(ctx, branchID, machineID); err == nil {
		return d, nil
	}

	n, err := s.store.CountActiveBranchDevices(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if int(n) >= b.Seats {
		return nil, ErrSeatLimit
	}

	now := time.Now().UTC()
	d := &model.BranchDevice{
		ID: idgen.New("bdv"), TenantID: tenantID, BranchID: branchID,
		MachineID: machineID, MachineName: machineName, OS: osName,
		Status: model.DeviceActive, BoundAt: now, LastSeenAt: now,
	}
	if err := s.store.InsertBranchDevice(ctx, d); err != nil {
		if mongostore.IsDuplicateKey(err) {
			// Same machine bound concurrently; return that binding.
			return s.store.ActiveBranchDeviceForMachine(ctx, branchID, machineID)
		}
		return nil, err
	}

	// The count-then-insert pair is racy under concurrent binds of different
	// machines; recount and roll back the overshoot.
	n, err = s.store.CountActiveBranchDevices(ctx, branchID)
	if err == nil && int(n) > b.Seats {
		_ = s.store.ReleaseBranchDevice(ctx, d.ID, time.Now().UTC())
		return nil, ErrSeatLimit
	}
	return d, nil
}

// ReleaseDevice frees a branch seat.
func (s *Service) ReleaseDevice(ctx context.Context, accountID, tenantID, deviceID string) error {
	d, err := s.store.BranchDeviceByID(ctx, deviceID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return ErrNotBound
	}
	if err != nil {
		return err
	}
	if d.TenantID != tenantID {
		return ErrForbidden
	}
	if _, err := s.owned(ctx, accountID, tenantID); err != nil {
		return err
	}
	if d.Status != model.DeviceActive {
		return nil // already released; idempotent
	}
	return s.store.ReleaseBranchDevice(ctx, d.ID, time.Now().UTC())
}

// --- sync tokens ---

// IssuedSyncToken is the result of IssueSyncToken: the signed JWT, its
// claims, and the gateway the device must sync against.
type IssuedSyncToken struct {
	Token      string
	Claims     *SyncClaims
	GatewayURL string
}

// IssueSyncToken mints the JWT a bound device presents to the DMS gateway
// (RS256; the gateway verifies with the public key only). Requires an active
// tenant with a provisioned central DB, an active branch, and an active device
// binding owned by the caller.
func (s *Service) IssueSyncToken(ctx context.Context, accountID, tenantID, deviceID string) (*IssuedSyncToken, error) {
	t, err := s.activeTenant(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if t.DBName == "" {
		return nil, ErrNotSubscribed
	}
	d, err := s.store.BranchDeviceByID(ctx, deviceID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil, ErrNotBound
	}
	if err != nil {
		return nil, err
	}
	if d.TenantID != tenantID {
		return nil, ErrForbidden
	}
	if d.Status != model.DeviceActive {
		return nil, ErrNotBound
	}
	b, err := s.store.BranchByID(ctx, d.BranchID)
	if err != nil {
		return nil, err
	}
	if b.Status != model.BranchActive {
		return nil, ErrBranchInactive
	}

	// Resolve the tenant's shard. For legacy tenants provisioned before sharding,
	// assign one now (same least-loaded path as ProvisionSync).
	shard, err := s.resolveShard(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("resolve shard: %w", err)
	}

	now := time.Now().UTC()
	claims := &SyncClaims{
		TenantID: tenantID,
		BranchID: d.BranchID,
		DeviceID: d.ID,
		DBName:   t.DBName,
		ShardID:  shard.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   d.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.syncTTL)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(s.syncKey)
	if err != nil {
		return nil, err
	}
	_ = s.store.TouchBranchDeviceSeen(ctx, d.ID, now)
	return &IssuedSyncToken{Token: tok, Claims: claims, GatewayURL: shard.GatewayURL}, nil
}

// OpsClaims is the JWT the fleet-rollout orchestrator presents to the gateway's
// /admin endpoints (roadmap E3). It carries no tenant/branch binding
// — only an "ops" scope — and is signed/verified with the same RS256 key as
// sync tokens, so only the license server can mint one.
type OpsClaims struct {
	Scope string `json:"scope"`
	jwt.RegisteredClaims
}

// IssueOpsToken mints a short-lived ops token authorising the gateway's
// migrate/schema endpoints during a fleet schema rollout.
func (s *Service) IssueOpsToken() (string, error) {
	now := time.Now().UTC()
	claims := &OpsClaims{
		Scope: "ops",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(s.syncKey)
}

// VerifySyncToken parses and verifies a sync token this server minted
// (IssueSyncToken, RS256). The gateway forwards the client's own sync token to
// the internal registry endpoint as proof it is serving that tenant; we trust
// our own signature and return the claims. (E5/D18)
func (s *Service) VerifySyncToken(tokenStr string) (*SyncClaims, error) {
	claims := &SyncClaims{}
	if _, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return &s.syncKey.PublicKey, nil
	}); err != nil {
		return nil, err
	}
	if claims.TenantID == "" {
		return nil, errors.New("sync token missing tenant")
	}
	return claims, nil
}

// RecordSyncCompleted stamps a branch's last completed sync round, called by
// the gateway's fire-and-forget callback after each successful /sync round.
// Authorised by a valid sync token (the caller passes its verified claims'
// tenant/branch), so the branch must belong to that tenant. Returns the
// recorded time. (The SSE bus for live console updates hooks in here later.)
func (s *Service) RecordSyncCompleted(ctx context.Context, tenantID, branchID string) (time.Time, error) {
	b, err := s.store.BranchByID(ctx, branchID)
	if err != nil {
		return time.Time{}, err
	}
	if b.TenantID != tenantID {
		return time.Time{}, ErrForbidden
	}
	now := time.Now().UTC()
	if err := s.store.SetBranchLastSync(ctx, branchID, now); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

// TenantRegistry returns a tenant's company (may be nil) and branches so the
// gateway can materialise them as FK anchors in the central DB (E5/D18).
// Authorised by a valid sync token, not account ownership.
func (s *Service) TenantRegistry(ctx context.Context, tenantID string) (*model.Company, []model.Branch, error) {
	company, err := s.store.CompanyByTenant(ctx, tenantID)
	if err != nil && !errors.Is(err, mongostore.ErrNotFound) {
		return nil, nil, err
	}
	branches, err := s.store.BranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	return company, branches, nil
}

// SyncPublicKeyPEM returns the PKIX PEM of the sync-token verification key —
// what the gateway needs to validate tokens offline.
func (s *Service) SyncPublicKeyPEM() (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&s.syncKey.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// ParseSyncToken validates a sync token (used in tests; gateways verify with
// the public key on their side).
func (s *Service) ParseSyncToken(token string) (*SyncClaims, error) {
	var claims SyncClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return &s.syncKey.PublicKey, nil
	})
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// --- sync provisioning (admin/billing; HTTP layer restricts callers) ---

// ProvisionSync subscribes a tenant to sync by assigning its central DB and a
// shard. The DB name is derived deterministically from the tenant id; calling it
// again is idempotent (same derived name, same shard if already assigned). The
// gateway lazily creates and migrates the DB on the tenant's first sync.
func (s *Service) ProvisionSync(ctx context.Context, tenantID string) (*model.Tenant, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	// Assign shard before setting db_name so the tenant never appears in
	// TenantsWithSync (which filters on db_name != "") without a shard. A
	// rollout running between the two writes would otherwise see the tenant
	// with an empty ShardID and silently skip it.
	if t.ShardID == "" {
		shard, err := s.store.LeastLoadedShard(ctx)
		if err != nil {
			return nil, fmt.Errorf("assign shard: %w", err)
		}
		if err := s.store.SetTenantShard(ctx, tenantID, shard.ID, now); err != nil {
			return nil, err
		}
		t.ShardID = shard.ID
	}

	dbName := "arib_" + strings.ToLower(strings.TrimPrefix(tenantID, "tnt_"))
	if err := s.store.SetTenantDBName(ctx, tenantID, dbName, now); err != nil {
		return nil, err
	}
	t.DBName = dbName
	return t, nil
}

// DeletionResult summarizes what DeleteTenant tore down, for the admin response.
type DeletionResult struct {
	TenantID        string `json:"tenant_id"`
	BranchesDeleted int64  `json:"branches_deleted"`
	DevicesDeleted  int64  `json:"devices_deleted"`
	CompanyDeleted  bool   `json:"company_deleted"`
	DBDropped       bool   `json:"db_dropped"`
}

// DeleteTenant permanently removes a tenant and everything under it: its
// central DB on the gateway (if sync-provisioned), branch-device seat
// bindings, branches, company and the tenant record itself. Admin/billing
// operation; the HTTP layer restricts who may call it.
//
// The central DB is dropped first — if the gateway call fails, nothing else
// is deleted, so a retry after the gateway recovers is safe. A tenant whose
// shard has since been decommissioned has its DB drop skipped (unreachable
// via the registry); the operator must clean it up out of band.
func (s *Service) DeleteTenant(ctx context.Context, actor, tenantID string) (*DeletionResult, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	res := &DeletionResult{TenantID: tenantID}

	if t.DBName != "" && t.ShardID != "" {
		shard, err := s.store.ShardByID(ctx, t.ShardID)
		if err != nil && !errors.Is(err, mongostore.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			if err := s.dropCentralDB(ctx, shard.GatewayURL, t.DBName); err != nil {
				return nil, fmt.Errorf("drop central db: %w", err)
			}
			res.DBDropped = true
		}
	}

	devCount, err := s.store.DeleteBranchDevicesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	res.DevicesDeleted = devCount

	branchCount, err := s.store.DeleteBranchesByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	res.BranchesDeleted = branchCount

	companyDeleted, err := s.store.DeleteCompanyByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	res.CompanyDeleted = companyDeleted

	if err := s.store.DeleteTenant(ctx, tenantID); err != nil {
		return nil, err
	}

	_ = s.store.InsertAudit(ctx, &model.AuditLog{
		ID:     idgen.New("aud"),
		Actor:  actor,
		Action: "delete_tenant",
		Target: tenantID,
		Meta: map[string]any{
			"db_name":          t.DBName,
			"shard_id":         t.ShardID,
			"db_dropped":       res.DBDropped,
			"branches_deleted": res.BranchesDeleted,
			"devices_deleted":  res.DevicesDeleted,
			"company_deleted":  res.CompanyDeleted,
		},
		CreatedAt: time.Now().UTC(),
	})
	return res, nil
}

// dropCentralDB asks the gateway to drop a tenant's central DB (the teardown
// counterpart of rollout.Service's migrate call).
func (s *Service) dropCentralDB(ctx context.Context, gateway, dbName string) error {
	tok, err := s.IssueOpsToken()
	if err != nil {
		return fmt.Errorf("mint ops token: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"db_name": dbName})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, gateway+"/admin/db", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || !body.OK {
		if body.Error != "" {
			return fmt.Errorf("%s", body.Error)
		}
		return fmt.Errorf("gateway status %d", resp.StatusCode)
	}
	return nil
}

// resolveShard returns the tenant's assigned shard, assigning a least-loaded one
// on the fly for legacy tenants that predate sharding or tenants whose shard has
// been decommissioned/deleted.
func (s *Service) resolveShard(ctx context.Context, t *model.Tenant) (*model.Shard, error) {
	if t.ShardID != "" {
		shard, err := s.store.ShardByID(ctx, t.ShardID)
		if err == nil {
			return shard, nil
		}
		if !errors.Is(err, mongostore.ErrNotFound) {
			return nil, err
		}
		// Shard was decommissioned; force-assign to a live shard (ops-time event,
		// not steady-state, so an unconditional write is acceptable here).
		shard, err = s.store.LeastLoadedShard(ctx)
		if err != nil {
			return nil, err
		}
		if err := s.store.SetTenantShard(ctx, t.ID, shard.ID, time.Now().UTC()); err != nil {
			return nil, err
		}
		t.ShardID = shard.ID
		return shard, nil
	}
	// Legacy tenant (no shard assigned yet). Use a DB-level CAS so that two
	// concurrent IssueSyncToken calls for the same tenant both end up with the
	// same shard_id — the loser re-reads and returns the winner's choice.
	shard, err := s.store.LeastLoadedShard(ctx)
	if err != nil {
		return nil, err
	}
	assigned, err := s.store.AssignShardIfEmpty(ctx, t.ID, shard.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if assigned {
		t.ShardID = shard.ID
		return shard, nil
	}
	// Lost the CAS race — another concurrent call already wrote a shard.
	// Re-fetch to get the actual assignment and return that shard.
	fresh, err := s.store.TenantByID(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.ShardID = fresh.ShardID
	return s.store.ShardByID(ctx, fresh.ShardID)
}

// --- helpers ---

func (s *Service) owned(ctx context.Context, accountID, tenantID string) (*model.Tenant, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if t.AccountID != accountID {
		return nil, ErrForbidden
	}
	return t, nil
}

func (s *Service) activeTenant(ctx context.Context, accountID, tenantID string) (*model.Tenant, error) {
	t, err := s.owned(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if t.Status != model.TenantActive {
		return nil, ErrTenantSuspended
	}
	return t, nil
}

func (s *Service) ownedBranch(ctx context.Context, accountID, tenantID, branchID string) (*model.Branch, error) {
	if _, err := s.owned(ctx, accountID, tenantID); err != nil {
		return nil, err
	}
	b, err := s.store.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if b.TenantID != tenantID {
		return nil, ErrForbidden
	}
	return b, nil
}

// normalizeGUID lowercases and validates a client-supplied GUID, or mints a
// new GUIDv7 when empty (matching the POS app's Guid.CreateVersion7 ids).
func normalizeGUID(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		u, err := uuid.NewV7()
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}
	u, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return "", fmt.Errorf("invalid guid: %w", err)
	}
	return u.String(), nil
}
