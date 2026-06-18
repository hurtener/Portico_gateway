# Portico Implementation Plans — Index

This directory contains the self-contained implementation plans that take Portico from an empty repo to V2. Phases 0–6 build the engine; Phase 7 lands the design system; Phases 8–11 build the operator surface (skill sources, Console CRUD, playground, telemetry replay); Phase 12 ships V1; Phase 13 (V1.5) adds the LLM gateway on the **Bifrost** engine (rewritten 2026-05-12); Phase 13.5 ships MCP Code Mode. **Phases 14–19 (V2)** make Portico the agentic gateway enterprises actually buy — see the [V2 roadmap](./v2-roadmap-agentgateway-parity.md) for the full strategic picture. The 2026-05-12 pivot: Phase 14 is now **Agent Profiles** (consumer-binding primitive), the Envoy-shaped substrate work was dropped, and Phase 15 (HTTP/gRPC reverse proxy) was deferred indefinitely. Phase 15.5 adds the Bifrost-shaped semantic cache + Virtual Keys + hierarchical budgets layer on top of Agent Profiles.

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
| 7 | [phase-7-design-system-implementation.md](./phase-7-design-system-implementation.md) | Token-driven Console design system: light + dark mode, component library, Inter / JetBrains Mono / Newsreader self-hosted, brand placement, accessibility pass. |
| 8 | [phase-8-skill-sources-first-class.md](./phase-8-skill-sources-first-class.md) | Skill sources first-class: Git + HTTP drivers + in-Portico authored skills, REST + Console CRUD, hot-reload propagation, validation pipeline with JSON-Pointer errors. |
| 9 | [phase-9-console-crud.md](./phase-9-console-crud.md) | Console CRUD for servers, tenants, secrets, policy editor; hot-reload everywhere; destructive actions go through the approval flow; permission scopes enforced. |
| 10 | [phase-10-playground.md](./phase-10-playground.md) | Interactive MCP playground: catalog browser, schema-driven tool call composer, streamed response, live trace + audit + policy + drift correlation, saved cases + replay. |
| 11 | [phase-11-telemetry-replay.md](./phase-11-telemetry-replay.md) | Self-contained span store, session bundle exporter/importer, time-travel inspector with state-at-time scrubber, cross-session pivots, FTS audit search, replay-from-inspector. |
| 12 | [phase-12-onboarding-distribution.md](./phase-12-onboarding-distribution.md) | First-run wizard, `portico init`, in-Console help system, embedded docs site, OpenAPI extractor, `make release` multi-arch + signed artifacts, MCP conformance suite. **V1 ships at the end of this phase.** |

## Phase order (V1.5 + V1.6)

| #     | Plan                                                                              | Phase summary                                                       |
|-------|-----------------------------------------------------------------------------------|---------------------------------------------------------------------|
| 13    | [phase-13-llm-gateway.md](./phase-13-llm-gateway.md) *(2026-05-12 rewrite)*       | LLM gateway on the **Bifrost** engine (`github.com/maximhq/bifrost/core`, Apache 2.0, pure Go): OpenAI-compatible northbound, 23 native providers + a `custom_openai` provider type with a curated template catalog (DeepSeek, Together, Anyscale, Lepton, Internal vLLM, httpbun mock, …), per-tenant provider + model + key registry with Bifrost-shaped weighted routing/fallback, vault-backed keys, tool-use bridging, quotas + cost telemetry, OpenAI conformance suite. |
| 13.5  | [phase-13.5-mcp-code-mode.md](./phase-13.5-mcp-code-mode.md) *(new 2026-05-12)*   | MCP Code Mode: four meta-tools (`listToolFiles`, `readToolFile`, `getToolDocs`, `executeToolCode`) over a virtual `.pyi` catalog, backed by a hardened Starlark sandbox (`go.starlark.net`). ~50% token savings on multi-tool sessions; approval-suspension via Starlark continuations; per-session opt-in; Skill Pack canonical-snippet integration. |

## Phase order (V2 — Bifrost-shaped agentic gateway)

