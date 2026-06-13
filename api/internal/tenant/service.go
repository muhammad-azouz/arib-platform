// Package tenant implements the multi-tenant registry: tenant registration,
// company/branch management, per-branch device seats and sync-token issuance.
package tenant

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
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
	ErrNotSubscribed   = errors.New("tenant has no sync subscription (no shard assigned)")
	ErrShardFull       = errors.New("shard is full or not accepting tenants")
	ErrCompanyExists   = errors.New("tenant already has a company (one company per tenant)")
	ErrNoCompany       = errors.New("tenant has no company yet")
	ErrNotFound        = mongostore.ErrNotFound
)

// Service coordinates the registry store and the sync-token signer.
type Service struct {
	store   *mongostore.Store
	syncKey *rsa.PrivateKey
	syncTTL time.Duration
}

// New builds a tenant Service. syncKey signs sync tokens (RS256 — gateways
// hold only the public key, so a compromised shard cannot mint tokens);
// syncTTL is their lifetime.
func New(store *mongostore.Store, syncKey *rsa.PrivateKey, syncTTL time.Duration) *Service {
	return &Service{store: store, syncKey: syncKey, syncTTL: syncTTL}
}

// Bundle is everything the app needs at activation/login: the tenant plus its
// cloud-authoritative company (exactly one per tenant, D15) and branches.
type Bundle struct {
	Tenant   model.Tenant
	Company  *model.Company // nil until the company is registered
	Branches []model.Branch
}

// SyncClaims is the JWT a bound device presents to a shard's DMS gateway.
type SyncClaims struct {
	TenantID string `json:"tenant_id"`
	BranchID string `json:"branch_id"`
	DeviceID string `json:"device_id"`
	ShardID  string `json:"shard_id"`
	DBName   string `json:"db_name"`
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
		Name: strings.TrimSpace(in.Name), Seats: seats, Status: model.BranchActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.UpsertBranch(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
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
// claims, and the shard gateway the device must sync against.
type IssuedSyncToken struct {
	Token      string
	Claims     *SyncClaims
	GatewayURL string
}

// IssueSyncToken mints the JWT a bound device presents to its shard's DMS
// gateway (RS256; gateways verify with the public key only). Requires an
// active tenant with a shard assignment, an active branch, and an active
// device binding owned by the caller.
func (s *Service) IssueSyncToken(ctx context.Context, accountID, tenantID, deviceID string) (*IssuedSyncToken, error) {
	t, err := s.activeTenant(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if t.ShardID == "" || t.DBName == "" {
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
	sh, err := s.store.ShardByID(ctx, t.ShardID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	claims := &SyncClaims{
		TenantID: tenantID,
		BranchID: d.BranchID,
		DeviceID: d.ID,
		ShardID:  t.ShardID,
		DBName:   t.DBName,
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
	return &IssuedSyncToken{Token: tok, Claims: claims, GatewayURL: sh.GatewayURL}, nil
}

// OpsClaims is the JWT the fleet-rollout orchestrator presents to a shard
// gateway's /admin endpoints (roadmap E3). It carries no tenant/branch binding
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

// SyncPublicKeyPEM returns the PKIX PEM of the sync-token verification key —
// what a shard gateway needs to validate tokens offline.
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

// --- shards (admin/billing; HTTP layer restricts callers) ---

// CreateShard registers a central VPS.
func (s *Service) CreateShard(ctx context.Context, name, host, gatewayURL string, maxTenants int) (*model.Shard, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(gatewayURL) == "" {
		return nil, errors.New("shard name and gateway url required")
	}
	if maxTenants < 1 {
		return nil, errors.New("max tenants must be >= 1")
	}
	now := time.Now().UTC()
	sh := &model.Shard{
		ID: idgen.New("shd"), Name: name, Host: host, GatewayURL: gatewayURL,
		MaxTenants: maxTenants, Status: model.ShardActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.InsertShard(ctx, sh); err != nil {
		return nil, err
	}
	return sh, nil
}

// Shards lists all shards.
func (s *Service) Shards(ctx context.Context) ([]model.Shard, error) {
	return s.store.ListShards(ctx)
}

// AssignShard places a tenant's central DB on a shard (sync provisioning).
// The DB name is derived deterministically from the tenant id.
func (s *Service) AssignShard(ctx context.Context, tenantID, shardID string) (*model.Tenant, error) {
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	sh, err := s.store.ShardByID(ctx, shardID)
	if err != nil {
		return nil, err
	}
	if sh.Status != model.ShardActive {
		return nil, ErrShardFull
	}
	n, err := s.store.CountTenantsOnShard(ctx, shardID)
	if err != nil {
		return nil, err
	}
	if int(n) >= sh.MaxTenants {
		return nil, ErrShardFull
	}
	dbName := "arib_" + strings.ToLower(strings.TrimPrefix(tenantID, "tnt_"))
	if err := s.store.AssignTenantShard(ctx, tenantID, shardID, dbName, time.Now().UTC()); err != nil {
		return nil, err
	}
	t.ShardID, t.DBName = shardID, dbName
	return t, nil
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
