package billing

import (
	"context"
	"errors"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// Service errors surfaced to the admin HTTP layer.
var (
	ErrInvalidAmount      = errors.New("amount must be greater than zero")
	ErrInvalidPeriod      = errors.New("ends_at must be after starts_at")
	ErrVoidReasonRequired = errors.New("void reason is required")
	ErrBillNotPaid        = errors.New("bill is not paid")
	ErrNotFound           = mongostore.ErrNotFound
)

const defaultCurrency = "EGP"

// Provisioner is the one tenant.Service method billing needs: assigning a
// central DB the first time a tenant gets a paid bill. Declared here instead
// of importing package tenant directly — tenant depends on billing for
// Derive/SyncAllowed (the sync-token gate), so billing importing tenant back
// would be a cycle. main.go wires the real *tenant.Service in, which already
// satisfies this signature.
type Provisioner interface {
	ProvisionSync(ctx context.Context, tenantID string) (*model.Tenant, error)
}

// Service creates and voids bills and reports derived subscription summaries.
type Service struct {
	store       *mongostore.Store
	provisioner Provisioner
}

// New builds a billing Service.
func New(store *mongostore.Store, provisioner Provisioner) *Service {
	return &Service{store: store, provisioner: provisioner}
}

// CreateResult is what Create reports back to the admin caller: the bill
// just recorded, whether this call auto-provisioned the tenant's central DB,
// and the tenant's subscription summary immediately after.
type CreateResult struct {
	Bill         *model.Bill
	Provisioned  bool   // true if this call provisioned the tenant's central DB
	ProvisionErr string // set if provisioning was attempted and failed
	Summary      Summary
}

// Create records a paid bill for a tenant. Currency defaults to
// defaultCurrency when empty. If the tenant has no central DB yet, it is
// auto-provisioned; a provisioning failure does not roll back the bill — the
// bill is a record of money already received — the caller sees
// CreateResult.ProvisionErr and the admin UI falls back to the existing
// manual "Provision sync" action.
func (s *Service) Create(ctx context.Context, tenantID string, amount int64, currency string, startsAt, endsAt time.Time, notes, createdBy string) (*CreateResult, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if !endsAt.After(startsAt) {
		return nil, ErrInvalidPeriod
	}
	if currency == "" {
		currency = defaultCurrency
	}

	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	b := &model.Bill{
		ID:        idgen.New("bil"),
		TenantID:  tenantID,
		Amount:    amount,
		Currency:  currency,
		StartsAt:  startsAt.UTC(),
		EndsAt:    endsAt.UTC(),
		Status:    model.BillPaid,
		Notes:     notes,
		CreatedBy: createdBy,
		Source:    "manual_admin",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.InsertBill(ctx, b); err != nil {
		return nil, err
	}
	s.audit(ctx, createdBy, "bill.create", b.ID, map[string]any{
		"tenant_id": tenantID, "amount": amount, "currency": currency,
		"starts_at": b.StartsAt, "ends_at": b.EndsAt,
	})

	res := &CreateResult{Bill: b}
	if t.DBName == "" {
		if _, err := s.provisioner.ProvisionSync(ctx, tenantID); err != nil {
			res.ProvisionErr = err.Error()
		} else {
			res.Provisioned = true
		}
	}

	bills, err := s.store.BillsByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	res.Summary = Derive(bills, now)
	return res, nil
}

// Void marks a bill void, recording who and why. Bills are append-only —
// this never deletes the document. Voiding a tenant's covering bill
// downgrades its derived subscription state on the next read; that is the
// intended effect of correcting a mis-entered bill.
func (s *Service) Void(ctx context.Context, billID, reason, actor string) error {
	if reason == "" {
		return ErrVoidReasonRequired
	}
	b, err := s.store.BillByID(ctx, billID)
	if err != nil {
		return err
	}
	if b.Status != model.BillPaid {
		return ErrBillNotPaid
	}
	if err := s.store.VoidBill(ctx, billID, reason, time.Now().UTC()); err != nil {
		return err
	}
	s.audit(ctx, actor, "bill.void", billID, map[string]any{
		"tenant_id": b.TenantID, "reason": reason,
	})
	return nil
}

// ListWithSummary returns every bill for a tenant (newest period first) and
// the tenant's current derived subscription Summary.
func (s *Service) ListWithSummary(ctx context.Context, tenantID string) ([]model.Bill, Summary, error) {
	bills, err := s.store.BillsByTenant(ctx, tenantID)
	if err != nil {
		return nil, Summary{}, err
	}
	return bills, Derive(bills, time.Now().UTC()), nil
}

func (s *Service) audit(ctx context.Context, actor, action, target string, meta map[string]any) {
	_ = s.store.InsertAudit(ctx, &model.AuditLog{
		ID:        idgen.New("aud"),
		Actor:     actor,
		Action:    action,
		Target:    target,
		Meta:      meta,
		CreatedAt: time.Now().UTC(),
	})
}
