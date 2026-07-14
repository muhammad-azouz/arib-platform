package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aribpos/license-api/internal/hq"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/go-chi/chi/v5"
)

// --- client: HQ reads (console → API → gateway → tenant central DB) ---

func (s *Server) handleHqBranchActivity(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	envelopes, err := s.hq.BranchActivity(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": envelopes})
}

func (s *Server) handleHqBranches(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	res, err := s.hq.Branches(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleTenantEvents streams tenant-scoped events over SSE. Registered
// outside the API's 30s timeout group (like /updates/*) — the stream lives
// for the tab's lifetime, kept open through proxies by a heartbeat comment.
// The browser EventSource API cannot set an Authorization header, so this one
// endpoint also accepts the access token as ?access_token= (the request
// logger records only the path, and nginx disables access logging on this
// location, so the token stays out of logs).
func (s *Server) handleTenantEvents(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}
	if token == "" {
		writeErr(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	claims, err := s.auth.TokenManager().Parse(token)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	tenantID := chi.URLParam(r, "id")
	if err := s.hq.CheckOwnership(r.Context(), claims.Subject, tenantID); err != nil {
		s.writeHqError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	events, cancel := s.events.Subscribe(tenantID)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no") // belt-and-braces with nginx's proxy_buffering off
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": hb\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e := <-events:
			payload, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) writeHqError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, hq.ErrForbidden):
		writeErr(w, http.StatusForbidden, "resource does not belong to this account")
	case errors.Is(err, hq.ErrNotSubscribed):
		writeErr(w, http.StatusPaymentRequired, "tenant has no sync subscription")
	case errors.Is(err, hq.ErrGatewayUnreachable):
		writeErr(w, http.StatusServiceUnavailable, "sync gateway unreachable")
	case errors.Is(err, mongostore.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusInternalServerError, "request failed")
	}
}
