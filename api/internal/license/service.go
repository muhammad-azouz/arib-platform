// Package license provisions licenses and mints signed device tokens.
package license

import (
	"context"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/aribpos/license-api/pkg/licensetoken"
)

// Clocks configures token validity windows.
type Clocks struct {
	RevalidateAfter time.Duration // -> token.revalidateBy
	HardExpireAfter time.Duration // -> token.hardExpiry
	TrialDuration   time.Duration
}

// Service issues licenses and signs tokens.
type Service struct {
	store  *mongostore.Store
	signer *licensetoken.Signer
	clocks Clocks
}

// New builds a license Service.
func New(store *mongostore.Store, signer *licensetoken.Signer, clocks Clocks) *Service {
	return &Service{store: store, signer: signer, clocks: clocks}
}

// CreateTrial provisions the one-per-account 7-day trial license created at signup.
func (s *Service) CreateTrial(ctx context.Context, accountID string) (*model.License, error) {
	now := time.Now().UTC()
	l := &model.License{
		ID:        idgen.New("lic"),
		Key:       idgen.LicenseKey(),
		AccountID: accountID,
		Type:      model.LicenseTrial,
		Features:  "Trial",
		Status:    model.LicenseActive,
		ExpiresAt: now.Add(s.clocks.TrialDuration),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.InsertLicense(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// CreatePaid provisions an admin-assigned license for an account.
func (s *Service) CreatePaid(ctx context.Context, accountID, features string, expiresAt time.Time, assignedBy, notes string) (*model.License, error) {
	now := time.Now().UTC()
	l := &model.License{
		ID:         idgen.New("lic"),
		Key:        idgen.LicenseKey(),
		AccountID:  accountID,
		Type:       model.LicensePaid,
		Features:   features,
		Status:     model.LicenseActive,
		ExpiresAt:  expiresAt.UTC(),
		AssignedBy: assignedBy,
		Notes:      notes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.store.InsertLicense(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// TokenFor mints a signed token binding a license to a machine, with the
// revalidate/hard-expiry clocks clamped to the license's own expiry.
func (s *Service) TokenFor(l *model.License, machineID string) (string, time.Time, time.Time, error) {
	now := time.Now().UTC()
	reval := earliest(now.Add(s.clocks.RevalidateAfter), l.ExpiresAt)
	hard := earliest(now.Add(s.clocks.HardExpireAfter), l.ExpiresAt)
	tok, err := s.signer.Sign(licensetoken.Payload{
		MachineID:    machineID,
		Features:     l.Features,
		HardExpiry:   hard,
		RevalidateBy: reval,
		LicenseID:    l.ID,
	})
	return tok, reval, hard, err
}

// SignOffline mints a fully-offline token (no revalidation expected) for the
// hidden manual-entry fallback. Both clocks are set to expiry.
func (s *Service) SignOffline(machineID, features string, expiry time.Time, licenseID string) (string, error) {
	return s.signer.Sign(licensetoken.Payload{
		MachineID:    machineID,
		Features:     features,
		HardExpiry:   expiry.UTC(),
		RevalidateBy: expiry.UTC(),
		LicenseID:    licenseID,
	})
}

// Usable reports whether a license can currently back a binding.
func Usable(l *model.License) bool {
	return l.Status == model.LicenseActive && time.Now().UTC().Before(l.ExpiresAt)
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
