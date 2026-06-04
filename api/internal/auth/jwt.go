package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are carried in the short-lived access token.
type Claims struct {
	Email string `json:"email"`
	Admin bool   `json:"admin"`
	jwt.RegisteredClaims
}

// TokenManager issues and verifies access tokens (HS256).
type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokenManager builds a TokenManager.
func NewTokenManager(secret []byte, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// AccessTTL exposes the configured access-token lifetime.
func (m *TokenManager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL exposes the configured refresh-token lifetime.
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

// Issue mints a signed access token for an account.
func (m *TokenManager) Issue(accountID, email string, admin bool) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		Email: email,
		Admin: admin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse validates an access token and returns its claims.
func (m *TokenManager) Parse(token string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return &claims, nil
}
