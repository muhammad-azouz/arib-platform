package tenant

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// testService connects to the Mongo given by TEST_MONGO_URI (skips otherwise),
// using a throwaway database dropped on cleanup.
func testService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	uri := os.Getenv("TEST_MONGO_URI")
	if uri == "" {
		t.Skip("TEST_MONGO_URI not set; skipping Mongo integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	dbName := fmt.Sprintf("arib_tenant_test_%d", time.Now().UnixNano())
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
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate sync key: %v", err)
	}
	// Seed the single test shard so ProvisionSync / IssueSyncToken have a target.
	now := time.Now().UTC()
	if err := store.UpsertShard(ctx, &model.Shard{
		ID:         "shd_test",
		GatewayURL: "https://sync.aribpos.test",
		Status:     model.ShardActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed shard: %v", err)
	}
	return New(store, key, time.Hour, nil), ctx
}

const owner = "acc_owner"

// setupTenant registers a tenant with one company and one 2-seat branch.
func setupTenant(t *testing.T, s *Service, ctx context.Context) (tenantID, companyID, branchID string) {
	t.Helper()
	tn, err := s.Register(ctx, owner, "متجر الاختبار")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	c, err := s.SetCompany(ctx, owner, tn.ID, CompanyInput{Name: "شركة أريب"})
	if err != nil {
		t.Fatalf("set company: %v", err)
	}
	b, err := s.AddBranch(ctx, owner, tn.ID, BranchInput{
		CompanyID: c.ID, Name: "فرع وسط البلد", Seats: 2,
		Phone1: "01000000000", Phone2: "0227000000", Address: "شارع وسط البلد",
	})
	if err != nil {
		t.Fatalf("add branch: %v", err)
	}
	return tn.ID, c.ID, b.ID
}

func TestActivationFlow(t *testing.T) {
	s, ctx := testService(t)
	tenantID, companyID, branchID := setupTenant(t, s, ctx)

	bundle, err := s.GetBundle(ctx, owner, tenantID)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if bundle.Company == nil || bundle.Company.ID != companyID || len(bundle.Branches) != 1 {
		t.Fatalf("bundle contents: company=%+v, %d branches", bundle.Company, len(bundle.Branches))
	}
	if bundle.Branches[0].Seats != 2 || bundle.Branches[0].Status != model.BranchActive {
		t.Fatalf("branch defaults: %+v", bundle.Branches[0])
	}
	// Contact fields (printed on POS receipts) must round-trip through the bundle.
	if got := bundle.Branches[0]; got.Phone1 != "01000000000" || got.Phone2 != "0227000000" || got.Address != "شارع وسط البلد" {
		t.Fatalf("branch contact not persisted: %+v", got)
	}

	// One company per tenant (D15): a second company with a different GUID is
	// rejected; updating without an id targets the existing company in place.
	if _, err := s.SetCompany(ctx, owner, tenantID, CompanyInput{
		ID: "0D5F0C33-9D3A-7B21-A000-0000000000AB", Name: "شركة ثانية",
	}); !errors.Is(err, ErrCompanyExists) {
		t.Fatalf("second company: want ErrCompanyExists, got %v", err)
	}
	updated, err := s.SetCompany(ctx, owner, tenantID, CompanyInput{Name: "شركة أريب المحدثة"})
	if err != nil || updated.ID != companyID || updated.Name != "شركة أريب المحدثة" {
		t.Fatalf("in-place update: %+v err=%v", updated, err)
	}

	// Adopting a local GUID works for a tenant that has no company yet.
	t2, _ := s.Register(ctx, owner, "منشأة ثانية")
	adopted, err := s.SetCompany(ctx, owner, t2.ID, CompanyInput{
		ID: "0D5F0C33-9D3A-7B21-A000-0000000000AB", Name: "شركة قائمة",
	})
	if err != nil {
		t.Fatalf("adopt company guid: %v", err)
	}
	if adopted.ID != "0d5f0c33-9d3a-7b21-a000-0000000000ab" {
		t.Fatalf("guid not normalized: %s", adopted.ID)
	}

	// AddBranch derives the tenant's company; a mismatching explicit id fails.
	if _, err := s.AddBranch(ctx, owner, t2.ID, BranchInput{Name: "فرع بدون شركة محددة"}); err != nil {
		t.Fatalf("add branch without company id: %v", err)
	}
	if _, err := s.AddBranch(ctx, owner, t2.ID, BranchInput{CompanyID: companyID, Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mismatched company id: want ErrForbidden, got %v", err)
	}

	// Ownership enforcement.
	if _, err := s.GetBundle(ctx, "acc_intruder", tenantID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
	if _, err := s.SetCompany(ctx, "acc_intruder", tenantID, CompanyInput{ID: companyID, Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("company takeover: want ErrForbidden, got %v", err)
	}

	// Branch lifecycle.
	if err := s.RenameBranch(ctx, owner, tenantID, branchID, "الفرع الرئيسي"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.SetBranchStatus(ctx, owner, tenantID, branchID, model.BranchDeactivated); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := s.BindDevice(ctx, owner, tenantID, branchID, "m-x", "", ""); !errors.Is(err, ErrBranchInactive) {
		t.Fatalf("bind to deactivated branch: want ErrBranchInactive, got %v", err)
	}
	if err := s.SetBranchStatus(ctx, owner, tenantID, branchID, model.BranchActive); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
}

func TestSeatLimitEnforcement(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, branchID := setupTenant(t, s, ctx) // 2 seats

	d1, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-1", "POS-1", "windows")
	if err != nil {
		t.Fatalf("bind 1: %v", err)
	}
	if _, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-2", "POS-2", "windows"); err != nil {
		t.Fatalf("bind 2: %v", err)
	}

	// THE seat-limit rejection case.
	if _, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-3", "POS-3", "windows"); !errors.Is(err, ErrSeatLimit) {
		t.Fatalf("third bind on 2 seats: want ErrSeatLimit, got %v", err)
	}

	// Rebinding an already-bound machine is idempotent, not a new seat.
	again, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-1", "POS-1", "windows")
	if err != nil || again.ID != d1.ID {
		t.Fatalf("idempotent rebind: %+v err=%v", again, err)
	}

	// Releasing frees the seat for another machine.
	if err := s.ReleaseDevice(ctx, owner, tenantID, d1.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-3", "POS-3", "windows"); err != nil {
		t.Fatalf("bind after release: %v", err)
	}

	// Raising the limit admits another device.
	if err := s.SetBranchSeats(ctx, branchID, 3); err != nil {
		t.Fatalf("set seats: %v", err)
	}
	if _, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-4", "POS-4", "windows"); err != nil {
		t.Fatalf("bind after seat upgrade: %v", err)
	}

	// Release is owner-only.
	if err := s.ReleaseDevice(ctx, "acc_intruder", tenantID, again.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("intruder release: want ErrForbidden, got %v", err)
	}
}

func TestSyncTokenIssuance(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, branchID := setupTenant(t, s, ctx)

	d, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-1", "POS-1", "windows")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// No central DB provisioned yet → not subscribed.
	if _, err := s.IssueSyncToken(ctx, owner, tenantID, d.ID); !errors.Is(err, ErrNotSubscribed) {
		t.Fatalf("token before subscription: want ErrNotSubscribed, got %v", err)
	}

	placed, err := s.ProvisionSync(ctx, tenantID)
	if err != nil {
		t.Fatalf("provision sync: %v", err)
	}
	if placed.DBName == "" {
		t.Fatalf("placement: %+v", placed)
	}

	issued, err := s.IssueSyncToken(ctx, owner, tenantID, d.ID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.GatewayURL != "https://sync.aribpos.test" {
		t.Fatalf("gateway url: %q", issued.GatewayURL)
	}
	parsed, err := s.ParseSyncToken(issued.Token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.TenantID != tenantID || parsed.BranchID != branchID || parsed.DeviceID != d.ID ||
		parsed.DBName != issued.Claims.DBName {
		t.Fatalf("claims round-trip: %+v", parsed)
	}
	if !parsed.ExpiresAt.After(time.Now()) || parsed.ExpiresAt.After(time.Now().Add(2*time.Hour)) {
		t.Fatalf("bad expiry: %v", parsed.ExpiresAt)
	}

	// A released device must not get tokens.
	if err := s.ReleaseDevice(ctx, owner, tenantID, d.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := s.IssueSyncToken(ctx, owner, tenantID, d.ID); !errors.Is(err, ErrNotBound) {
		t.Fatalf("token for released device: want ErrNotBound, got %v", err)
	}

	// Tampered token must not parse.
	if _, err := s.ParseSyncToken(issued.Token + "x"); err == nil {
		t.Fatal("tampered token parsed")
	}
}

func TestProvisionSyncIdempotent(t *testing.T) {
	s, ctx := testService(t)

	tn, _ := s.Register(ctx, owner, "T1")

	first, err := s.ProvisionSync(ctx, tn.ID)
	if err != nil {
		t.Fatalf("provision 1: %v", err)
	}
	if first.DBName == "" {
		t.Fatalf("no db name assigned: %+v", first)
	}
	// Re-provisioning derives the same name and must not error.
	second, err := s.ProvisionSync(ctx, tn.ID)
	if err != nil {
		t.Fatalf("provision 2: %v", err)
	}
	if second.DBName != first.DBName {
		t.Fatalf("db name not deterministic: %q != %q", second.DBName, first.DBName)
	}
}

func TestDeleteTenant_RemovesAllData(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, branchID := setupTenant(t, s, ctx)

	dev, err := s.BindDevice(ctx, owner, tenantID, branchID, "machine-1", "POS-1", "windows")
	if err != nil {
		t.Fatalf("bind device: %v", err)
	}

	res, err := s.DeleteTenant(ctx, "admin@aribpos.test", tenantID)
	if err != nil {
		t.Fatalf("delete tenant: %v", err)
	}
	if res.BranchesDeleted != 1 || res.DevicesDeleted != 1 || !res.CompanyDeleted || res.DBDropped {
		t.Fatalf("unexpected deletion summary: %+v", res)
	}

	if _, err := s.store.TenantByID(ctx, tenantID); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("tenant still present: %v", err)
	}
	if _, err := s.store.CompanyByTenant(ctx, tenantID); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("company still present: %v", err)
	}
	if _, err := s.store.BranchByID(ctx, branchID); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("branch still present: %v", err)
	}
	if _, err := s.store.BranchDeviceByID(ctx, dev.ID); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("branch device still present: %v", err)
	}
}

func TestDeleteTenant_NotFound(t *testing.T) {
	s, ctx := testService(t)
	if _, err := s.DeleteTenant(ctx, "admin@aribpos.test", "tnt_missing"); !errors.Is(err, mongostore.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestDeleteTenant_GatewayFailureAbortsDeletion verifies the central-DB drop
// runs first: if the gateway is unreachable, nothing else is deleted, so a
// retry once the gateway recovers is safe.
func TestDeleteTenant_GatewayFailureAbortsDeletion(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, _ := setupTenant(t, s, ctx)

	if _, err := s.ProvisionSync(ctx, tenantID); err != nil {
		t.Fatalf("provision sync: %v", err)
	}

	if _, err := s.DeleteTenant(ctx, "admin@aribpos.test", tenantID); err == nil {
		t.Fatalf("expected gateway drop failure, got nil error")
	}

	if _, err := s.store.TenantByID(ctx, tenantID); err != nil {
		t.Fatalf("tenant should still exist after aborted deletion: %v", err)
	}
	if _, err := s.store.CompanyByTenant(ctx, tenantID); err != nil {
		t.Fatalf("company should still exist after aborted deletion: %v", err)
	}
}

// TestRecordSyncCompleted covers the gateway's sync-completed callback: the
// branch's last_sync_at is stamped, a claims/branch tenant mismatch is
// forbidden, and an unknown branch is not found.
func TestRecordSyncCompleted(t *testing.T) {
	s, ctx := testService(t)
	tenantID, _, branchID := setupTenant(t, s, ctx)

	before := time.Now().UTC().Add(-time.Second)
	at, err := s.RecordSyncCompleted(ctx, tenantID, branchID)
	if err != nil {
		t.Fatalf("record sync completed: %v", err)
	}
	if at.Before(before) {
		t.Fatalf("recorded time %v is before the call", at)
	}

	b, err := s.store.BranchByID(ctx, branchID)
	if err != nil {
		t.Fatalf("branch by id: %v", err)
	}
	if b.LastSyncAt == nil || b.LastSyncAt.Before(before) {
		t.Fatalf("branch last_sync_at not stamped: %v", b.LastSyncAt)
	}

	t.Run("tenant mismatch is forbidden", func(t *testing.T) {
		if _, err := s.RecordSyncCompleted(ctx, "tnt_other", branchID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})

	t.Run("unknown branch is not found", func(t *testing.T) {
		if _, err := s.RecordSyncCompleted(ctx, tenantID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, mongostore.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}
