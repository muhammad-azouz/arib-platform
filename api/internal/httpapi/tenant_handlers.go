package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aribpos/license-api/internal/hq"
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

// handleInternalTenantRegistry serves a tenant's company + branches to the
// gateway so it can seed FK anchors into the central DB (E5/D18).
// Authorised by the client's sync token (forwarded by the gateway), not an
// account session — the token already scopes the caller to one tenant.
func (s *Server) handleInternalTenantRegistry(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		writeErr(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	claims, err := s.tenant.VerifySyncToken(strings.TrimPrefix(auth, "Bearer "))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid sync token")
		return
	}
	company, branches, err := s.tenant.TenantRegistry(r.Context(), claims.TenantID)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, registryResponse(company, branches))
}

// handleInternalSyncCompleted is the gateway's fire-and-forget callback after
// each successful sync round: it stamps the branch's last_sync_at on the
// control plane. Authorised like the registry endpoint — by the client's
// forwarded sync token, whose claims name the tenant and branch.
func (s *Server) handleInternalSyncCompleted(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		writeErr(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	claims, err := s.tenant.VerifySyncToken(strings.TrimPrefix(auth, "Bearer "))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid sync token")
		return
	}
	at, err := s.tenant.RecordSyncCompleted(r.Context(), claims.TenantID, claims.BranchID)
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	s.events.Publish(claims.TenantID, hq.Event{Type: "branch-synced", BranchID: claims.BranchID, At: at})
	writeJSON(w, http.StatusOK, map[string]any{"status": "recorded", "last_sync_at": at})
}

// registryResponse shapes the gateway-facing registry payload (snake_case).
func registryResponse(c *model.Company, branches []model.Branch) map[string]any {
	resp := map[string]any{}
	if c != nil {
		resp["company"] = map[string]any{
			"id":         c.ID,
			"name":       c.Name,
			"phone":      c.Phone,
			"address":    c.Address,
			"tax_number": c.TaxNumber,
		}
	}
	bs := make([]map[string]any, 0, len(branches))
	for i := range branches {
		b := &branches[i]
		bs = append(bs, map[string]any{
			"id":         b.ID,
			"company_id": b.CompanyID,
			"name":       b.Name,
			"phone1":     b.Phone1,
			"phone2":     b.Phone2,
			"phone3":     b.Phone3,
			"address":    b.Address,
			"is_active":  b.Status == model.BranchActive,
		})
	}
	resp["branches"] = bs
	return resp
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
		Phone1    string `json:"phone1"`
		Phone2    string `json:"phone2"`
		Phone3    string `json:"phone3"`
		Address   string `json:"address"`
		Seats     int    `json:"seats"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	b, err := s.tenant.AddBranch(r.Context(), c.Subject, chi.URLParam(r, "id"), tenant.BranchInput{
		ID: req.ID, CompanyID: req.CompanyID, Name: req.Name,
		Phone1: req.Phone1, Phone2: req.Phone2, Phone3: req.Phone3, Address: req.Address,
		Seats: req.Seats,
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
		Name    string `json:"name"`
		Phone1  string `json:"phone1"`
		Phone2  string `json:"phone2"`
		Phone3  string `json:"phone3"`
		Address string `json:"address"`
		Status  string `json:"status"` // "" | "active" | "deactivated"
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
	if req.Phone1 != "" || req.Phone2 != "" || req.Phone3 != "" || req.Address != "" {
		if err := s.tenant.SetBranchContact(r.Context(), c.Subject, tenantID, branchID, tenant.BranchInput{
			Phone1: req.Phone1, Phone2: req.Phone2, Phone3: req.Phone3, Address: req.Address,
		}); err != nil {
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
		"db_name":     issued.Claims.DBName,
		"gateway_url": issued.GatewayURL,
	})
}

// --- admin: sync provisioning ---

func (s *Server) handleAdminProvisionSync(w http.ResponseWriter, r *http.Request) {
	t, err := s.tenant.ProvisionSync(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if mongostore.IsDuplicateKey(err) {
			writeErr(w, http.StatusConflict, "db name already taken")
			return
		}
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleAdminDeleteTenant permanently removes a tenant and its company,
// branches, device seat bindings, and central DB (if sync-provisioned).
func (s *Server) handleAdminDeleteTenant(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	res, err := s.tenant.DeleteTenant(r.Context(), c.Email, chi.URLParam(r, "id"))
	if err != nil {
		s.writeTenantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
