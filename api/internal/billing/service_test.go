package billing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// testStore connects to the Mongo given by TEST_MONGO_URI (skips otherwise),
// using a throwaway database dropped on cleanup.
func testStore(t *testing.T) (*mongostore.Store, context.Context) {
	t.Helper()
	uri := os.Getenv("TEST_MONGO_URI")
	if uri == "" {
		t.Skip("TEST_MONGO_URI not set; skipping Mongo integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	dbName := fmt.Sprintf("arib_billing_test_%d", time.Now().UnixNano())
	store, err := mongostore.Connect(ctx, uri, dbName)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() {
		_ = store.DropDatabase(context.Background())
		_ = store.Close(context.Background())
	})
	return store, ctx
}

// fakeProvisioner stands in for tenant.Service.ProvisionSync so this package
// never needs to import package tenant (which itself imports billing).
type fakeProvisioner struct {
	calls   []string
	failErr error
}

func (f *fakeProvisioner) ProvisionSync(_ context.Context, tenantID string) (*model.Tenant, error) {
	f.calls = append(f.calls, tenantID)
	if f.failErr != nil {
		return nil, f.failErr
	}
	return &model.Tenant{ID: tenantID, DBName: "arib_" + tenantID}, nil
}

func seedTenant(t *testing.T, store *mongostore.Store, ctx context.Context, dbName string) string {
	t.Helper()
	now := time.Now().UTC()
	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "Test Tenant",
		Status: model.TenantActive, DBName: dbName, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return tn.ID
}

func auditCount(t *testing.T, store *mongostore.Store, ctx context.Context, action, target string) int64 {
	t.Helper()
	n, err := store.Audit.CountDocuments(ctx, bson.D{{Key: "action", Value: action}, {Key: "target", Value: target}})
	if err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}

func TestCreate_AutoProvisionsUnprovisionedTenant(t *testing.T) {
	store, ctx := testStore(t)
	tenantID := seedTenant(t, store, ctx, "") // no DBName yet
	prov := &fakeProvisioner{}
	svc := New(store, prov)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	res, err := svc.Create(ctx, tenantID, 500000, "", start, end, "first payment", "owner@arib.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !res.Provisioned {
		t.Fatalf("expected auto-provision on unprovisioned tenant, got %+v", res)
	}
	if len(prov.calls) != 1 || prov.calls[0] != tenantID {
		t.Fatalf("provisioner calls = %v, want one call for %s", prov.calls, tenantID)
	}
	if res.Bill.Currency != defaultCurrency {
		t.Fatalf("currency = %s, want default %s", res.Bill.Currency, defaultCurrency)
	}
	if res.Summary.State != StateActive {
		t.Fatalf("summary state = %s, want %s", res.Summary.State, StateActive)
	}
	if auditCount(t, store, ctx, "bill.create", res.Bill.ID) != 1 {
		t.Fatalf("expected one bill.create audit row")
	}
}

func TestCreate_SkipsProvisionWhenAlreadyProvisioned(t *testing.T) {
	store, ctx := testStore(t)
	tenantID := seedTenant(t, store, ctx, "arib_already") // already provisioned
	prov := &fakeProvisioner{}
	svc := New(store, prov)

	start := time.Now().UTC()
	res, err := svc.Create(ctx, tenantID, 100000, "EGP", start, start.AddDate(0, 1, 0), "", "owner@arib.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Provisioned || len(prov.calls) != 0 {
		t.Fatalf("expected no provisioning for an already-provisioned tenant, got %+v calls=%v", res, prov.calls)
	}
}

func TestCreate_ProvisionFailureStillPersistsBill(t *testing.T) {
	store, ctx := testStore(t)
	tenantID := seedTenant(t, store, ctx, "")
	prov := &fakeProvisioner{failErr: errors.New("shard unavailable")}
	svc := New(store, prov)

	start := time.Now().UTC()
	res, err := svc.Create(ctx, tenantID, 100000, "", start, start.AddDate(0, 1, 0), "", "owner@arib.com")
	if err != nil {
		t.Fatalf("create must succeed even when provisioning fails: %v", err)
	}
	if res.Provisioned {
		t.Fatalf("expected Provisioned=false on provisioner failure")
	}
	if res.ProvisionErr == "" {
		t.Fatalf("expected ProvisionErr to carry the failure detail")
	}
	// The bill itself must be durably recorded regardless.
	if _, err := store.BillByID(ctx, res.Bill.ID); err != nil {
		t.Fatalf("bill not persisted after provisioning failure: %v", err)
	}
}

func TestCreate_ValidatesAmountAndPeriod(t *testing.T) {
	store, ctx := testStore(t)
	tenantID := seedTenant(t, store, ctx, "arib_x")
	svc := New(store, &fakeProvisioner{})
	start := time.Now().UTC()

	if _, err := svc.Create(ctx, tenantID, 0, "", start, start.AddDate(0, 1, 0), "", "a@b.com"); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("zero amount: want ErrInvalidAmount, got %v", err)
	}
	if _, err := svc.Create(ctx, tenantID, -100, "", start, start.AddDate(0, 1, 0), "", "a@b.com"); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("negative amount: want ErrInvalidAmount, got %v", err)
	}
	if _, err := svc.Create(ctx, tenantID, 100, "", start, start, "", "a@b.com"); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("equal dates: want ErrInvalidPeriod, got %v", err)
	}
	if _, err := svc.Create(ctx, tenantID, 100, "", start, start.Add(-time.Hour), "", "a@b.com"); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("ends before starts: want ErrInvalidPeriod, got %v", err)
	}
}

