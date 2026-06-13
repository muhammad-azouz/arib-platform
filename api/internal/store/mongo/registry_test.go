package mongostore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
)

// testStore connects to the Mongo given by TEST_MONGO_URI (skips otherwise),
// using a unique throwaway database that is dropped on cleanup.
func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	uri := os.Getenv("TEST_MONGO_URI")
	if uri == "" {
		t.Skip("TEST_MONGO_URI not set; skipping Mongo integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	dbName := fmt.Sprintf("arib_license_test_%d", time.Now().UnixNano())
	s, err := Connect(ctx, uri, dbName)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Drop(context.Background())
		_ = s.Close(context.Background())
	})
	return s, ctx
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func TestTenantCRUD(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	tn := &model.Tenant{
		ID: idgen.New("tnt"), AccountID: "acc_1", Name: "متجر الاختبار",
		Status: model.TenantActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertTenant(ctx, tn); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := s.TenantByID(ctx, tn.ID)
	if err != nil || got.Name != tn.Name || got.Status != model.TenantActive {
		t.Fatalf("byID: %+v err=%v", got, err)
	}
	if got.ShardID != "" || got.DBName != "" {
		t.Fatalf("new tenant must have no shard placement, got %q/%q", got.ShardID, got.DBName)
	}

	list, err := s.TenantsByAccount(ctx, "acc_1")
	if err != nil || len(list) != 1 {
		t.Fatalf("byAccount: n=%d err=%v", len(list), err)
	}

	if err := s.UpdateTenantStatus(ctx, tn.ID, model.TenantSuspended, now()); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := s.UpdateTenantPlan(ctx, tn.ID, "sync-basic", now()); err != nil {
		t.Fatalf("plan: %v", err)
	}
	got, _ = s.TenantByID(ctx, tn.ID)
	if got.Status != model.TenantSuspended || got.Plan != "sync-basic" {
		t.Fatalf("updates not applied: %+v", got)
	}

	if _, err := s.TenantByID(ctx, "tnt_missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.UpdateTenantStatus(ctx, "tnt_missing", model.TenantActive, now()); err != ErrNotFound {
		t.Fatalf("update missing: want ErrNotFound, got %v", err)
	}
}

func TestTenantShardPlacement(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	t1 := &model.Tenant{ID: idgen.New("tnt"), AccountID: "acc_1", Name: "T1", Status: model.TenantActive, CreatedAt: at, UpdatedAt: at}
	t2 := &model.Tenant{ID: idgen.New("tnt"), AccountID: "acc_2", Name: "T2", Status: model.TenantActive, CreatedAt: at, UpdatedAt: at}
	for _, tn := range []*model.Tenant{t1, t2} {
		if err := s.InsertTenant(ctx, tn); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	if err := s.AssignTenantShard(ctx, t1.ID, "shd_1", "tenant_t1", now()); err != nil {
		t.Fatalf("assign: %v", err)
	}
	// Same DB name on the same shard must be rejected by shard_db_unique.
	if err := s.AssignTenantShard(ctx, t2.ID, "shd_1", "tenant_t1", now()); !IsDuplicateKey(err) {
		t.Fatalf("want duplicate-key on shard/db collision, got %v", err)
	}
	// Same DB name on a DIFFERENT shard is fine.
	if err := s.AssignTenantShard(ctx, t2.ID, "shd_2", "tenant_t1", now()); err != nil {
		t.Fatalf("assign other shard: %v", err)
	}

	n, err := s.CountTenantsOnShard(ctx, "shd_1")
	if err != nil || n != 1 {
		t.Fatalf("count shd_1: n=%d err=%v", n, err)
	}
}

func TestCompanyCRUD(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	c := &model.Company{
		ID: "0d5f0c33-9d3a-7b21-a000-000000000001", TenantID: "tnt_1",
		Name: "شركة أريب", Phone: "0100000000", CreatedAt: at, UpdatedAt: at,
	}
	if err := s.UpsertCompany(ctx, c); err != nil {
		t.Fatalf("upsert insert: %v", err)
	}

	c.Name = "شركة أريب المحدثة"
	c.UpdatedAt = now()
	if err := s.UpsertCompany(ctx, c); err != nil {
		t.Fatalf("upsert replace: %v", err)
	}

	got, err := s.CompanyByID(ctx, c.ID)
	if err != nil || got.Name != "شركة أريب المحدثة" {
		t.Fatalf("byID after replace: %+v err=%v", got, err)
	}

	byTenant, err := s.CompanyByTenant(ctx, "tnt_1")
	if err != nil || byTenant.ID != c.ID {
		t.Fatalf("byTenant: %+v err=%v", byTenant, err)
	}

	// One company per tenant (D15): a second company doc for the same tenant
	// must be rejected by the unique index.
	dup := *c
	dup.ID = "0d5f0c33-9d3a-7b21-a000-0000000000ff"
	if err := s.UpsertCompany(ctx, &dup); !IsDuplicateKey(err) {
		t.Fatalf("second company for tenant: want duplicate-key, got %v", err)
	}

	if _, err := s.CompanyByID(ctx, "no-such-guid"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestBranchCRUD(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	b := &model.Branch{
		ID: "1d5f0c33-9d3a-7b21-a000-000000000001", TenantID: "tnt_1", CompanyID: "cmp-guid",
		Name: "فرع وسط البلد", Seats: 3, Status: model.BranchActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.UpsertBranch(ctx, b); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	list, err := s.BranchesByTenant(ctx, "tnt_1")
	if err != nil || len(list) != 1 || list[0].Seats != 3 {
		t.Fatalf("byTenant: %+v err=%v", list, err)
	}

	if err := s.SetBranchSeats(ctx, b.ID, 5, now()); err != nil {
		t.Fatalf("seats: %v", err)
	}
	if err := s.SetBranchStatus(ctx, b.ID, model.BranchDeactivated, now()); err != nil {
		t.Fatalf("status: %v", err)
	}
	got, err := s.BranchByID(ctx, b.ID)
	if err != nil || got.Seats != 5 || got.Status != model.BranchDeactivated {
		t.Fatalf("after updates: %+v err=%v", got, err)
	}

	if err := s.SetBranchSeats(ctx, "missing", 1, now()); err != ErrNotFound {
		t.Fatalf("update missing: want ErrNotFound, got %v", err)
	}
}

func TestBranchDeviceSeats(t *testing.T) {
	s, ctx := testStore(t)
	at := now()
	const branchID = "1d5f0c33-9d3a-7b21-a000-000000000001"

	bind := func(machine string) (*model.BranchDevice, error) {
		d := &model.BranchDevice{
			ID: idgen.New("bdv"), TenantID: "tnt_1", BranchID: branchID,
			MachineID: machine, MachineName: "POS-" + machine, OS: "windows",
			Status: model.DeviceActive, BoundAt: at, LastSeenAt: at,
		}
		return d, s.InsertBranchDevice(ctx, d)
	}

	d1, err := bind("machine-1")
	if err != nil {
		t.Fatalf("bind 1: %v", err)
	}
	if _, err := bind("machine-2"); err != nil {
		t.Fatalf("bind 2: %v", err)
	}

	// Same machine, same branch, while active → unique index must reject.
	if _, err := bind("machine-1"); !IsDuplicateKey(err) {
		t.Fatalf("want duplicate-key on double bind, got %v", err)
	}

	n, err := s.CountActiveBranchDevices(ctx, branchID)
	if err != nil || n != 2 {
		t.Fatalf("active count: n=%d err=%v", n, err)
	}

	got, err := s.ActiveBranchDeviceForMachine(ctx, branchID, "machine-1")
	if err != nil || got.ID != d1.ID {
		t.Fatalf("active for machine: %+v err=%v", got, err)
	}

	// Release frees the seat: count drops and the machine can re-bind.
	if err := s.ReleaseBranchDevice(ctx, d1.ID, now()); err != nil {
		t.Fatalf("release: %v", err)
	}
	if n, _ = s.CountActiveBranchDevices(ctx, branchID); n != 1 {
		t.Fatalf("count after release: %d", n)
	}
	if _, err := s.ActiveBranchDeviceForMachine(ctx, branchID, "machine-1"); err != ErrNotFound {
		t.Fatalf("released machine must not be active, got %v", err)
	}
	if _, err := bind("machine-1"); err != nil {
		t.Fatalf("re-bind after release: %v", err)
	}

	all, err := s.BranchDevicesByBranch(ctx, branchID)
	if err != nil || len(all) != 3 {
		t.Fatalf("list: n=%d err=%v", len(all), err)
	}

	if err := s.TouchBranchDeviceSeen(ctx, d1.ID, now().Add(time.Minute)); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if err := s.ReleaseBranchDevice(ctx, "bdv_missing", now()); err != ErrNotFound {
		t.Fatalf("release missing: want ErrNotFound, got %v", err)
	}
}

func TestShardCRUD(t *testing.T) {
	s, ctx := testStore(t)
	at := now()

	sh := &model.Shard{
		ID: idgen.New("shd"), Name: "shard-eu-1", Host: "10.0.0.5",
		GatewayURL: "https://sync1.aribpos.com", MaxTenants: 100,
		Status: model.ShardActive, CreatedAt: at, UpdatedAt: at,
	}
	if err := s.InsertShard(ctx, sh); err != nil {
		t.Fatalf("insert: %v", err)
	}

	dup := *sh
	dup.ID = idgen.New("shd")
	if err := s.InsertShard(ctx, &dup); !IsDuplicateKey(err) {
		t.Fatalf("want duplicate-key on shard name, got %v", err)
	}

	got, err := s.ShardByID(ctx, sh.ID)
	if err != nil || got.GatewayURL != sh.GatewayURL {
		t.Fatalf("byID: %+v err=%v", got, err)
	}

	if err := s.UpdateShardStatus(ctx, sh.ID, model.ShardDraining, now()); err != nil {
		t.Fatalf("status: %v", err)
	}
	list, err := s.ListShards(ctx)
	if err != nil || len(list) != 1 || list[0].Status != model.ShardDraining {
		t.Fatalf("list: %+v err=%v", list, err)
	}

	if _, err := s.ShardByID(ctx, "shd_missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
