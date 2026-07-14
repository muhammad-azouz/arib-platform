package mongostore

import (
	"context"
	"errors"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// UpsertBranch inserts or fully replaces a branch by its GUID (cloud-minted,
// or adopted from a standalone install on subscribe).
func (s *Store) UpsertBranch(ctx context.Context, b *model.Branch) error {
	_, err := s.Branches.ReplaceOne(ctx,
		bson.D{{Key: "_id", Value: b.ID}}, b, options.Replace().SetUpsert(true))
	return err
}

// BranchByID returns a branch by its GUID.
func (s *Store) BranchByID(ctx context.Context, id string) (*model.Branch, error) {
	var b model.Branch
	err := s.Branches.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&b)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &b, err
}

// BranchesByTenant lists a tenant's branches (the app's branch picker).
func (s *Store) BranchesByTenant(ctx context.Context, tenantID string) ([]model.Branch, error) {
	cur, err := s.Branches.Find(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Branch
	return out, cur.All(ctx, &out)
}

// SetBranchStatus activates/deactivates a branch (a licensing event).
func (s *Store) SetBranchStatus(ctx context.Context, id string, status model.BranchStatus, at time.Time) error {
	return s.updateBranch(ctx, id, bson.D{
		{Key: "status", Value: status},
		{Key: "updated_at", Value: at},
	})
}

// SetBranchSeats changes the per-branch device-seat limit.
func (s *Store) SetBranchSeats(ctx context.Context, id string, seats int, at time.Time) error {
	return s.updateBranch(ctx, id, bson.D{
		{Key: "seats", Value: seats},
		{Key: "updated_at", Value: at},
	})
}

// SetBranchLastSync stamps the branch's last completed sync round (gateway
// sync-completed callback). Deliberately leaves updated_at alone — this is
// telemetry, not a registry edit.
func (s *Store) SetBranchLastSync(ctx context.Context, id string, at time.Time) error {
	return s.updateBranch(ctx, id, bson.D{
		{Key: "last_sync_at", Value: at},
	})
}

// DeleteBranchesByTenant removes every branch owned by the tenant, returning
// the count deleted.
func (s *Store) DeleteBranchesByTenant(ctx context.Context, tenantID string) (int64, error) {
	res, err := s.Branches.DeleteMany(ctx, bson.D{{Key: "tenant_id", Value: tenantID}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (s *Store) updateBranch(ctx context.Context, id string, set bson.D) error {
	res, err := s.Branches.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}