package hq

// Branch staff (console «موظفو الفروع»): typed proxy for the gateway's /hq/staff*
// and /hq/pos-roles endpoints — the AribOne POS users and roles, which are NOT
// console members (those live in Mongo; see spec-console-rbac.md). The gateway
// writes the Tier-A Users/UserRoles rows and every branch picks them up on its
// next sync.
//
// What this layer owns, beyond marshalling:
//
//  1. A first copy of the validation table (the gateway's ValidateStaff is the
//     authoritative second), so the console gets one Arabic message per field.
//  2. Branch scoping. A staff member belongs to exactly one branch, so — unlike
//     promotions — there is no company-wide case: every operation has a branch
//     identity to check. A scoped member sees and manages only staff of their
//     own branches; an out-of-allowlist row is a 404 (never a 403, so ids
//     cannot be probed), and a write's TARGET branch must be in the allowlist
//     (moving a person between branches needs both ends).
//  3. A typed response. Rows are decoded into StaffRow and re-encoded, so even
//     a gateway that one day returned a password or PIN hash field could not
//     leak it through here — it would be dropped on decode.
//
// POS roles are company-wide (the Roles table has no branch column), so the
// list needs staff.view and no branch narrowing. Editing them is a separate,
// unscoped-only operation: see service_posroles.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
)

// ErrDuplicateLoginName means another POS user already has that login name.
// Login names are unique company-wide because Users replicate to every branch.
var ErrDuplicateLoginName = errors.New("duplicate_login_name")

// --- wire types ---

// StaffRow is one POS user as the console sees it. There is deliberately no
// password or PIN field of any kind: HasPin/PinLocked/UsesDefaultPassword are
// the only things the console learns about credentials.
type StaffRow struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	LoginName           string    `json:"login_name"`
	BranchID            string    `json:"branch_id"`
	IsActive            bool      `json:"is_active"`
	RoleIDs             []string  `json:"role_ids"`
	HasPin              bool      `json:"has_pin"`
	PinLocked           bool      `json:"pin_locked"`
	UsesDefaultPassword bool      `json:"uses_default_password"`
	CreatedAt           time.Time `json:"created_at"`
}

type StaffList struct {
	Items []StaffRow `json:"items"`
}

type StaffListEnvelope struct {
	Data   StaffList  `json:"data"`
	Source string     `json:"source"`
	AsOf   *time.Time `json:"as_of,omitempty"`
}

type StaffEnvelope struct {
	Data   StaffRow   `json:"data"`
	Source string     `json:"source"`
	AsOf   *time.Time `json:"as_of,omitempty"`
}

// PosRoleRow is one POS role with the permissions it grants and how many staff
// hold it.
type PosRoleRow struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	PermissionIDs []string `json:"permission_ids"`
	StaffCount    int      `json:"staff_count"`
}

type PosPermissionRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type PosRoles struct {
	Roles       []PosRoleRow       `json:"roles"`
	Permissions []PosPermissionRow `json:"permissions"`
}

type PosRolesEnvelope struct {
	Data   PosRoles   `json:"data"`
	Source string     `json:"source"`
	AsOf   *time.Time `json:"as_of,omitempty"`
}

// StaffInput is a whole staff form, for create and update (no partial patch).
// Password is required on create; blank/absent on update keeps the current
// one. The PIN has three states on update: absent = unchanged, Pin = set,
// ClearPin = remove. Plaintext secrets exist only in this struct and the
// outbound request body; nothing here logs them.
type StaffInput struct {
	Name      string   `json:"name"`
	LoginName string   `json:"login_name"`
	Password  string   `json:"password,omitempty"`
	BranchID  string   `json:"branch_id"`
	IsActive  bool     `json:"is_active"`
	Pin       string   `json:"pin,omitempty"`
	ClearPin  bool     `json:"clear_pin,omitempty"`
	RoleIDs   []string `json:"role_ids"`
}

// StaffWriteResult is the gateway's write receipt.
type StaffWriteResult struct {
	ID        string    `json:"id"`
	WrittenAt time.Time `json:"written_at"`
}

const (
	staffMinPasswordLen = 6
	staffMinPinLen      = 4
	staffMaxPinLen      = 12
	staffMinLoginLen    = 3
)

// StaffInvalidError is a validation refusal. Message is Arabic and
// console-ready; Field names the offending input.
type StaffInvalidError struct {
	Field   string
	Message string
}

