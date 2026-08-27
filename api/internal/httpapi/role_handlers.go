package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// --- client: roles (spec-console-rbac T107) ---

func (s *Server) handleRoleList(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	list, err := s.tenant.Roles(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRoleCreate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	role, err := s.tenant.CreateRole(r.Context(), c.Subject, chi.URLParam(r, "id"), req.Name, req.Permissions)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleRoleUpdate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	role, err := s.tenant.UpdateRole(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "roleId"), req.Name, req.Permissions)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) handleRoleDelete(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	err := s.tenant.DeleteRole(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "roleId"))
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleRolePermissions(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	codes, err := s.tenant.Permissions(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, codes)
}
