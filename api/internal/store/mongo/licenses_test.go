package mongostore

import (
	"errors"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
)

func TestSetLicenseUpdatesUntil(t *testing.T) {
	s, ctx := testStore(t)

	l := &model.License{
		ID:        idgen.New("lic"),
		Key:       idgen.New("key"),
		AccountID: idgen.New("acc"),
		Type:      model.LicensePaid,
		Status:    model.LicenseActive,
		CreatedAt: now(),
		UpdatedAt: now(),
	}
	if err := s.InsertLicense(ctx, l); err != nil {
		t.Fatal(err)
	}

	until := now().Add(150 * 24 * time.Hour)
	if err := s.SetLicenseUpdatesUntil(ctx, l.ID, &until); err != nil {
		t.Fatal(err)
	}
	got, err := s.LicenseByID(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdatesUntil == nil || !got.UpdatesUntil.Equal(until) {
		t.Fatalf("UpdatesUntil = %v, want %v", got.UpdatesUntil, until)
	}

	// Clearing goes back to unlimited (grandfathered).
	if err := s.SetLicenseUpdatesUntil(ctx, l.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.LicenseByID(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdatesUntil != nil {
		t.Fatalf("UpdatesUntil = %v, want nil after clear", got.UpdatesUntil)
	}

	if err := s.SetLicenseUpdatesUntil(ctx, "lic_missing", &until); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing license err = %v, want ErrNotFound", err)
	}
}
