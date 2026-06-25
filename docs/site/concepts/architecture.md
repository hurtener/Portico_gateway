# Architecture

Portico is a single static Go binary — built with `CGO_ENABLED=0`, no native dependencies — that acts as a unified control plane for agentic traffic. One listener, one artifact, three protocol surfaces: the MCP gateway, the A2A peer endpoint, and an OpenAI-compatible LLM API. Every request, regardless of which surface it arrives on, passes through the same governance envelope before touching any downstream server or inference provider.

This page describes the overall structure: how the binary is composed, how the governance envelope is applied, what northbound and southbound mean, how extensibility seams keep the architecture open to new backends, and how storage is wired.

---

## Single-listener, multi-protocol model

All protocol surfaces share one HTTP listener, configured via `server.bind` in `portico.yaml` (default: `127.0.0.1:8080`). The router mounts each surface at a distinct path prefix:

| Path prefix | Protocol surface |
|---|---|
| `/mcp` | MCP gateway — northbound JSON-RPC 2.0 over Streamable HTTP (SSE) |
| `/a2a` | A2A peer — inbound agent-to-agent JSON-RPC |
| `/v1` | OpenAI-compatible LLM API |
| `/api` | Management REST API (tenants, servers, profiles, governance, audit) |
| `/` | Embedded operator Console (SvelteKit SPA, served from `embed.FS`) |
| `/healthz`, `/readyz` | Health probes (no auth required) |

There is no separate proxy, no sidecar, no second process. `portico dev` binds to `127.0.0.1:18080` by default and synthesises a `dev` tenant automatically; `portico serve --config portico.yaml` is the production form. Both use the same boot path and the same governance envelope — dev mode only skips JWT validation.

---

## ASCII architecture diagram

```
┌──────────────────────────────────────────────────────────────────┐
│  AI Client / Agent / IDE / Desktop Host                          │
└───────────────────────────────┬──────────────────────────────────┘
                                │  HTTP (MCP / A2A / LLM / REST)
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│  One Listener  (server.bind)                                     │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  Authentication boundary                                   │  │
│  │  JWT validation (RS256/ES256)  |  Virtual Key HMAC verify  │  │
│  │  Tenant resolution → context.Context                       │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  Agent Profile resolver                                    │  │
│  │  principal → Profile{AllowedServers, Tools, Skills,        │  │
│  │               ModelAliases, A2APeers, Scopes}              │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │  MCP Gateway │  │  A2A         │  │  LLM Gateway (/v1)   │   │
│  │  /mcp        │  │  /a2a        │  │  OpenAI-compatible   │   │
│  │  northbound  │  │  northbound  │  │  + semantic cache    │   │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬───────────┘   │
│         │                 │                      │               │
│  ┌──────▼─────────────────▼──────────────────────▼────────────┐  │
│  │  Governance envelope (applied to every surface)             │  │
│  │  Policy engine → approval flow → credential injection       │  │
│  │  Budget pre-check + post-call reconcile                     │  │
│  │  Audit emit (redacted) + OTel span                          │  │
│  └──────┬──────────────────────────────────────────────────────┘  │
│         │                                                        │
│  ┌──────▼──────────────────────────────────────────────────┐   │
│  │  Southbound dispatchers                                  │   │
│  │  MCP southbound (stdio / HTTP)  |  A2A southbound (HTTP) │   │
│  │  LLM engine (pure-Go, pluggable driver)                  │   │
│  └──────┬──────────────────────────────────────────────────┘   │
│         │                                                        │
│  ┌──────▼──────────────────────────────────────────────────┐   │
│  │  SQLite (modernc.org/sqlite, CGo-free)                   │   │
│  │  All tenant-scoped tables · WAL mode · forward migrations │   │
│  └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
         │          │          │
         ▼          ▼          ▼
   stdio MCP    HTTP MCP    A2A peer
   servers      servers     agents
```

---

## The governance envelope

Every request — `tools/call` on the MCP surface, `message/send` on the A2A surface, `POST /v1/chat/completions` on the LLM surface — traverses the same sequence of gates before anything reaches a downstream server or inference provider.

### 1. Authentication and tenant resolution

