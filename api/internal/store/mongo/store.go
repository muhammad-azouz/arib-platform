// Package mongostore is the MongoDB persistence layer.
package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrNotFound is returned when a lookup matches no document.
var ErrNotFound = errors.New("not found")

// Store wraps the database and the typed collections used by the API.
type Store struct {
	client *mongo.Client
	db     *mongo.Database

	Accounts  *mongo.Collection
	Licenses  *mongo.Collection
	Devices   *mongo.Collection
	Trials    *mongo.Collection
	OTPs      *mongo.Collection
	Sessions  *mongo.Collection
	Exchanges *mongo.Collection
	Audit     *mongo.Collection

	// Multi-tenant registry (control plane).
	Tenants       *mongo.Collection
	Companies     *mongo.Collection
	Branches      *mongo.Collection
	BranchDevices *mongo.Collection
	Shards        *mongo.Collection
	Bills         *mongo.Collection
}

// Connect dials MongoDB, pings it, and returns a Store with collection handles.
func Connect(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	db := client.Database(dbName)
	return &Store{
		client:    client,
		db:        db,
		Accounts:  db.Collection("accounts"),
		Licenses:  db.Collection("licenses"),
		Devices:   db.Collection("devices"),
		Trials:    db.Collection("trial_ledger"),
		OTPs:      db.Collection("otps"),
		Sessions:  db.Collection("sessions"),
		Exchanges: db.Collection("oauth_exchanges"),
		Audit:     db.Collection("audit_log"),

		Tenants:       db.Collection("tenants"),
		Companies:     db.Collection("companies"),
		Branches:      db.Collection("branches"),
		BranchDevices: db.Collection("branch_devices"),
		Shards:        db.Collection("shards"),
		Bills:         db.Collection("bills"),
	}, nil
}

// Close disconnects the client.
func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

// DropDatabase drops the whole database (test teardown only).
func (s *Store) DropDatabase(ctx context.Context) error { return s.db.Drop(ctx) }

// EnsureIndexes creates the unique and TTL indexes the API relies on.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	specs := []struct {
		coll  *mongo.Collection
		model mongo.IndexModel
	}{
		{s.Accounts, mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{s.Licenses, mongo.IndexModel{Keys: bson.D{{Key: "key", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{s.Licenses, mongo.IndexModel{Keys: bson.D{{Key: "account_id", Value: 1}}}},
		// Dedupes Phase-2 billing webhook retries (only set for billing-sourced issuance).
		{s.Licenses, mongo.IndexModel{
			Keys: bson.D{{Key: "external_ref", Value: 1}},
			Options: options.Index().
				SetName("external_ref_unique").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "external_ref", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$gt", Value: ""}}}}),
		}},
		{s.Devices, mongo.IndexModel{Keys: bson.D{{Key: "account_id", Value: 1}}}},
		{s.Devices, mongo.IndexModel{Keys: bson.D{{Key: "license_id", Value: 1}}}},
		{s.Devices, mongo.IndexModel{Keys: bson.D{{Key: "machine_id", Value: 1}}}},
		// One active binding per license seat.
		{s.Devices, mongo.IndexModel{
			Keys: bson.D{{Key: "license_id", Value: 1}},
			Options: options.Index().
				SetName("license_id_active_unique").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "status", Value: string("active")}}),
		}},
		{s.OTPs, mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}}},
		// TTL: expire OTPs / exchanges automatically.
		{s.OTPs, mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
		{s.Exchanges, mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
		{s.Sessions, mongo.IndexModel{Keys: bson.D{{Key: "account_id", Value: 1}}}},
		{s.Sessions, mongo.IndexModel{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{s.Sessions, mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
		{s.Audit, mongo.IndexModel{Keys: bson.D{{Key: "created_at", Value: -1}}}},

		// --- Multi-tenant registry ---
		{s.Tenants, mongo.IndexModel{Keys: bson.D{{Key: "account_id", Value: 1}}}},
		// Globally-unique central DB name (only for sync-provisioned tenants).
		{s.Tenants, mongo.IndexModel{
			Keys: bson.D{{Key: "db_name", Value: 1}},
			Options: options.Index().
				SetName("db_name_unique").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "db_name", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$gt", Value: ""}}}}),
		}},
		// One company per tenant (D15).
		{s.Companies, mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{s.Branches, mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}}},
		{s.Branches, mongo.IndexModel{Keys: bson.D{{Key: "company_id", Value: 1}}}},
		{s.BranchDevices, mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}}}},
		{s.BranchDevices, mongo.IndexModel{Keys: bson.D{{Key: "branch_id", Value: 1}, {Key: "status", Value: 1}}}},
		{s.BranchDevices, mongo.IndexModel{Keys: bson.D{{Key: "machine_id", Value: 1}}}},
		// One active binding per machine per branch (seat dedupe; the seat
		// COUNT limit is enforced in the service layer).
		{s.BranchDevices, mongo.IndexModel{
			Keys: bson.D{{Key: "branch_id", Value: 1}, {Key: "machine_id", Value: 1}},
			Options: options.Index().
				SetName("branch_machine_active_unique").
				SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "status", Value: string("active")}}),
		}},
		// Coverage lookups: latest-ending bill per tenant.
		{s.Bills, mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "ends_at", Value: -1}}}},
	}
	for _, sp := range specs {
		if _, err := sp.coll.Indexes().CreateOne(ctx, sp.model); err != nil {
			return fmt.Errorf("create index on %s: %w", sp.coll.Name(), err)
		}
	}
	return nil
}
