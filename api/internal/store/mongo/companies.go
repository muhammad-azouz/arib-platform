package mongostore

import (
	"context"
	"errors"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// UpsertCompany inserts or fully replaces a company by its GUID. Used both for
// cloud-minted companies and for adopting a standalone tenant's existing local
// company on subscribe (cloud is authoritative afterwards). The unique index on
// tenant_id enforces one company per tenant (D15).
func (s *Store) UpsertCompany(ctx context.Context, c *model.Company) error {
	_, err := s.Companies.ReplaceOne(ctx,
		bson.D{{Key: "_id", Value: c.ID}}, c, options.Replace().SetUpsert(true))
	return err
}

// CompanyByTenant returns the tenant's single company (D15).
func (s *Store) CompanyByTenant(ctx context.Context, tenantID string) (*model.Company, error) {
	var c model.Company
	err := s.Companies.FindOne(ctx, bson.D{{Key: "tenant_id", Value: tenantID}}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &c, err
}

// DeleteCompanyByTenant removes the tenant's company, if any, returning
// whether one existed.
func (s *Store) DeleteCompanyByTenant(ctx context.Context, tenantID string) (bool, error) {
	res, err := s.Companies.DeleteOne(ctx, bson.D{{Key: "tenant_id", Value: tenantID}})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

// CompanyByID returns a company by its GUID.
func (s *Store) CompanyByID(ctx context.Context, id string) (*model.Company, error) {
	var c model.Company
	err := s.Companies.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &c, err
}