The first gate reads the `Authorization` header. Two credential forms are accepted:

- **Bearer JWT** — validated against configured JWKS or a static public key. Allowed algorithms: RS256, RS384, RS512, ES256, ES384, ES512. The `tenant` claim resolves to a tenant record; `sub` identifies the user; `scope` carries permissions; `exp` is enforced. HS\* and `none` algorithms are rejected unconditionally.
- **Virtual Key** (`pk-portico-<id>.<secret>`) — looked up by id, verified by constant-time HMAC-SHA256. Only `salt` + `HMAC(salt, secret)` are stored; the secret is never persisted and is returned once at creation. A Virtual Key resolves to its tenant, scope set, and model/server allowlists.

In dev mode (`portico dev`), the auth gate synthesises a `dev` tenant and skips JWT validation. No production surface is weakened — dev mode requires binding to `127.0.0.1`.

After validation, tenant identity is written into `context.Context` via the tenant package. Every internal package that touches tenant-scoped data reads it with `tenant.MustFrom(ctx)`; it never travels through global state.

### 2. Agent Profile resolution

After authentication, the Agent Profile resolver maps the principal (JWT `sub` or Virtual Key id) to a `Profile` value. A Profile is the single source of truth for consumer entitlement: which MCP servers, tools, Skill Packs, LLM model aliases, and A2A peers this logical agent may reach.

The resolved `Profile` is written into `context.Context`. Every gating surface — `tools/list`, `tools/call`, the prompt and resource surfaces, the LLM handler, the A2A dispatcher — reads it from context and calls the `Profile.Allows*` decision methods. There is no parallel allowlist on any surface; all entitlement decisions flow through the Profile.

A principal with no Profile bound resolves to a synthesised **default Profile** that allows the tenant's full surface. A deployment that configures no Profiles behaves exactly as if Agent Profiles did not exist. The default Profile is a code construct, never a stored row, and is never returned from the profile list API.

### 3. Policy engine and approval flow

The policy engine evaluates the request against the tenant's rule set (tool allowlists, denylists, risk classifications, Skill Pack binding policy). Tools flagged `requires_approval` are intercepted here: Portico emits an `elicitation/create` request if the host declared elicitation support during `initialize`, or returns a structured `approval_required` JSON-RPC error (code `-32001`) otherwise. The pending approval is persisted in SQLite and visible in the Console.

Approvals are never bypassed. The policy engine's decision — `allowed`, `denied`, or `approval_required` — is audited on every request.

### 4. Budget pre-check and reconcile

For LLM calls and any request that carries token or cost cost accounting, the budget layer performs a hierarchical pre-check across Virtual Key → Team → Customer → Tenant levels. The lowest level that would be exceeded fires with a `429 budget_exceeded` response before the call is dispatched. Post-call, the actual usage is debited atomically across all applicable levels in a single transaction.

### 5. Credential injection

Downstream MCP servers and inference providers expect credentials. Portico's vault is keyed by `(tenant, name)` — cross-tenant reads are impossible by API construction. The credential injector resolves the appropriate credential for the `(tenant, server, user)` tuple and injects it via the appropriate strategy: HTTP Authorization header injection, environment variable injection for stdio servers, or OAuth token exchange. The agent never sees the upstream credential.

### 6. Audit and telemetry

Every request emits a structured audit event before returning to the caller. Arguments and results pass through the audit redactor before persistence — raw tool arguments and results are never logged. OTel spans, created via `internal/telemetry`, carry `tenant_id`, `request_id`, `trace_id`, `session_id`, `server_id`, and `tool` attributes and are exported over OTLP.

---

## Northbound vs southbound

**Northbound** is the surface Portico presents to AI clients:

- `internal/mcp/northbound/http/` — Streamable HTTP transport for MCP, targeting protocol revision `2025-11-25`. Handles `initialize`, `tools/list`, `tools/call`, `resources/*`, `prompts/*`, `notifications/*`, server-initiated requests (elicitation, list-changed), and ping. The Origin guard enforces the 403 requirement from the `2025-11-25` spec revision.
- `internal/a2a/northbound/http/` — JSON-RPC 2.0 over HTTP for inbound A2A calls, mounted at `/a2a` inside the auth group. Tenant identity and the resolved Agent Profile are always in context — A2A never bypasses the governance envelope.
- `internal/llm/northbound/` — The OpenAI-compatible `/v1/chat/completions` surface (plus supporting endpoints). Requests are resolved through the model alias registry before being dispatched to the engine.

