// Package device handles binding, revalidating and releasing license seats.
package device

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/license"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// Service errors surfaced to clients.
var (
	ErrNoLicense       = errors.New("no available license to bind")
	ErrTrialUsed       = errors.New("this device has already used a free trial")
	ErrNotBound        = errors.New("no active binding for this device")
	ErrLicenseInactive = errors.New("license is suspended or expired")
	ErrCooldown        = errors.New("device was released too recently; try again later")
	ErrReleaseLimit    = errors.New("release limit reached for this period")
	ErrForbidden       = errors.New("device does not belong to this account")
)

// CooldownPolicy limits self-service releases.
type CooldownPolicy struct {
	MinInterval time.Duration
	MaxPerMonth int
}

// Service coordinates the store and license signer.
type Service struct {
	store    *mongostore.Store
	licenses *license.Service
	cooldown CooldownPolicy
}

// New builds a device Service.
func New(store *mongostore.Store, licenses *license.Service, cooldown CooldownPolicy) *Service {
	return &Service{store: store, licenses: licenses, cooldown: cooldown}
}

// Result is returned to the client after bind/validate.
type Result struct {
	License      string    `json:"license"` // signed token for license.lic
	LicenseID    string    `json:"license_id"`
	Features     string    `json:"features"`
	RevalidateBy time.Time `json:"revalidate_by"`
	HardExpiry   time.Time `json:"hard_expiry"`
	DeviceID     string    `json:"device_id"`
}

// Bind attaches a machine to one of the account's free license seats (or
// re-issues a token if the machine is already bound), returning a signed token.
func (s *Service) Bind(ctx context.Context, accountID, machineID, machineName, os string) (*Result, error) {
	if machineID == "" {
		return nil, errors.New("machine id required")
	}
	licenses, err := s.store.LicensesByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	devices, err := s.store.DevicesByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*model.License, len(licenses))
	for i := range licenses {
		byID[licenses[i].ID] = &licenses[i]
	}

	// Already bound on this machine? Re-issue (idempotent activation).
	for i := range devices {
		d := &devices[i]
		if d.Status == model.DeviceActive && d.MachineID == machineID {
			l := byID[d.LicenseID]
			if l != nil && license.Usable(l) {
				return s.issue(ctx, l, d, machineID)
			}
		}
	}

	activeSeat := make(map[string]bool)
	for i := range devices {
		if devices[i].Status == model.DeviceActive {
			activeSeat[devices[i].LicenseID] = true
		}
	}

	pick := pickLicense(licenses, activeSeat)
	if pick == nil {
		return nil, ErrNoLicense
	}

	// Per-machine trial anti-abuse.
	if pick.Type == model.LicenseTrial {
		used, err := s.store.TrialUsed(ctx, machineID)
		if err != nil {
			return nil, err
		}
		if used {
			return nil, ErrTrialUsed
		}
	}

	now := time.Now().UTC()
	dev := &model.Device{
		ID:              idgen.New("dev"),
		LicenseID:       pick.ID,
		AccountID:       accountID,
		MachineID:       machineID,
		MachineName:     machineName,
		OS:              os,
		Status:          model.DeviceActive,
		BoundAt:         now,
		LastSeenAt:      now,
		LastValidatedAt: now,
	}
	if err := s.store.InsertDevice(ctx, dev); err != nil {
		if mongostore.IsDuplicateKey(err) {
			// Someone bound this seat concurrently; retry the whole flow.
			return s.Bind(ctx, accountID, machineID, machineName, os)
		}
		return nil, err
	}
	if pick.Type == model.LicenseTrial {
		_ = s.store.RecordTrial(ctx, &model.TrialLedger{MachineID: machineID, AccountID: accountID, UsedAt: now})
	}
	return s.issue(ctx, pick, dev, machineID)
}

// Validate re-checks an active binding and returns a fresh token (resetting the
// revalidate/hard-expiry clocks).
func (s *Service) Validate(ctx context.Context, accountID, machineID string) (*Result, error) {
	devices, err := s.store.DevicesByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	var dev *model.Device
	for i := range devices {
		if devices[i].Status == model.DeviceActive && devices[i].MachineID == machineID {
			dev = &devices[i]
			break
		}
	}
	if dev == nil {
		return nil, ErrNotBound
	}
	l, err := s.store.LicenseByID(ctx, dev.LicenseID)
	if err != nil {
		return nil, err
	}
	if !license.Usable(l) {
		return nil, ErrLicenseInactive
	}
	return s.issue(ctx, l, dev, machineID)
}

// Release frees a seat. selfService enforces the abuse cooldown.
func (s *Service) Release(ctx context.Context, accountID, deviceID string, selfService bool) error {
	dev, err := s.store.DeviceByID(ctx, deviceID)
	if errors.Is(err, mongostore.ErrNotFound) {
		return ErrNotBound
	}
	if err != nil {
		return err
	}
	if dev.AccountID != accountID && selfService {
		return ErrForbidden
	}
	if dev.Status != model.DeviceActive {
		return nil // already released; idempotent
	}
	now := time.Now().UTC()
	if selfService {
		if dev.LastReleaseAt != nil && now.Sub(*dev.LastReleaseAt) < s.cooldown.MinInterval {
			return ErrCooldown
		}
		since := now.AddDate(0, -1, 0)
		count, err := s.store.CountSelfReleasesSince(ctx, dev.MachineID, since)
		if err != nil {
			return err
		}
		if int(count) >= s.cooldown.MaxPerMonth {
			return ErrReleaseLimit
		}
	}
	return s.store.ReleaseDevice(ctx, dev.ID, now, selfService)
}

func (s *Service) issue(ctx context.Context, l *model.License, d *model.Device, machineID string) (*Result, error) {
	tok, reval, hard, err := s.licenses.TokenFor(l, machineID)
	if err != nil {
		return nil, err
	}
	_ = s.store.TouchDeviceValidated(ctx, d.ID, time.Now().UTC())
	return &Result{
		License:      tok,
		LicenseID:    l.ID,
		Features:     l.Features,
		RevalidateBy: reval,
		HardExpiry:   hard,
		DeviceID:     d.ID,
	}, nil
}

// pickLicense chooses a usable license with a free seat, preferring paid over
// trial and the latest-expiring option.
func pickLicense(licenses []model.License, activeSeat map[string]bool) *model.License {
	candidates := make([]*model.License, 0, len(licenses))
	for i := range licenses {
		l := &licenses[i]
		if license.Usable(l) && !activeSeat[l.ID] {
			candidates = append(candidates, l)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.Type == model.LicensePaid) != (b.Type == model.LicensePaid) {
			return a.Type == model.LicensePaid // paid first
		}
		return a.ExpiresAt.After(b.ExpiresAt) // then latest expiry
	})
	return candidates[0]
}
