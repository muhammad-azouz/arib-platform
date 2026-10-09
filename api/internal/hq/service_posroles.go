package hq

// POS role management (console «أدوار نقطة البيع», write side): typed proxy for
// the gateway's POST/PUT/DELETE /hq/pos-roles. The Roles and RolePermissions
// tables are Tier-A and have no branch column, so a role is company-wide by
// construction and every branch receives the same set on its next sync.
//
// Because a role write lands at every branch and carries no branch identity of
// its own, it is refused for a branch-scoped member (requireUnscoped,
// ErrForbiddenUnscoped) however many permissions their console role holds. The
// route rule requires pos_roles.manage; this file owns the unscoped half.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrDuplicateRoleName means another POS role already has that name
	// (case-insensitive).
	ErrDuplicateRoleName = errors.New("duplicate_role_name")
	// ErrRoleInUse means staff still hold the role, so it cannot be deleted.
	ErrRoleInUse = errors.New("role_in_use")
	// ErrRoleProtected means the seeded Administrator role, which is immutable
	// from the console so nobody can lock a branch out of its own admin screens.
	ErrRoleProtected = errors.New("role_protected")
)

const (
	posRoleMaxName        = 50
	posRoleMaxDescription = 200
)

// PosRoleInput is a whole role form. PermissionIDs replaces the role's
// permission set; an empty set is allowed (the desktop allows it too).
type PosRoleInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	PermissionIDs []string `json:"permission_ids"`
}

// posRoleFieldMessage turns a gateway "field" name into an Arabic message for a
// refusal this layer's own table missed.
var posRoleFieldMessage = map[string]string{
	"name":           "اسم الدور غير صالح",
	"description":    "وصف الدور غير صالح",
	"permission_ids": "إحدى الصلاحيات المحددة غير موجودة",
}

// validatePosRole is the first of two validation copies (the gateway's
// ValidatePosRole is the authoritative one), in the same order as its twin.
func validatePosRole(in PosRoleInput) *StaffInvalidError {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return staffInvalid("name", "اسم الدور مطلوب")
	}
	if len([]rune(name)) > posRoleMaxName {
		return staffInvalid("name", "اسم الدور لا يتجاوز ٥٠ حرفاً")
	}
	if len([]rune(strings.TrimSpace(in.Description))) > posRoleMaxDescription {
		return staffInvalid("description", "وصف الدور لا يتجاوز ٢٠٠ حرف")
	}
	for _, id := range in.PermissionIDs {
		if strings.TrimSpace(id) == "" {
			return staffInvalid("permission_ids", "إحدى الصلاحيات المحددة غير صالحة")
		}
	}
	return nil
}

// CreatePosRole creates a company-wide role. Unscoped members only.
func (s *Service) CreatePosRole(ctx context.Context, accountID, tenantID string, in PosRoleInput) (*StaffWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := requireUnscoped(scope); err != nil {
		return nil, err
	}
	if verr := validatePosRole(in); verr != nil {
		return nil, verr
	}
	return s.writePosRole(ctx, http.MethodPost, shard.GatewayURL+"/hq/pos-roles", t.DBName, in)
}

// UpdatePosRole replaces a role's name, description and permission set.
func (s *Service) UpdatePosRole(ctx context.Context, accountID, tenantID, roleID string, in PosRoleInput) (*StaffWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := requireUnscoped(scope); err != nil {
		return nil, err
	}
	if verr := validatePosRole(in); verr != nil {
		return nil, verr
	}
	return s.writePosRole(ctx, http.MethodPut, shard.GatewayURL+"/hq/pos-roles/"+url.PathEscape(roleID), t.DBName, in)
}

// DeletePosRole removes an unused role. The gateway refuses a role that staff
// still hold (ErrRoleInUse) and the Administrator role (ErrRoleProtected).
func (s *Service) DeletePosRole(ctx context.Context, accountID, tenantID, roleID string) (*StaffWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := requireUnscoped(scope); err != nil {
		return nil, err
	}
	return s.writePosRole(ctx, http.MethodDelete, shard.GatewayURL+"/hq/pos-roles/"+url.PathEscape(roleID), t.DBName, nil)
}

// writePosRole performs one HQ-token-authed gateway write and maps its
// statuses, using the gateway's "code" on a 409 to tell a name clash from a
// role that is still in use.
func (s *Service) writePosRole(ctx context.Context, method, url, dbName string, in any) (*StaffWriteResult, error) {
	tok, err := s.tokens.IssueHQToken(dbName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	var body []byte
	if in != nil {
		if body, err = json.Marshal(in); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusForbidden:
		return nil, ErrRoleProtected
	case http.StatusConflict:
		var b struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&b)
		if b.Code == "role_in_use" {
			return nil, ErrRoleInUse
		}
		return nil, ErrDuplicateRoleName
	case http.StatusBadRequest:
		var b struct {
			Field string `json:"field"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&b)
		msg := posRoleFieldMessage[b.Field]
		if msg == "" {
			msg = "بيانات الدور غير صالحة"
		}
		return nil, &StaffInvalidError{Field: b.Field, Message: msg}
	case http.StatusServiceUnavailable:
		return nil, ErrTenantNotProvisioned
	case http.StatusOK, http.StatusCreated:
		var result StaffWriteResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}
