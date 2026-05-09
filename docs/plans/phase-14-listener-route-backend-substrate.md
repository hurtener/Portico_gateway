# Phase 14 — Listener / Route / Backend Substrate (V2 foundation)

> Self-contained implementation plan. Builds on Phase 0–13. **Foundational refactor.** Introduces explicit `Bind / Listener / Route / Backend` abstractions that every subsequent V2 phase plugs into. **Zero new tenant-visible features.** Every Phase 1–13 acceptance criterion must still pass against the V2 binary at the end of this phase.

## Goal

Refactor Portico's boot and request-dispatch path so that the existing fixed surfaces (MCP at `/mcp`, LLM at `/v1`, REST control plane at `/api`, Console SPA at `/`) sit on a real data-plane substrate of `Bind → Listener → Route → Backend`. After Phase 14:

- Multiple binds (e.g. `127.0.0.1:8080` for plaintext + `0.0.0.0:8443` for TLS) are configurable.
- Multiple listeners can be attached to a bind, each with a protocol decoder.
- Routes inside a listener match on path/method/header/host (and protocol-specific predicates such as JSON-RPC `method`).
- Backends are pluggable drivers behind a single interface, registered via the §4.4 seam pattern (interface in `internal/dataplane/backends/ifaces`, drivers in `internal/dataplane/backends/<driver>`, blank-imported from `cmd/portico`).

The phase ships **no new protocols and no new tenant-visible CRUD**. It is a substrate refactor that makes Phases 15–19 land cheaply.

## Why this phase exists

The V2 roadmap (`docs/plans/v2-roadmap-agentgateway-parity.md`) commits Portico to grow from "MCP + Skills gateway" into a multi-protocol agentic gateway peer to `agentgateway`. Every subsequent V2 phase needs a place to attach a new listener type or a new backend type. Today there is none:

- `cmd/portico/cmd_serve.go::runWithConfig` opens **one** `http.Server` at `cfg.Server.Bind`, hands it **one** `chi.Router` from `internal/server/api/router.go`, and that router hard-codes mounts for the MCP handler, the REST API, the Console SPA, the playground, and the LLM gateway (Phase 13).
- Adding HTTP-proxy routes (Phase 15) without a substrate means another conditional branch in `router.go`, another set of middleware threads, another set of mount points. By Phase 17 the router is unreviewable.
- Adding the A2A wire protocol (Phase 16) without a substrate means a second top-level handler that has to re-implement tenant resolution, JWT validation, policy, audit, telemetry, and approval handoff — all already wrapped around the MCP handler.
- Adding a dynamic configuration API (Phase 18) without a substrate has nothing to write *to*: there is no in-memory data-plane state object that represents the gateway's routing table.

