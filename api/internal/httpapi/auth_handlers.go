package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/aribpos/license-api/internal/auth"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleEmailStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	exists, err := s.auth.StartEmailLogin(r.Context(), req.Email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// `exists` lets the console skip the name fields for returning users. This
	// deliberately reveals account existence to callers of this endpoint
	// (product decision); the OTP send is rate-limited (see route wiring).
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent", "exists": exists})
}

func (s *Server) handleEmailVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email     string `json:"email"`
		Code      string `json:"code"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	toks, acc, err := s.auth.VerifyEmailLogin(r.Context(), req.Email, req.Code, req.FirstName, req.LastName)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(toks, acc.ID, acc.Email))
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	toks, err := s.auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	writeJSON(w, http.StatusOK, toks)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	_ = s.auth.Logout(r.Context(), req.RefreshToken)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleExchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	toks, acc, err := s.auth.RedeemExchange(r.Context(), req.Code)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(toks, acc.ID, acc.Email))
}

// handleOAuthStart redirects the browser to the provider's consent screen.
// The desktop app calls this with ?cb=<loopback url> to receive the result.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := auth.ProviderName(chi.URLParam(r, "provider"))
	o := s.auth.OAuth()
	if !o.Configured(provider) {
		writeErr(w, http.StatusNotFound, "provider not available")
		return
	}
	cb := r.URL.Query().Get("cb")
	if !isLoopback(cb) {
		writeErr(w, http.StatusBadRequest, "cb must be a loopback (127.0.0.1) URL")
		return
	}
	authURL, err := o.AuthCodeURL(provider, cb)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not start oauth")
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOAuthCallback completes the provider exchange, provisions the account,
// then redirects the browser back to the desktop loopback with a one-time code.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := auth.ProviderName(chi.URLParam(r, "provider"))
	o := s.auth.OAuth()
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		writeErr(w, http.StatusBadRequest, "oauth denied: "+e)
		return
	}
	cb, err := o.VerifyState(provider, q.Get("state"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid state")
		return
	}
	extUser, err := o.Exchange(r.Context(), provider, q.Get("code"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "provider exchange failed")
		return
	}
	acc, err := s.auth.LoginExternal(r.Context(), provider, extUser)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	code, err := s.auth.CreateExchange(r.Context(), acc.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not finalize login")
		return
	}
	http.Redirect(w, r, appendQuery(cb, "code", code), http.StatusFound)
}

func (s *Server) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCode):
		writeErr(w, http.StatusUnauthorized, "invalid or expired code")
	case errors.Is(err, auth.ErrTooManyTries):
		writeErr(w, http.StatusTooManyRequests, "too many attempts; request a new code")
	case errors.Is(err, auth.ErrNoEmail):
		writeErr(w, http.StatusBadRequest, "the provider did not share an email address")
	default:
		writeErr(w, http.StatusInternalServerError, "login failed")
	}
}

func sessionResponse(t *auth.Tokens, accountID, email string) map[string]any {
	return map[string]any{
		"access_token":  t.AccessToken,
		"refresh_token": t.RefreshToken,
		"expires_in":    t.ExpiresIn,
		"account_id":    accountID,
		"email":         email,
	}
}

func isLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func appendQuery(raw, key, val string) string {
	if strings.Contains(raw, "?") {
		return raw + "&" + key + "=" + url.QueryEscape(val)
	}
	return raw + "?" + key + "=" + url.QueryEscape(val)
}
