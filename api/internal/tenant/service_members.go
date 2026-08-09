package tenant

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// Errors specific to membership management (T14).
var (
	ErrOwnerOnly         = errors.New("only the tenant owner can manage members")
	ErrAlreadyMember     = errors.New("this email is already a member of the tenant")
	ErrCannotRemoveOwner = errors.New("the tenant owner cannot be removed")
	ErrInvalidEmail      = errors.New("invalid email")
)

// MemberView is a TenantMember enriched with the account's display info —
// the console identifies people by email, not by opaque account/member ids.
type MemberView struct {
	ID        string           `json:"id"`
	AccountID string           `json:"account_id"`
	Email     string           `json:"email"`
	FirstName string           `json:"first_name,omitempty"`
	LastName  string           `json:"last_name,omitempty"`
	Role      model.MemberRole `json:"role"`
	InvitedBy string           `json:"invited_by,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

// Members lists a tenant's members. Any member may view the list —
// InviteMember/RevokeMember are the owner-only operations.
func (s *Service) Members(ctx context.Context, accountID, tenantID string) ([]MemberView, error) {
	if _, err := s.owned(ctx, accountID, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.store.MembersByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	views := make([]MemberView, 0, len(rows))
	for _, m := range rows {
		acc, err := s.store.AccountByID(ctx, m.AccountID)
		if err != nil {
			return nil, err
		}
		views = append(views, memberView(&m, acc))
	}
	return views, nil
}

// InviteMember adds a new member to the tenant by email — owner only. An
// email with no Account yet gets a bare one now, the same find-or-create
// admin.Service.FindOrCreateClient already does for admin-assigned licenses:
// no trial, no password, just an anchor for the TenantMember row and for
// AccountByEmail to find on first OTP sign-in. The existing OTP/OAuth
// findOrCreateAccount path (auth/service.go) is what actually lets the
// invitee sign in — unchanged by this feature, so "gets one on first sign-in"
// holds for the account itself even when this creates it early.
func (s *Service) InviteMember(ctx context.Context, accountID, tenantID, email string) (*MemberView, error) {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if role != model.RoleOwner {
		return nil, ErrOwnerOnly
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, ErrInvalidEmail
	}

	acc, err := s.store.AccountByEmail(ctx, email)
	if errors.Is(err, mongostore.ErrNotFound) {
		now := time.Now().UTC()
		acc = &model.Account{
			ID:          idgen.New("acc"),
			Email:       email,
			Providers:   []model.Provider{},
			ProviderIDs: map[string]string{},
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.store.InsertAccount(ctx, acc); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if _, err := s.store.MemberRole(ctx, tenantID, acc.ID); err == nil {
		return nil, ErrAlreadyMember
	} else if !errors.Is(err, mongostore.ErrNotFound) {
		return nil, err
	}

	m := &model.TenantMember{
		ID:        idgen.New("mem"),
		TenantID:  tenantID,
		AccountID: acc.ID,
		Role:      model.RoleMember,
		InvitedBy: accountID,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.InsertMember(ctx, m); err != nil {
		return nil, err
	}
	// Durable, one-directional: this account is now (and remains, even past
	// a future revoke) ineligible for Register's self-serve tenant creation.
	if err := s.store.MarkHasBeenMember(ctx, acc.ID); err != nil {
		return nil, err
	}
	view := memberView(m, acc)
	return &view, nil
}

// RevokeMember removes a member — owner only, and the owner row itself can
// never be removed (a tenant must always have exactly one owner). Effective
// immediately: the next request from the revoked account hits owned()'s
// membership lookup and gets ErrForbidden. Control-plane only — no sync
// round involved, since console access has nothing to do with a branch DB.
func (s *Service) RevokeMember(ctx context.Context, accountID, tenantID, memberID string) error {
	role, err := s.memberRole(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if role != model.RoleOwner {
		return ErrOwnerOnly
	}
	target, err := s.store.MemberByID(ctx, tenantID, memberID)
	if err != nil {
		return err
	}
	if target.Role == model.RoleOwner {
		return ErrCannotRemoveOwner
	}
	deleted, err := s.store.DeleteMember(ctx, tenantID, memberID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

func memberView(m *model.TenantMember, acc *model.Account) MemberView {
	return MemberView{
		ID: m.ID, AccountID: m.AccountID, Email: acc.Email,
		FirstName: acc.FirstName, LastName: acc.LastName,
		Role: m.Role, InvitedBy: m.InvitedBy, CreatedAt: m.CreatedAt,
	}
}
