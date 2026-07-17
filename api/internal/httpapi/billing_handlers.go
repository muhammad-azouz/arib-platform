package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/aribpos/license-api/internal/billing"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/go-chi/chi/v5"
)

// --- admin: bills (subscription billing) ---

func (s *Server) handleAdminCreateBill(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Amount   int64     `json:"amount"`
		Currency string    `json:"currency"`
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
		Notes    string    `json:"notes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := s.billing.Create(r.Context(), chi.URLParam(r, "id"),
		req.Amount, req.Currency, req.StartsAt, req.EndsAt, req.Notes, c.Email)
	if err != nil {
		s.writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"bill":          res.Bill,
		"provisioned":   res.Provisioned,
		"provision_err": res.ProvisionErr,
		"summary":       res.Summary,
	})
}

func (s *Server) handleAdminListBills(w http.ResponseWriter, r *http.Request) {
	bills, summary, err := s.billing.ListWithSummary(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bills": bills, "summary": summary})
}

func (s *Server) handleAdminVoidBill(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.billing.Void(r.Context(), chi.URLParam(r, "id"), req.Reason, c.Email); err != nil {
		s.writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "voided"})
}

func (s *Server) writeBillingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrInvalidAmount),
		errors.Is(err, billing.ErrInvalidPeriod),
		errors.Is(err, billing.ErrVoidReasonRequired),
		errors.Is(err, billing.ErrBillNotPaid):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, mongostore.ErrNotFound), errors.Is(err, billing.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusInternalServerError, "request failed")
	}
}
