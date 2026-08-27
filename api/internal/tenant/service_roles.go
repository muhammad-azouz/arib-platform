package tenant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
)

// Errors specific to role management (spec-console-rbac T107). Bad
// permission codes surface as perm.ErrUnknownCode / perm.ErrEmptyPermissions
// directly — no need to wrap those, callers already errors.Is against them.
var (
	ErrInvalidRoleName   = errors.New("role name is required")
	ErrDuplicateRoleName = errors.New("a role with this name already exists on this tenant")
)

// RoleAssignedError means DeleteRole was refused because Count members still
// hold the role (spec D8: refuse, don't cascade — reassign first). A
// distinct type rather than a sentinel error since the console needs the
// count to word its own confirmation UI, not just a yes/no.
type RoleAssignedError struct{ Count int64 }

func (e *RoleAssignedError) Error() string {
	return fmt.Sprintf("role is assigned to %d member(s)", e.Count)
}

// RoleView shapes a TenantRole for the API. model.TenantRole carries only
// bson tags (same convention as MemberView vs. TenantMember); AssignedCount
// is not stored at all — the API surface note ("list roles + assigned
// counts") wants the console able to warn before a delete D8 will refuse
// anyway.
type RoleView struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Permissions   []string  `json:"permissions"`
	AssignedCount int       `json:"assigned_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func roleView(r model.TenantRole, assignedCount int) RoleView {
	return RoleView{
		ID: r.ID, Name: r.Name, Permissions: r.Permissions,
		AssignedCount: assignedCount, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Roles lists a tenant's custom roles with how many members currently hold
// each — any member may view them, since the members table labels each row
// with its role name (only CreateRole/UpdateRole/DeleteRole are owner-only).
// One MembersByTenant read tallied in-memory, not one CountMembersByRole
// call per role.
func (s *Service) Roles(ctx context.Context, accountID, tenantID string) ([]RoleView, error) {
	t, err := s.owned(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	members, err := s.store.MembersByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(members))
	for _, m := range members {
		if m.RoleID != "" {
			counts[m.RoleID]++
		}
	}
	views := make([]RoleView, 0, len(t.Roles))
	for _, r := range t.Roles {
		views = append(views, roleView(r, counts[r.ID]))
	}
	return views, nil
}

// Permissions returns the full permission catalog (codes only — the console
// owns the Arabic labels) for the role editor's checklist. Membership-gated
// like every other tenant-scoped read; the catalog itself never varies per
// tenant.
func (s *Service) Permissions(ctx context.Context, accountID, tenantID string) ([]string, error) {
	if _, err := s.owned(ctx, accountID, tenantID); err != nil {
		return nil, err
	}
	return perm.All, nil
}

// CreateRole adds a new custom role — owner only. permissions is normalized
// (D3's manage-implies-view rule) before storage so every future read of
// perm.Can already reflects it, rather than re-deriving the implication
// each time; name must be unique per tenant (application check — the
// embedded array has no index of its own, spec D1).
func (s *Service) CreateRole(ctx context.Context, accountID, tenantID, name string, permissions []string) (*RoleView, error) {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if role != model.RoleOwner {
		return nil, ErrOwnerOnly
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidRoleName
	}
	normalized, err := perm.Normalize(permissions)
	if err != nil {
		return nil, err
	}
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, r := range t.Roles {
		if r.Name == name {
			return nil, ErrDuplicateRoleName
		}
	}
	now := time.Now().UTC()
	newRole := model.TenantRole{
		ID: idgen.New("rol"), Name: name, Permissions: normalized,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.AddTenantRole(ctx, tenantID, newRole); err != nil {
		return nil, err
	}
	view := roleView(newRole, 0) // brand new — cannot be assigned to anyone yet
	return &view, nil
}

// UpdateRole renames a role and/or replaces its permission set — owner
// only. Same validation as CreateRole; the duplicate-name check excludes
// the role being edited (renaming a role to its own current name is not a
// conflict).
func (s *Service) UpdateRole(ctx context.Context, accountID, tenantID, roleID, name string, permissions []string) (*RoleView, error) {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if role != model.RoleOwner {
		return nil, ErrOwnerOnly
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidRoleName
	}
	normalized, err := perm.Normalize(permissions)
	if err != nil {
		return nil, err
	}
	t, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var existing *model.TenantRole
	for i := range t.Roles {
		r := &t.Roles[i]
		if r.ID == roleID {
			existing = r
			continue
		}
		if r.Name == name {
			return nil, ErrDuplicateRoleName
		}
	}
	if existing == nil {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	if err := s.store.UpdateTenantRole(ctx, tenantID, roleID, name, normalized, now); err != nil {
		return nil, err
	}
	count, err := s.store.CountMembersByRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	view := roleView(model.TenantRole{
		ID: roleID, Name: name, Permissions: normalized,
		CreatedAt: existing.CreatedAt, UpdatedAt: now,
	}, int(count))
	return &view, nil
}

// DeleteRole removes a role — owner only, and refused (not cascaded) while
// any member still holds it (spec D8); the caller learns exactly how many
// so the console can word "reassign N members first" instead of a bare
// refusal.
func (s *Service) DeleteRole(ctx context.Context, accountID, tenantID, roleID string) error {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if role != model.RoleOwner {
		return ErrOwnerOnly
	}
	count, err := s.store.CountMembersByRole(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	if count > 0 {
		return &RoleAssignedError{Count: count}
	}
	return s.store.RemoveTenantRole(ctx, tenantID, roleID, time.Now().UTC())
}