func TestVoid_RequiresReasonAndDowngradesSummary(t *testing.T) {
	store, ctx := testStore(t)
	tenantID := seedTenant(t, store, ctx, "arib_y")
	svc := New(store, &fakeProvisioner{})

	start := time.Now().UTC().Add(-15 * 24 * time.Hour)
	res, err := svc.Create(ctx, tenantID, 100000, "", start, start.AddDate(0, 1, 0), "", "owner@arib.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Void(ctx, res.Bill.ID, "", "owner@arib.com"); !errors.Is(err, ErrVoidReasonRequired) {
		t.Fatalf("empty reason: want ErrVoidReasonRequired, got %v", err)
	}

	if err := svc.Void(ctx, res.Bill.ID, "entered wrong period", "owner@arib.com"); err != nil {
		t.Fatalf("void: %v", err)
	}
	if err := svc.Void(ctx, res.Bill.ID, "again", "owner@arib.com"); !errors.Is(err, ErrBillNotPaid) {
		t.Fatalf("re-void: want ErrBillNotPaid, got %v", err)
	}
	if auditCount(t, store, ctx, "bill.void", res.Bill.ID) != 1 {
		t.Fatalf("expected one bill.void audit row")
	}

	_, summary, err := svc.ListWithSummary(ctx, tenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if summary.State != StateNone {
		t.Fatalf("state after voiding the only bill = %s, want %s", summary.State, StateNone)
	}
}

func TestListWithSummary_NewestFirst(t *testing.T) {
	store, ctx := testStore(t)
	tenantID := seedTenant(t, store, ctx, "arib_z")
	svc := New(store, &fakeProvisioner{})

	start1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, tenantID, 100000, "", start1, start1.AddDate(0, 1, 0), "", "a@b.com"); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	start2 := start1.AddDate(0, 1, 0)
	res2, err := svc.Create(ctx, tenantID, 100000, "", start2, start2.AddDate(0, 1, 0), "", "a@b.com")
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	bills, _, err := svc.ListWithSummary(ctx, tenantID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bills) != 2 || bills[0].ID != res2.Bill.ID {
		t.Fatalf("want newest-first with %s first, got %+v", res2.Bill.ID, bills)
	}
}
