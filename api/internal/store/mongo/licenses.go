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

// InsertLicense stores a new license.
func (s *Store) InsertLicense(ctx context.Context, l *model.License) error {
	_, err := s.Licenses.InsertOne(ctx, l)
	return err
}

// LicenseByID returns a license by id.
func (s *Store) LicenseByID(ctx context.Context, id string) (*model.License, error) {
	var l model.License
	err := s.Licenses.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&l)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &l, err
}

// LicensesByAccount lists every license owned by an account, newest first.
func (s *Store) LicensesByAccount(ctx context.Context, accountID string) ([]model.License, error) {
	cur, err := s.Licenses.Find(ctx,
		bson.D{{Key: "account_id", Value: accountID}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	var out []model.License
	return out, cur.All(ctx, &out)
}

// SetLicenseStatus updates the lifecycle status of a license.
func (s *Store) SetLicenseStatus(ctx context.Context, id string, status model.LicenseStatus) error {
	_, err := s.Licenses.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "status", Value: status},
		{Key: "updated_at", Value: time.Now().UTC()},
	}}})
	return err
}

// SetLicenseUpdatesUntil moves a license's update-entitlement window
// (renewal). nil clears it to unlimited (grandfathered).
func (s *Store) SetLicenseUpdatesUntil(ctx context.Context, id string, until *time.Time) error {
	res, err := s.Licenses.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "updates_until", Value: until},
		{Key: "updated_at", Value: time.Now().UTC()},
	}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}
