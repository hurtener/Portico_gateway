// Package api is the REST + management HTTP layer. Phase 0 ships health,
// audit stub, admin tenants. Later phases mount additional routes alongside
// without touching the middleware chain.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"errors"

	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	mcpnb "github.com/hurtener/Portico_gateway/internal/mcp/northbound/http"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/server/ui"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Deps bundles the runtime objects every route handler needs.
type Deps struct {
	Logger      *slog.Logger
	Validator   *jwt.Validator // nil in dev mode
	DevMode     bool
	DevTenant   string
	Tenants     ifaces.TenantStore
	Audit       ifaces.AuditStore
	Version     string
	BuildCommit string

	// Phase 1 additions: MCP gateway. Optional in tests (nil = no /mcp).
	Sessions   *mcpgw.SessionRegistry
	Dispatcher *mcpgw.Dispatcher
	Manager    *southboundmgr.Manager
}

// NewRouter wires the full HTTP routing surface.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	// Order matters. RequestID -> Recover -> Logger -> Tenant auth.
	r.Use(chimw.RequestID)
	r.Use(chimw.Recoverer)
	r.Use(slogRequestLogger(d.Logger))

	// Health endpoints attach BEFORE auth (they're in the always-allow list inside
	// the auth middleware too, but mounting them outside the auth group is the
	// most defensible posture).
	r.Get("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler(d))

	// Auth applies to everything below.
	r.Group(func(r chi.Router) {
		r.Use(tenant.Middleware(tenant.MiddlewareConfig{
			Validator:   d.Validator,
			DevMode:     d.DevMode,
			DevTenant:   d.DevTenant,
			TenantStore: d.Tenants,
			Logger:      d.Logger,
		}))

		// REST: tenant-scoped audit (Phase 5 fills the body)
		r.Get("/v1/audit/events", auditQueryHandler(d))

		// Phase 1: northbound MCP transport. Mounted under the auth group so the
		// dev-mode bypass + JWT path both produce a tenant identity for the session.
		if d.Sessions != nil && d.Dispatcher != nil {
			h := mcpnb.NewHandler(d.Sessions, d.Dispatcher, d.Logger)
			r.Method("POST", "/mcp", h)
			r.Method("GET", "/mcp", h)
			r.Method("DELETE", "/mcp", h)
		}

		// Admin endpoints
		r.Group(func(r chi.Router) {
			r.Use(scope.Require("admin"))
			r.Get("/v1/admin/tenants", listTenantsHandler(d))
			r.Get("/v1/admin/tenants/{id}", getTenantHandler(d))
			r.Post("/v1/admin/tenants", upsertTenantHandler(d))
		})

		// Console UI (HTML + static)
		ui.Mount(r, ui.Deps{Logger: d.Logger, Version: d.Version, DevMode: d.DevMode})
	})

	// 404 / 405 fall through to JSON handlers
	r.NotFound(notFoundHandler)
	r.MethodNotAllowed(methodNotAllowedHandler)

	return r
}

// IsErrNotFound branches on the canonical storage not-found sentinel without
// requiring callers to import storage/ifaces themselves.
func IsErrNotFound(err error) bool {
	return errors.Is(err, ifaces.ErrNotFound)
}
