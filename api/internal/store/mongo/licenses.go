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

// SetLicenseModules replaces a license's modules and AribLink seat count.
func (s *Store) SetLicenseModules(ctx context.Context, id string, modules []string, seats int) error {
	res, err := s.Licenses.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "modules", Value: modules},
		{Key: "seats", Value: seats},
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

// BackfillCatalogModules grants the full module catalog to every license
// whose modules predate it (model.IsLegacyModuleList): those owners always
// had every feature, so moving them onto the catalog must not narrow them.
// Idempotent — a backfilled license carries the core Bills code and is
// skipped on the next boot.
func (s *Store) BackfillCatalogModules(ctx context.Context) (int, error) {
	cur, err := s.Licenses.Find(ctx, bson.D{{Key: "modules", Value: bson.D{{Key: "$ne", Value: model.ModuleBills}}}},
		options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return 0, err
	}
	var rows []struct {
		ID string `bson:"_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		res, err := s.Licenses.UpdateOne(ctx,
			bson.D{{Key: "_id", Value: r.ID}, {Key: "modules", Value: bson.D{{Key: "$ne", Value: model.ModuleBills}}}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "modules", Value: model.AllModules},
				{Key: "updated_at", Value: time.Now().UTC()},
			}}})
		if err != nil {
			return n, err
		}
		n += int(res.ModifiedCount)
	}
	return n, nil
}