Phase 14 builds that state object, the interfaces around it, and the boot path that hydrates it from `portico.yaml` (and later, from Phase 18's API). Nothing else.

This phase deliberately *does not* introduce new operator-facing concepts. The Console gains a single read-only surface that *describes* the dataplane state (so operators can confirm the refactor produced what their YAML asked for), and that is the entire UX surface.

## Prerequisites

Phases 0–13 complete. Specifically:

- `internal/server/api/router.go` is the current single mount point for everything REST/MCP. Phase 14 splits that into per-listener handler trees.
- `internal/mcp/northbound/http/transport.go` owns the MCP wire decode/encode and the SSE correlator. Phase 14 wraps it in a `mcp` listener implementation; the transport itself does not move.
- `internal/llm/gateway/handler.go` (Phase 13) owns the OpenAI-compatible surface. Phase 14 wraps it in an `openai` listener implementation; the handler itself does not move.
- The `internal/storage` factory pattern (the reference §4.4 implementation) is the template for `internal/dataplane/backends`.
- The §4.5 frontend conventions and the §4.5.1 operator UX gates apply to the read-only Console addition.

## Out of scope (explicit)

- **No new wire protocols.** No HTTP-proxy logic, no gRPC, no A2A, no WebSocket MCP. Those are Phases 15–16.
- **No new backend behaviours.** The MCP backend driver wraps the existing dispatcher; the LLM backend driver wraps the existing handler. Code is moved, not added.
- **No new auth modes.** JWT and dev-mode stay as-is. Per-route auth-strategy injection (so a route to a downstream service can attach an OAuth bearer at egress) is Phase 15.
- **No dynamic configuration API.** `portico.yaml` (with the new optional `listeners:` block) is the only configuration source in this phase. Phase 18 adds the API.
- **No tenant-visible CRUD over listeners/routes/backends.** Console screens are read-only. Operator-facing CRUD lands in Phase 18.
- **No request transformation, retries, circuit-breaking, or load-balancing.** These are upstream concerns owned by Phase 15 (HTTP proxy) and Phase 16 (A2A peering). The MCP and LLM backend drivers in Phase 14 keep their existing single-upstream behaviour.
- **No observability shape changes.** Existing `tenant_id`, `request_id`, `trace_id`, `session_id`, `server_id`, `tool` attributes still appear on every span and audit event; new fields (`listener_id`, `route_id`, `backend_id`) are added but no field is removed or renamed.
- **No microbenchmark regressions.** The substrate must not add measurable per-request overhead at the existing surfaces; the refactor's correctness gate is "Phase 13 microbenchmark numbers within ±5%."

## Deliverables

1. **`internal/dataplane/binds`** — typed `Bind` struct (`host`, `port`, `tls`), with TLS material loaded once at boot (cert/key files, optional client-CA bundle, ALPN list).
2. **`internal/dataplane/listeners`** — `Listener` interface plus an in-memory registry. A listener owns its protocol decoder and its route table. Built-in listener kinds in this phase: `mcp`, `openai`, `rest_api`, `spa`. Each kind has its own implementation under `internal/dataplane/listeners/<kind>/`.
3. **`internal/dataplane/routes`** — `Route` struct (match predicates, middleware chain reference, backend reference) plus a `RouteTable` per listener. `Match` shape includes `path_prefix`, `path_regex`, `method`, `header_eq`, `header_present`, `host`, and (for JSON-RPC listeners) `jsonrpc_method`.
4. **`internal/dataplane/backends/ifaces`** — `Backend` interface defining `Dispatch(ctx, *Request) (*Response, error)` with neutral `Request`/`Response` types that carry the tenant envelope. Plus a `Driver` interface for self-registration (`Name() string`, `New(cfg map[string]any) (Backend, error)`).
5. **Backend drivers (this phase only):**
   - `internal/dataplane/backends/mcp` — wraps `mcpgw.Server` and the existing aggregator. Routes that target this backend dispatch through the existing MCP namespacing and snapshot logic.
   - `internal/dataplane/backends/llm` — wraps the Phase 13 LLM handler.
   - `internal/dataplane/backends/control_plane` — wraps the `internal/server/api` REST router.
   - `internal/dataplane/backends/spa` — wraps the embedded SvelteKit SPA handler.
6. **Backend factory + registry** — `internal/dataplane/backends/backends.go` implements `Open(name string, cfg map[string]any) (Backend, error)` using the registered drivers; the error message lists registered drivers (per §4.4).
7. **`internal/dataplane/server.go`** — the new top-level `DataPlane` type. Owns binds, listeners, route tables, backends, and the lifecycle (`Start(ctx) error`, `Shutdown(ctx) error`). Replaces the ad-hoc `http.Server` boot in `cmd_serve.go`.
8. **Middleware chain** — `internal/dataplane/middleware/` houses the protocol-agnostic stages: tenant resolution, JWT, policy precheck, audit-open, tracing-open, audit-close, tracing-close. Each stage is a `func(http.Handler) http.Handler`. The chain is composed once per route and applies to every request flowing through.
9. **Configuration extension** — `config.Config` gains `Listeners []ListenerConfig` (optional). When absent, the loader synthesises a default listeners block that reproduces today's behaviour exactly (see §6).
10. **Read-only Console screens** — `/dataplane` (list of binds + listeners), `/dataplane/listeners/[name]` (route table view), `/dataplane/backends` (registered drivers + instantiated backends). All read-only. No `+ Add` CTA in this phase. Surfaces what the YAML produced.
11. **Read-only REST surface** — `GET /api/dataplane/binds`, `GET /api/dataplane/listeners`, `GET /api/dataplane/listeners/{name}`, `GET /api/dataplane/backends`. Returns the materialised state. Used by the Console screens above and by Phase 18's API as the read side.
12. **Smoke** — `scripts/smoke/phase-14.sh` covers default-config behaviour parity (every Phase 1–13 endpoint still answers identically), the new read-only `/api/dataplane/*` endpoints, and a multi-bind config that proves a listener can be brought up on a second port.
13. **Microbenchmark gate** — `internal/dataplane/bench_test.go` measures the per-request overhead of the new middleware chain at the MCP and LLM surfaces. CI compares against the Phase 13 baseline; >5% regression fails the build.

## Acceptance criteria

1. **Behavioural parity.** `make preflight` against a Phase 14 build with the synthesised default config passes every Phase 0–13 smoke check, with `OK` counts identical to the Phase 13 baseline. Zero `FAIL`.
2. **Multi-bind.** A `portico.yaml` with two binds (e.g. plaintext `:8080` and TLS `:8443`) brings up both listeners. A smoke check exercises the same MCP `tools/list` over both and asserts identical responses.
3. **Multi-listener-on-bind.** A single bind hosts `mcp`, `rest_api`, and `spa` listeners simultaneously. Routes resolve to the right listener by `path_prefix` (or by host when present). A smoke check fetches `/mcp`, `/api/healthz`, and `/` and asserts the right content type / shape.
4. **Backend driver registration.** The four backend drivers register at `init()` time. Removing the blank import from `cmd/portico` for any one of them is detected by a smoke that asserts the driver is missing in `GET /api/dataplane/backends`. (This is the parity check for the §4.4 seam.)
5. **Refactored `cmd_serve.go`.** The boot path no longer instantiates `http.Server` directly; it constructs a `dataplane.DataPlane` and calls `Start(ctx)`. The cyclomatic-complexity nolint waiver on `runWithConfig` can be dropped or its reason updated.
6. **Middleware chain identity.** Every span emitted by an MCP request through the new substrate carries the same attributes (`tenant_id`, `request_id`, `trace_id`, `session_id`, `server_id`, `tool`) as Phase 13 did, plus `listener_id`, `route_id`, `backend_id`. A test fixture compares span attributes pre/post refactor.
7. **Audit envelope identity.** Every audit event has the same shape; `listener_id`, `route_id`, `backend_id` are added as optional fields. A migration adds the columns; existing queries continue to work without those columns.
8. **Approval flow unchanged.** Phase 5 elicitation + structured-error fallback works identically. The MCP backend driver delegates to the existing `policy/approval` package; no approval logic moves into the substrate.
9. **Console parity.** Every existing operator screen (servers, skills, tenants, secrets, snapshots, playground, audit, approvals) renders and behaves identically. The new `/dataplane` screens are added; nothing else is moved or styled differently.
10. **Test plan executed.** All test names in §11 exist; `go test -race ./...` passes on Linux + macOS; coverage on `internal/dataplane/...` ≥ 80%.
11. **Microbenchmark gate.** `go test -bench=. -benchmem ./internal/dataplane/...` shows median per-request overhead ≤ 5% vs. the Phase 13 baseline saved as `bench/phase-13-baseline.txt`.
12. **Smoke gate.** `scripts/smoke/phase-14.sh` shows OK ≥ 14, FAIL = 0; prior phases' smokes show no regression.

## Architecture

### 7.1 Package layout

```
internal/dataplane/
├── binds/
│   ├── bind.go               # Bind struct, TLS loading
│   └── bind_test.go
├── listeners/
│   ├── ifaces/
│   │   └── listener.go       # Listener interface
│   ├── registry.go           # in-memory listener registry
│   ├── mcp/                  # Listener kind: MCP HTTP+SSE
│   │   ├── listener.go
│   │   └── listener_test.go
│   ├── openai/               # Listener kind: OpenAI-compatible HTTP
│   ├── rest/                 # Listener kind: control-plane REST API
│   └── spa/                  # Listener kind: embedded SvelteKit SPA
├── routes/
│   ├── route.go              # Route struct
│   ├── match.go              # Match predicates + matcher
│   ├── table.go              # RouteTable per listener
│   └── match_test.go
├── backends/
│   ├── ifaces/
│   │   └── backend.go        # Backend, Driver interfaces
│   ├── backends.go           # Open(name, cfg) factory + registry
│   ├── mcp/
│   │   └── backend.go
│   ├── llm/
│   │   └── backend.go
│   ├── control_plane/
│   │   └── backend.go
│   └── spa/
│       └── backend.go
├── middleware/
│   ├── tenant.go
│   ├── jwt.go
│   ├── policy.go
│   ├── audit.go
│   ├── tracing.go
│   └── chain.go              # composes the standard chain
├── server.go                 # DataPlane type, Start/Shutdown
├── config.go                 # ListenerConfig / RouteConfig / BackendConfig
└── bench_test.go             # per-request overhead microbench
```

### 7.2 Core interfaces

```go
// internal/dataplane/listeners/ifaces/listener.go
package ifaces

type Listener interface {
    Name() string
    Bind() Bind            // the bind this listener is attached to
    Protocol() string      // "mcp" | "openai" | "rest_api" | "spa" | future
    Handler() http.Handler // ready for net/http; the dataplane wraps it in middleware
    Routes() []Route       // for read-only inspection / Console
}
```

```go
// internal/dataplane/backends/ifaces/backend.go
package ifaces

// Request carries the tenant envelope and the protocol-specific payload.
// Backends never reach into the http.Request directly; they get the
// neutral shape so the same backend can serve MCP, HTTP, gRPC, A2A.
type Request struct {
    TenantID  string
    UserID    string
    Scopes    []string
    SessionID string
    TraceID   string
    Method    string            // protocol-specific verb (HTTP method, JSON-RPC method, etc.)
    Path      string            // the post-routing path
    Headers   map[string]string
    Body      io.ReadCloser
    Meta      map[string]any    // protocol-specific extras (e.g. JSON-RPC id, MCP _meta)
}

type Response struct {
    StatusCode int
    Headers    map[string]string
    Body       io.ReadCloser
    Trailers   map[string]string
    Meta       map[string]any
}

type Backend interface {
    Name() string
    Dispatch(ctx context.Context, req *Request) (*Response, error)
    Close(ctx context.Context) error
}

type Driver interface {
    Name() string
    New(cfg map[string]any, deps Deps) (Backend, error)
}

// Deps carries the cross-cutting services every driver may need
// (logger, tracer, vault, audit emitter, registry, etc.).
type Deps struct {
    Logger   *slog.Logger
    Tracer   trace.Tracer
    Audit    audit.Emitter
    Vault    secrets.Vault
    Registry registry.Store
    // …
}

// Register is called from a driver's init() to make it discoverable.
func Register(d Driver) { /* ... */ }
```

### 7.3 Request lifecycle

A request that lands on a bind:

1. `binds.Bind` receives the connection (TLS-terminated if configured).
2. The bind's `http.ServeMux` selects the listener by `Host` header (or by SNI if multi-listener-by-SNI is enabled — Phase 18 work; Phase 14 dispatches by path prefix only on a single host).
3. The listener decodes the protocol envelope (HTTP request, MCP JSON-RPC, OpenAI HTTP, etc.). For protocols that require body inspection to route (MCP `tools/call` namespacing), the listener parses just enough.
4. The listener's `RouteTable` matches the request against routes; the first match wins. Match order is: explicit `host` → `path_regex` → `path_prefix` (longest first) → method/header predicates as tiebreakers.
5. The matched route's middleware chain is invoked. The chain is the §5.2 envelope from the V2 roadmap. Each stage may short-circuit (e.g. `audit-open` always runs; `policy.precheck` may emit `approval_required` and stop).
6. After the chain, the route's `Backend.Dispatch(ctx, req)` is called.
7. The response flows back through the chain in reverse for `audit-close` and `tracing-close`.

### 7.4 Backend wrapping (this phase)

The four backend drivers in this phase **delegate to existing handlers**. They do not duplicate logic:

- **`mcp` backend** — embeds an `*mcpgw.Server`. `Dispatch` re-encodes the neutral `Request` as a JSON-RPC message and invokes the existing dispatcher path. Returns the JSON-RPC response (or SSE stream proxy) as the neutral `Response`. The aggregator, snapshot, namespacing, and tool-virtualisation logic stays in `internal/server/mcpgw` and the related packages.
- **`llm` backend** — embeds an `*llm.Handler` (Phase 13). `Dispatch` proxies to the existing OpenAI-compatible handler chain.
- **`control_plane` backend** — embeds the `internal/server/api` `chi.Router`. `Dispatch` is a passthrough call into the router. (For this phase, the REST API `chi.Router` is *the* implementation of the control plane; Phase 18 may split it.)
- **`spa` backend** — embeds the SPA handler from `internal/server/ui`. `Dispatch` serves static files via the existing `embed.FS`.

### 7.5 Configuration synthesis (backward compatibility)

When a `portico.yaml` has no `listeners:` block, the loader synthesises:

```yaml
listeners:
  - name: default-mcp
    bind: { name: default, host: 127.0.0.1, port: 8080 }
    protocol: mcp
    routes:
      - { match: { path_prefix: /mcp }, backend: mcp-aggregator }

  - name: default-llm                                # only if Phase 13 enabled
    bind: { name: default, host: 127.0.0.1, port: 8080 }
    protocol: openai
    routes:
      - { match: { path_prefix: /v1 }, backend: llm-gateway }

  - name: default-rest
    bind: { name: default, host: 127.0.0.1, port: 8080 }
    protocol: rest_api
    routes:
      - { match: { path_prefix: /api }, backend: control-plane }

  - name: default-spa
    bind: { name: default, host: 127.0.0.1, port: 8080 }
    protocol: spa
    routes:
      - { match: { path_prefix: / }, backend: console-spa }
```

Synthesis happens in `internal/config/loader.go::applyListenerDefaults`. The synthesised state is observable via `GET /api/dataplane/listeners` so operators can confirm what they got. Once the operator declares a `listeners:` block, no synthesis happens — the operator owns the routing table.

`cfg.Server.Bind` (Phase 0 field) is still accepted; if no `binds:` block exists, the loader synthesises a single default bind from `cfg.Server.Bind`.

### 7.6 New configuration types

```yaml
binds:
  - name: default
    host: 127.0.0.1
    port: 8080
  - name: tls
    host: 0.0.0.0
    port: 8443
    tls:
      cert_file: /etc/portico/tls.crt
      key_file:  /etc/portico/tls.key
      client_ca: /etc/portico/clients.pem    # optional mTLS

listeners:
  - name: default-mcp
    bind: default
    protocol: mcp
    routes:
      - match: { path_prefix: /mcp }
        backend: mcp-aggregator
        middleware:                          # optional override; defaults to standard chain
          - tenant
          - jwt
          - policy
          - audit
          - tracing

backends:
  - name: mcp-aggregator
    driver: mcp
    config: {}                               # MCP backend takes no extra config in Phase 14
  - name: llm-gateway
    driver: llm
    config: {}
  - name: control-plane
    driver: control_plane
    config: {}
  - name: console-spa
    driver: spa
    config: {}
```

The shape is deliberately Envoy-adjacent: explicit names, references by name across the three sections, no nesting that would make the dynamic-config API of Phase 18 awkward.

## Configuration extensions

- `binds:` (new, optional) — list of named binds with optional TLS material.
- `listeners:` (new, optional) — list of named listeners. Each references a bind by name and declares a protocol.
- `backends:` (new, optional) — list of named backends. Each declares a driver and a driver-specific config map.
- `cfg.Server.Bind` (Phase 0, retained) — used to synthesise a default bind when `binds:` is absent.
- Validation: every `listener.bind` must reference a known bind name; every `route.backend` must reference a known backend name; backend-driver names must be registered. All errors point at a YAML path in the loader's error messages.

Hot reload: yes for `routes` and `backends` (additive change → new route table swapped atomically; removal → existing in-flight requests drained gracefully). No for `binds` (changing a bind requires a restart since it changes the listening socket).

## REST APIs

All read-only in Phase 14. JSON. Tenant-scoped where applicable; `admin` scope required where listed.

| Method | Path                                | Scope    | Returns                                                       |
|--------|-------------------------------------|----------|----------------------------------------------------------------|
| GET    | `/api/dataplane/binds`              | admin    | `[{name, host, port, tls: {...} | null, listeners: [name]}]`  |
| GET    | `/api/dataplane/listeners`          | admin    | `[{name, bind, protocol, routes: [...]}]`                     |
| GET    | `/api/dataplane/listeners/{name}`   | admin    | full route table for the listener                             |
| GET    | `/api/dataplane/backends`           | admin    | `[{name, driver, status: "ready"|"error", error?}]`           |
| GET    | `/api/dataplane/drivers`            | admin    | `[{name, kinds: ["backend"|"listener"]}]` — registered drivers |

Errors follow the standard `{"error":"<code>","message":"...","details":{}}` shape from `docs/plans/README.md` §"Errors on the wire".

## Implementation walkthrough

Order matters; each step lands as one commit.

### Step 1 — interfaces and registries (no behaviour change)

- Create `internal/dataplane/{binds,listeners,routes,backends,middleware}` directories with the package skeletons.
- Define `Listener`, `Backend`, `Driver` interfaces.
- Implement the in-memory `backends.Open(name, cfg)` factory + registry. The registry's error message lists registered drivers (per §4.4 reference).
- Add `cmd/portico/dataplane_wiring.go` containing only blank imports for the four backend drivers (drivers themselves do not exist yet — this commit just stubs the wiring file).

Test: package compiles; `backends.Open("nonexistent", nil)` returns "registered drivers: …" error.

### Step 2 — middleware chain extracted from existing handlers

- Move tenant resolution out of `internal/server/api/router.go` and `internal/mcp/northbound/http/transport.go` into `internal/dataplane/middleware/tenant.go`.
- Same for JWT, policy precheck, audit-open/close, tracing-open/close.
- Each middleware writes its outputs into `r.Context()` using the established context keys.
- The existing handlers continue to compile by re-importing the middleware from the new location.

Test: every existing test still passes; new unit tests for each middleware in isolation; chain composition test asserts ordering.

### Step 3 — backend drivers wrapping existing handlers

- `internal/dataplane/backends/mcp/backend.go` — embeds the existing `*mcpgw.Server` and exposes `Dispatch` that proxies to it. The driver self-registers from `init()`.
- Same shape for `llm`, `control_plane`, `spa`.
- `cmd/portico/dataplane_wiring.go` blank-imports all four.

Test: each driver has a unit test that constructs the backend with a fake dependency injection and asserts `Dispatch` returns the same response shape as the existing handler.

### Step 4 — listener kinds wrapping existing transports

- `internal/dataplane/listeners/mcp/listener.go` — wraps `internal/mcp/northbound/http`. Provides `Handler()` that returns an `http.Handler` which decodes MCP wire shape, applies the route table (Phase 14 default route table has one route → mcp backend), and dispatches.
- Same shape for `openai`, `rest_api`, `spa`.

Test: each listener has a unit test that boots the listener against an in-process bind, sends one request, asserts the right backend was invoked.

### Step 5 — `DataPlane` orchestrator

- `internal/dataplane/server.go::DataPlane` owns: binds, listeners, backends, middleware composition, lifecycle.
- `Start(ctx)` opens every bind, registers every listener's handler under the bind's mux (matched by host), and starts serving.
- `Shutdown(ctx)` calls `http.Server.Shutdown` for each bind in parallel with a deadline; then `Close` on every backend.

Test: `TestDataPlane_StartsAndShutsDownCleanly` boots a multi-bind config, exercises one request per listener, shuts down, asserts goroutine count returns to baseline (per `AGENTS.md` §11 goroutine-leak rule).

### Step 6 — `cmd/portico/cmd_serve.go` rewired

- Replace the `http.Server` instantiation in `runWithConfig` with `dataplane.New(cfg, deps).Start(ctx)`.
- The synthesised default config (no `listeners:` block in YAML) reproduces today's behaviour exactly.
- The cyclomatic-complexity nolint waiver is dropped or its reason updated to reflect the slimmer function.

Test: full E2E: boot the binary against `examples/portico.yaml`, run all phase-1..13 smokes, assert OK counts identical to the Phase 13 baseline.

### Step 7 — read-only REST surface

- `internal/server/api/handlers_dataplane.go` (new) — five endpoints from §"REST APIs" above.
- All require `admin` scope; cross-tenant by definition.

Test: `handlers_dataplane_test.go` covers happy path + RBAC denial.

### Step 8 — Console screens

- `web/console/src/routes/dataplane/+page.svelte` — list of binds, each with its listeners.
- `web/console/src/routes/dataplane/listeners/[name]/+page.svelte` — route table view, showing match → backend pairs.
- `web/console/src/routes/dataplane/backends/+page.svelte` — registered drivers + instantiated backends with status.
- All use existing primitives (`MetricStrip`, `KeyValueGrid`, `CodeBlock`, `Inspector`).
- Playwright spec: `web/console/tests/dataplane.spec.ts` — boots the page, asserts the default listeners are visible.

### Step 9 — smoke + microbenchmark gates

- `scripts/smoke/phase-14.sh`:
  - `assert_status 200 /api/dataplane/listeners` (admin token)
  - `assert_json_path '.[0].name' 'default-mcp'`
  - For every Phase 1–13 endpoint touched by the refactor, re-assert response shape parity (re-uses earlier phase smokes).
  - Multi-bind smoke uses a temp config with `binds: [default, tls-localhost]` and asserts both serve `/api/dataplane/binds`.
- `internal/dataplane/bench_test.go`:
  - `BenchmarkRoute_MCP_ToolsList` — neutral request through the chain to the `mcp` backend.
  - `BenchmarkRoute_LLM_ChatCompletion_NonStreaming` — same for LLM.
  - CI compares against `bench/phase-13-baseline.txt`; >5% regression fails.

## Test plan

Unit (next to source):

- `TestBind_LoadsTLSMaterial`
- `TestBind_RejectsBadTLS`
- `TestRoute_MatchPathPrefix`
- `TestRoute_MatchPathRegex`
- `TestRoute_MatchHeaderEq`
- `TestRoute_MatchHostHeader`
- `TestRoute_MatchOrdering_HostBeatsPath` — explicit host wins over a path match
- `TestRouteTable_FirstMatchWins`
- `TestBackends_RegisterAndOpen`
- `TestBackends_OpenUnknownLists` — error message contains all registered driver names
- `TestMiddleware_TenantFromJWT`
- `TestMiddleware_TenantFromDevMode`
- `TestMiddleware_PolicyShortCircuit_OnApprovalRequired`
- `TestMiddleware_AuditOpenAndClose_OnSuccessAndError`
- `TestMiddleware_TracingSpanLifecycle`
- `TestMiddleware_ChainComposition_Order`
- `TestListener_MCP_HandlesToolsList`
- `TestListener_OpenAI_HandlesChatCompletions`
- `TestListener_REST_HandlesHealthz`
- `TestListener_SPA_ServesIndex`
- `TestBackend_MCP_DispatchProxiesToAggregator`
- `TestBackend_LLM_DispatchProxiesToHandler`
- `TestBackend_ControlPlane_DispatchProxiesToRouter`
- `TestBackend_SPA_DispatchServesEmbeddedFile`

Integration (`test/integration/`):

- `TestE2E_DataPlane_DefaultConfig_BehaviourParity` — boots binary with no `listeners:` block, runs every Phase 1–13 smoke, asserts identical outputs.
- `TestE2E_DataPlane_MultiBind_BothListen` — two binds, both serve.
- `TestE2E_DataPlane_HotReload_AddsRoute` — append a route, send SIGHUP (or hot-reload trigger), new route resolves; old in-flight requests complete.
- `TestE2E_DataPlane_HotReload_RemovesBackend_DrainsInflight` — remove a backend, in-flight request completes, subsequent request hits 404.
- `TestE2E_DataPlane_ShutdownCleanup_NoGoroutineLeaks`
- `TestE2E_DataPlane_AdminScopeRequired_ForReadOnlyEndpoints`

Microbench (`internal/dataplane/bench_test.go`):

- `BenchmarkRoute_MCP_ToolsList`
- `BenchmarkRoute_MCP_ToolsCall`
- `BenchmarkRoute_LLM_ChatCompletion_NonStreaming`
- `BenchmarkRoute_RestAPI_HealthZ`
- `BenchmarkMiddlewareChain_FullPath`

Coverage targets:

- `internal/dataplane/...` ≥ 80% (per package).
- `internal/dataplane/backends/...` ≥ 85% (these are the seam — heavily tested).
- No regression on existing packages.

## Common pitfalls

1. **Importing concrete drivers from production code.** §13 forbidden practice. The dataplane talks only to `backends/ifaces`. The factory dispatches.
2. **Putting protocol logic in middleware.** Middleware is protocol-agnostic. If a middleware needs to know the JSON-RPC method of an MCP request, that's a sign the listener should expose the parsed envelope as `Request.Meta` and the middleware reads it from there.
3. **Conflating Listener and Route.** A listener owns the wire and the route table; a route owns one match → backend mapping. Don't put route-matching code into the listener kind packages.
4. **Re-implementing tenant/JWT/policy in a backend driver.** Backends receive `Request` with the tenant envelope already populated. Touching the raw `http.Request` from a backend is a code smell.
5. **Forgetting to wire blank imports in `cmd/portico/dataplane_wiring.go`.** Without the blank import the driver's `init()` does not run; the factory will not know about it. The error message helps but only if the operator reads it.
6. **Synthesised-config drift.** The synthesised default listeners block must reproduce today's behaviour byte-for-byte (modulo the new optional fields). A regression here breaks every Phase 1–13 acceptance criterion. The smoke gate exists exactly for this.
7. **Goroutine leaks in `Shutdown`.** Each backend's `Close` and each listener's `Shutdown` must respect the deadline in the passed context. The leak test in §11 catches forgotten ones.
8. **Audit / span attribute renames.** Adding `listener_id`, `route_id`, `backend_id` is fine. Renaming or removing existing attributes breaks downstream tooling. The attribute-identity test in acceptance §6 prevents this.
9. **Hot reload that swaps a backend mid-request.** Drain inflight first; only then swap. The `TestE2E_DataPlane_HotReload_RemovesBackend_DrainsInflight` test catches the race.
10. **Trying to add `+ Add` CTAs in Phase 14.** The Console additions are read-only. Operator-facing CRUD over routes/backends is Phase 18; pulling it forward defeats the §4.5.1 review gate.

## Hand-off to Phase 15

Phase 15 inherits:

- `internal/dataplane/backends/ifaces.Backend` — adds `http_proxy` and `grpc_proxy` drivers under `internal/dataplane/backends/http_proxy/` and `internal/dataplane/backends/grpc_proxy/`.
- `internal/dataplane/listeners/rest/` — Phase 14 hosts the control-plane REST API here; Phase 15 may add a separate `http` listener kind for arbitrary HTTP termination, or reuse `rest` and route by `match.host`.
- `internal/dataplane/middleware/` — Phase 15 adds `auth_egress` (per-route auth-strategy injection at the upstream side) and `transform` (header/path/body transformation).
- The configuration shape (`binds`, `listeners`, `backends`) is stable; Phase 15 only adds new `backend.driver` values.
- The Console `/dataplane/*` screens are read-only; Phase 15 makes them aware of HTTP-proxy backends but stays read-only. Operator CRUD is Phase 18.

Phase 15's first job: implement `http_proxy` driver, integration-test it against `examples/servers/mock/cmd/mockhttp/` (a new mock alongside `mockmcp`), wire a route to it in a smoke, prove that an HTTP request to `/billing/*` flows through the full V2 envelope and lands on the mock.