The V2 line is described end-to-end in [v2-roadmap-agentgateway-parity.md](./v2-roadmap-agentgateway-parity.md). After the 2026-05-12 pivot it positions Portico as a Bifrost-shaped agentic gateway with a primitive neither Bifrost nor agentgateway has — **Agent Profiles** as the unified consumer-binding object. We deliberately do NOT compete with agentgateway on general-purpose HTTP/gRPC reverse-proxy capability (Phase 15 is deferred indefinitely); customers keep their existing HTTP gateway and consolidate *agentic* traffic on Portico.

| #     | Plan                                                                                                | Phase summary                                                                                  |
|-------|-----------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------|
| **14**    | **[phase-14-agent-profiles.md](./phase-14-agent-profiles.md) *(new 2026-05-12; replaces the retired Envoy substrate plan)*** | **Agent Profiles — the first-class consumer-binding primitive. A Profile binds a logical agent to allowed MCP servers + tools + Skill Packs + LLM aliases + scopes + N attached Virtual Keys. One CRUD surface (`/agents`) replaces the prior composition of Phase 5 scopes + Phase 6 snapshot scoping + Phase 4 Skill enablement + Phase 15.5 VK MCP allowlists. The Console headline moves from `/servers` to `/agents`.** |
| 15    | [phase-15-http-grpc-proxy.md](./phase-15-http-grpc-proxy.md) *(DEFERRED post-V2 — 2026-05-12)*       | General-purpose HTTP/gRPC reverse proxy. Dropped from the V2 line. File retained for reference if customer demand re-opens the case. |
| 15.5  | [phase-15.5-semantic-cache-and-virtual-keys.md](./phase-15.5-semantic-cache-and-virtual-keys.md) *(new 2026-05-12)* | Bifrost-shaped governance layer: **semantic cache** (Weaviate/Redis/Qdrant pluggable, §4.4 seam) + **Virtual Keys** (`pk-portico-*` HMAC-bound credentials attached to Agent Profiles) + **hierarchical budgets** (VK → Team → Customer → Tenant). |
| 16    | [phase-16-a2a-protocol.md](./phase-16-a2a-protocol.md) *(reshaped 2026-05-12)*                       | A2A as a peer protocol to MCP — same single listener, same envelope, same Profile resolver. Catalog rows for A2A tasks/skills. Opt-in MCP↔A2A bridges configured on the Agent Profile. |
| 17    | [phase-17-tool-poisoning-defense.md](./phase-17-tool-poisoning-defense.md)                          | Schema attestation, drift gates that block (not just detect), description + result prompt-injection scanning, supply-chain digest pinning. Applies uniformly to MCP and A2A. |
| 18    | [phase-18-dynamic-config-api.md](./phase-18-dynamic-config-api.md) *(reshaped 2026-05-12)*           | GitOps + watch channel over **existing** Portico CRUD surfaces (Agent Profiles, VKs, Servers, Skills, Policies, A2A peers, Security). Bulk apply with transactional rollback. **No Envoy ADS adapter, no Listener/Route/Backend resources** — those don't exist in Portico. |
| 19    | [phase-19-production-scale-out.md](./phase-19-production-scale-out.md) *(updated 2026-05-12)*        | Postgres-default, Redis coordination, Kubernetes operator + Helm chart, federation, sandboxed runtime modes. CRD set maps to Portico's resource model (AgentProfile/VK/Server/Skill/etc.) — **no Listener/Route/Backend CRDs**. |
| **20**    | **[phase-20-productization.md](./phase-20-productization.md) *(new 2026-06-17)*** | **Productization — workflow-driven pre-launch verification. Adversarially proves every load-bearing claim is true, every Console page's UX is accurate/correct/reachable, and each feature delivers real value against the live binary. Driven by the orchestrator's multi-agent Workflow fan-out + adversarial verify. Runs AFTER 13.5–19, immediately BEFORE Phase 12 (launch). Fixes land as normal PRs; top findings get regression locks.** |

> **Build/launch sequencing (2026-06-17):** it is ALL V1 — nothing launches until the whole roadmap is built. Order: 13 ✅ → 13.5 ✅ → 14 → 15.5 → 16 → 17 → 18 → 19 → **20 (productization)** → **12 (onboarding/distribution/launch, dead last)**. Phase 12's "V1 ships at the end of this phase" still holds — it's just sequenced last, after Phase 20's verification pass. The free-agent build loop funds building the full scope before launch.

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

