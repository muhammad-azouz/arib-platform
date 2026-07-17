// Package httpapi wires the HTTP transport: router, middleware and handlers.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/aribpos/license-api/internal/admin"
	"github.com/aribpos/license-api/internal/auth"
	"github.com/aribpos/license-api/internal/billing"
	"github.com/aribpos/license-api/internal/device"
	"github.com/aribpos/license-api/internal/hq"
	"github.com/aribpos/license-api/internal/rollout"
	"github.com/aribpos/license-api/internal/tenant"
	"github.com/aribpos/license-api/pkg/licensetoken"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Server bundles the dependencies the HTTP handlers need.
type Server struct {
	auth    *auth.Service
	device  *device.Service
	admin   *admin.Service
	tenant  *tenant.Service
	billing *billing.Service
	rollout *rollout.Service
	hq      *hq.Service
	events  *hq.EventBus // in-memory: single API instance (see hq.EventBus)
	log     *slog.Logger

	corsOrigins []string
	otpLimiter  *keyedLimiter

	updatesDir    string
	updatesAuth   bool
	tokenVerifier *licensetoken.Signer
}

// New builds an HTTP Server. corsOrigins are the browser origins (admin
// dashboard) allowed to call the API; nil disables CORS. updatesDir is the
// root of the Velopack update feed served at /updates/*; empty disables it.
// updatesAuth turns on the feed entitlement gate, verifying license tokens
// with tokenVerifier (the same RSA keypair that signs them).
func New(authSvc *auth.Service, deviceSvc *device.Service, adminSvc *admin.Service, tenantSvc *tenant.Service, billingSvc *billing.Service, rolloutSvc *rollout.Service, hqSvc *hq.Service, corsOrigins []string, log *slog.Logger, updatesDir string, updatesAuth bool, tokenVerifier *licensetoken.Signer) *Server {
	return &Server{
		auth:          authSvc,
		device:        deviceSvc,
		admin:         adminSvc,
		tenant:        tenantSvc,
		billing:       billingSvc,
		rollout:       rolloutSvc,
		hq:            hqSvc,
		events:        hq.NewEventBus(),
		log:           log,
		corsOrigins:   corsOrigins,
		otpLimiter:    newKeyedLimiter(rateEvery(time.Minute), 3),
		updatesDir:    updatesDir,
		updatesAuth:   updatesAuth,
		tokenVerifier: tokenVerifier,
	}
}

