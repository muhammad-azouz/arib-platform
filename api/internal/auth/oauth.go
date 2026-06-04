package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// ProviderName identifies a supported social login.
type ProviderName string

const (
	Google   ProviderName = "google"
	Facebook ProviderName = "facebook"
)

// ExternalUser is the normalized profile returned by a provider.
type ExternalUser struct {
	Subject   string
	Email     string
	FirstName string
	LastName  string
}

// OAuth holds per-provider oauth2 configs and signs/verifies the browser state.
type OAuth struct {
	configs   map[ProviderName]*oauth2.Config
	stateKey  []byte
	publicURL string
}

// NewOAuth builds provider configs. A provider with an empty client id is
// simply unavailable (its endpoints return 404).
func NewOAuth(publicBaseURL string, stateKey []byte, googleID, googleSecret, fbID, fbSecret string) *OAuth {
	cb := func(p ProviderName) string {
		return fmt.Sprintf("%s/v1/auth/%s/callback", publicBaseURL, p)
	}
	configs := map[ProviderName]*oauth2.Config{}
	if googleID != "" {
		configs[Google] = &oauth2.Config{
			ClientID:     googleID,
			ClientSecret: googleSecret,
			Endpoint:     endpoints.Google,
			RedirectURL:  cb(Google),
			Scopes:       []string{"openid", "email", "profile"},
		}
	}
	if fbID != "" {
		configs[Facebook] = &oauth2.Config{
			ClientID:     fbID,
			ClientSecret: fbSecret,
			Endpoint:     endpoints.Facebook,
			RedirectURL:  cb(Facebook),
			Scopes:       []string{"email", "public_profile"},
		}
	}
	return &OAuth{configs: configs, stateKey: stateKey, publicURL: publicBaseURL}
}

// Configured reports whether a provider is available.
func (o *OAuth) Configured(p ProviderName) bool { _, ok := o.configs[p]; return ok }

type stateClaims struct {
	Provider ProviderName `json:"p"`
	Callback string       `json:"cb"`
	jwt.RegisteredClaims
}

// AuthCodeURL builds the provider consent URL, embedding a signed state that
// carries the desktop loopback callback to return to afterwards.
func (o *OAuth) AuthCodeURL(p ProviderName, loopbackCallback string) (string, error) {
	cfg, ok := o.configs[p]
	if !ok {
		return "", fmt.Errorf("provider %q not configured", p)
	}
	state, err := jwt.NewWithClaims(jwt.SigningMethodHS256, stateClaims{
		Provider: p,
		Callback: loopbackCallback,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
	}).SignedString(o.stateKey)
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

// VerifyState validates the state JWT and returns the loopback callback URL.
func (o *OAuth) VerifyState(p ProviderName, state string) (callback string, err error) {
	var c stateClaims
	_, err = jwt.ParseWithClaims(state, &c, func(*jwt.Token) (any, error) { return o.stateKey, nil })
	if err != nil {
		return "", err
	}
	if c.Provider != p {
		return "", fmt.Errorf("state provider mismatch")
	}
	return c.Callback, nil
}

// Exchange swaps an authorization code for the provider's normalized profile.
func (o *OAuth) Exchange(ctx context.Context, p ProviderName, code string) (*ExternalUser, error) {
	cfg, ok := o.configs[p]
	if !ok {
		return nil, fmt.Errorf("provider %q not configured", p)
	}
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	switch p {
	case Google:
		return fetchGoogle(ctx, cfg, tok)
	case Facebook:
		return fetchFacebook(ctx, cfg, tok)
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

func fetchGoogle(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (*ExternalUser, error) {
	var body struct {
		Sub        string `json:"sub"`
		Email      string `json:"email"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if err := getJSON(ctx, cfg, tok, "https://openidconnect.googleapis.com/v1/userinfo", &body); err != nil {
		return nil, err
	}
	return &ExternalUser{Subject: body.Sub, Email: body.Email, FirstName: body.GivenName, LastName: body.FamilyName}, nil
}

func fetchFacebook(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (*ExternalUser, error) {
	var body struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	url := "https://graph.facebook.com/v19.0/me?fields=id,email,first_name,last_name"
	if err := getJSON(ctx, cfg, tok, url, &body); err != nil {
		return nil, err
	}
	return &ExternalUser{Subject: body.ID, Email: body.Email, FirstName: body.FirstName, LastName: body.LastName}, nil
}

func getJSON(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token, url string, out any) error {
	client := cfg.Client(ctx, tok)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("userinfo %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
