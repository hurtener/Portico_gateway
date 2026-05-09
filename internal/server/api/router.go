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
	apimw "github.com/hurtener/Portico_gateway/internal/server/api/middleware"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/server/ui"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// GatewayInfo carries the connection facts the Console / external
// operators need to actually use the gateway. Populated at startup
// from cfg.Server + cfg.Auth so handlers can serve the public
// /api/gateway/info read-only endpoint without re-reading the file.
type GatewayInfo struct {
	Bind    string
	MCPPath string
	// JWT — empty when DevMode is true.
	JWTIssuer      string
	JWTAudiences   []string
	JWTJWKSURL     string
	JWTTenantClaim string
	JWTScopeClaim  string
}

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
	Gateway     GatewayInfo

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

	// Phase 8: skill source registry + authored skills store + the
	// validate-only pipeline. All optional — handlers return 503 when
	// nil, and the corresponding routes are skipped.
	SkillSources   SkillSourcesController
	AuthoredSkills AuthoredSkillsController
	SkillValidator SkillValidator

	// Phase 9: Console CRUD additions. Each is optional — handlers gate
	// on the corresponding nil so phase-N+1 builds boot cleanly when a
	// dependency is missing.
	AuditEmitter   AuditEmitter
	EntityActivity ifaces.EntityActivityStore
	PolicyRules    PolicyRulesController
	ServerRuntime  ifaces.ServerRuntimeStore
	VaultReveal    VaultRevealManager

	// Phase 10: playground service + saved-case store. Both are
	// optional — when nil, /api/playground/* returns 503.
	Playground       PlaygroundController
	PlaygroundStore  ifaces.PlaygroundStore
	ApprovalStoreRaw ifaces.ApprovalStore
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