func (e *StaffInvalidError) Error() string { return e.Message }

func staffInvalid(field, message string) *StaffInvalidError {
	return &StaffInvalidError{Field: field, Message: message}
}

// validateStaff is the first of two validation copies (the gateway's
// ValidateStaff is the authoritative one). Same order as its twin so the two
// can be diffed by eye.
func validateStaff(in StaffInput, isCreate bool) *StaffInvalidError {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return staffInvalid("name", "اسم الموظف مطلوب")
	}
	if len([]rune(name)) > 50 {
		return staffInvalid("name", "اسم الموظف لا يتجاوز ٥٠ حرفاً")
	}

	login := strings.TrimSpace(in.LoginName)
	n := len([]rune(login))
	if n < staffMinLoginLen || n > 50 {
		return staffInvalid("login_name", "اسم الدخول من ٣ إلى ٥٠ حرفاً")
	}
	if strings.IndexFunc(login, unicode.IsSpace) >= 0 {
		return staffInvalid("login_name", "اسم الدخول لا يحتوي على مسافات")
	}

	if isCreate && in.Password == "" {
		return staffInvalid("password", "كلمة المرور مطلوبة")
	}
	if in.Password != "" && len([]rune(in.Password)) < staffMinPasswordLen {
		return staffInvalid("password", "كلمة المرور لا تقل عن ٦ أحرف")
	}

	if strings.TrimSpace(in.BranchID) == "" {
		return staffInvalid("branch_id", "اختر الفرع")
	}

	if in.ClearPin && in.Pin != "" {
		return staffInvalid("pin", "لا يمكن تعيين رقم PIN وحذفه معاً")
	}
	if in.Pin != "" {
		for _, r := range in.Pin {
			if r < '0' || r > '9' {
				return staffInvalid("pin", "رقم PIN يجب أن يتكون من أرقام فقط")
			}
		}
		if len(in.Pin) < staffMinPinLen {
			return staffInvalid("pin", "رقم PIN يجب ألا يقل عن ٤ أرقام")
		}
		if len(in.Pin) > staffMaxPinLen {
			return staffInvalid("pin", "رقم PIN يجب ألا يزيد عن ١٢ رقماً")
		}
	}

	for _, id := range in.RoleIDs {
		if strings.TrimSpace(id) == "" {
			return staffInvalid("role_ids", "أحد الأدوار المحددة غير صالح")
		}
	}
	return nil
}

// staffFieldMessage turns a gateway "field" name into an Arabic message for the
// case where the gateway rejects something this layer's own table missed.
var staffFieldMessage = map[string]string{
	"name":       "اسم الموظف غير صالح",
	"login_name": "اسم الدخول غير صالح",
	"password":   "كلمة المرور غير صالحة",
	"branch_id":  "الفرع غير موجود",
	"pin":        "رقم PIN غير صالح",
	"role_ids":   "أحد الأدوار المحددة غير موجود",
}

// --- reads ---

