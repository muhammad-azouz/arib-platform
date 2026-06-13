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

// InsertShard registers a central VPS. The unique index on name rejects dupes.
func (s *Store) InsertShard(ctx context.Context, sh *model.Shard) error {
	_, err := s.Shards.InsertOne(ctx, sh)
	return err
}

// ShardByID returns a shard by id.
func (s *Store) ShardByID(ctx context.Context, id string) (*model.Shard, error) {
	var sh model.Shard
	err := s.Shards.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&sh)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &sh, err
}

// ListShards returns all shards (placement decisions, ops dashboard).
func (s *Store) ListShards(ctx context.Context) ([]model.Shard, error) {
	cur, err := s.Shards.Find(ctx, bson.D{},
		options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Shard
	return out, cur.All(ctx, &out)
}

// UpdateShardStatus flips a shard between active/full/draining.
func (s *Store) UpdateShardStatus(ctx context.Context, id string, status model.ShardStatus, at time.Time) error {
	res, err := s.Shards.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "status", Value: status},
		{Key: "updated_at", Value: at},
	}}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}
