# Portico Implementation Plans — Index

This directory contains seven self-contained implementation plans, one per phase, that take Portico from an empty repo to V1.

## How to use these plans

- **Read in order.** Each phase builds on the previous one. The "Prerequisites" and "Hand-off to next phase" sections at top and bottom of each plan make the boundary explicit.
- **Each plan is self-contained.** It includes goals, acceptance criteria, package layout, public types/interfaces, SQL DDL, MCP message shapes, configuration extensions, REST APIs, an implementation walkthrough, a test plan with named test cases, common pitfalls, and an explicit "out of scope" section.
- **Treat acceptance criteria as the definition of done.** Don't move on until every criterion passes.
- **Tests are not optional.** The "Test plan" section names every test that must exist; coverage targets are stated.
- **The RFC (`docs/rfc/RFC-001-Portico.md` — currently `RFC-001-Portico.md` at repo root) is the source of truth for product intent.** If a plan and the RFC disagree, the RFC wins; flag it and update the plan.

## Phase order (V1)

| # | Plan                                                          | Phase summary                                                       |
|---|---------------------------------------------------------------|---------------------------------------------------------------------|
| 0 | [phase-0-skeleton-tenant-foundation.md](./phase-0-skeleton-tenant-foundation.md) | Repo scaffolding, config loader, tenant context, JWT auth (with dev bypass), SQLite store, Console shell, CLI. |
| 1 | [phase-1-mcp-gateway-core.md](./phase-1-mcp-gateway-core.md) | MCP protocol types, northbound HTTP+SSE, southbound stdio + HTTP clients, tool aggregation with namespacing, dispatcher. |
| 2 | [phase-2-registry-lifecycle.md](./phase-2-registry-lifecycle.md) | Dynamic per-tenant registry, full process supervisor, runtime modes (`shared_global`, `per_tenant`, `per_user`, `per_session`, `remote_static`), env interpolation, log capture. |
| 3 | [phase-3-resources-prompts-mcp-apps.md](./phase-3-resources-prompts-mcp-apps.md) | Resources, resource templates, prompts, MCP Apps (`ui://`) with CSP, list-changed mux. |
| 4 | [phase-4-skills-runtime-virtual-directory.md](./phase-4-skills-runtime-virtual-directory.md) | Skill manifest format + JSON Schema, `SkillSource` interface + `LocalDir`, virtual directory exposed as `skill://` resources/prompts, per-session enablement, four reference packs. |
| 5 | [phase-5-auth-policy-credentials-approval.md](./phase-5-auth-policy-credentials-approval.md) | Real vault (AES-256-GCM), OAuth 2.0 token exchange, credential injection strategies, policy engine with risk classes, approval flow (elicitation + structured-error fallback), persisted audit store. |
| 6 | [phase-6-catalog-snapshots-observability.md](./phase-6-catalog-snapshots-observability.md) | Per-session catalog snapshots, schema fingerprinting + drift detection, OpenTelemetry tracing end-to-end, session inspector UI. |

## Cross-cutting conventions all plans assume

These are stated in pieces across plans; centralizing here for the implementor.

### Go style

- Go 1.22+.
- Module path: `github.com/hurtener/Portico_gateway`.
- All exported types and functions documented with godoc-style comments.
- No package-level mutable state except registered metrics and the global tracer.
- Errors wrapped with `fmt.Errorf("...: %w", err)`. Sentinel errors (`var ErrFoo = errors.New(...)`) for typed comparisons; avoid string matching.
- Context flows everywhere; never store `context.Context` in a struct.
- Goroutines started by long-lived components must be cancelled by a `context.Context` and joined on shutdown. No goroutine leaks.

### Logging