**Southbound** is the surface Portico uses to talk to downstream resources:

- `internal/mcp/southbound/stdio/` — manages stdio child processes via the process supervisor. Responsible for spawn, restart, health checks, idle timeout, log capture, and graceful shutdown.
- `internal/mcp/southbound/http/` — HTTP+SSE client for remote MCP servers. Shares the same `southbound.Client` interface as the stdio client.
- `internal/a2a/southbound/` — HTTP client for outbound A2A calls to registered peer agents.
- `internal/llm/engine/` — the LLM engine dispatch layer, described below.

The southbound client interface is defined once. Adding a new MCP transport (such as WebSocket) extends it in one place; no callers need to change.

---

## The extensibility-seam pattern

Portico enforces a consistent pattern for every subsystem with plausible alternate backends. The canonical implementation is the storage layer, which is described in the next section, but the same shape applies to the LLM engine, the semantic cache, skill sources, and credential vaults.

The pattern has four parts:

1. **Interface** — defined in `internal/<area>/ifaces/` (or `internal/<area>/<area>.go` when that is the more natural home). Callers depend only on the interface; they never import a concrete driver.
2. **Concrete driver** — lives in `internal/<area>/<driver>/`. Each driver subdirectory is self-contained and self-registers.
3. **Factory + registry** — at `internal/<area>/<area>.go`. Exposes a `Register(name, factory)` function that drivers call from `init()`, and an `Open(ctx, cfg, log)` function that dispatches by driver name. The error message from `Open` on an unknown driver lists all registered names.
4. **Blank import at the binary entry point** — `cmd/portico` blank-imports each driver to trigger its `init()`. Nothing outside `cmd/portico` (or that driver's own tests) imports a concrete driver package.

This means adding a new storage backend, a new cache driver, or a new LLM engine requires writing one new driver package and adding one blank import. No existing code changes.

::: tip
The pattern is enforced by project policy. Importing a concrete driver package from anywhere other than `cmd/portico` or that driver's own test file is a rejection-on-sight reason in code review.
:::

---

## Storage

Portico stores all persistent state — tenants, server registrations, sessions, catalog snapshots, audit events, approvals, Skill Pack enablement, Agent Profiles, Virtual Keys, budgets, LLM cost records, semantic cache entries, A2A peer records, Code Mode continuations — in a single SQLite database.

The SQLite driver is `modernc.org/sqlite`, a pure-Go implementation that requires no CGo and produces a fully static binary with the rest of the code.

### The Backend interface

`internal/storage/ifaces/backend.go` defines the `Backend` interface. Concrete drivers implement it; all production code calls it. `ifaces.ErrNotFound` is the canonical sentinel for absent rows; drivers wrap it with `%w` so callers can use `errors.Is` without importing the driver.

```go
// Abbreviated; full definition in internal/storage/ifaces/backend.go
type Backend interface {
    Tenants()        TenantStore
    Audit()          AuditStore
    Registry()       RegistryStore
    Skills()         SkillEnablementStore
    Approvals()      ApprovalStore
    Snapshots()      SnapshotStore
    AgentProfiles()  AgentProfileStore
    CodeMode()       CodeModeStore
    // ... additional stores added per phase
    Health(ctx context.Context) error
    Driver() string
    Close() error
}
```

### The factory

`internal/storage/storage.go` is the factory. The SQLite driver registers itself from `init()`:

```go
// internal/storage/sqlite/sqlite.go (abridged)
const DriverName = "sqlite"

func init() {
    storage.Register(DriverName, factory)
}
```

Callers open a backend without naming the driver package:

```go
backend, err := storage.Open(ctx, cfg.Storage, log)
```

`cfg.Storage.Driver` selects the driver (currently `"sqlite"`); `cfg.Storage.DSN` is the connection string (e.g. `"file:./portico.db?cache=shared"`).

### Schema and migrations

SQL migrations live in `internal/storage/sqlite/migrations/` and are numbered monotonically. The journal mode is WAL, set in the initial migration. Migrations are forward-only in V1: if a column or table needs to change, a new migration appends the change. Existing migrations are never edited after merge.

All queries are parameterised. Every tenant-scoped table carries a `tenant_id NOT NULL` column, and every query that reads from such a table includes `WHERE tenant_id = ?`. Cross-tenant reads are impossible by construction.

---

## LLM engine seam

The LLM gateway uses the same extensibility-seam pattern as storage. `internal/llm/engine/ifaces/` defines the `Engine` interface; a pure-Go, Apache-2.0 inference engine driver is registered at `internal/llm/engine/` and blank-imported in `cmd/portico`. Swapping or adding an engine driver (an alternative provider adapter, a local inference runtime) is a matter of writing one new package that implements the `Engine` interface and adding a blank import.

The semantic cache follows the same seam: `internal/llm/cache/ifaces/` defines the interface; `inmem`, `redis`, and `none` drivers ship out of the box; embedding-similarity drivers (Weaviate, Qdrant) are config-only additions over the same seam.

---

## Tenant isolation guarantees

Multi-tenancy is foundational, not layered on top. The guarantees that hold across the entire codebase:

- Tenant identity is always in `context.Context`. It is never stored in package-level state.
- Every tenant-scoped storage table carries `tenant_id NOT NULL`. Every read includes a tenant filter. Cross-tenant queries require an explicit `admin` scope on the JWT.
- Process supervision uses tenant ID as a keying dimension. In `per_tenant` or `per_user` runtime modes, two tenants never share a stdio process.
- The credential vault is keyed by `(tenant, name)`. The API makes cross-tenant reads impossible.
- Audit events carry `tenant_id` and are queryable per tenant by default.
- An integration test asserts that cross-tenant data access attempts fail.

See [Multi-tenancy](/concepts/multi-tenancy) for the full policy.

---

## Request lifecycle: end to end

A `tools/call` from a Claude Desktop session illustrates the full path:

```
Claude Desktop
  │  POST /mcp  (JSON-RPC, Streamable HTTP)
  ▼
northbound HTTP transport
  │  reads Authorization header
  ▼
auth boundary
  │  JWT validated → tenant_id="acme", sub="alice"
  │  tenant written into context.Context
  ▼
Agent Profile resolver
  │  looks up profile bound to "alice" in acme tenant
  │  profile written into context (AllowedServers=["github"], ...)
  ▼
MCP dispatcher (internal/server/mcpgw)
  │  routes tools/call
  ▼
policy pipeline
  │  tool "github.create_review_comment" → risk_class=external_side_effect
  │  requires_approval=true → emits elicitation/create to host
  │  records approval in SQLite
  │  returns approval_required error if host does not support elicitation
  ▼
(after operator approves)
credential injector
  │  resolves (acme, github, alice) → injects GitHub token as env var
  ▼
southbound stdio dispatcher
  │  routes to the per-user github MCP process
  │  dispatches tools/call on the downstream connection
  ▼
github MCP server
  │  executes the call, returns result
  ▼
audit + telemetry
  │  emits tool_call.completed event (redacted)
  │  closes OTel span
  ▼
northbound transport
  │  sends JSON-RPC response to Claude Desktop
```

---

## Related

- [Multi-tenancy](/concepts/multi-tenancy) — tenant isolation guarantees and the storage keying model
- [MCP Gateway](/concepts/mcp-gateway) — northbound and southbound MCP surfaces in detail
- [LLM Gateway](/concepts/llm-gateway) — the OpenAI-compatible `/v1` surface and provider model
- [A2A](/concepts/a2a) — the A2A peer protocol and how it plugs into the governance envelope
- [Agent Profiles](/concepts/agent-profiles) — entitlement binding and the `Allows*` decision methods
- [Security Model](/concepts/security-model) — risk classes, approval flow, credential handling, and isolation controls
- [Observability](/concepts/observability) — audit events, OTel traces, and structured logging
- [Configuration Reference](/reference/configuration) — `server.bind`, `storage.driver`, `storage.dsn`, and all other top-level keys
