// Package admin implements operator workflows: managing clients, licenses and
// device bindings, and minting offline fallback licenses.
package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/license"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Service implements admin operations.
type Service struct {
	store    *mongostore.Store
	licenses *license.Service
}

// New builds the admin Service.
func New(store *mongostore.Store, licenses *license.Service) *Service {
	return &Service{store: store, licenses: licenses}
}

// ClientView bundles an account with its licenses and devices for the dashboard.
type ClientView struct {
	Account  *model.Account  `json:"account"`
	Licenses []model.License `json:"licenses"`
	Devices  []model.Device  `json:"devices"`
}

// FindOrCreateClient looks up an account by email, creating it if missing.
// Admin-created accounts do not receive an automatic trial.
func (s *Service) FindOrCreateClient(ctx context.Context, email, first, last, notes string) (*model.Account, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, errors.New("invalid email")
	}
	acc, err := s.store.AccountByEmail(ctx, email)
	if err == nil {
		return acc, nil
	}
	if !errors.Is(err, mongostore.ErrNotFound) {
		return nil, err
	}
	now := time.Now().UTC()
	acc = &model.Account{
		ID:          idgen.New("acc"),
		Email:       email,
		FirstName:   strings.TrimSpace(first),
		LastName:    strings.TrimSpace(last),
		Providers:   []model.Provider{},
		ProviderIDs: map[string]string{},
		Notes:       notes,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.InsertAccount(ctx, acc); err != nil {
		return nil, err
	}
	return acc, nil
}

// UpdateClient edits a client's name and admin notes after creation.
func (s *Service) UpdateClient(ctx context.Context, adminEmail, accountID, first, last, notes string) (*model.Account, error) {
	acc, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	acc.FirstName = strings.TrimSpace(first)
	acc.LastName = strings.TrimSpace(last)
	acc.Notes = strings.TrimSpace(notes)
	if err := s.store.UpdateAccount(ctx, acc); err != nil {
		return nil, err
	}
	s.audit(ctx, adminEmail, "update_client", accountID, nil)
	return acc, nil
}

// Stats holds the overview counts shown on the dashboard home.
type Stats struct {
	Clients             int64 `json:"clients"`
	LicensesActive      int64 `json:"licenses_active"`
	LicensesSuspended   int64 `json:"licenses_suspended"`
	LicensesTrial       int64 `json:"licenses_trial"`
	LicensesPaid        int64 `json:"licenses_paid"`
	DevicesActive       int64 `json:"devices_active"`
	LicensesExpiring30d int64 `json:"licenses_expiring_30d"`
}

// Stats computes the dashboard overview counts.
func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	count := func(coll *mongo.Collection, filter bson.D) (int64, error) {
		return coll.CountDocuments(ctx, filter)
	}
	out := &Stats{}
	var err error
	if out.Clients, err = count(s.store.Accounts, bson.D{}); err != nil {
		return nil, err
	}
	if out.LicensesActive, err = count(s.store.Licenses, bson.D{{Key: "status", Value: model.LicenseActive}}); err != nil {
		return nil, err
	}
	if out.LicensesSuspended, err = count(s.store.Licenses, bson.D{{Key: "status", Value: model.LicenseSuspended}}); err != nil {
		return nil, err
	}
	if out.LicensesTrial, err = count(s.store.Licenses, bson.D{{Key: "type", Value: model.LicenseTrial}}); err != nil {
		return nil, err
	}
	if out.LicensesPaid, err = count(s.store.Licenses, bson.D{{Key: "type", Value: model.LicensePaid}}); err != nil {
		return nil, err
	}
	if out.DevicesActive, err = count(s.store.Devices, bson.D{{Key: "status", Value: model.DeviceActive}}); err != nil {
		return nil, err
	}
	soon := time.Now().UTC().Add(30 * 24 * time.Hour)
	out.LicensesExpiring30d, err = count(s.store.Licenses, bson.D{
		{Key: "status", Value: model.LicenseActive},
		{Key: "expires_at", Value: bson.D{{Key: "$lte", Value: soon}}},
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AssignLicenses creates n paid license seats for a client (by email).
func (s *Service) AssignLicenses(ctx context.Context, email, features string, expiresAt time.Time, count int, adminEmail, notes string) ([]model.License, error) {
	if count < 1 {
		count = 1
	}
	acc, err := s.FindOrCreateClient(ctx, email, "", "", "")
	if err != nil {
		return nil, err
	}
	out := make([]model.License, 0, count)
	for i := 0; i < count; i++ {
		l, err := s.licenses.CreatePaid(ctx, acc.ID, features, expiresAt, adminEmail, notes)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	s.audit(ctx, adminEmail, "assign_licenses", acc.ID, map[string]any{"count": count, "features": features})
	return out, nil
}

// SetLicenseStatus suspends or reactivates a license.
func (s *Service) SetLicenseStatus(ctx context.Context, adminEmail, licenseID string, status model.LicenseStatus) error {
	if err := s.store.SetLicenseStatus(ctx, licenseID, status); err != nil {
		return err
	}
	s.audit(ctx, adminEmail, "set_license_status", licenseID, map[string]any{"status": status})
	return nil
}

// ForceRelease releases any device binding regardless of cooldown.
func (s *Service) ForceRelease(ctx context.Context, adminEmail, deviceID string) error {
	dev, err := s.store.DeviceByID(ctx, deviceID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return mongostore.ErrNotFound
	}
	if err != nil {
		return err
	}
	if dev.Status != model.DeviceActive {
		return nil
	}
	if err := s.store.ReleaseDevice(ctx, deviceID, time.Now().UTC(), false); err != nil {
		return err
	}
	s.audit(ctx, adminEmail, "force_release", deviceID, nil)
	return nil
}

// GetClient returns a client's full record for the dashboard.
func (s *Service) GetClient(ctx context.Context, accountID string) (*ClientView, error) {
	acc, err := s.store.AccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	lics, err := s.store.LicensesByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	devs, err := s.store.DevicesByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// Ensure slices serialize as [] not null when empty.
	if lics == nil {
		lics = []model.License{}
	}
	if devs == nil {
		devs = []model.Device{}
	}
	return &ClientView{Account: acc, Licenses: lics, Devices: devs}, nil
}

// SearchClients lists accounts matching a query.
func (s *Service) SearchClients(ctx context.Context, q string) ([]model.Account, error) {
	return s.store.SearchAccounts(ctx, q, 100)
}

// SignOffline mints an offline fallback license string for a given license and
// machine, used by the hidden manual-entry screen in the POS app.
func (s *Service) SignOffline(ctx context.Context, adminEmail, licenseID, machineID string) (string, error) {
	l, err := s.store.LicenseByID(ctx, licenseID)
	if err != nil {
		return "", err
	}
	tok, err := s.licenses.SignOffline(machineID, l.Features, l.ExpiresAt, l.ID)
	if err != nil {
		return "", err
	}
	s.audit(ctx, adminEmail, "sign_offline", licenseID, map[string]any{"machine_id": machineID})
	return tok, nil
}

// ListAudit returns recent audit entries, newest first.
func (s *Service) ListAudit(ctx context.Context, limit int64) ([]model.AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	cur, err := s.store.Audit.Find(ctx, bson.D{},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	var out []model.AuditLog
	return out, cur.All(ctx, &out)
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