- Single logger: `log/slog` with the JSON handler in production and the text handler in dev.
- Loggers carry `tenant_id`, `request_id`, `trace_id` as attributes via `slog.Logger.With(...)`.
- Severity guidance: `Debug` = only useful when debugging; `Info` = lifecycle events worth telling an operator; `Warn` = unexpected but recovered; `Error` = the request/operation failed.
- Never log secrets. The audit store (Phase 5) has a redactor; for slog, keep payloads small and pre-redacted.

### Tenant scoping

- Every storage method that touches tenant-scoped tables takes a `tenantID string` parameter and uses it in a `WHERE tenant_id = ?`.
- A repo-level vet test asserts every tenant-scoped store function name uses tenantID (test enforces presence of the param via reflection).
- Internal handler code reads tenant from `tenant.MustFrom(ctx)`.

### Configuration

- Source of truth is `portico.yaml`. Hot reload supported for fields each phase calls out.
- Env vars: `PORTICO_DEV_TENANT`, `PORTICO_VAULT_KEY`, `OTEL_EXPORTER_OTLP_*` (standard).
- Defaults applied during `config.Validate`. Validation errors point at the offending field path.

### Testing

- Unit tests next to source. Integration tests in `test/integration/`.
- `go test -race ./...` is the gate. CI runs this on every push.
- Use `t.TempDir()` for filesystem fixtures. Never write outside the test dir.
- Mock MCP servers come from `examples/servers/mock/` (in-process for unit, standalone binary for integration).
- `testdata/` directories alongside packages for declarative fixtures.
- Tests are named per Phase plan; do not skip listed tests.

### Errors on the wire

- JSON-RPC: standard codes + Portico-defined codes documented in `internal/mcp/protocol/errors.go`.
- REST: errors are JSON `{"error":"<code>","message":"<msg>","details":...}` with a typed `code` slug. HTTP status used per standard semantics (400 invalid input, 401 unauthorized, 403 forbidden, 404 not found, 409 conflict, 422 entity invalid, 5xx server faults).

### Concurrency

- HTTP handlers handle each request on its own goroutine via Go stdlib.
- Long-running operations (process supervisor loops, drift detector, audit batcher) run in dedicated goroutines started at boot.
- Bounded channels with explicit drop policies on backpressure (drop-oldest with audit event, never block).
- `sync.Mutex` for in-memory state; `sync.RWMutex` only when contention measurably justifies it.

### Database

- SQLite via `modernc.org/sqlite`. Pure Go; no CGo.
- All migrations in `internal/storage/sqlite/migrations/NNNN_*.sql`. Applied by version number tracked in `schema_migrations`.
- All queries parameterized. No string concatenation.

### Build + release

- `make build` produces `bin/portico`. Static, CGo-free, < 30 MB on linux-amd64.
- `Dockerfile` produces a distroless `nonroot` image.
- `make release` (post-V1) produces multi-arch binaries.
- CI runs `make vet test build` on every push.

### Documentation

- Each phase's plan is treated as binding for that phase. Updates require a PR that rev-bumps the plan or files an exception.
- README at repo root carries the Quickstart and points at the RFC + plans.
- Concept docs (post-V1) live in `docs/concepts/`.

## Hand-off discipline

Each plan ends with "Hand-off to Phase N+1" naming exactly what the next phase inherits and what its first job is. When closing out a phase, update that section if anything materially differs from what was anticipated.

## Things deliberately NOT in V1

Cross-reference against the RFC §15 boundary:

- Postgres as default store (post-V1).
- Kubernetes deployment artifacts (post-V1).
- Redis-backed multi-instance coordination (post-V1).
- Sidecar / per_request runtime modes (post-V1).
- Quota enforcement (post-V1).
- Async approval channels (Slack, email, ticketing) (post-V1).
- Container / microVM stdio isolation (post-V1).
- Git / OCI / HTTP skill sources (post-V1).
- Hosted SaaS (post-V1).
- Alternative auth backends (mTLS, SSO direct) (post-V1).

These have placeholder hooks in V1 (interfaces ready) so they're additive when picked up.
