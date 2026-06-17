package mongostore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// UpsertShard creates a shard record or updates its mutable fields in place.
// created_at is preserved on existing documents — only set on first insert —
// so re-running this on every API restart doesn't erase the true provisioning
// date.
func (s *Store) UpsertShard(ctx context.Context, sh *model.Shard) error {
	_, err := s.Shards.UpdateOne(
		ctx,
		bson.D{{Key: "_id", Value: sh.ID}},
		bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "gateway_url", Value: sh.GatewayURL},
				{Key: "status", Value: sh.Status},
				{Key: "updated_at", Value: sh.UpdatedAt},
			}},
			{Key: "$setOnInsert", Value: bson.D{
				{Key: "created_at", Value: sh.CreatedAt},
			}},
		},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// ShardByID returns a shard by its ID.
func (s *Store) ShardByID(ctx context.Context, id string) (*model.Shard, error) {
	var sh model.Shard
	err := s.Shards.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&sh)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &sh, err
}

// ListActiveShards returns all shards with status = "active", sorted by ID.
func (s *Store) ListActiveShards(ctx context.Context) ([]model.Shard, error) {
	cur, err := s.Shards.Find(
		ctx,
		bson.D{{Key: "status", Value: string(model.ShardActive)}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	var out []model.Shard
	return out, cur.All(ctx, &out)
}

// CountTenantsByShard aggregates the number of sync-provisioned tenants per
// shard. Only tenants with a non-empty db_name (i.e. sync-subscribed) are
// counted. Returns an empty map if none exist.
func (s *Store) CountTenantsByShard(ctx context.Context) (map[string]int, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "db_name", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$gt", Value: ""}}},
		}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$shard_id"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}
	cur, err := s.Tenants.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	type row struct {
		ID    string `bson:"_id"`
		Count int    `bson:"count"`
	}
	var rows []row
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Count
	}
	return out, nil
}

// SetTenantShard records the shard assignment for a tenant unconditionally.
func (s *Store) SetTenantShard(ctx context.Context, tenantID, shardID string, at time.Time) error {
	return s.updateTenant(ctx, tenantID, bson.D{
		{Key: "shard_id", Value: shardID},
		{Key: "updated_at", Value: at},
	})
}

// AssignShardIfEmpty atomically sets shard_id only when the tenant currently
// has no shard assigned (empty or missing field). Returns true when the write
// landed, false when a concurrent writer already set a shard (lost the CAS).
func (s *Store) AssignShardIfEmpty(ctx context.Context, tenantID, shardID string, at time.Time) (bool, error) {
	res, err := s.Tenants.UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: tenantID},
			{Key: "$or", Value: bson.A{
				bson.D{{Key: "shard_id", Value: ""}},
				bson.D{{Key: "shard_id", Value: bson.D{{Key: "$exists", Value: false}}}},
			}},
		},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "shard_id", Value: shardID},
			{Key: "updated_at", Value: at},
		}}},
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

// LeastLoadedShard returns the active shard with the fewest sync-provisioned
// tenants. Tie-breaks deterministically on shard ID (alphabetical lowest).
// Returns ErrNotFound if there are no active shards.
func (s *Store) LeastLoadedShard(ctx context.Context) (*model.Shard, error) {
	shards, err := s.ListActiveShards(ctx)
	if err != nil {
		return nil, err
	}
	if len(shards) == 0 {
		return nil, ErrNotFound
	}
	counts, err := s.CountTenantsByShard(ctx)
	if err != nil {
		return nil, err
	}
	// Sort: fewest tenants first; tie → lowest shard ID (already sorted by ID
	// from ListActiveShards, so a stable sort preserves that).
	sort.SliceStable(shards, func(i, j int) bool {
		return counts[shards[i].ID] < counts[shards[j].ID]
	})
	return &shards[0], nil
}
