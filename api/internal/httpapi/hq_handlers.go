package httpapi

import (
	"errors"
	"net/http"

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
	views, err := s.hq.Branches(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": views})
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
