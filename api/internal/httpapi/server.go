// Package httpapi wires the HTTP transport: router, middleware and handlers.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/aribpos/license-api/internal/admin"
	"github.com/aribpos/license-api/internal/auth"
	"github.com/aribpos/license-api/internal/device"
	"github.com/aribpos/license-api/internal/rollout"
	"github.com/aribpos/license-api/internal/tenant"
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
	rollout *rollout.Service
	log     *slog.Logger

	corsOrigins []string
	otpLimiter  *keyedLimiter
}

// New builds an HTTP Server. corsOrigins are the browser origins (admin
// dashboard) allowed to call the API; nil disables CORS.
func New(authSvc *auth.Service, deviceSvc *device.Service, adminSvc *admin.Service, tenantSvc *tenant.Service, rolloutSvc *rollout.Service, corsOrigins []string, log *slog.Logger) *Server {
	return &Server{
		auth:        authSvc,
		device:      deviceSvc,
		admin:       adminSvc,
		tenant:      tenantSvc,
		rollout:     rolloutSvc,
		log:         log,
		corsOrigins: corsOrigins,
		otpLimiter:  newKeyedLimiter(rateEvery(time.Minute), 3),
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
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/v1/sync-public-key", func(w http.ResponseWriter, _ *http.Request) {
		pemStr, err := s.tenant.SyncPublicKeyPEM()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "key unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write([]byte(pemStr))
	})

	r.Route("/v1", func(r chi.Router) {
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
			r.Post("/devices/{id}/release", s.handleAdminForceRelease)
			r.Get("/audit", s.handleAdminAudit)

			// Multi-tenant registry (subscription & billing levers).
			r.Post("/tenants/{id}/provision-sync", s.handleAdminProvisionSync)
			r.Delete("/tenants/{id}", s.handleAdminDeleteTenant)
			r.Post("/branches/{id}/seats", s.handleAdminBranchSeats)

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
