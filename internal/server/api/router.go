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

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	mcpnb "github.com/hurtener/Portico_gateway/internal/mcp/northbound/http"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
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

	// Phase 2 addition: server registry. Optional in tests (nil = no /v1/servers).
	Registry *registry.Registry

	// Phase 3 addition: MCP Apps registry (ui:// resource index).
	// Optional — handlers gate on nil so tests can omit it.
	Apps *apps.Registry

	// AllowedOrigins is the operator-configured allow-list passed
	// through to the northbound HTTP transport's Origin guard
	// (spec 2025-11-25). Empty in dev mode is fine; localhost is auto-
	// allowed when DevMode is true.
	AllowedOrigins []string
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

		// REST: tenant-scoped server registry (Phase 2). Mounted only when
		// the registry dependency is provided so test scaffolding can omit it.
		if d.Registry != nil {
			r.Get("/v1/servers", listServersHandler(d))
			r.Post("/v1/servers", upsertServerHandler(d, false))
			r.Get("/v1/servers/{id}", getServerHandler(d))
			r.Put("/v1/servers/{id}", upsertServerHandler(d, true))
			r.Delete("/v1/servers/{id}", deleteServerHandler(d))
			r.Post("/v1/servers/{id}/reload", reloadServerHandler(d))
			r.Post("/v1/servers/{id}/enable", enableServerHandler(d, true))
			r.Post("/v1/servers/{id}/disable", enableServerHandler(d, false))
			r.Get("/v1/servers/{id}/instances", listInstancesHandler(d))
		}

		// Phase 3: resources, prompts, apps. Gated on the dispatcher
		// having Phase 3 aggregators wired (otherwise paths return 503).
		if d.Dispatcher != nil {
			r.Get("/v1/resources", listResourcesHandler(d))
			r.Get("/v1/resources/templates", listResourceTemplatesHandler(d))
			r.Get("/v1/resources/*", readResourceHandler(d))
			r.Get("/v1/prompts", listPromptsHandler(d))
			r.Post("/v1/prompts/{name}", getPromptHandler(d))
		}
		if d.Apps != nil {
			r.Get("/v1/apps", listAppsHandler(d))
		}

		// Phase 1: northbound MCP transport. Mounted under the auth group so the
		// dev-mode bypass + JWT path both produce a tenant identity for the session.
		if d.Sessions != nil && d.Dispatcher != nil {
			h := mcpnb.NewHandlerWithConfig(d.Sessions, d.Dispatcher, d.Logger, mcpnb.HandlerConfig{
				AllowedOrigins:        d.AllowedOrigins,
				AllowLocalhostOrigins: d.DevMode,
			})
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
