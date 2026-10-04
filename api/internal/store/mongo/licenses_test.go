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

func TestSetLicenseModulesAndBackfill(t *testing.T) {
	s, ctx := testStore(t)

	mk := func(modules []string) *model.License {
		l := &model.License{
			ID: idgen.New("lic"), Key: idgen.New("key"), AccountID: idgen.New("acc"),
			Type: model.LicensePaid, Status: model.LicenseActive, Modules: modules,
			CreatedAt: now(), UpdatedAt: now(),
		}
		if err := s.InsertLicense(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	legacy := mk([]string{"purchase", "sales", "customers", "accounting"})
	empty := mk(nil)
	current := mk([]string{"bills", "users"})

	n, err := s.BackfillCatalogModules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("backfilled %d, want 2", n)
	}
	for _, id := range []string{legacy.ID, empty.ID} {
		got, _ := s.LicenseByID(ctx, id)
		if len(got.Modules) != len(model.AllModules) {
			t.Fatalf("%s modules = %v, want full catalog", id, got.Modules)
		}
	}
	got, _ := s.LicenseByID(ctx, current.ID)
	if len(got.Modules) != 2 {
		t.Fatalf("catalog-era license was touched: %v", got.Modules)
	}
	if n, err := s.BackfillCatalogModules(ctx); err != nil || n != 0 {
		t.Fatalf("second backfill = %d, %v; want 0, nil", n, err)
	}

	if err := s.SetLicenseModules(ctx, current.ID, []string{"bills", "users", "ariblink"}, 5); err != nil {
		t.Fatal(err)
	}
	got, _ = s.LicenseByID(ctx, current.ID)
	if got.Seats != 5 || len(got.Modules) != 3 {
		t.Fatalf("after update: modules=%v seats=%d", got.Modules, got.Seats)
	}
	if err := s.SetLicenseModules(ctx, "lic_missing", nil, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing license err = %v, want ErrNotFound", err)
	}
}