// Router returns the configured chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(s.log))
	if len(s.corsOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.corsOrigins,
			AllowedMethods:   []string{"GET", "POST", "PATCH", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type"},
			AllowCredentials: false,
			MaxAge:           300,
		}))
	}
	// Velopack update feed. Outside the API timeout group: package downloads
	// are ~75 MB and must not be cut off after 30s on slow POS connections.
	r.Group(func(r chi.Router) {
		r.Get("/updates/*", s.handleUpdates)
		r.Head("/updates/*", s.handleUpdates)
	})

	// SSE event stream. Outside the API timeout group like /updates/*: the
	// stream lives for the console tab's lifetime (heartbeats keep it open).
	r.Get("/v1/tenants/{id}/events", s.handleTenantEvents)

	// Everything below (the API proper) keeps the 30s timeout; chi forbids
	// r.Use after routes are registered, so it's applied per-registration.
	apiTimeout := middleware.Timeout(30 * time.Second)

	r.With(apiTimeout).Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.With(apiTimeout).Get("/v1/sync-public-key", func(w http.ResponseWriter, _ *http.Request) {
		pemStr, err := s.tenant.SyncPublicKeyPEM()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "key unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write([]byte(pemStr))
	})

	r.With(apiTimeout).Route("/v1", func(r chi.Router) {
		// --- Public auth ---
		r.Route("/auth", func(r chi.Router) {
			r.With(s.rateLimitOTP).Post("/email/start", s.handleEmailStart)
			r.Post("/email/verify", s.handleEmailVerify)
			r.Post("/exchange", s.handleExchange)
			r.Post("/refresh", s.handleRefresh)
			r.Post("/logout", s.handleLogout)
			r.Get("/{provider}/start", s.handleOAuthStart)
			r.Get("/{provider}/callback", s.handleOAuthCallback)
		})

		// --- Internal (gateway → control plane): authorised by the client's
		//     forwarded sync token, not an account session. Serves a tenant's
		//     company+branches so the gateway can seed central FK anchors
		//     (E5/D18). ---
		r.Get("/internal/tenant-registry", s.handleInternalTenantRegistry)
		r.Post("/internal/sync-completed", s.handleInternalSyncCompleted)

		// --- Authenticated client ---
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/me", s.handleMe)
			r.Post("/devices/bind", s.handleBind)
			r.Post("/devices/validate", s.handleValidate)
			r.Post("/devices/release", s.handleRelease)

			// Multi-tenant registry.
			r.Route("/tenants", func(r chi.Router) {
				r.Post("/", s.handleTenantCreate)
				r.Get("/", s.handleTenantList)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleTenantBundle)
					r.Put("/company", s.handleTenantSetCompany)
					r.Post("/branches", s.handleBranchAdd)
					r.Patch("/branches/{branchId}", s.handleBranchUpdate)
					r.Post("/branches/{branchId}/bind", s.handleBranchBind)
					r.Post("/devices/{deviceId}/release", s.handleBranchDeviceRelease)
					r.Post("/sync-token", s.handleSyncToken)
					r.Get("/subscription", s.handleTenantSubscription)

					// HQ reads: business data from the tenant's central DB,
					// proxied via the sync gateway (freshness-enveloped).
					r.Get("/hq/branch-activity", s.handleHqBranchActivity)
					r.Get("/hq/branches", s.handleHqBranches)
					r.Get("/hq/catalog/groups", s.handleHqCatalogGroups)
					r.Get("/hq/catalog/products", s.handleHqCatalogProducts)
					r.Get("/hq/catalog/products/{productId}", s.handleHqCatalogProduct)
					r.Put("/hq/catalog/products/{productId}/prices", s.handleHqCatalogProductPrices)
					r.Post("/hq/catalog/products", s.handleHqCatalogProductCreate)
					r.Get("/hq/catalog/products/{productId}/movements", s.handleHqProductMovements)
					r.Get("/hq/inventory/branches", s.handleHqInventoryBranches)
					r.Get("/hq/inventory/products", s.handleHqInventoryProducts)
					r.Get("/hq/inventory/attention", s.handleHqInventoryAttention)
					r.Get("/hq/conflicts", s.handleHqConflicts)
					r.Post("/hq/conflicts/ack", s.handleHqConflictsAck)
					r.Get("/hq/reports/sales", s.handleHqReportSales)
					r.Get("/hq/reports/products", s.handleHqReportProducts)
					r.Get("/hq/reports/branches", s.handleHqReportBranches)
					r.Get("/hq/reports/staff", s.handleHqReportStaff)
					r.Get("/hq/customer-groups", s.handleHqCustomerGroups)
					r.Get("/hq/customers", s.handleHqCustomers)
					r.Post("/hq/customers", s.handleHqCustomerCreate)
					r.Put("/hq/customers/bulk", s.handleHqCustomersBulkUpdate)
					r.Get("/hq/customers/export", s.handleHqCustomersExport)
					r.Post("/hq/customers/import", s.handleHqCustomersImport)
					r.Get("/hq/customers/insights", s.handleHqCustomerInsights)
					r.Get("/hq/customers/{customerId}", s.handleHqCustomerDetail)
					r.Put("/hq/customers/{customerId}", s.handleHqCustomerUpdate)
					r.Get("/hq/customers/{customerId}/purchases", s.handleHqCustomerPurchases)
					r.Get("/hq/customers/{customerId}/ledger", s.handleHqCustomerLedger)
					// Suppliers (slice 8): same route shape as Customers above, one
					// prefix over. /hq/customer-groups is reused for suppliers too —
					// groups aren't type-scoped in the schema, no /hq/supplier-groups.
					r.Get("/hq/suppliers", s.handleHqSuppliers)
					r.Post("/hq/suppliers", s.handleHqSupplierCreate)
					r.Put("/hq/suppliers/bulk", s.handleHqSuppliersBulkUpdate)
					r.Get("/hq/suppliers/export", s.handleHqSuppliersExport)
					r.Post("/hq/suppliers/import", s.handleHqSuppliersImport)
					r.Get("/hq/suppliers/insights", s.handleHqSupplierInsights)
					r.Get("/hq/suppliers/{supplierId}", s.handleHqSupplierDetail)
					r.Put("/hq/suppliers/{supplierId}", s.handleHqSupplierUpdate)
					r.Get("/hq/suppliers/{supplierId}/purchases", s.handleHqSupplierPurchases)
					r.Get("/hq/suppliers/{supplierId}/ledger", s.handleHqSupplierLedger)
				})
			})
		})

		// --- Admin ---
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.requireAdmin)
			r.Get("/stats", s.handleAdminStats)
			r.Get("/clients", s.handleAdminSearchClients)
			r.Post("/clients", s.handleAdminCreateClient)
			r.Get("/clients/{id}", s.handleAdminGetClient)
			r.Patch("/clients/{id}", s.handleAdminUpdateClient)
			r.Post("/licenses", s.handleAdminAssignLicenses)
			r.Post("/licenses/{id}/status", s.handleAdminLicenseStatus)
			r.Post("/licenses/{id}/sign-offline", s.handleAdminSignOffline)
			r.Post("/licenses/{id}/extend-updates", s.handleAdminExtendUpdates)
			r.Post("/devices/{id}/release", s.handleAdminForceRelease)
			r.Get("/audit", s.handleAdminAudit)

			// Multi-tenant registry (subscription & billing levers).
			r.Post("/tenants/{id}/provision-sync", s.handleAdminProvisionSync)
			r.Delete("/tenants/{id}", s.handleAdminDeleteTenant)
			r.Post("/branches/{id}/seats", s.handleAdminBranchSeats)

			// Bills (Phase 10 billing): amount + period recorded against a
			// tenant; a paid bill auto-provisions sync if not already.
			r.Post("/tenants/{id}/bills", s.handleAdminCreateBill)
			r.Get("/tenants/{id}/bills", s.handleAdminListBills)
			r.Post("/bills/{id}/void", s.handleAdminVoidBill)

			// Fleet schema rollout (E3): migrate sync tenant DBs to the
			// gateway's version; report mixed-version state.
			r.Post("/rollout", s.handleAdminRollout)
			r.Get("/schema-report", s.handleAdminSchemaReport)
		})
	})
	return r
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
