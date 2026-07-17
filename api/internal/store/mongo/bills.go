package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// InsertBill records a new paid bill.
func (s *Store) InsertBill(ctx context.Context, b *model.Bill) error {
	_, err := s.Bills.InsertOne(ctx, b)
	return err
}

// BillByID returns a bill by id.
func (s *Store) BillByID(ctx context.Context, id string) (*model.Bill, error) {
	var b model.Bill
	err := s.Bills.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&b)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &b, err
}

// BillsByTenant lists every bill for a tenant, newest period first.
func (s *Store) BillsByTenant(ctx context.Context, tenantID string) ([]model.Bill, error) {
	cur, err := s.Bills.Find(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}},
		options.Find().SetSort(bson.D{{Key: "ends_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Bill
	return out, cur.All(ctx, &out)
}

// VoidBill flips a paid bill to void, recording who/why. Voiding an
// already-void bill is rejected — bills are append-only, not editable.
func (s *Store) VoidBill(ctx context.Context, id, reason string, at time.Time) error {
	res, err := s.Bills.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}, {Key: "status", Value: model.BillPaid}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "status", Value: model.BillVoid},
			{Key: "void_reason", Value: reason},
			{Key: "updated_at", Value: at},
		}}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		if _, err := s.BillByID(ctx, id); err != nil {
			return err // ErrNotFound if it truly doesn't exist
		}
		return fmt.Errorf("bill %s is not paid", id)
	}
	return nil
}
