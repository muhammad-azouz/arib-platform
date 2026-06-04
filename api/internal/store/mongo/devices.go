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

// ActiveDeviceForLicense returns the active binding for a license, if any.
func (s *Store) ActiveDeviceForLicense(ctx context.Context, licenseID string) (*model.Device, error) {
	var d model.Device
	err := s.Devices.FindOne(ctx, bson.D{
		{Key: "license_id", Value: licenseID},
		{Key: "status", Value: model.DeviceActive},
	}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &d, err
}

// DeviceByID returns a device by id.
func (s *Store) DeviceByID(ctx context.Context, id string) (*model.Device, error) {
	var d model.Device
	err := s.Devices.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &d, err
}

// DevicesByAccount lists all bindings (active and released) for an account.
func (s *Store) DevicesByAccount(ctx context.Context, accountID string) ([]model.Device, error) {
	cur, err := s.Devices.Find(ctx,
		bson.D{{Key: "account_id", Value: accountID}},
		options.Find().SetSort(bson.D{{Key: "bound_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Device
	return out, cur.All(ctx, &out)
}

// InsertDevice creates a new active binding. The unique partial index on
// (license_id, status=active) guarantees only one active device per seat.
func (s *Store) InsertDevice(ctx context.Context, d *model.Device) error {
	_, err := s.Devices.InsertOne(ctx, d)
	return err
}

// IsDuplicateKey reports whether err is a Mongo duplicate-key (E11000) error.
func IsDuplicateKey(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}

// TouchDeviceValidated updates last_seen/last_validated for a heartbeat.
func (s *Store) TouchDeviceValidated(ctx context.Context, id string, at time.Time) error {
	_, err := s.Devices.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "last_seen_at", Value: at},
		{Key: "last_validated_at", Value: at},
	}}})
	return err
}

// ReleaseDevice marks a binding released, freeing the seat. selfService bumps
// the release counters used for the anti-abuse cooldown.
func (s *Store) ReleaseDevice(ctx context.Context, id string, at time.Time, selfService bool) error {
	set := bson.D{
		{Key: "status", Value: model.DeviceReleased},
		{Key: "released_at", Value: at},
	}
	update := bson.D{{Key: "$set", Value: set}}
	if selfService {
		set = append(set, bson.E{Key: "last_release_at", Value: at})
		update = bson.D{
			{Key: "$set", Value: set},
			{Key: "$inc", Value: bson.D{{Key: "release_count", Value: 1}}},
		}
	}
	_, err := s.Devices.UpdateByID(ctx, id, update)
	return err
}

// CountSelfReleasesSince counts self-service releases of a machine since t.
func (s *Store) CountSelfReleasesSince(ctx context.Context, machineID string, since time.Time) (int64, error) {
	return s.Devices.CountDocuments(ctx, bson.D{
		{Key: "machine_id", Value: machineID},
		{Key: "last_release_at", Value: bson.D{{Key: "$gte", Value: since}}},
	})
}
