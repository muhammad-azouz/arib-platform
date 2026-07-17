package mongostore

import (
	"testing"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
)

func TestBillCRUD(t *testing.T) {
	s, ctx := testStore(t)
	at := now()
	const tenantID = "tnt_1"

	older := &model.Bill{
		ID: idgen.New("bil"), TenantID: tenantID,
		Amount: 100000, Currency: "EGP",
		StartsAt: at, EndsAt: at.AddDate(0, 1, 0),
		Status: model.BillPaid, CreatedBy: "owner@arib.com", Source: "manual_admin",
		CreatedAt: at, UpdatedAt: at,
	}
	newer := &model.Bill{
		ID: idgen.New("bil"), TenantID: tenantID,
		Amount: 100000, Currency: "EGP",
		StartsAt: older.EndsAt, EndsAt: older.EndsAt.AddDate(0, 1, 0),
		Status: model.BillPaid, CreatedBy: "owner@arib.com", Source: "manual_admin",
		CreatedAt: at, UpdatedAt: at,
	}
	for _, b := range []*model.Bill{older, newer} {
		if err := s.InsertBill(ctx, b); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := s.BillByID(ctx, older.ID)
	if err != nil || got.Amount != 100000 || got.Status != model.BillPaid {
		t.Fatalf("byID: %+v err=%v", got, err)
	}

	list, err := s.BillsByTenant(ctx, tenantID)
	if err != nil || len(list) != 2 || list[0].ID != newer.ID {
		t.Fatalf("byTenant: want newest-first [newer,older], got %+v err=%v", list, err)
	}

	if err := s.VoidBill(ctx, older.ID, "mis-entered period", now()); err != nil {
		t.Fatalf("void: %v", err)
	}
	got, _ = s.BillByID(ctx, older.ID)
	if got.Status != model.BillVoid || got.VoidReason != "mis-entered period" {
		t.Fatalf("after void: %+v", got)
	}

	// Void bills are never deleted — still present in the tenant listing.
	list, err = s.BillsByTenant(ctx, tenantID)
	if err != nil || len(list) != 2 {
		t.Fatalf("byTenant after void: n=%d err=%v", len(list), err)
	}

	// Voiding an already-void bill is rejected (append-only, not editable).
	if err := s.VoidBill(ctx, older.ID, "again", now()); err == nil {
		t.Fatalf("want error voiding an already-void bill, got nil")
	}

	if _, err := s.BillByID(ctx, "bil_missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.VoidBill(ctx, "bil_missing", "x", now()); err != ErrNotFound {
		t.Fatalf("void missing: want ErrNotFound, got %v", err)
	}
}