// Staff lists POS users. A scoped member's allowlist is injected as repeated
// branch_id params (applyScope), which the gateway turns into a SQL filter; a
// caller-supplied branch_id outside the allowlist is ErrForbiddenScope.
func (s *Service) Staff(ctx context.Context, accountID, tenantID string, params url.Values) (*StaffListEnvelope, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	params, err = applyScope(params, scope)
	if err != nil {
		return nil, err
	}

	u := shard.GatewayURL + "/hq/staff"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var raw struct {
		Staff []StaffRow `json:"staff"`
	}
	if err := s.getJSON(ctx, u, t.DBName, &raw); err != nil {
		return nil, err
	}
	items := raw.Staff
	if items == nil {
		items = []StaffRow{}
	}
	for i := range items {
		if items[i].RoleIDs == nil {
			items[i].RoleIDs = []string{}
		}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &StaffListEnvelope{Data: StaffList{Items: items}, Source: source, AsOf: asOf}, nil
}

// StaffMember returns one POS user. A user at a branch outside a scoped
// caller's allowlist is ErrNotFound, never ErrForbiddenScope, so ids cannot be
// probed for existence.
func (s *Service) StaffMember(ctx context.Context, accountID, tenantID, staffID string) (*StaffEnvelope, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	row, err := s.staffRow(ctx, shard, t.DBName, staffID)
	if err != nil {
		return nil, err
	}
	if err := hideOutOfScopeRow(scope, row.BranchID); err != nil {
		return nil, err
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &StaffEnvelope{Data: *row, Source: source, AsOf: asOf}, nil
}

// PosRoles lists the POS roles and the permission catalog their checklist is
// drawn from. Company-wide data: no branch narrowing, any staff.view member.
func (s *Service) PosRoles(ctx context.Context, accountID, tenantID string) (*PosRolesEnvelope, error) {
	t, shard, _, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	var data PosRoles
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/pos-roles", t.DBName, &data); err != nil {
		return nil, err
	}
	if data.Roles == nil {
		data.Roles = []PosRoleRow{}
	}
	for i := range data.Roles {
		if data.Roles[i].PermissionIDs == nil {
			data.Roles[i].PermissionIDs = []string{}
		}
	}
	if data.Permissions == nil {
		data.Permissions = []PosPermissionRow{}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &PosRolesEnvelope{Data: data, Source: source, AsOf: asOf}, nil
}

func (s *Service) staffRow(ctx context.Context, shard *model.Shard, dbName, staffID string) (*StaffRow, error) {
	var row StaffRow
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/staff/"+url.PathEscape(staffID), dbName, &row); err != nil {
		return nil, err
	}
	if row.RoleIDs == nil {
		row.RoleIDs = []string{}
	}
	return &row, nil
}

// --- writes ---

// CreateStaff validates, requires the target branch to be in the allowlist,
// then forwards.
func (s *Service) CreateStaff(ctx context.Context, accountID, tenantID string, in StaffInput) (*StaffWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if verr := validateStaff(in, true); verr != nil {
		return nil, verr
	}
	if err := requireBranchInScope(scope, in.BranchID); err != nil {
		return nil, err
	}
	return s.writeStaff(ctx, http.MethodPost, shard.GatewayURL+"/hq/staff", t.DBName, in)
}

// UpdateStaff authorizes BOTH ends of a possible branch move: the row as it
// stands (404 if outside the allowlist) and the branch it is being moved to
// (403 forbidden_scope). The lookup runs only for a scoped caller — an
// unscoped one never pays the extra gateway round trip.
func (s *Service) UpdateStaff(ctx context.Context, accountID, tenantID, staffID string, in StaffInput) (*StaffWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if verr := validateStaff(in, false); verr != nil {
		return nil, verr
	}
	if err := s.requireStaffInScope(ctx, shard, t.DBName, scope, staffID); err != nil {
		return nil, err
	}
	if err := requireBranchInScope(scope, in.BranchID); err != nil {
		return nil, err
	}
	return s.writeStaff(ctx, http.MethodPut, shard.GatewayURL+"/hq/staff/"+url.PathEscape(staffID), t.DBName, in)
}

// ClearStaffLockout clears a staff member's PIN lockout (counters only; the
// PIN itself is untouched).
func (s *Service) ClearStaffLockout(ctx context.Context, accountID, tenantID, staffID string) (*StaffWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.requireStaffInScope(ctx, shard, t.DBName, scope, staffID); err != nil {
		return nil, err
	}
	return s.writeStaff(ctx, http.MethodPost, shard.GatewayURL+"/hq/staff/"+url.PathEscape(staffID)+"/clear-lockout", t.DBName, nil)
}

// requireStaffInScope hides (404) a staff member whose branch is outside a
// scoped caller's allowlist, and is a no-op for an unscoped caller.
func (s *Service) requireStaffInScope(ctx context.Context, shard *model.Shard, dbName string, scope *perm.Scope, staffID string) error {
	if scope == nil || scope.IsUnscoped() {
		return nil
	}
	current, err := s.staffRow(ctx, shard, dbName, staffID)
	if err != nil {
		return err
	}
	return hideOutOfScopeRow(scope, current.BranchID)
}

// writeStaff performs one HQ-token-authed gateway write and maps its statuses.
// The request body carries plaintext secrets, so it is never logged and error
// text is built only from the gateway's own field name, never from the body.
func (s *Service) writeStaff(ctx context.Context, method, url, dbName string, in any) (*StaffWriteResult, error) {
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
	case http.StatusConflict:
		return nil, ErrDuplicateLoginName
	case http.StatusBadRequest:
		var b struct {
			Field string `json:"field"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&b)
		msg := staffFieldMessage[b.Field]
		if msg == "" {
			msg = "بيانات الموظف غير صالحة"
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
