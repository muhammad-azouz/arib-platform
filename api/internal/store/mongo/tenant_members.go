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

// InsertMember adds a tenant member. The tenant_account_unique index rejects
// a duplicate (tenant_id, account_id) pair.
func (s *Store) InsertMember(ctx context.Context, m *model.TenantMember) error {
	_, err := s.TenantMembers.InsertOne(ctx, m)
	return err
}

// InsertMemberIfAbsent inserts a member unless one already exists for
// (tenant_id, account_id), reporting whether it inserted. Used by
// BackfillOwnerMembers, which must be safe to re-run on every startup.
func (s *Store) InsertMemberIfAbsent(ctx context.Context, m *model.TenantMember) (bool, error) {
	_, err := s.TenantMembers.InsertOne(ctx, m)
	if IsDuplicateKey(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MemberByAccount returns the account's full membership row on the tenant,
// or ErrNotFound if the account has no membership row there. This is the
// one lookup every /tenants/{id}/** authorization check now goes through
// (T13); it decodes the whole TenantMember document, so role_id/branch_ids
// (console RBAC, spec-console-rbac D9) are free on every request instead of
// costing a second read. Formerly MemberRole, which decoded the same row
// and returned only .Role.
func (s *Store) MemberByAccount(ctx context.Context, tenantID, accountID string) (*model.TenantMember, error) {
	var m model.TenantMember
	err := s.TenantMembers.FindOne(ctx, bson.D{
		{Key: "tenant_id", Value: tenantID},
		{Key: "account_id", Value: accountID},
	}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CountMembersByRole counts a tenant's members currently assigned roleID —
// the delete guard for spec-console-rbac D8 (deleting a role that is still
// assigned is refused with the count, not cascaded). Reads tenant_members
// directly rather than tracking holders on the role itself.
func (s *Store) CountMembersByRole(ctx context.Context, tenantID, roleID string) (int64, error) {
	return s.TenantMembers.CountDocuments(ctx, bson.D{
		{Key: "tenant_id", Value: tenantID},
		{Key: "role_id", Value: roleID},
	})
}

// MemberByID returns a member row by id, scoped to tenantID, or ErrNotFound.
func (s *Store) MemberByID(ctx context.Context, tenantID, id string) (*model.TenantMember, error) {
	var m model.TenantMember
	err := s.TenantMembers.FindOne(ctx, bson.D{
		{Key: "_id", Value: id},
		{Key: "tenant_id", Value: tenantID},
	}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkMemberAccepted stamps accepted_at on a member's first authenticated
// request on this tenant (spec-console-rbac D6) — "pending" is derived from
// accepted_at being unset, never tracked as a separate status field. The
// accepted_at:{$exists:false} filter makes the write a genuine no-op once
// already stamped — including under two concurrent first requests — so the
// caller (httpapi.requirePerm) doesn't have to serialize on its own in-memory
// read of the row to get "exactly once" right.
func (s *Store) MarkMemberAccepted(ctx context.Context, tenantID, memberID string) error {
	_, err := s.TenantMembers.UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: memberID},
			{Key: "tenant_id", Value: tenantID},
			{Key: "accepted_at", Value: bson.D{{Key: "$exists", Value: false}}},
		},
		bson.D{{Key: "$set", Value: bson.D{{Key: "accepted_at", Value: time.Now().UTC()}}}},
	)
	return err
}

// TenantIDsForAccount returns the ids of every tenant the account has a
// membership row on — owner or member — sourced from TenantMembers so it
// stays correct across revocation for the caller (unlike a snapshot on the
// account itself, revocation deletes the row and this simply stops
// returning it).
func (s *Store) TenantIDsForAccount(ctx context.Context, accountID string) ([]string, error) {
	cur, err := s.TenantMembers.Find(ctx, bson.D{{Key: "account_id", Value: accountID}})
	if err != nil {
		return nil, err
	}
	var rows []model.TenantMember
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, m := range rows {
		ids[i] = m.TenantID
	}
	return ids, nil
}

// MemberAccountIDs returns the distinct account ids holding at least one
// member-role (non-owner) TenantMember row — used only by the one-time
// HasBeenMember backfill for members invited before that flag existed.
func (s *Store) MemberAccountIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.TenantMembers.Distinct(ctx, "account_id", bson.D{{Key: "role", Value: model.RoleMember}}).Decode(&ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// MembersByTenant lists a tenant's members, oldest first.
func (s *Store) MembersByTenant(ctx context.Context, tenantID string) ([]model.TenantMember, error) {
	cur, err := s.TenantMembers.Find(ctx,
		bson.D{{Key: "tenant_id", Value: tenantID}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.TenantMember
	return out, cur.All(ctx, &out)
}

// DeleteMember removes one member by id, scoped to tenantID, reporting
// whether a row was deleted.
func (s *Store) DeleteMember(ctx context.Context, tenantID, memberID string) (bool, error) {
	res, err := s.TenantMembers.DeleteOne(ctx, bson.D{
		{Key: "_id", Value: memberID},
		{Key: "tenant_id", Value: tenantID},
	})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

// UpdateMemberRole sets role_id and branch_ids on a member row, scoped to
// tenantID, reporting whether a row was found (spec-console-rbac T109 —
// reassign role and branches without revoking and re-inviting). Both
// validation (does roleID exist, do the branchIDs belong to this tenant) and
// the owner-row guard are the caller's job (tenant.Service.AssignMemberRole)
// — this is a bare, unconditional write.
func (s *Store) UpdateMemberRole(ctx context.Context, tenantID, memberID, roleID string, branchIDs []string) (bool, error) {
	res, err := s.TenantMembers.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: memberID}, {Key: "tenant_id", Value: tenantID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "role_id", Value: roleID},
			{Key: "branch_ids", Value: branchIDs},
		}}},
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}
