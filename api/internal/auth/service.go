package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/idgen"
	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
)

// Common auth errors.
var (
	ErrInvalidCode  = errors.New("invalid or expired code")
	ErrTooManyTries = errors.New("too many attempts")
	ErrNoEmail      = errors.New("provider did not return an email")
)

// Mailer delivers login codes.
type Mailer interface {
	SendOTP(ctx context.Context, email, code string) error
}

// TrialProvisioner creates the signup trial license.
type TrialProvisioner interface {
	CreateTrial(ctx context.Context, accountID string) (*model.License, error)
}

// Tokens is the session pair returned to a client after login.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // access-token lifetime, seconds
}

// Service implements account provisioning, OTP, sessions and OAuth handoff.
type Service struct {
	store          *mongostore.Store
	tokens         *TokenManager
	oauth          *OAuth
	mail           Mailer
	trials         TrialProvisioner
	isAdmin        func(email string) bool
	otpTTL         time.Duration
	otpMaxAttempts int
}

// Deps bundles Service dependencies.
type Deps struct {
	Store          *mongostore.Store
	Tokens         *TokenManager
	OAuth          *OAuth
	Mail           Mailer
	Trials         TrialProvisioner
	IsAdmin        func(email string) bool
	OTPTTL         time.Duration
	OTPMaxAttempts int
}

// NewService builds the auth Service.
func NewService(d Deps) *Service {
	return &Service{
		store: d.Store, tokens: d.Tokens, oauth: d.OAuth, mail: d.Mail,
		trials: d.Trials, isAdmin: d.IsAdmin,
		otpTTL: d.OTPTTL, otpMaxAttempts: d.OTPMaxAttempts,
	}
}

// OAuth exposes the provider helper for HTTP handlers.
func (s *Service) OAuth() *OAuth { return s.oauth }

// TokenManager exposes the access-token manager (for middleware).
func (s *Service) TokenManager() *TokenManager { return s.tokens }

// StartEmailLogin generates and emails a one-time code. It reports whether the
// email already belongs to an account so the client can decide whether to
// collect profile (name) fields for a first-time signup.
func (s *Service) StartEmailLogin(ctx context.Context, email string) (exists bool, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return false, fmt.Errorf("invalid email")
	}
	if _, err := s.store.AccountByEmail(ctx, email); err == nil {
		exists = true
	} else if !errors.Is(err, mongostore.ErrNotFound) {
		return false, err
	}
	code := idgen.NumericCode(6)
	now := time.Now().UTC()
	otp := &model.OTP{
		ID:        idgen.New("otp"),
		Email:     email,
		CodeHash:  idgen.Hash(code),
		ExpiresAt: now.Add(s.otpTTL),
		CreatedAt: now,
	}
	if err := s.store.UpsertOTP(ctx, otp); err != nil {
		return false, err
	}
	if err := s.mail.SendOTP(ctx, email, code); err != nil {
		return false, err
	}
	return exists, nil
}

// VerifyEmailLogin checks the code, provisioning a new account (with trial) on
// first login, then returns a session.
func (s *Service) VerifyEmailLogin(ctx context.Context, email, code, firstName, lastName string) (*Tokens, *model.Account, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	otp, err := s.store.LatestOTP(ctx, email)
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil, nil, ErrInvalidCode
	}
	if err != nil {
		return nil, nil, err
	}
	if otp.Attempts >= s.otpMaxAttempts {
		return nil, nil, ErrTooManyTries
	}
	if time.Now().UTC().After(otp.ExpiresAt) {
		return nil, nil, ErrInvalidCode
	}
	if !idgen.VerifyHash(code, otp.CodeHash) {
		_ = s.store.IncOTPAttempts(ctx, otp.ID)
		return nil, nil, ErrInvalidCode
	}
	_ = s.store.DeleteOTP(ctx, otp.ID)

	acc, _, err := s.findOrCreateAccount(ctx, email, firstName, lastName, model.ProviderEmail, "")
	if err != nil {
		return nil, nil, err
	}
	toks, err := s.IssueSession(ctx, acc, "")
	return toks, acc, err
}

// LoginExternal provisions/looks up an account from an OAuth profile and returns it.
func (s *Service) LoginExternal(ctx context.Context, p ProviderName, u *ExternalUser) (*model.Account, error) {
	if strings.TrimSpace(u.Email) == "" {
		return nil, ErrNoEmail
	}
	acc, _, err := s.findOrCreateAccount(ctx, u.Email, u.FirstName, u.LastName, model.Provider(p), u.Subject)
	return acc, err
}

