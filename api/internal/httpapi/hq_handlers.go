package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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

// --- Catalog (slice 3): same auth chain, read-only master tables. ---

func (s *Server) handleHqCatalogGroups(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	env, err := s.hq.CatalogGroups(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCatalogProducts(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"search", "group_id", "page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.CatalogProducts(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCatalogProduct(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	env, err := s.hq.CatalogProductDetail(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "productId"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// maxPriceChanges bounds one write's batch size — generous headroom over the
// realistic max of a handful of UoMs per product — so a malformed client
// can't send an unbounded body.
const maxPriceChanges = 50

// handleHqCatalogProductPrices is the first HQ write (slice 3, T24): a batch
// of per-unit price updates, forwarded to the gateway after cheap validation
// (bounds, non-negative prices) that never needs a gateway round-trip to
// reject. The gateway itself is the source of truth for "does this unit_id
// belong to this product" (ErrInvalidUnits) since only it can see the DB.
func (s *Server) handleHqCatalogProductPrices(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Changes []hq.PriceChange `json:"changes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(req.Changes) == 0 || len(req.Changes) > maxPriceChanges {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("changes must have between 1 and %d entries", maxPriceChanges))
		return
	}
	for _, ch := range req.Changes {
		if ch.UnitID == "" {
			writeErr(w, http.StatusBadRequest, "unit_id is required for every change")
			return
		}
		for _, v := range []*float64{
			ch.Sale, ch.Buy, ch.Price1, ch.Price2, ch.Price3, ch.Price4,
			ch.Price5, ch.Price6, ch.Price7, ch.Price8, ch.Price9,
		} {
			if v != nil && *v < 0 {
				writeErr(w, http.StatusBadRequest, "prices must not be negative")
				return
			}
		}
	}

	tenantID, productID := chi.URLParam(r, "id"), chi.URLParam(r, "productId")
	result, err := s.hq.ChangeProductPrices(r.Context(), c.Subject, tenantID, productID, req.Changes)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	// HQ writes should be traceable: who changed prices on which product/tenant.
	s.log.Info("hq.price_change",
		"tenant_id", tenantID, "product_id", productID,
		"account_id", c.Subject, "email", c.Email, "units", len(req.Changes))
	writeJSON(w, http.StatusOK, result)
}

// handleHqCatalogProductCreate is the second HQ write (slice 3, T26): create
// a product with at least one unit. Validation here mirrors the console
// form's zod schema (defense in depth, and fast feedback with no gateway
// round-trip for the cheap checks); the gateway is the only thing that can
// check group existence and tenant-wide barcode uniqueness, since only it
// has the tenant DB.
func (s *Server) handleHqCatalogProductCreate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name     string  `json:"name"`
		Kind     int     `json:"kind"`
		GroupID  string  `json:"group_id"`
		ReOrder  float64 `json:"re_order"`
		IsExpire bool    `json:"is_expire"`
		Units    []struct {
			Name     string   `json:"name"`
			ValSub   float64  `json:"val_sub"`
			Buy      float64  `json:"buy"`
			Sale     float64  `json:"sale"`
			Barcodes []string `json:"barcodes"`
		} `json:"units"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "product name is required")
		return
	}
	if req.Kind < 0 || req.Kind > 2 {
		writeErr(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if req.ReOrder < 0 {
		writeErr(w, http.StatusBadRequest, "re_order must not be negative")
		return
	}
	if len(req.Units) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one unit is required")
		return
	}
	units := make([]hq.NewProductUnit, 0, len(req.Units))
	for _, u := range req.Units {
		uname := strings.TrimSpace(u.Name)
		if uname == "" {
			writeErr(w, http.StatusBadRequest, "unit name is required")
			return
		}
		if u.ValSub <= 0 {
			writeErr(w, http.StatusBadRequest, "unit factor (val_sub) must be positive")
			return
		}
		if u.Buy < 0 || u.Sale < 0 {
			writeErr(w, http.StatusBadRequest, "prices must not be negative")
			return
		}
		units = append(units, hq.NewProductUnit{
			Name: uname, ValSub: u.ValSub, Buy: u.Buy, Sale: u.Sale, Barcodes: u.Barcodes,
		})
	}

	var groupID *string
	if g := strings.TrimSpace(req.GroupID); g != "" {
		groupID = &g
	}

	tenantID := chi.URLParam(r, "id")
	result, err := s.hq.CreateProduct(r.Context(), c.Subject, tenantID, hq.NewProduct{
		Name: name, Kind: req.Kind, GroupID: groupID,
		ReOrder: req.ReOrder, IsExpire: req.IsExpire, Units: units,
	})
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	s.log.Info("hq.product_create",
		"tenant_id", tenantID, "product_id", result.ID,
		"account_id", c.Subject, "email", c.Email)
	writeJSON(w, http.StatusCreated, result)
}

// --- Inventory (slice 4): same auth chain, one dataset three perspectives. ---

func (s *Server) handleHqInventoryBranches(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	env, err := s.hq.InventoryByBranch(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqInventoryProducts(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	status := r.URL.Query().Get("status")
	if status != "" {
		switch status {
		case "negative", "out", "low", "attention":
		default:
			writeErr(w, http.StatusBadRequest, "invalid status")
			return
		}
	}
	params := url.Values{}
	for _, k := range []string{"search", "group_id", "branch_id", "status", "page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.InventoryProducts(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqInventoryAttention(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"branch_id", "page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.InventoryAttention(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// --- Conflicts (slice 5): the ConflictLog review chain. ---

func (s *Server) handleHqConflicts(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"page", "page_size", "all"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.Conflicts(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// maxAckIDs bounds one ack's explicit-id list — a whole page is at most 200
// rows (the gateway's page_size clamp), so this never constrains a real
// client; bulk clears go through up_to_id.
const maxAckIDs = 200

func (s *Server) handleHqConflictsAck(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		IDs    []int64 `json:"ids"`
		UpToID *int64  `json:"up_to_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(req.IDs) == 0 && req.UpToID == nil {
		writeErr(w, http.StatusBadRequest, "ids or up_to_id is required")
		return
	}
	if len(req.IDs) > maxAckIDs {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("ids must have at most %d entries", maxAckIDs))
		return
	}
	for _, id := range req.IDs {
		if id <= 0 {
			writeErr(w, http.StatusBadRequest, "ids must be positive")
			return
		}
	}
	if req.UpToID != nil && *req.UpToID <= 0 {
		writeErr(w, http.StatusBadRequest, "up_to_id must be positive")
		return
	}

	tenantID := chi.URLParam(r, "id")
	result, err := s.hq.AckConflicts(r.Context(), c.Subject, tenantID, req.IDs, req.UpToID)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	// Reviewing a conflict is an HQ action worth tracing, like other HQ writes.
	s.log.Info("hq.conflicts_ack",
		"tenant_id", tenantID, "account_id", c.Subject, "email", c.Email,
		"acked", result.Acked)
	writeJSON(w, http.StatusOK, result)
}

// dateParamRE validates from/to as a plain YYYY-MM-DD date — the gateway
// interprets them in its own local time (same assumption as BranchSnapshot's
// day scope), so anything with a time/zone component would be misleading.
var dateParamRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Server) handleHqProductMovements(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	for _, k := range []string{"from", "to"} {
		if v := r.URL.Query().Get(k); v != "" && !dateParamRE.MatchString(v) {
			writeErr(w, http.StatusBadRequest, k+" must be YYYY-MM-DD")
			return
		}
	}
	params := url.Values{}
	for _, k := range []string{"branch_id", "from", "to", "page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.ProductMovements(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "productId"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// --- Reports (slice 6): question-organized period aggregates. from/to are
// validated here as plain YYYY-MM-DD dates (dateParamRE — the gateway
// interprets them in its local day-scope and owns defaulting/clamping);
// anything else is rejected before a gateway round-trip. ---

// validReportPeriod rejects malformed from/to params, writing the 400 itself.
// Returns false when the request has already been answered.
func validReportPeriod(w http.ResponseWriter, r *http.Request) bool {
	for _, k := range []string{"from", "to"} {
		if v := r.URL.Query().Get(k); v != "" && !dateParamRE.MatchString(v) {
			writeErr(w, http.StatusBadRequest, k+" must be YYYY-MM-DD")
			return false
		}
	}
	return true
}

func (s *Server) handleHqReportSales(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	if !validReportPeriod(w, r) {
		return
	}
	params := url.Values{}
	for _, k := range []string{"from", "to", "branch_id"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.ReportSales(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqReportProducts(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	if !validReportPeriod(w, r) {
		return
	}
	if sort := r.URL.Query().Get("sort"); sort != "" {
		switch sort {
		case "revenue", "qty", "profit":
		default:
			writeErr(w, http.StatusBadRequest, "invalid sort")
			return
		}
	}
	params := url.Values{}
	for _, k := range []string{"from", "to", "branch_id", "group_id", "sort", "page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.ReportProducts(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqReportBranches(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	if !validReportPeriod(w, r) {
		return
	}
	params := url.Values{}
	for _, k := range []string{"from", "to"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.ReportBranches(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqReportStaff(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	if !validReportPeriod(w, r) {
		return
	}
	params := url.Values{}
	for _, k := range []string{"from", "to", "branch_id"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.ReportStaff(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// handleTenantEvents streams tenant-scoped events over SSE. Registered
// outside the API's 30s timeout group (like /updates/*) — the stream lives
// for the tab's lifetime, kept open through proxies by a heartbeat comment.
// The browser EventSource API cannot set an Authorization header, so this one
// endpoint also accepts the access token as ?access_token= (the request
// logger records only the path, and nginx disables access logging on this
// location, so the token stays out of logs).
// --- Customers (slice 7): read-mostly, branch-specific. active/debt are
// validated here (cheap, no gateway round-trip) before claimsFrom, so the
// zero-gateway-calls 400 path needs no auth context to exercise in a test. ---

var validDebtFilters = map[string]bool{"has_debt": true, "credit": true, "exceeding": true}

func (s *Server) handleHqCustomerGroups(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	env, err := s.hq.CustomerGroups(r.Context(), c.Subject, chi.URLParam(r, "id"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCustomers(w http.ResponseWriter, r *http.Request) {
	if v := r.URL.Query().Get("active"); v != "" && v != "true" && v != "false" {
		writeErr(w, http.StatusBadRequest, "active must be true or false")
		return
	}
	if v := r.URL.Query().Get("debt"); v != "" && !validDebtFilters[v] {
		writeErr(w, http.StatusBadRequest, "invalid debt filter")
		return
	}
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"search", "branch_id", "group_id", "active", "debt", "page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.Customers(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCustomerDetail(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	env, err := s.hq.CustomerDetail(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "customerId"))
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCustomerPurchases(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.CustomerPurchases(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "customerId"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCustomerLedger(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"page", "page_size"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.CustomerLedger(r.Context(), c.Subject, chi.URLParam(r, "id"), chi.URLParam(r, "customerId"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleHqCustomerInsights(w http.ResponseWriter, r *http.Request) {
	if !validReportPeriod(w, r) {
		return
	}
	c := claimsFrom(r.Context())
	params := url.Values{}
	for _, k := range []string{"branch_id", "from", "to"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	env, err := s.hq.CustomerInsights(r.Context(), c.Subject, chi.URLParam(r, "id"), params)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// handleHqCustomerCreate is HQ's first write into a Tier-B table (slice 7,
// T55/T57). Validation here mirrors the console form's zod schema, same
// defense-in-depth reasoning as handleHqCatalogProductCreate; branch_id/
// group_id existence can only be checked by the gateway, which owns the
// tenant DB.
func (s *Server) handleHqCustomerCreate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name        string   `json:"name"`
		Phone1      string   `json:"phone1"`
		Phone2      *string  `json:"phone2"`
		Phone3      *string  `json:"phone3"`
		Address     *string  `json:"address"`
		Note        *string  `json:"note"`
		GroupID     *string  `json:"group_id"`
		CreditLimit *float64 `json:"credit_limit"`
		BranchID    string   `json:"branch_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeErr(w, http.StatusBadRequest, "name is required and must be at most 100 characters")
		return
	}
	phone1 := strings.TrimSpace(req.Phone1)
	if phone1 == "" || len(phone1) > 12 {
		writeErr(w, http.StatusBadRequest, "phone1 is required and must be at most 12 characters")
		return
	}
	if strings.TrimSpace(req.BranchID) == "" {
		writeErr(w, http.StatusBadRequest, "branch_id is required")
		return
	}
	if req.CreditLimit != nil && *req.CreditLimit < 0 {
		writeErr(w, http.StatusBadRequest, "credit_limit must not be negative")
		return
	}

	tenantID := chi.URLParam(r, "id")
	result, err := s.hq.CreateCustomer(r.Context(), c.Subject, tenantID, hq.NewCustomer{
		Name: name, Phone1: phone1, Phone2: req.Phone2, Phone3: req.Phone3,
		Address: req.Address, Note: req.Note, GroupID: req.GroupID,
		CreditLimit: req.CreditLimit, BranchID: req.BranchID,
	})
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	s.log.Info("hq.customers_create",
		"tenant_id", tenantID, "customer_id", result.ID,
		"account_id", c.Subject, "email", c.Email)
	writeJSON(w, http.StatusCreated, result)
}

// handleHqCustomerUpdate is a flat partial update (slice 7, T56/T57):
// "deactivate" is just is_active:false through this same route.
func (s *Server) handleHqCustomerUpdate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		Name        *string  `json:"name"`
		Phone1      *string  `json:"phone1"`
		Phone2      *string  `json:"phone2"`
		Phone3      *string  `json:"phone3"`
		Address     *string  `json:"address"`
		Note        *string  `json:"note"`
		GroupID     *string  `json:"group_id"`
		CreditLimit *float64 `json:"credit_limit"`
		IsActive    *bool    `json:"is_active"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 100 {
			writeErr(w, http.StatusBadRequest, "name must be 1-100 characters")
			return
		}
		req.Name = &n
	}
	if req.Phone1 != nil {
		p := strings.TrimSpace(*req.Phone1)
		if p == "" || len(p) > 12 {
			writeErr(w, http.StatusBadRequest, "phone1 must be 1-12 characters")
			return
		}
		req.Phone1 = &p
	}
	if req.CreditLimit != nil && *req.CreditLimit < 0 {
		writeErr(w, http.StatusBadRequest, "credit_limit must not be negative")
		return
	}

	tenantID, customerID := chi.URLParam(r, "id"), chi.URLParam(r, "customerId")
	result, err := s.hq.UpdateCustomer(r.Context(), c.Subject, tenantID, customerID, hq.CustomerEdit{
		Name: req.Name, Phone1: req.Phone1, Phone2: req.Phone2, Phone3: req.Phone3,
		Address: req.Address, Note: req.Note, GroupID: req.GroupID,
		CreditLimit: req.CreditLimit, IsActive: req.IsActive,
	})
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	s.log.Info("hq.customers_update",
		"tenant_id", tenantID, "customer_id", customerID,
		"account_id", c.Subject, "email", c.Email)
	writeJSON(w, http.StatusOK, result)
}

// maxBulkCustomerIDs mirrors the gateway's own 500-id cap (T58) — checked
// here too so a malformed client 400s before a gateway round-trip.
const maxBulkCustomerIDs = 500

func (s *Server) handleHqCustomersBulkUpdate(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	var req struct {
		IDs       []string `json:"ids"`
		GroupID   *string  `json:"group_id"`
		PriceTier *int     `json:"price_tier"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxBulkCustomerIDs {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("ids must have between 1 and %d entries", maxBulkCustomerIDs))
		return
	}
	if req.GroupID == nil && req.PriceTier == nil {
		writeErr(w, http.StatusBadRequest, "group_id or price_tier is required")
		return
	}

	tenantID := chi.URLParam(r, "id")
	result, err := s.hq.BulkUpdateCustomers(r.Context(), c.Subject, tenantID, req.IDs, req.GroupID, req.PriceTier)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	s.log.Info("hq.customers_bulk_update",
		"tenant_id", tenantID, "account_id", c.Subject, "email", c.Email, "updated", result.Updated)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleHqCustomersExport(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	if v := r.URL.Query().Get("active"); v != "" && v != "true" && v != "false" {
		writeErr(w, http.StatusBadRequest, "active must be true or false")
		return
	}
	if v := r.URL.Query().Get("debt"); v != "" && !validDebtFilters[v] {
		writeErr(w, http.StatusBadRequest, "invalid debt filter")
		return
	}
	params := url.Values{}
	for _, k := range []string{"search", "branch_id", "group_id", "active", "debt"} {
		if v := r.URL.Query().Get(k); v != "" {
			params.Set(k, v)
		}
	}
	if err := s.hq.ExportCustomers(r.Context(), c.Subject, chi.URLParam(r, "id"), params, w); err != nil {
		s.writeHqError(w, err)
		return
	}
}

// maxImportBytes bounds one CSV upload — generous headroom over the
// gateway's 1000-row cap — so a malformed client can't send an unbounded body.
const maxImportBytes = 5 << 20 // 5 MiB

func (s *Server) handleHqCustomersImport(w http.ResponseWriter, r *http.Request) {
	c := claimsFrom(r.Context())
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		writeErr(w, http.StatusBadRequest, "multipart form with a file field is required")
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxImportBytes)

	tenantID := chi.URLParam(r, "id")
	result, err := s.hq.ImportCustomers(r.Context(), c.Subject, tenantID, r.Header.Get("Content-Type"), body)
	if err != nil {
		s.writeHqError(w, err)
		return
	}
	s.log.Info("hq.customers_import",
		"tenant_id", tenantID, "account_id", c.Subject, "email", c.Email,
		"created", result.Created, "errors", len(result.Errors))
	writeJSON(w, http.StatusOK, result)
}

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
	var dup *hq.DuplicateBarcodeError
	if errors.As(err, &dup) {
		writeErr(w, http.StatusConflict, dup.Error())
		return
	}
	var badInput *hq.InvalidCustomerInputError
	if errors.As(err, &badInput) {
		writeErr(w, http.StatusBadRequest, badInput.Error())
		return
	}
	switch {
	case errors.Is(err, hq.ErrForbidden):
		writeErr(w, http.StatusForbidden, "resource does not belong to this account")
	case errors.Is(err, hq.ErrNotSubscribed):
		writeErr(w, http.StatusPaymentRequired, "tenant has no sync subscription")
	case errors.Is(err, hq.ErrGatewayUnreachable):
		writeErr(w, http.StatusServiceUnavailable, "sync gateway unreachable")
	case errors.Is(err, hq.ErrNotFound), errors.Is(err, mongostore.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, hq.ErrInvalidUnits):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, hq.ErrInvalidGroup):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, hq.ErrTenantNotProvisioned):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, hq.ErrMissingAccountOperand):
		writeErr(w, http.StatusInternalServerError, err.Error())
	default:
		s.log.Error("hq.unhandled_error", "err", err.Error())
		writeErr(w, http.StatusInternalServerError, "request failed")
	}
}