Cross-reference against the RFC §15 boundary. Note that several items in the original RFC §15 list move into V1 in Phases 8–12 (Git + HTTP skill sources land in Phase 8; LLM-quota-style enforcement lands in Phase 13's surface). What remains genuinely post-V1:

- Postgres as default store (post-V1).
- Kubernetes deployment artifacts (post-V1; Compose example ships in Phase 12).
- Redis-backed multi-instance coordination (post-V1).
- Sidecar / per_request runtime modes (post-V1).
- Async approval channels (Slack, email, ticketing) (post-V1).
- Container / microVM stdio isolation (post-V1).
- OCI skill source (post-V1; HTTP + Git ship in Phase 8).
- Hosted SaaS (post-V1).
- Alternative auth backends (mTLS, SSO direct) (post-V1).
- Cross-instance distributed tracing / replay (post-V1; Phase 11 covers single-instance).
- Visual / drag-drop manifest builder for skills (post-V1).
- Per-user (sub-tenant) RBAC (post-V1).
- Mobile-first Console layouts (post-V1).
- LLM gateway extras: fine-tuning APIs, multimodal, hosted vector indices (post-V1.5).

These have placeholder hooks in V1 (interfaces ready, factories registered) so they're additive when picked up.

## V1 vs. V1.5 vs. V2 boundary

V1 is feature-complete with Phase 12. The binary that ships at the end of Phase 12 is the artifact a public V1 announcement points at: full MCP gateway, multi-tenant operator surface, observability stack, polished Console + docs + conformance suite + signed multi-arch release.

V1.5 (Phase 13) is the LLM gateway extension. As of the 2026-05-12 rewrite it runs on the **Bifrost** Go SDK (pure Go, CGo-free, Apache 2.0) and ships a custom-provider template catalog that closes the `agentgateway` openai-compatible-providers gap (DeepSeek, internal vLLM, Together, Anyscale, Lepton, …). It is additive — V1 deployments continue working untouched.

V1.6 (Phase 13.5) is MCP Code Mode. Token-saving virtualised tool surface + Starlark sandbox. Opt-in per session; existing clients see no change. **Status: done** — the JSON-Schema→`.pyi` stub translator, the hardened Starlark sandbox (`internal/mcp/codemode/runtime/`, threat-modelled in `docs/security/code-mode-threat-model.md`), the catalog projector, the session opt-in + four meta-tools, `executeToolCode` with the governed in-sandbox dispatch path (acceptance #8 proven by integration test), the approval-suspend continuation flow (acceptance #9 — hardened across four adversarial red-team rounds), the `code_mode` policy matchers/actions including `require_approval_on_executeToolCode`, the observability API + dashboard and interactive playground Console screens (acceptance #11/#12), the `portico code-mode render|exec` CLI, and the RFC §8.5 Code Mode section all shipped. Concept docs: [`code-mode`](../concepts/code-mode.md), [`code-mode-savings`](../concepts/code-mode-savings.md), [`use-code-mode`](../how-to/use-code-mode.md).

V2 (Phases 14–19, plus 15.5) is the **Bifrost-shaped agentic gateway line**. It is additive: a V1 / V1.5 / V1.6 deployment continues to work against a V2 binary if the operator does not create any Agent Profiles, Virtual Keys, Budgets, A2A peers, or scale-out configuration. Phase 14's Agent Profile resolver synthesises a "default profile" (full tenant surface) for any principal that has no explicit Profile bound, preserving backward compatibility by construction. The umbrella document is [`v2-roadmap-agentgateway-parity.md`](./v2-roadmap-agentgateway-parity.md); it is binding for the V2 line in the same way each phase plan is binding for its phase, with the standard precedence (RFC > phase plan > roadmap > AGENTS.md).

The 2026-05-12 pivot is documented in the roadmap's frontmatter and in the affected phase plans (13 rewrite for the Bifrost engine swap; 14 rewrite as Agent Profiles; 15 deferred; 18 reshaped; 16, 19 updated). The retired Envoy-substrate Phase 14 file (`phase-14-listener-route-backend-substrate.md`) is removed; git history preserves it.

Phases beyond V2 are not pre-planned. They are negotiated when the work is queued, drawing on the patterns these plans establish.
