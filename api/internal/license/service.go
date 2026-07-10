// Package license provisions licenses and mints signed device tokens.
package license

import (
	"context"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/aribpos/license-api/pkg/licensetoken"
)

// perpetualHorizon is the far-future hard-expiry stamped on perpetual paid
// licenses, so an offline machine is never blocked by inability to reach the
// server; suspension only takes effect once a revalidation succeeds.
const perpetualHorizon = 100 * 365 * 24 * time.Hour

// Clocks configures token validity windows.
type Clocks struct {
	RevalidateAfter time.Duration // -> token.revalidateBy
	HardExpireAfter time.Duration // -> token.hardExpiry
	TrialDuration   time.Duration
	// UpdatesWindow is the update-entitlement period granted to a new paid
	// license (license.UpdatesUntil = issuance + window). Trials get their
	// trial expiry instead.
	UpdatesWindow time.Duration
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

// CreateTrial provisions the one-per-account time-limited trial license
// created at signup, granting every module.
func (s *Service) CreateTrial(ctx context.Context, accountID string) (*model.License, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.clocks.TrialDuration)
	l := &model.License{
		ID:        idgen.New("lic"),
		Key:       idgen.LicenseKey(),
		AccountID: accountID,
		Type:      model.LicenseTrial,
		Features:  "Trial",
		Modules:   model.AllModules,
		Status:    model.LicenseActive,
		ExpiresAt: &expiresAt,
		// Trials get updates for exactly the trial period.
		UpdatesUntil: &expiresAt,
		Source:       "signup_trial",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.InsertLicense(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// CreatePaid provisions a license for an account granting the given modules.
// A nil expiresAt makes the license perpetual (no expiry).
func (s *Service) CreatePaid(ctx context.Context, accountID string, modules []string, expiresAt *time.Time, source, externalRef, assignedBy, notes string) (*model.License, error) {
	now := time.Now().UTC()
	var exp *time.Time
	if expiresAt != nil {
		v := expiresAt.UTC()
		exp = &v
	}
	updatesUntil := now.Add(s.clocks.UpdatesWindow)
	l := &model.License{
		ID:           idgen.New("lic"),
		Key:          idgen.LicenseKey(),
		AccountID:    accountID,
		Type:         model.LicensePaid,
		Modules:      modules,
		Status:       model.LicenseActive,
		ExpiresAt:    exp,
		UpdatesUntil: &updatesUntil,
		Source:       source,
		ExternalRef:  externalRef,
		AssignedBy:   assignedBy,
		Notes:        notes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.InsertLicense(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// TokenFor mints a signed token binding a license to a machine. Perpetual
// licenses (nil ExpiresAt) get a far-future hard expiry and a hard-expiry
// always-due revalidation clock; dated licenses (trials) clamp both clocks to
// the license's own expiry.
//
// includeUpdatesUntil appends the 6th payload field (update entitlement).
// It must only be true when the requesting client advertised an appVersion:
// deployed pre-updater clients parse by field count and would reject a
// 6-field token at revalidation, bricking their license state. A nil
// license UpdatesUntil (grandfathered) always omits the field.
func (s *Service) TokenFor(l *model.License, machineID string, includeUpdatesUntil bool) (string, time.Time, time.Time, error) {
	now := time.Now().UTC()
	var reval, hard time.Time
	if l.ExpiresAt == nil {
		hard = now.Add(perpetualHorizon)
		reval = now
	} else {
		reval = earliest(now.Add(s.clocks.RevalidateAfter), *l.ExpiresAt)
		hard = *l.ExpiresAt
	}
	p := licensetoken.Payload{
		MachineID:    machineID,
		Features:     encodeModules(l.Modules),
		HardExpiry:   hard,
		RevalidateBy: reval,
		LicenseID:    l.ID,
	}
	if includeUpdatesUntil {
		p.UpdatesUntil = l.UpdatesUntil
	}
	tok, err := s.signer.Sign(p)
	return tok, reval, hard, err
}

// SignOffline mints a fully-offline token (no revalidation expected) for the
// hidden manual-entry fallback. A nil expiry mints a perpetual sentinel; both
// clocks are otherwise set to expiry.
func (s *Service) SignOffline(machineID string, modules []string, expiry *time.Time, licenseID string) (string, error) {
	now := time.Now().UTC()
	hard, reval := now.Add(perpetualHorizon), now
	if expiry != nil {
		hard, reval = expiry.UTC(), expiry.UTC()
	}
	return s.signer.Sign(licensetoken.Payload{
		MachineID:    machineID,
		Features:     encodeModules(modules),
		HardExpiry:   hard,
		RevalidateBy: reval,
		LicenseID:    licenseID,
	})
}

// encodeModules renders the versioned module encoding carried in the token's
// features field. The "v1:" prefix discriminates this format from legacy
// free-text Features labels and lets the encoding extend later without
// re-breaking the client. Empty Modules (legacy/in-flight rows) fall back to
// AllModules — never the raw legacy label, which a module-aware client can't
// parse and would grant nothing.
func encodeModules(modules []string) string {
	if len(modules) == 0 {
		modules = model.AllModules
	}
	return "v1:" + strings.Join(modules, ",")
}

// Usable reports whether a license can currently back a binding. A nil
// ExpiresAt (perpetual) never expires.
func Usable(l *model.License) bool {
	return l.Status == model.LicenseActive && (l.ExpiresAt == nil || time.Now().UTC().Before(*l.ExpiresAt))
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
