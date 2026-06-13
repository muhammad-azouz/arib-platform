package httpapi

import (
	"errors"
	"net/http"

	"github.com/aribpos/license-api/internal/model"
	mongostore "github.com/aribpos/license-api/internal/store/mongo"
	"github.com/aribpos/license-api/internal/tenant"
	"github.com/go-chi/chi/v5"
)

// --- client: tenants ---

func (s *Server) handleTenantCreate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	t, err := s.tenant.Register(r.Context(), c.Subject, req.Name)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleTenantList(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	list, err := s.tenant.Tenants(r.Context(), c.Subject)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleTenantBundle(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	bundle, err := s.tenant.GetBundle(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

// --- client: company ---

func (s *Server) handleTenantSetCompany(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Phone     string `json:"phone"`
		Address   string `json:"address"`
		TaxNumber string `json:"tax_number"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	company, err := s.tenant.SetCompany(r.Context(), c.Subject, chi.URLParam(r, "id"), tenant.CompanyInput{
		ID: req.ID, Name: req.Name, Phone: req.Phone, Address: req.Address, TaxNumber: req.TaxNumber,
	})
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, company)
}

// --- client: branches ---

func (s *Server) handleBranchAdd(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		ID        string `json:"id"`
		CompanyID string `json:"company_id"`
		Name      string `json:"name"`
		Seats     int    `json:"seats"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	b, err := s.tenant.AddBranch(r.Context(), c.Subject, chi.URLParam(r, "id"), tenant.BranchInput{
		ID: req.ID, CompanyID: req.CompanyID, Name: req.Name, Seats: req.Seats,
	})
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleBranchUpdate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name   string `json:"name"`
		Status string `json:"status"` // "" | "active" | "deactivated"
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	tenantID, branchID := chi.URLParam(r, "id"), chi.URLParam(r, "branchId")
	if req.Name != "" {
		if err := s.tenant.RenameBranch(r.Context(), c.Subject, tenantID, branchID, req.Name); err != nil {
			s.writeTenantError(w, err)
			return
		}
	}
	if req.Status != "" {
		st := model.BranchStatus(req.Status)
		if st != model.BranchActive && st != model.BranchDeactivated {
			writeErr(w, http.StatusBadRequest, "invalid status")
			return
		}
		if err := s.tenant.SetBranchStatus(r.Context(), c.Subject, tenantID, branchID, st); err != nil {
			s.writeTenantError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- client: device seats ---

func (s *Server) handleBranchBind(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		MachineID   string `json:"machine_id"`
		MachineName string `json:"machine_name"`
		OS          string `json:"os"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	d, err := s.tenant.BindDevice(r.Context(), c.Subject,
		chi.URLParam(r, "id"), chi.URLParam(r, "branchId"),
		req.MachineID, req.MachineName, req.OS)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleBranchDeviceRelease(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	err := s.tenant.ReleaseDevice(r.Context(), c.Subject,
		chi.URLParam(r, "id"), chi.URLParam(r, "deviceId"))
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

// --- client: sync token ---

func (s *Server) handleSyncToken(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	issued, err := s.tenant.IssueSyncToken(r.Context(), c.Subject, chi.URLParam(r, "id"), req.DeviceID)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":       issued.Token,
		"expires_at":  issued.Claims.ExpiresAt.Time,
		"shard_id":    issued.Claims.ShardID,
		"db_name":     issued.Claims.DBName,
		"gateway_url": issued.GatewayURL,
	})
}

// --- admin: shards & placement ---

func (s *Server) handleAdminCreateShard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Host       string `json:"host"`
		GatewayURL string `json:"gateway_url"`
		MaxTenants int    `json:"max_tenants"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	sh, err := s.tenant.CreateShard(r.Context(), req.Name, req.Host, req.GatewayURL, req.MaxTenants)
	if err != nil {
		if mongostore.IsDuplicateKey(err) {
			writeErr(w, http.StatusConflict, "shard name already exists")
			return
		}
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) handleAdminListShards(w http.ResponseWriter, r *http.Request) {
	list, err := s.tenant.Shards(r.Context())
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAdminAssignShard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShardID string `json:"shard_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	t, err := s.tenant.AssignShard(r.Context(), chi.URLParam(r, "id"), req.ShardID)
	if err != nil {
		if mongostore.IsDuplicateKey(err) {
			writeErr(w, http.StatusConflict, "db name already taken on that shard")
			return
		}
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleAdminBranchSeats(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seats int `json:"seats"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.tenant.SetBranchSeats(r.Context(), chi.URLParam(r, "id"), req.Seats); err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- error mapping ---

func (s *Server) writeTenantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tenant.ErrForbidden):
		writeErr(w, http.StatusForbidden, "resource does not belong to this account")
	case errors.Is(err, tenant.ErrTenantSuspended):
		writeErr(w, http.StatusForbidden, "tenant is suspended")
	case errors.Is(err, tenant.ErrBranchInactive):
		writeErr(w, http.StatusForbidden, "branch is deactivated")
	case errors.Is(err, tenant.ErrSeatLimit):
		writeErr(w, http.StatusConflict, "branch seat limit reached — release a device or upgrade seats")
	case errors.Is(err, tenant.ErrNotBound):
		writeErr(w, http.StatusNotFound, "no such device binding")
	case errors.Is(err, tenant.ErrNotSubscribed):
		writeErr(w, http.StatusPaymentRequired, "tenant has no sync subscription")
	case errors.Is(err, tenant.ErrShardFull):
		writeErr(w, http.StatusConflict, "shard is full or not accepting tenants")
	case errors.Is(err, tenant.ErrCompanyExists):
		writeErr(w, http.StatusConflict, "tenant already has a company (one company per tenant)")
	case errors.Is(err, tenant.ErrNoCompany):
		writeErr(w, http.StatusConflict, "register the company before adding branches")
	case errors.Is(err, mongostore.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusInternalServerError, "request failed")
	}
}
