package mongostore

import (
	"context"
	"errors"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
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

// TenantsForAccount lists every tenant the account can reach — as owner or
// as an invited member — via TenantMembers, so an active member's console
// resolver (GET /v1/tenants) actually shows the tenant they were invited
// into instead of the owner-only query treating them as tenant-less.
func (s *Store) TenantsForAccount(ctx context.Context, accountID string) ([]model.Tenant, error) {
	ids, err := s.TenantIDsForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []model.Tenant{}, nil
	}
	cur, err := s.Tenants.Find(ctx,
		bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Tenant
	return out, cur.All(ctx, &out)
}

// AllTenants lists every tenant in the registry, regardless of owner —
// used only by the one-time BackfillOwnerMembers pass (T13).
func (s *Store) AllTenants(ctx context.Context) ([]model.Tenant, error) {
	cur, err := s.Tenants.Find(ctx, bson.D{})
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

// AddTenantRole appends a new role to the tenant's embedded role array
// (spec-console-rbac D1).
func (s *Store) AddTenantRole(ctx context.Context, tenantID string, role model.TenantRole) error {
	res, err := s.Tenants.UpdateByID(ctx, tenantID, bson.D{
		{Key: "$push", Value: bson.D{{Key: "roles", Value: role}}},
		{Key: "$set", Value: bson.D{{Key: "updated_at", Value: role.UpdatedAt}}},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateTenantRole renames a role and/or replaces its permission set by id.
// The update is a positional arrayFilters $set against the one matched
// array element, not a rewrite of the whole roles array, so editing two
// different roles on one tenant concurrently cannot clobber either
// (spec-console-rbac D1's stated cost of embedding). Returns ErrNotFound if
// the tenant doesn't exist, or if no role in its array matches roleID.
func (s *Store) UpdateTenantRole(ctx context.Context, tenantID, roleID, name string, permissions []string, at time.Time) error {
	// Self-heal: a tenant that has never had a role added (Roles is
	// `bson:"roles,omitempty"` and unset at Register time — see
	// AddTenantRole) has no `roles` field at all, and Mongo rejects a
	// `roles.$[r]` arrayFilters update against a path that doesn't exist as
	// an array, rather than simply matching zero elements. Seeding an empty
	// array first (guarded so it never runs once the field is present, so
	// it can't race a concurrent AddTenantRole/UpdateTenantRole into
	// clobbering it) makes the arrayFilters update below behave exactly
	// like "no role in the array matches roleID" either way.
	if _, err := s.Tenants.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: tenantID}, {Key: "roles", Value: bson.D{{Key: "$exists", Value: false}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "roles", Value: bson.A{}}}}},
	); err != nil {
		return err
	}
	// Whether roleID actually matched can't be read off MatchedCount or
	// ModifiedCount: MatchedCount reflects the top-level tenant document, so
	// an arrayFilters update that selects zero elements still "matches" the
	// tenant; and ModifiedCount is equally unreliable here, since the
	// sibling top-level `updated_at` in this same $set always changes (a
	// fresh `at` on every call) regardless of whether roles.$[r] matched
	// anything — confirmed empirically, not just in theory. FindOneAndUpdate
	// sidesteps both by returning the document as it was *before* this
	// update (the driver's default ReturnDocument) in the same atomic
	// operation, so checking roleID against that snapshot's Roles is
	// race-free against a concurrent edit of the same role.
	var before model.Tenant
	err := s.Tenants.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: tenantID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "roles.$[r].name", Value: name},
			{Key: "roles.$[r].permissions", Value: permissions},
			{Key: "roles.$[r].updated_at", Value: at},
			{Key: "updated_at", Value: at},
		}}},
		options.FindOneAndUpdate().SetArrayFilters([]any{bson.D{{Key: "r.id", Value: roleID}}}),
	).Decode(&before)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	for _, r := range before.Roles {
		if r.ID == roleID {
			return nil
		}
	}
	return ErrNotFound
}