// NewRouter wires the full HTTP routing surface. The complexity is
// structural: every Phase N+ route group is gated on its own optional
// Deps field so a partially-wired build still serves the surfaces it
// has. Splitting would obscure the routing surface operators read
// top-to-bottom.
//
//nolint:gocyclo // structural complexity from optional Deps gating.
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

	// Phase 10.9: gateway connection info. Public read-only — exposes
	// only what an operator can already observe by probing the
	// listener. See handlers_gateway.go for the rationale.
	r.Get("/api/gateway/info", gatewayInfoHandler(d))

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

			// Phase 9: /api/servers surface — adds restart, logs (SSE),
			// health, partial-update PATCH, and per-server activity.
			r.Get("/api/servers", listServersHandler(d))
			r.Post("/api/servers", createAPIServerHandler(d))
			r.Get("/api/servers/{id}", getServerHandler(d))
			r.Put("/api/servers/{id}", upsertServerHandler(d, true))
			r.Patch("/api/servers/{id}", patchServerHandler(d))
			r.Delete("/api/servers/{id}", deleteServerHandler(d))
			r.Post("/api/servers/{id}/restart", restartServerHandler(d))
			r.Get("/api/servers/{id}/logs", logsServerHandler(d))
			r.Get("/api/servers/{id}/health", healthServerHandler(d))
			r.Get("/api/servers/{id}/activity", activityHandler(d, "server"))
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

		// Phase 8: skill source registry CRUD.
		if d.SkillSources != nil {
			r.Get("/api/skill-sources", listSkillSourcesHandler(d))
			r.Post("/api/skill-sources", upsertSkillSourceHandler(d, false))
			r.Get("/api/skill-sources/{name}", getSkillSourceHandler(d))
			r.Put("/api/skill-sources/{name}", upsertSkillSourceHandler(d, true))
			r.Delete("/api/skill-sources/{name}", deleteSkillSourceHandler(d))
			r.Post("/api/skill-sources/{name}/refresh", refreshSkillSourceHandler(d))
			r.Get("/api/skill-sources/{name}/packs", listSkillSourcePacksHandler(d))
		}
		// Phase 8: authored skills CRUD + publish + validate.
		if d.AuthoredSkills != nil {
			r.Get("/api/skills/authored", listAuthoredHandler(d))
			r.Post("/api/skills/authored", createAuthoredHandler(d))
			r.Get("/api/skills/authored/{id}", getAuthoredActiveHandler(d))
			r.Get("/api/skills/authored/{id}/versions", historyAuthoredHandler(d))
			r.Get("/api/skills/authored/{id}/versions/{v}", getAuthoredVersionHandler(d))
			r.Put("/api/skills/authored/{id}/versions/{v}", updateAuthoredHandler(d))
			r.Post("/api/skills/authored/{id}/versions/{v}/publish", publishAuthoredHandler(d))
			r.Post("/api/skills/authored/{id}/versions/{v}/archive", archiveAuthoredHandler(d))
			r.Delete("/api/skills/authored/{id}/versions/{v}", deleteAuthoredDraftHandler(d))
		}
		if d.SkillValidator != nil {
			r.Post("/api/skills/validate", validateSkillHandler(d))
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
			r.Post("/v1/admin/tenants", upsertTenantHandler(d, false))

			// Phase 9 / 10 carry-over: build a per-verb approval gate
			// when the approval store is wired. The gate intercepts the
			// first request, returns 202 + approval_request_id, and only
			// lets the verb through when the operator re-issues with
			// X-Approval-Token: <id> after the row reaches `approved`.
			//
			// Mounted opt-in per route — never as a router-wide wrapper.
			approvalGateFor := func(verb string) func(http.Handler) http.Handler {
				if d.Approvals == nil {
					return func(next http.Handler) http.Handler { return next }
				}
				return apimw.NewApprovalGate(apimw.Config{
					Store: d.Approvals,
					Audit: d.AuditEmitter,
					Verb:  verb,
				})
			}

			// Phase 9: /api/admin/tenants full CRUD.
			r.Get("/api/admin/tenants", listTenantsHandler(d))
			r.Post("/api/admin/tenants", upsertTenantHandler(d, false))
			r.Get("/api/admin/tenants/{id}", getTenantHandler(d))
			r.Put("/api/admin/tenants/{id}", upsertTenantHandler(d, true))
			r.With(approvalGateFor("tenant.delete")).Delete("/api/admin/tenants/{id}", deleteTenantHandler(d))
			r.With(approvalGateFor("tenant.purge")).Post("/api/admin/tenants/{id}/purge", purgeTenantHandler(d))
			r.Get("/api/admin/tenants/{id}/activity", activityHandler(d, "tenant"))

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
			// Phase 9: richer /api/admin/secrets surface. Mounted
			// regardless of d.Vault so the routes shadow the SPA fallback
			// — handlers return 503 when the vault is unconfigured.
			// Specific paths first so chi prefers them over the {name}
			// catch-all.
			r.Get("/api/admin/secrets/reveal/{token}", revealConsumeHandler(d))
			r.With(approvalGateFor("secret.rotate_root")).Post("/api/admin/secrets/rotate-root", rotateRootHandler(d))
			r.Post("/api/admin/secrets/{name}/rotate", rotateAPISecretHandler(d))
			r.Post("/api/admin/secrets/{name}/reveal", revealIssueHandler(d))
			r.Get("/api/admin/secrets/{name}/activity", activityHandler(d, "secret"))
			r.Get("/api/admin/secrets/{name}", getAPISecretMetadataHandler(d))
			r.Put("/api/admin/secrets/{name}", putAPISecretHandler(d))
			r.With(approvalGateFor("secret.delete")).Delete("/api/admin/secrets/{name}", deleteAPISecretHandler(d))
			r.Get("/api/admin/secrets", listAPISecretsHandler(d))
			r.Post("/api/admin/secrets", createAPISecretHandler(d))
		})

		// Phase 10: Interactive Playground. Mounted under the auth
		// group so the synth-tenant identity in dev mode reaches the
		// handlers. Gated on Deps.Playground; saved-case CRUD only
		// requires the store.
		if d.Playground != nil {
			r.Post("/api/playground/sessions", startPlaygroundSessionHandler(d))
			r.Get("/api/playground/sessions/{sid}", getPlaygroundSessionHandler(d))
			r.Delete("/api/playground/sessions/{sid}", endPlaygroundSessionHandler(d))
			r.Get("/api/playground/sessions/{sid}/catalog", catalogPlaygroundHandler(d))
			r.Post("/api/playground/sessions/{sid}/calls", issueCallHandler(d))
			r.Get("/api/playground/sessions/{sid}/calls/{cid}/stream", streamCallHandler(d))
			r.Get("/api/playground/sessions/{sid}/correlation", playgroundCorrelationHandler(d))
			r.Post("/api/playground/sessions/{sid}/skills/{skill_id}/enable", setSessionSkillEnabledHandler(d, true))
			r.Post("/api/playground/sessions/{sid}/skills/{skill_id}/disable", setSessionSkillEnabledHandler(d, false))
			r.Post("/api/playground/cases/{id}/replay", replayPlaygroundCaseHandler(d))
			r.Get("/api/playground/runs/{run_id}", runDetailHandler(d))
			r.Get("/api/playground/runs/{run_id}/correlation", runCorrelationHandler(d))
		}
		if d.PlaygroundStore != nil {
			r.Get("/api/playground/cases", listPlaygroundCasesHandler(d))
			r.Post("/api/playground/cases", createPlaygroundCaseHandler(d))
			r.Get("/api/playground/cases/{id}", getPlaygroundCaseHandler(d))
			r.Put("/api/playground/cases/{id}", updatePlaygroundCaseHandler(d))
			r.Delete("/api/playground/cases/{id}", deletePlaygroundCaseHandler(d))
			r.Get("/api/playground/cases/{id}/runs", caseRunsHandler(d))
		}

		// Phase 9: Policy editor endpoints. Mounted under the auth group;
		// tenant scope is honoured implicitly by every store call.
		if d.PolicyRules != nil {
			r.Get("/api/policy/rules", listPolicyRulesHandler(d))
			r.Put("/api/policy/rules", putPolicyRulesHandler(d))
			r.Post("/api/policy/rules", postPolicyRuleHandler(d))
			r.Put("/api/policy/rules/{id}", putPolicyRuleHandler(d))
			r.Delete("/api/policy/rules/{id}", deletePolicyRuleHandler(d))
			r.Post("/api/policy/dry-run", dryRunPolicyHandler(d))
			r.Get("/api/policy/activity", listPolicyActivityHandler(d))
		}

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
