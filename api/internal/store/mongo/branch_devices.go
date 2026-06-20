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

// InsertBranchDevice creates an active branch-seat binding. The unique partial
// index on (branch_id, machine_id, status=active) rejects double-binding the
// same machine; the seat-count limit is the service layer's job.
func (s *Store) InsertBranchDevice(ctx context.Context, d *model.BranchDevice) error {
	_, err := s.BranchDevices.InsertOne(ctx, d)
	return err
}

// BranchDeviceByID returns a branch-seat binding by id.
func (s *Store) BranchDeviceByID(ctx context.Context, id string) (*model.BranchDevice, error) {
	var d model.BranchDevice
	err := s.BranchDevices.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &d, err
}

// ActiveBranchDeviceForMachine returns the active binding of a machine in a
// branch, if any.
func (s *Store) ActiveBranchDeviceForMachine(ctx context.Context, branchID, machineID string) (*model.BranchDevice, error) {
	var d model.BranchDevice
	err := s.BranchDevices.FindOne(ctx, bson.D{
		{Key: "branch_id", Value: branchID},
		{Key: "machine_id", Value: machineID},
		{Key: "status", Value: model.DeviceActive},
	}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &d, err
}

// CountActiveBranchDevices returns how many seats a branch currently uses.
func (s *Store) CountActiveBranchDevices(ctx context.Context, branchID string) (int64, error) {
	return s.BranchDevices.CountDocuments(ctx, bson.D{
		{Key: "branch_id", Value: branchID},
		{Key: "status", Value: model.DeviceActive},
	})
}

// BranchDevicesByBranch lists all bindings of a branch, newest first.
func (s *Store) BranchDevicesByBranch(ctx context.Context, branchID string) ([]model.BranchDevice, error) {
	cur, err := s.BranchDevices.Find(ctx,
		bson.D{{Key: "branch_id", Value: branchID}},
		options.Find().SetSort(bson.D{{Key: "bound_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	var out []model.BranchDevice
	return out, cur.All(ctx, &out)
}

// ReleaseBranchDevice frees a seat.
func (s *Store) ReleaseBranchDevice(ctx context.Context, id string, at time.Time) error {
	res, err := s.BranchDevices.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "status", Value: model.DeviceReleased},
		{Key: "released_at", Value: at},
	}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBranchDevicesByTenant removes every seat binding (active or released)
// owned by the tenant, returning the count deleted.
func (s *Store) DeleteBranchDevicesByTenant(ctx context.Context, tenantID string) (int64, error) {
	res, err := s.BranchDevices.DeleteMany(ctx, bson.D{{Key: "tenant_id", Value: tenantID}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// TouchBranchDeviceSeen updates the heartbeat timestamp.
func (s *Store) TouchBranchDeviceSeen(ctx context.Context, id string, at time.Time) error {
	_, err := s.BranchDevices.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "last_seen_at", Value: at},
	}}})
	return err
}