// RemoveTenantRole deletes a role from the tenant's embedded role array by
// id. Callers must check CountMembersByRole first — deleting an assigned
// role is refused, not cascaded (spec-console-rbac D8).
//
// Whether roleID actually existed can't be read off ModifiedCount here the
// way UpdateTenantRole reads it off its array-filtered element: updated_at
// is set at the top level, alongside (not inside) the $pull, so it alone
// always flips ModifiedCount to 1 — a fresh timestamp on every call — even
// when $pull matched nothing. FindOneAndUpdate sidesteps that by returning
// the document as it was *before* this update (the driver's default
// ReturnDocument), in the same atomic operation, so checking roleID against
// that snapshot's Roles is race-free against a concurrent remove of the
// same role.
func (s *Store) RemoveTenantRole(ctx context.Context, tenantID, roleID string, at time.Time) error {
	var before model.Tenant
	err := s.Tenants.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: tenantID}},
		bson.D{
			{Key: "$pull", Value: bson.D{{Key: "roles", Value: bson.D{{Key: "id", Value: roleID}}}}},
			{Key: "$set", Value: bson.D{{Key: "updated_at", Value: at}}},
		},
	).Decode(&before)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	for _, r := range before.Roles {
		if r.ID == roleID {
			return nil
		}
	}
	return ErrNotFound
}

// Default role names seeded by BackfillRolesAndMembers (T103). Not
// protected — once created they are ordinary, owner-authored roles, just
// as editable and deletable as any other (spec-console-rbac D1).
const (
	backfillFullAccessRoleName = "وصول كامل"
	backfillReadOnlyRoleName   = "قراءة فقط"
)

// backfillReadOnlyPermissions is every ".view" code in the D3 catalog —
// what «قراءة فقط» grants.
var backfillReadOnlyPermissions = []string{
	perm.BranchesView, perm.CatalogView, perm.InventoryView,
	perm.CustomersView, perm.SuppliersView, perm.OrdersView,
	perm.ReportsView, perm.ConflictsView,
}

// BackfillRolesAndMembers seeds two default roles per tenant — «وصول كامل»
// (every permission) and «قراءة فقط» (every .view permission) — then points
// each pre-existing Role: "member" row that has no RoleID yet at «وصول
// كامل», unscoped, with AcceptedAt set to the member's own CreatedAt: they
// already had full, unscoped console access before roles existed (there
// was nothing else to have), so the migration must preserve that rather
// than narrow it, and they are not "pending" — they've been using the
// console all along (T103, spec-console-rbac D1/D3).
//
// Idempotent and safe on every boot, same shape as BackfillOwnerMembers: a
// role is (re-)seeded only if no role of that name exists yet on the
// tenant, and a member row is touched only while its RoleID is still
// empty, so a second run makes zero changes. Owner rows are never assigned
// a RoleID. A tenant with no members still gets both seeded roles.
func (s *Store) BackfillRolesAndMembers(ctx context.Context) (rolesSeeded, membersUpdated int, err error) {
	tenants, err := s.AllTenants(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, t := range tenants {
		now := time.Now().UTC()

		var fullAccessID string
		haveFull, haveReadOnly := false, false
		for _, r := range t.Roles {
			switch r.Name {
			case backfillFullAccessRoleName:
				haveFull, fullAccessID = true, r.ID
			case backfillReadOnlyRoleName:
				haveReadOnly = true
			}
		}
		if !haveFull {
			fullAccessID = idgen.New("rol")
			if err := s.AddTenantRole(ctx, t.ID, model.TenantRole{
				ID: fullAccessID, Name: backfillFullAccessRoleName,
				Permissions: perm.All, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return rolesSeeded, membersUpdated, err
			}
			rolesSeeded++
		}
		if !haveReadOnly {
			if err := s.AddTenantRole(ctx, t.ID, model.TenantRole{
				ID: idgen.New("rol"), Name: backfillReadOnlyRoleName,
				Permissions: backfillReadOnlyPermissions, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return rolesSeeded, membersUpdated, err
			}
			rolesSeeded++
		}

		members, err := s.MembersByTenant(ctx, t.ID)
		if err != nil {
			return rolesSeeded, membersUpdated, err
		}
		for _, m := range members {
			if m.Role != model.RoleMember || m.RoleID != "" {
				continue
			}
			res, err := s.TenantMembers.UpdateByID(ctx, m.ID, bson.D{{Key: "$set", Value: bson.D{
				{Key: "role_id", Value: fullAccessID},
				{Key: "accepted_at", Value: m.CreatedAt},
			}}})
			if err != nil {
				return rolesSeeded, membersUpdated, err
			}
			if res.ModifiedCount > 0 {
				membersUpdated++
			}
		}
	}
	return rolesSeeded, membersUpdated, nil
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
