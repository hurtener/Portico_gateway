// Package api is the REST + management HTTP layer. Phase 0 ships health,
// audit stub, admin tenants. Later phases mount additional routes alongside
// without touching the middleware chain.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"errors"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	mcpnb "github.com/hurtener/Portico_gateway/internal/mcp/northbound/http"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
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

	// Phase 4 addition: skills runtime. Optional — when nil, /v1/skills
	// returns 503.
	Skills SkillsManager

	// Phase 5 additions: approval store + flow + vault + server-initiated
	// requester (the northbound transport routes JSON-RPC responses
	// through it to wake pending elicitation calls).
	Approvals    ifaces.ApprovalStore
	ApprovalFlow *approvalFlow
	Vault        VaultManager
	ServerInit   *mcpnb.ServerInitiatedRequester

	// Phase 6: snapshot service + lazy session→snapshot binder.
	Snapshots      *snapshots.Service
	SnapshotBinder *mcpgw.SnapshotBinder
}

// approvalFlow is the slice of internal/policy/approval.Flow the API
// package needs (just ResolveManually). Declared as a struct here to keep
// the api package from importing approval directly.
type approvalFlow struct {
	resolveManually func(ctx context.Context, tenantID, id, status, actorUserID string) (*approval.Approval, error)
}

// NewApprovalFlowAdapter wraps a *internal/policy/approval.Flow's
// ResolveManually so the api package can stay free of the approval
// import beyond the DTO conversion. The cmd/portico wiring constructs
// it.
func NewApprovalFlowAdapter(resolve func(ctx context.Context, tenantID, id, status, actorUserID string) (*approval.Approval, error)) *approvalFlow {
	return &approvalFlow{resolveManually: resolve}
}

// ResolveManually exposes the wrapped function with the approval flow's
// signature — convenient for handlers.
func (f *approvalFlow) ResolveManually(ctx context.Context, tenantID, id, status, actor string) (*approval.Approval, error) {
	if f == nil || f.resolveManually == nil {
		return nil, errors.New("approval flow not configured")
	}
	return f.resolveManually(ctx, tenantID, id, status, actor)
}

// SkillsManager is the API-facing surface of the skills runtime. The
// real type is internal/skills/runtime.Manager; declared as an
// interface so this package doesn't import the runtime directly.
type SkillsManager = skillsManager

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

		// Phase 5: approvals (read-only — admin scope required for resolve).
		if d.Approvals != nil {
			r.Get("/v1/approvals", listApprovalsHandler(d))
			r.Get("/v1/approvals/{id}", getApprovalHandler(d))
		}

		// Phase 6: snapshots + per-session inspector hook.
		if d.Snapshots != nil {
			r.Post("/v1/catalog/resolve", resolveCatalogHandler(d))
			r.Get("/v1/catalog/snapshots", listSnapshotsHandler(d))
			r.Get("/v1/catalog/snapshots/{id}", getSnapshotHandler(d))
			r.Get("/v1/catalog/snapshots/{a}/diff/{b}", diffSnapshotsHandler(d))
		}
		if d.SnapshotBinder != nil {
			r.Get("/v1/sessions/{session_id}/snapshot", sessionSnapshotHandler(d))
		}

		// Phase 4: skills runtime APIs.
		if d.Skills != nil {
			r.Get("/v1/skills", listSkillsHandler(d))
			r.Get("/v1/skills/{id}", getSkillHandler(d))
			r.Get("/v1/skills/{id}/manifest.yaml", getSkillManifestYAML(d))
			r.Post("/v1/skills/{id}/enable", enableSkillHandler(d, true))
			r.Post("/v1/skills/{id}/disable", enableSkillHandler(d, false))

			r.Get("/v1/sessions/{session_id}/skills", listSessionSkillsHandler(d))
			r.Post("/v1/sessions/{session_id}/skills/enable", sessionSkillEnableHandler(d, true))
			r.Post("/v1/sessions/{session_id}/skills/disable", sessionSkillEnableHandler(d, false))
		}

		// Phase 1: northbound MCP transport. Mounted under the auth group so the
		// dev-mode bypass + JWT path both produce a tenant identity for the session.
		if d.Sessions != nil && d.Dispatcher != nil {
			h := mcpnb.NewHandlerWithConfig(d.Sessions, d.Dispatcher, d.Logger, mcpnb.HandlerConfig{
				AllowedOrigins:        d.AllowedOrigins,
				AllowLocalhostOrigins: d.DevMode,
			})
			if d.ServerInit != nil {
				h.SetServerInitiated(d.ServerInit)
			}
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

			// Phase 5: manual approval resolution + secrets management.
			if d.ApprovalFlow != nil {
				r.Post("/v1/approvals/{id}/approve", resolveApprovalHandler(d, approval.StatusApproved))
				r.Post("/v1/approvals/{id}/deny", resolveApprovalHandler(d, approval.StatusDenied))
			}
			if d.Vault != nil {
				r.Get("/v1/admin/secrets", listAdminSecretsHandler(d))
				r.Put("/v1/admin/secrets/{tenant}/{name}", putAdminSecretHandler(d))
				r.Delete("/v1/admin/secrets/{tenant}/{name}", deleteAdminSecretHandler(d))
			}
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
