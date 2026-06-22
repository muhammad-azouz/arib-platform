package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleAdminSearchClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.admin.SearchClients(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": clients})
}

func (s *Server) handleAdminCreateClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Notes     string `json:"notes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	acc, err := s.admin.FindOrCreateClient(r.Context(), req.Email, req.FirstName, req.LastName, req.Notes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleAdminGetClient(w http.ResponseWriter, r *http.Request) {
	view, err := s.admin.GetClient(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, mongostore.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "client not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleAdminUpdateClient(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Notes     string `json:"notes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	acc, err := s.admin.UpdateClient(r.Context(), c.Email, chi.URLParam(r, "id"), req.FirstName, req.LastName, req.Notes)
	if errors.Is(err, mongostore.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "client not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.admin.Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "stats failed")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAdminAssignLicenses(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Email     string     `json:"email"`
		Modules   []string   `json:"modules"`
		ExpiresAt *time.Time `json:"expires_at"` // nil/omitted = perpetual
		Count     int        `json:"count"`
		Notes     string     `json:"notes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	modules, err := model.NormalizeModules(req.Modules)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(modules) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one module is required")
		return
	}
	lics, err := s.admin.AssignLicenses(r.Context(), req.Email, modules, req.ExpiresAt, req.Count, c.Email, req.Notes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"licenses": lics})
}

func (s *Server) handleAdminLicenseStatus(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Status string `json:"status"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	status := model.LicenseStatus(req.Status)
	if status != model.LicenseActive && status != model.LicenseSuspended && status != model.LicenseExpired {
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := s.admin.SetLicenseStatus(r.Context(), c.Email, chi.URLParam(r, "id"), status); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminSignOffline(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		MachineID string `json:"machine_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.MachineID == "" {
		writeErr(w, http.StatusBadRequest, "machine_id is required")
		return
	}
	token, err := s.admin.SignOffline(r.Context(), c.Email, chi.URLParam(r, "id"), req.MachineID)
	if errors.Is(err, mongostore.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "license not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "sign failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token})
}

func (s *Server) handleAdminForceRelease(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	err := s.admin.ForceRelease(r.Context(), c.Email, chi.URLParam(r, "id"))
	if errors.Is(err, mongostore.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "release failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

// handleAdminRollout migrates the sync tenant DBs up to the gateway's current
// schema version (retrying behind/failed tenants) and returns a mixed-version
// report (E3).
func (s *Server) handleAdminRollout(w http.ResponseWriter, r *http.Request) {
	rep, err := s.rollout.Rollout(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleAdminSchemaReport returns the current mixed-version view without
// migrating anything (E3).
func (s *Server) handleAdminSchemaReport(w http.ResponseWriter, r *http.Request) {
	rep, err := s.rollout.SchemaReport(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.admin.ListAudit(r.Context(), 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}
