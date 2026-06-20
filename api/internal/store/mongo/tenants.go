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

// InsertTenant creates a tenant.
func (s *Store) InsertTenant(ctx context.Context, t *model.Tenant) error {
	_, err := s.Tenants.InsertOne(ctx, t)
	return err
}

// TenantByID returns a tenant by id.
func (s *Store) TenantByID(ctx context.Context, id string) (*model.Tenant, error) {
	var t model.Tenant
	err := s.Tenants.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &t, err
}

// TenantsByAccount lists the tenants owned by an account.
func (s *Store) TenantsByAccount(ctx context.Context, accountID string) ([]model.Tenant, error) {
	cur, err := s.Tenants.Find(ctx,
		bson.D{{Key: "account_id", Value: accountID}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Tenant
	return out, cur.All(ctx, &out)
}

// UpdateTenantStatus flips a tenant between active and suspended.
func (s *Store) UpdateTenantStatus(ctx context.Context, id string, status model.TenantStatus, at time.Time) error {
	return s.updateTenant(ctx, id, bson.D{
		{Key: "status", Value: status},
		{Key: "updated_at", Value: at},
	})
}

// UpdateTenantPlan records a subscription plan change.
func (s *Store) UpdateTenantPlan(ctx context.Context, id, plan string, at time.Time) error {
	return s.updateTenant(ctx, id, bson.D{
		{Key: "plan", Value: plan},
		{Key: "updated_at", Value: at},
	})
}

// SetTenantDBName provisions a tenant's central DB on the sync server (sets the
// tenant→db map). The db_name_unique index rejects a name already in use.
func (s *Store) SetTenantDBName(ctx context.Context, id, dbName string, at time.Time) error {
	return s.updateTenant(ctx, id, bson.D{
		{Key: "db_name", Value: dbName},
		{Key: "updated_at", Value: at},
	})
}

// TenantsWithSync lists every tenant that has a central DB provisioned, i.e. is
// subscribed to sync (fleet rollout, E3).
func (s *Store) TenantsWithSync(ctx context.Context) ([]model.Tenant, error) {
	cur, err := s.Tenants.Find(ctx,
		bson.D{{Key: "db_name", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$gt", Value: ""}}}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Tenant
	return out, cur.All(ctx, &out)
}

// UpdateTenantSchema records the result of a migrate attempt against a tenant's
// central DB (E3): the verified schema version, rollout status, last error and
// attempt counter.
func (s *Store) UpdateTenantSchema(ctx context.Context, id string, version int, status model.RolloutStatus, errMsg string, attempts int, at time.Time) error {
	return s.updateTenant(ctx, id, bson.D{
		{Key: "schema_version", Value: version},
		{Key: "rollout_status", Value: status},
		{Key: "rollout_error", Value: errMsg},
		{Key: "rollout_attempts", Value: attempts},
		{Key: "rollout_at", Value: at},
		{Key: "updated_at", Value: at},
	})
}

// DeleteTenant removes a tenant's registry record. Callers must tear down its
// dependent data (company, branches, devices) and central DB first.
func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	res, err := s.Tenants.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) updateTenant(ctx context.Context, id string, set bson.D) error {
	res, err := s.Tenants.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}
