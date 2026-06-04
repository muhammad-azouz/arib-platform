package mongostore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// AccountByEmail returns the account for a (case-insensitive) email.
func (s *Store) AccountByEmail(ctx context.Context, email string) (*model.Account, error) {
	var a model.Account
	err := s.Accounts.FindOne(ctx, bson.D{{Key: "email", Value: normEmail(email)}}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &a, err
}

// AccountByID returns the account with the given id.
func (s *Store) AccountByID(ctx context.Context, id string) (*model.Account, error) {
	var a model.Account
	err := s.Accounts.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &a, err
}

// InsertAccount stores a new account.
func (s *Store) InsertAccount(ctx context.Context, a *model.Account) error {
	a.Email = normEmail(a.Email)
	_, err := s.Accounts.InsertOne(ctx, a)
	return err
}

// UpdateAccount replaces mutable fields and bumps updated_at.
func (s *Store) UpdateAccount(ctx context.Context, a *model.Account) error {
	a.UpdatedAt = time.Now().UTC()
	_, err := s.Accounts.UpdateByID(ctx, a.ID, bson.D{{Key: "$set", Value: bson.D{
		{Key: "first_name", Value: a.FirstName},
		{Key: "last_name", Value: a.LastName},
		{Key: "providers", Value: a.Providers},
		{Key: "provider_ids", Value: a.ProviderIDs},
		{Key: "notes", Value: a.Notes},
		{Key: "updated_at", Value: a.UpdatedAt},
	}}})
	return err
}

// SearchAccounts returns accounts matching an email/name substring (admin).
func (s *Store) SearchAccounts(ctx context.Context, q string, limit int64) ([]model.Account, error) {
	filter := bson.D{}
	if q = strings.TrimSpace(q); q != "" {
		rx := bson.D{{Key: "$regex", Value: q}, {Key: "$options", Value: "i"}}
		filter = bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "email", Value: rx}},
			bson.D{{Key: "first_name", Value: rx}},
			bson.D{{Key: "last_name", Value: rx}},
		}}}
	}
	cur, err := s.Accounts.Find(ctx, filter, findLimitSorted(limit))
	if err != nil {
		return nil, err
	}
	var out []model.Account
	return out, cur.All(ctx, &out)
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