func (s *Service) findOrCreateAccount(ctx context.Context, email, first, last string, provider model.Provider, subject string) (*model.Account, bool, error) {
	acc, err := s.store.AccountByEmail(ctx, email)
	if err == nil {
		// Link the provider if it is new for this account.
		if linkProvider(acc, provider, subject) {
			_ = s.store.UpdateAccount(ctx, acc)
		}
		return acc, false, nil
	}
	if !errors.Is(err, mongostore.ErrNotFound) {
		return nil, false, err
	}

	now := time.Now().UTC()
	acc = &model.Account{
		ID:          idgen.New("acc"),
		Email:       email,
		FirstName:   strings.TrimSpace(first),
		LastName:    strings.TrimSpace(last),
		Providers:   []model.Provider{provider},
		ProviderIDs: map[string]string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if subject != "" {
		acc.ProviderIDs[string(provider)] = subject
	}
	if err := s.store.InsertAccount(ctx, acc); err != nil {
		// Lost a race; fall back to the now-existing account.
		if mongostore.IsDuplicateKey(err) {
			return s.findOrCreateAccount(ctx, email, first, last, provider, subject)
		}
		return nil, false, err
	}
	// New signup: provision the 7-day trial.
	if _, err := s.trials.CreateTrial(ctx, acc.ID); err != nil {
		return nil, false, fmt.Errorf("provision trial: %w", err)
	}
	return acc, true, nil
}

func linkProvider(a *model.Account, p model.Provider, subject string) bool {
	for _, ex := range a.Providers {
		if ex == p {
			return false
		}
	}
	a.Providers = append(a.Providers, p)
	if subject != "" {
		if a.ProviderIDs == nil {
			a.ProviderIDs = map[string]string{}
		}
		a.ProviderIDs[string(p)] = subject
	}
	return true
}

// IssueSession mints an access token and a stored refresh token.
func (s *Service) IssueSession(ctx context.Context, acc *model.Account, machineID string) (*Tokens, error) {
	access, err := s.tokens.Issue(acc.ID, acc.Email, s.isAdmin(acc.Email))
	if err != nil {
		return nil, err
	}
	refresh := idgen.Secret()
	now := time.Now().UTC()
	ses := &model.Session{
		ID:         idgen.New("ses"),
		AccountID:  acc.ID,
		TokenHash:  idgen.Hash(refresh),
		MachineID:  machineID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.tokens.RefreshTTL()),
		LastUsedAt: now,
	}
	if err := s.store.InsertSession(ctx, ses); err != nil {
		return nil, err
	}
	return &Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.tokens.AccessTTL().Seconds()),
	}, nil
}

// Refresh rotates a refresh token and returns a fresh session pair.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	ses, err := s.store.SessionByTokenHash(ctx, idgen.Hash(refreshToken))
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil, ErrInvalidCode
	}
	if err != nil {
		return nil, err
	}
	if time.Now().UTC().After(ses.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, ses.ID)
		return nil, ErrInvalidCode
	}
	acc, err := s.store.AccountByID(ctx, ses.AccountID)
	if err != nil {
		return nil, err
	}
	access, err := s.tokens.Issue(acc.ID, acc.Email, s.isAdmin(acc.Email))
	if err != nil {
		return nil, err
	}
	newRefresh := idgen.Secret()
	if err := s.store.RotateSession(ctx, ses.ID, idgen.Hash(newRefresh), time.Now().UTC().Add(s.tokens.RefreshTTL())); err != nil {
		return nil, err
	}
	return &Tokens{AccessToken: access, RefreshToken: newRefresh, ExpiresIn: int(s.tokens.AccessTTL().Seconds())}, nil
}

// Logout revokes a refresh token.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	ses, err := s.store.SessionByTokenHash(ctx, idgen.Hash(refreshToken))
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.DeleteSession(ctx, ses.ID)
}

// CreateExchange mints a short-lived one-time code that the desktop loopback
// swaps for a session (OAuth handoff).
func (s *Service) CreateExchange(ctx context.Context, accountID string) (string, error) {
	code := idgen.Secret()
	err := s.store.InsertExchange(ctx, &model.OAuthExchange{
		Code:      code,
		AccountID: accountID,
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
	return code, err
}

// RedeemExchange swaps a one-time code for a session.
func (s *Service) RedeemExchange(ctx context.Context, code string) (*Tokens, *model.Account, error) {
	ex, err := s.store.ConsumeExchange(ctx, code)
	if errors.Is(err, mongostore.ErrNotFound) {
		return nil, nil, ErrInvalidCode
	}
	if err != nil {
		return nil, nil, err
	}
	if time.Now().UTC().After(ex.ExpiresAt) {
		return nil, nil, ErrInvalidCode
	}
	acc, err := s.store.AccountByID(ctx, ex.AccountID)
	if err != nil {
		return nil, nil, err
	}
	toks, err := s.IssueSession(ctx, acc, "")
	return toks, acc, err
}
