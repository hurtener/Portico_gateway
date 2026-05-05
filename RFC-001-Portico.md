# RFC-001 — Portico

**An MCP gateway and Skill runtime**

- **Status:** Draft v3
- **Owner:** Santi (@hurtener)
- **Date:** 2026-05-05
- **License:** open source (TBD)
- **Repo:** github.com/hurtener/Portico_gateway

---

## Abstract

Portico is a managed MCP gateway and Skill runtime. It lets AI clients connect to many MCP servers through one governed, multi-tenant, session-aware control plane — and packages those servers with **Skill Packs** that turn raw tools into reliable workflows.

The gateway is the substrate. The Skill Pack runtime is the moat: it binds the open Skills spec to specific MCP servers, tools, policies, entitlements, and UI resources, and exposes the result through native MCP primitives so any compliant client benefits.

---

## 0. Changelog from v2

- **Multi-tenant from V1, not later.** Tenant identity flows through every layer from Phase 0; per-tenant runtime mode ships in V1; storage is keyed by tenant everywhere. The original mono-tenant-with-hooks plan is dropped.
- **Tenant identification: Bearer JWT with `tenant` claim.** Dev mode (bind to `127.0.0.1`, or `portico dev`) short-circuits to a synthetic `dev` tenant.
- **Full MCP spec including MCP Apps (`ui://`).** Sampling, roots, elicitation, list-changed, cancellation, progress, `_meta`, resource templates — all in V1. MCP Apps brings CSP and resource sandboxing into V1 scope.
- **Approval flow: headless, host renders.** Portico uses MCP `elicitation/create` when the host supports it; falls back to a structured `approval_required` error otherwise. The Console surfaces pending approvals as a read-only inspector. Async approvals (Slack, ticketing) are post-V1.
- **Skill distribution: MCP-first virtual directory.** Skills exposed as `skill://` resources via `resources/list`/`resources/read`. Backing storage is pluggable; V1 ships `LocalDir`; `Git`/`OCI`/`HTTP` sources are post-V1.
- **Process isolation: plain subprocesses for V1**, with the supervisor abstracting `Spawn(spec)` so a sandboxed implementation drops in later. Linux builds optionally apply seccomp + landlock when present.
- **V1 boundary explicit.** V1 = end of Phase 6 (catalog snapshots + observability). Post-V1 = Production Readiness (Postgres-default, K8s, Redis multi-instance, sidecar runtime, quotas).
- **Open question list shrunk** to items that genuinely remain unresolved.

---

## 1. Naming

**Portico.** An architectural entrance — the threshold between the outside world and the inner system. The metaphor matches the product: a controlled entry point between AI clients and external MCP servers, with structure visible from the outside.

Portico is independent. It is not Pengui-branded and not Agentiv-branded. The project's identity is its own.

Sub-product names follow the same metaphor:
- **Portico Gateway** — the MCP gateway and runtime.
- **Portico Skills** — the Skill Pack catalog and execution layer.
- **Portico Registry** — the MCP server registry.
- **Portico Console** — the operator UI.

---

## 2. Vision and positioning

### 2.1 One-line description

> *Portico is a Skill runtime and MCP gateway that turns raw tools into reliable, governed, multi-tenant workflows for AI agents.*

### 2.2 Why this exists

MCP solved access. Skills solved teaching. Nothing yet solves the runtime in between.

MCP standardized how agents reach tools, resources, and prompts across systems. The open Skills spec standardized how agents are taught when and how to use those capabilities. But running this in production requires a layer that:

- Manages many MCP servers and their lifecycles per tenant.
- Binds Skills to specific servers, tools, policies, and UI resources.
- Resolves what each session is actually entitled to.
- Injects credentials safely, without leaking broad tokens to agents.
- Audits everything that happened, with stable catalog snapshots.
- Isolates tenants by process, storage, credentials, and audit.

Portico is that runtime.

### 2.3 Positioning relative to the ecosystem

Portico is deliberately not:
- **Not a competitor to MCP.** Portico speaks MCP outward and inward.
- **Not a competitor to Skills.** Portico runs Skills and extends them with runtime metadata.
- **Not a generic API gateway.** Portico is protocol-aware about MCP, sessions, and Skills.
- **Not an agent framework.** Portico assumes a separate planner or framework above it.
- **Not a model router.** Portico does not pick LLMs.

### 2.4 V1 deployment shape

V1 ships as a **single static Go binary**, runnable as a local developer tool, a single-binary self-hosted instance, or a Docker container. It is multi-tenant by design but does not require Postgres or Redis to operate — SQLite is the default store. Hosted SaaS is out of scope for V1; production hardening (Postgres-default, K8s, Redis multi-instance) is post-V1.

---

## 3. The differentiator: Skill Packs

### 3.1 The gap

A tool schema tells a model what a tool accepts. It does not reliably tell the model:
- When to use the tool, and when not to.
- In what order related tools should be called.
- Which tools are dangerous and require approval.
- Which resources should be read first to ground reasoning.
- How to recover from common errors.
- How to present results back to the user.
- Which UI panel, if any, accompanies the workflow.

Anthropic's open Skills spec begins to fill that gap with a portable format for instructions, resources, and scripts. But Skills alone don't bind those instructions to specific MCP servers, enforce policy at the runtime layer, or compose with credentials and entitlements. That binding is the Skill Pack.

### 3.2 What a Skill Pack is

A Skill Pack is a versioned package that:

1. Conforms to the open Skills spec for instructions, resources, and prompts (so it remains portable to any Skills-aware client).
2. Adds Portico-specific binding metadata: which MCP servers it depends on, which tools it requires, which entitlements it needs, what risk class each tool carries, and which UI resource accompanies it.
3. Is exposed to MCP clients through standard primitives — resources and prompts — so a vanilla MCP client still gets value from it.
4. Is exposed to Portico-aware clients through native APIs that surface the binding metadata.

### 3.3 Example manifest

```yaml
id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1            # open Skills spec compliance

# Open-spec content
instructions: ./SKILL.md
resources:
  - resources/guide.md
  - resources/examples.json
prompts:
  - prompts/review_pr.md
  - prompts/summarize_diff.md

# Portico binding metadata
binding:
  server_dependencies:
    - github
  required_tools:
    - github.get_pull_request
    - github.get_pull_request_diff
    - github.get_file_contents
  optional_tools:
    - github.create_review_comment
  policy:
    requires_approval:
      - github.create_review_comment
    risk_classes:
      github.create_review_comment: external_side_effect
  ui:
    resource_uri: ui://github/code-review-panel.html
  entitlements:
    plans: [pro, enterprise]
```

### 3.4 Why this is the moat

Gateways are commoditizing. Smithery, mcp-proxy, the Cloudflare MCP work, Composio-adjacent offerings, and Anthropic's own MCP App direction all overlap with the gateway features in this RFC. Competing on namespacing and process supervision against vendor-backed teams is a hard fight for an independent project.

The Skill Pack layer is different. It sits at the intersection of two open specs — MCP and Skills — and the work of binding them well, with policy and entitlement and UI awareness, is genuinely under-served. That is where Portico plants its flag.

---

## 4. Goals (V1)

In priority order:

1. **A Skill Pack runtime** — manifest format, JSON Schema validator, virtual-directory loader, binding to MCP servers, exposure through MCP primitives and native APIs, version management, per-session enablement.
2. **A managed MCP gateway** — single outward MCP endpoint, dynamic registry, lifecycle management for stdio and HTTP transports, tool/resource/prompt aggregation with namespacing, full MCP spec including MCP Apps.
3. **Multi-tenant identity and isolation** — JWT-authenticated tenants, per-tenant registry, credentials, audit, snapshots, and process keying. `per_tenant` and `per_user` runtime modes shippable.
4. **Entitlement-aware catalog resolution** — per session, factoring tenant plan, user, agent, environment, and policy. Stable catalog snapshots by default.
5. **Per-request credential injection** — credentials live behind the gateway. Agents never receive broad downstream tokens. OAuth token exchange works end-to-end.
6. **Headless approval flow** — uses MCP elicitation when supported, falls back to structured error. Console surfaces pending approvals as read-only.
7. **An operator UI** — registry, Skill catalog, session inspector, policy view, pending approvals, audit log. Tenant-scoped from day one.
8. **Auditability and observability** — structured events for every tool call, policy decision, Skill activation, and credential injection. OpenTelemetry traces span gateway, runtime, and Skills.
9. **App resource discovery** — index `ui://` resources exposed by MCP servers and Skills, surface them with CSP enforcement.

---

## 5. Non-goals (V1)

- General-purpose API gateway replacement.
- Full agent framework or planner.
- Model router.
- Workflow engine for non-AI workloads.
- Proprietary replacement for MCP or Skills.
- Domain-specific analytics or advertising product.
- Hosted SaaS in V1. Local and self-hosted only.
- Postgres as the default store (Postgres optional in V1; default post-V1).
- Kubernetes deployment artifacts (post-V1).
- Container/microVM isolation for stdio servers (subprocess + optional seccomp/landlock in V1).
- Async approval flows (Slack, email, ticketing). Post-V1.
- Skill sources beyond `LocalDir` (Git/OCI/HTTP are post-V1).

---

## 6. Architecture

### 6.1 Core concept

External clients see Portico as one MCP server. Internally, Portico behaves as an MCP client to many downstream servers, with a Skill runtime sitting alongside that mediates how tools are surfaced and used. Every request is bound to a tenant.

```
                AI Client / Agent / IDE / Desktop Host
                              |
                              v
              +---------------------------------------+
              |  Portico Gateway                       |
              |  +---------------------------------+   |
              |  | Auth + Tenant Resolver (JWT)    |   |
              |  +---------------------------------+   |
              |  | Skill Runtime + Virtual Dir     |   |
              |  | Catalog Resolver + Snapshots    |   |
              |  | Policy Engine + Approval Flow   |   |
              |  | Credential Vault                |   |
              |  | Process Supervisor              |   |
              |  +---------------------------------+   |
              +-----------+---------------------------+
                          |
        +-----------+-----+----+------------------+
        |           |          |                  |
        v           v          v                  v
   GitHub MCP  Postgres MCP  Linear MCP   Filesystem MCP
   (stdio,     (http,        (stdio,      (stdio,
    per_user)   shared)       per_tenant)  per_session)
```

### 6.2 Tenant model

Every request that enters Portico is bound to a tenant. The tenant is the unit of:
- credential ownership
- registry visibility
- skill catalog scope
- audit attribution
- runtime process keying (for `per_tenant`)
- entitlement evaluation

#### Tenant identification

- **Production**: Bearer JWT in Authorization header with a `tenant` claim. Issuer URL and signing keys (JWKS or static public key) configured per-deployment in `portico.yaml`. Token validated on every northbound request. Required claims: `tenant` (string), `sub` (user id), `exp`. Optional: `scope` (space-delimited list of permissions), `plan` (overrides tenant default plan).
- **Dev mode**: `portico dev` (or any bind to `127.0.0.1` with no `auth:` block configured) auto-creates a synthetic `dev` tenant and skips JWT validation. Override possible via `PORTICO_DEV_TENANT=<name>` env var.

#### Tenant context propagation

Tenant ID is attached to every `context.Context` value (via `tenantctx.From(ctx)`) and is required by every internal API that touches tenant-scoped data. It is logged on every event and OTel span.

#### Tenant provisioning in V1

Declarative, via the `tenants:` key in `portico.yaml`:

```yaml
tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    credentials_ref: secrets/acme.yaml
    entitlements:
      skills: [github.*, postgres.sql-analyst]
      max_sessions: 200
  - id: beta
    display_name: Beta Industries
    plan: pro
    credentials_ref: secrets/beta.yaml
```

The synthetic `dev` tenant is materialized only when dev mode is active.

#### Multi-tenancy guarantees in V1

- All persistent data (registry, snapshots, audit, skill enablement, sessions) carries a `tenant_id` column and is queried with a `tenant_id` filter.
- The process supervisor uses tenant ID as a keying dimension for `per_tenant` and `per_user` modes. Two tenants never share a stdio process unless the runtime mode is `shared_global`.
- Credentials are stored in a tenant-scoped vault. Cross-tenant credential reads are impossible by API construction.
- Audit events include `tenant_id` and are queryable per tenant.
- An integration test asserts that cross-tenant data access attempts fail.

### 6.3 Runtime modes

| Mode             | Description                          | V1?        | Typical use                      |
|------------------|--------------------------------------|------------|----------------------------------|
| `shared_global`  | One process for all users            | Yes        | Stateless read-only tools        |
| `per_tenant`     | One process per tenant               | Yes        | Enterprise isolation             |
| `per_user`       | One process per user                 | Yes        | User-scoped credentials          |
| `per_session`    | One process per chat session         | Yes        | Strong isolation, statefulness   |
| `remote_static`  | Remote MCP, no local process         | Yes        | Hosted MCP services              |
| `per_request`    | Cold start per call                  | Post-V1    | Highest isolation, slow          |
| `sidecar`        | Container/sandbox per tenant         | Post-V1    | Untrusted servers, stricter iso  |

The supervisor handles spawn, restart, health checks, idle timeout, crash recovery, graceful shutdown, resource limits, environment injection, log capture, and per-(tenant, session) ownership.

### 6.4 Catalog resolution

Clients may request servers and Skills via headers, but Portico decides the final effective catalog:

```
requested_by_client
∩ entitled_by_tenant_subscription
∩ allowed_by_tenant_policy
∩ allowed_by_user_scopes
∩ allowed_by_agent_policy
∩ available_and_healthy_servers
= effective_catalog
```

Each session receives a stable catalog snapshot (Phase 6). Tool availability does not silently change mid-session unless the client explicitly opts in to live updates. Snapshots make audits reproducible and tool drift detectable.

### 6.5 Credential handling

Credentials live behind the gateway. Agents never see them.

| Strategy                       | Best for                                             |
|--------------------------------|------------------------------------------------------|
| HTTP Authorization injection   | Remote HTTP MCP servers                              |
| Environment variable injection | Stdio servers started per user/session               |
| Credential shim                | Long-lived stdio processes needing per-request creds |
| OAuth token exchange           | Enterprise identity delegation                       |
| Secret reference               | Tenant-managed secrets                               |

Portico does not blindly forward incoming client tokens to downstream servers. Token exchange is the default; passthrough is opt-in and audited.

Credentials are resolved per (tenant, server, user) tuple. The vault is file-backed in V1 (encrypted at rest with a master key from `PORTICO_VAULT_KEY`) with an interface that allows post-V1 backends (HashiCorp Vault, AWS Secrets Manager, etc.).

### 6.6 Approval flow

When a tool call hits a policy that requires approval — either from the Skill Pack's `policy.requires_approval` list or because the tool's risk class demands it:

1. **Detect**: the policy engine intercepts `tools/call` and emits an `approval_pending` audit event.
2. **Try elicitation**: Portico checks the host's negotiated capabilities. If `elicitation` is present, Portico sends an `elicitation/create` request to the host with a structured payload (tool name, args summary, risk class, rationale from skill pack). The host renders an approval UI; Portico waits.
3. **Fallback**: if the host did not advertise elicitation, Portico responds to the original `tools/call` with a JSON-RPC error of code `-32001` (Portico-defined) and an `approval_required` payload (same structured fields). Hosts that understand it surface the approval UI; hosts that don't surface the error to the user.
4. **Persist**: the pending approval is recorded in SQLite (`approvals` table) and visible in the Console as read-only.
5. **Resolution**: on host approval, Portico re-executes the tool call. On denial, returns a `policy_denied` error. Timeout: 5 minutes default, configurable per-tenant.

Async approvals (Slack, email, ticketing) are post-V1 — they require durable workflow state that fights the local-binary V1.

---

## 7. Security model

Portico assumes MCP servers and tool outputs may be unsafe unless explicitly trusted.

### 7.1 Risk classes

| Class                  | Meaning                                       | Default                |
|------------------------|-----------------------------------------------|------------------------|
| `read`                 | Reads external data                           | Allowed if entitled    |
| `write`                | Creates or updates data                       | Policy-controlled      |
| `destructive`          | Deletes or irreversibly changes data          | Approval required      |
| `external_side_effect` | Sends messages, posts comments, triggers jobs | Approval or scoped policy |
| `sensitive_read`       | Reads secrets, PII, private data              | Restricted and audited |

### 7.2 Controls

- Server allowlist and command allowlist for stdio servers.
- Tool allowlist and denylist with risk classification.
- Human approval hooks emitted as events; the host decides UX.
- Secret redaction in logs and traces (regex-based scrubber on event emission).
- Maximum argument and result sizes; output truncation with artifacting.
- Resource URI allowlists; UI resource sandboxing with CSP injection.
- Schema fingerprinting and drift alerts.
- Token exchange instead of passthrough.
- Tenant and session isolation at the process level.
- Audit logs for every tool call, policy decision, and credential injection.

### 7.3 Multi-tenant isolation

- **Process-level isolation**: `per_tenant` and `per_user` runtime modes produce dedicated stdio processes; no two tenants share a process unless explicitly opted into `shared_global`.
- **Credential isolation**: vault keyed by tenant; no cross-tenant credential read paths exist in the API.
- **Storage isolation**: every tenant-scoped table has a `tenant_id NOT NULL` column; every read uses a tenant filter; an integration test asserts cross-tenant query attempts fail.
- **Audit isolation**: audit events are queryable per tenant; the global query is operator-only (require `admin` scope on the JWT).
- **UI isolation**: the Console scopes everything by the operator's tenant, with an admin-only cross-tenant view.

---

## 8. Skill Packs in detail

### 8.1 Manifest schema

Skill Packs use the open Skills spec for content and add a `binding:` block for Portico-specific metadata. JSON Schema for the manifest is canonical and validated at load time. See `internal/skills/manifest/schema.json` (Phase 4).

Top-level fields:
- `id` (string, dotted notation, e.g. `github.code-review`)
- `title` (string, human-readable)
- `version` (semver string)
- `spec` (literal `skills/v1`)
- `description` (string, optional)
- `instructions` (path to SKILL.md, relative to manifest)
- `resources` (list of paths)
- `prompts` (list of paths)
- `binding` (object, see below)

Binding subfields:
- `server_dependencies` (list of server IDs)
- `required_tools` (list of namespaced tool names; load fails if missing)
- `optional_tools` (list; load warns if missing)
- `policy.requires_approval` (list of tool names)
- `policy.risk_classes` (map of tool name → risk class, overrides server default)
- `ui.resource_uri` (`ui://...` URI for accompanying app panel)
- `entitlements.plans` (list of plan labels: `free`, `pro`, `enterprise`)

### 8.2 Virtual directory model

A Skill Pack lives at a logical path: `skill://<namespace>/<id>/...`. The path tree is presented uniformly to clients regardless of the backing storage.

```go
// internal/skills/source/source.go
type SkillSource interface {
    Name() string
    List(ctx context.Context) ([]SkillRef, error)
    Open(ctx context.Context, ref SkillRef) (Manifest, error)
    ReadFile(ctx context.Context, ref SkillRef, relpath string) (io.ReadCloser, ContentInfo, error)
    Watch(ctx context.Context) (<-chan SkillEvent, error) // optional; LocalDir supports it
}

type SkillRef struct {
    ID      string // e.g. "github.code-review"
    Version string // semver
    Source  string // backend name, e.g. "local"
}

type ContentInfo struct {
    MIMEType string
    Size     int64
    ModTime  time.Time
}

type SkillEvent struct {
    Kind SkillEventKind // Added | Updated | Removed
    Ref  SkillRef
}
```

V1 implementation: `LocalDir` reads from a config-pinned directory:

```
skills/
  github/
    code-review/
      manifest.yaml
      SKILL.md
      prompts/
        review_pr.md
        summarize_diff.md
      resources/
        guide.md
        examples.json
```

Post-V1: `Git`, `OCI`, `HTTP` implementations. The interface signature settles in V1; adding sources is additive.

### 8.3 Exposure through MCP

- **`resources/list`** includes every skill file as a `skill://` URI:
  - `skill://github/code-review/manifest.yaml`
  - `skill://github/code-review/SKILL.md`
  - `skill://github/code-review/resources/guide.md`
- **`resources/read`** returns raw content; `Content-Type` set from file extension (markdown, JSON, YAML, etc.).
- **`skill://_index`** is a synthesized JSON resource listing every available (and entitled) skill pack — designed for cheap LLM-side discovery:
  ```json
  {
    "version": 1,
    "tenant": "acme",
    "skills": [
      {
        "id": "github.code-review",
        "version": "0.1.0",
        "title": "GitHub Code Review",
        "description": "Review a GitHub PR following best practices.",
        "required_servers": ["github"],
        "manifest_uri": "skill://github/code-review/manifest.yaml"
      }
    ]
  }
  ```
- **`prompts/list`** auto-includes every skill prompt with the convention `<skill_id>.<prompt_filename>` (e.g. `github.code-review.review_pr`).
- **`prompts/get`** returns the rendered prompt template.

This means a vanilla MCP client (Claude Desktop with no Portico knowledge) sees skills as discoverable resources and callable prompts. No proprietary handshake required.

### 8.4 Native Portico APIs

- `GET /v1/skills` — list all skills available to the caller's tenant.
- `GET /v1/skills/{id}` — full manifest including binding metadata.
- `POST /v1/skills/{id}/enable` — enable for caller's tenant.
- `POST /v1/skills/{id}/disable` — disable.
- `POST /v1/sessions/{session_id}/skills/enable` — per-session enablement.
- `GET /v1/skills/{id}/manifest.yaml` — raw manifest download.

---

## 9. Observability

Portico emits structured events for the full lifecycle: session created, catalog resolved, server process started/stopped/crashed, tool list changed, Skill enabled/disabled, tool call started/completed/failed, resource read, prompt fetched, token injected, policy allowed/denied/approval-required, UI resource discovered, schema drift detected.

Example event:

```json
{
  "type": "tool_call.completed",
  "tenant_id": "acme",
  "session_id": "sess_123",
  "user_id": "alice@acme",
  "catalog_snapshot_id": "cat_abc123",
  "skill_id": "github.code-review",
  "server_id": "github",
  "tool": "github.get_pull_request_diff",
  "duration_ms": 842,
  "policy_decision": "allowed",
  "result_size_bytes": 18231,
  "trace_id": "0123456789abcdef0123456789abcdef",
  "span_id": "0123456789abcdef"
}
```

Integrations:
- OpenTelemetry traces (OTLP exporter) — gateway, runtime, skill activations.
- JSON logs via `log/slog` (default writer is stdout, configurable).
- Local dev console (Console UI session inspector).
- Optional Postgres event store (post-V1; SQLite is V1 default).

---

## 10. Operator UI

The UI ships from V0.1. It does not need to be polished, but it does need to make the system legible. Built as a SvelteKit SPA with `@sveltejs/adapter-static`, compiled to static HTML/JS/CSS, and embedded into the Go binary via `//go:embed`. The same HTTP server that handles the REST and MCP endpoints serves the Console — no separate process, no proxy, one artifact to ship.

### 10.1 Surfaces

- **Server registry** — registered servers, transport, runtime mode, health, processes, tools, resources, prompts, attached Skills, credential strategy, last schema hash, last error.
- **Skill catalog** — available Skill Packs, required servers and tools, instructions, prompts, resources, UI bindings, policies, entitlements, version history.
- **Session inspector** — active sessions, effective catalog snapshots, tool calls, policy decisions, redacted credential events, errors and retries.
- **Policy view** — allowlists and denylists, risk classes, approval requirements, entitlements, environment-specific restrictions.
- **Pending approvals** — read-only list of approvals awaiting host decision.
- **Audit log** — searchable, tenant-scoped.

All views are tenant-scoped. An admin view (requires `admin` JWT scope) shows cross-tenant aggregates.

---

## 11. Technology choices

### 11.1 Implementation language: Go

Portico is implemented in Go. The decision is settled, not open. Reasons unchanged from v2: networking + process supervision are Go's home turf, single static binary helps OSS adoption, low contributor friction, scale not a concern.

### 11.2 Concrete library choices

| Concern                | Library                                              | Notes                                       |
|------------------------|------------------------------------------------------|---------------------------------------------|
| HTTP routing           | `github.com/go-chi/chi/v5`                           | Lightweight, idiomatic                      |
| JWT                    | `github.com/golang-jwt/jwt/v5`                       | Standard for Go                             |
| SQLite driver          | `modernc.org/sqlite`                                 | Pure Go, no CGo                             |
| YAML                   | `gopkg.in/yaml.v3`                                   | Standard                                    |
| JSON Schema            | `github.com/santhosh-tekuri/jsonschema/v5`           | Draft 2020-12                               |
| Logging                | `log/slog`                                           | Stdlib, structured                          |
| OpenTelemetry          | `go.opentelemetry.io/otel` + sdk + exporters/otlp    | OTLP via gRPC or HTTP                       |
| MCP SDK                | `github.com/modelcontextprotocol/go-sdk`             | Verify availability at Phase 1 kickoff      |
| Frontend framework     | SvelteKit + `@sveltejs/adapter-static`               | SPA built to static assets; embedded        |
| Frontend build         | Vite (driven by SvelteKit)                           | Standard tooling; fast dev loop             |
| Frontend embed         | stdlib `embed.FS` over the SvelteKit `build/` output | One Go binary serves the Console            |
| Frontend type-check    | `svelte-check` in CI                                 | Catches Svelte/TS errors before merge       |
| Component library      | Skeleton (default; swappable)                        | Pre-built admin components; do not rebuild  |
| Design tokens          | Single CSS-variables file `src/lib/tokens.css`       | One swap point for theme/branding           |
| Process supervision    | stdlib `os/exec` + custom supervisor                 |                                             |
| Linux sandboxing       | `github.com/elastic/go-seccomp-bpf` (optional)       | Build-tagged, Linux only                    |
| Crypto for vault       | stdlib `crypto/aes` + `crypto/rand` (AES-256-GCM)    |                                             |
| Test                   | stdlib + `github.com/stretchr/testify` (asserts)     |                                             |
| Mock MCP servers       | hand-rolled in `examples/servers/mock/`              |                                             |

If `github.com/modelcontextprotocol/go-sdk` is not available or insufficient at Phase 1 kickoff, fall back to a hand-rolled types package at `internal/mcp/protocol/`.

### 11.3 Other choices

| Concern        | Choice                                        | Reason                                      |
|----------------|-----------------------------------------------|---------------------------------------------|
| UI             | Embedded in binary; SvelteKit SPA (adapter-static) | One artifact to ship; backend serves UI     |
| Storage        | SQLite default; Postgres optional             | Zero-setup for local; Postgres for ops      |
| Config         | YAML with hot reload                          | Familiar to ops; live editing               |
| Native API     | REST+JSON only in V1                          | Simpler client integration; gRPC deferred   |
| Skill manifest | YAML + JSON Schema                            | Tooling and validation from day one         |
| Dev mode       | Bind to `127.0.0.1` → synthetic `dev` tenant  | Friction-free local development             |

---

## 12. Deployment

```bash
# Local developer mode
portico dev --config portico.yaml

# Single binary mode (production)
portico serve --config portico.yaml

# Docker
docker run -p 8080:8080 -v ./portico.yaml:/etc/portico.yaml portico/portico

# Kubernetes (post-V1)
# Deployment + ConfigMap registry + Secret-backed credentials
# Optional Postgres + optional Redis
```

V1 supports the first three. Kubernetes artifacts are post-V1.

---

## 13. Native APIs

In addition to the outward MCP endpoint, Portico exposes management APIs. All endpoints except `/healthz` and `/readyz` require a valid JWT. Admin-scoped endpoints additionally require the `admin` scope.

```
# Health
GET    /healthz
GET    /readyz

# Servers (tenant-scoped)
GET    /v1/servers
POST   /v1/servers
GET    /v1/servers/{id}
POST   /v1/servers/{id}/reload
POST   /v1/servers/{id}/disable

# Tools / Resources / Prompts (tenant-scoped, derived)
GET    /v1/tools
GET    /v1/resources
GET    /v1/prompts

# Skills (tenant-scoped)
GET    /v1/skills
POST   /v1/skills
GET    /v1/skills/{id}
GET    /v1/skills/{id}/manifest.yaml
POST   /v1/skills/{id}/enable
POST   /v1/skills/{id}/disable

# Catalog
POST   /v1/catalog/resolve
GET    /v1/catalog/snapshots/{id}

# Sessions
GET    /v1/sessions
GET    /v1/sessions/{id}
DELETE /v1/sessions/{id}
POST   /v1/sessions/{id}/skills/enable
POST   /v1/sessions/{id}/skills/disable

# Approvals
GET    /v1/approvals
GET    /v1/approvals/{id}

# Audit
GET    /v1/audit/events

# Tenants (admin only)
GET    /v1/admin/tenants
POST   /v1/admin/tenants
```

All list endpoints support `limit`, `cursor`, and basic filters via query string. JSON pagination uses opaque cursors.

---

## 14. V0.1: the demo

Great OSS infrastructure wins on a sharp problem and zero friction to first value, not on feature breadth. Portico's V0.1 is therefore designed around a specific demo, and the architecture serves the demo, not the other way around.

### 14.1 The five-minute story

A new user should be able to do this in under five minutes:

1. Run one command to install Portico.
2. Drop in a `portico.yaml` that registers the GitHub MCP server (single tenant: `dev`).
3. Drop in a `github.code-review` Skill Pack from the example gallery.
4. Connect Claude Desktop (or any MCP client) to Portico's endpoint.
5. Ask the agent to review a real PR. Watch it follow the Skill: read PR metadata, then the diff, then targeted file contents. Comments are blocked by policy unless explicitly approved.
6. Open the Console at `localhost:8080`, see the session, the catalog snapshot, the tool calls, and the policy decisions in real time.

If a viewer cannot watch a 90-second screen recording of that flow and immediately understand what Portico is and why it matters, V0.1 is not done.

### 14.2 V0.1 scope

- **One** transport: stdio (gateway exposes northbound HTTP+SSE; downstream is stdio).
- **One** runtime mode: `per_session`.
- **One** example MCP server: GitHub.
- **One** example Skill Pack: `github.code-review`.
- **One** policy primitive: approval-required tools (via fallback structured error; elicitation in Phase 5).
- **One** UI surface: a session inspector that shows registry + Skills + tool calls.
- **One** binary: `portico serve --config portico.yaml`.
- **Multi-tenant infrastructure is in place** (tables keyed by tenant, JWT middleware compiled in) but only the `dev` tenant is active.

### 14.3 What V0.1 deliberately does not include

- HTTP transport for downstream MCP servers.
- Production JWT auth (dev mode only).
- OAuth token exchange.
- All runtime modes other than `per_session`.
- Sidecar containers.
- App/UI resource sandboxing (CSP injection lands in Phase 3).
- Postgres or any external state store.
- Kubernetes deployment artifacts.

Each of these has a phase. None of them block the demo.

---

## 15. Phases and V1 boundary

V1 = end of Phase 6. Post-V1 = Production Readiness.

| Phase | Theme                                  | V0.1 Scope                          | V1 Scope                                 |
|-------|----------------------------------------|-------------------------------------|------------------------------------------|
| 0     | Skeleton + tenant foundation           | Dev mode only; bare auth middleware | Full JWT auth; tenants table; SQLite     |
| 1     | MCP gateway core                       | Stdio only; one server              | Stdio + HTTP; many servers; full spec    |
| 2     | Registry + lifecycle                   | per_session only                    | All 5 V1 modes; hot reload               |
| 3     | Resources, prompts, MCP Apps           | Resources only                      | Full primitives + MCP Apps with CSP      |
| 4     | Skills runtime + virtual directory     | One example pack; LocalDir          | Catalog + APIs + 4 reference packs       |
| 5     | Auth, policy, credentials, approval    | approval-required fallback only     | Full policy + creds + elicitation flow   |
| 6     | Catalog snapshots + observability      | Basic events                        | Snapshots + drift detection + OTel       |

Detailed phase plans live in `docs/plans/phase-{0..6}-*.md`.

**Post-V1 (Production Readiness, not in this RFC)**: Postgres-default with migrations, K8s deployment artifacts, Redis-backed multi-instance coordination, sidecar/per_request runtime modes, quota enforcement, async approval flows, container/microVM isolation, Git/OCI/HTTP skill sources, hosted SaaS evaluation, alternative auth backends (mTLS, SSO).

---

## 16. Design principles

1. **Skills first.** Tools without workflows are not enough. The runtime that binds them is the product.
2. **Protocol-compatible, product-opinionated.** Stay compatible with MCP and Skills. Add product value above them.
3. **Multi-tenant from the foundation.** Tenancy is not a feature; it is a substrate. Every internal API takes a tenant context.
4. **Stable catalogs over dynamic chaos.** Each session sees an explicit snapshot.
5. **Client hints are not authority.** Clients may request, Portico decides.
6. **Credentials stay behind the gateway.** Agents do not get broad downstream tokens.
7. **Approvals are headless.** The host owns UX. Portico emits and waits.
8. **Demo first, architecture second.** Every feature traces back to a moment a user can see.
9. **UI is part of the runtime.** Operators need visibility from day one.
10. **Security is foundational.** Tool execution, resource reading, and UI rendering are all policy surfaces.

---

## 17. Open questions

1. Should the JSON Schema for Skill manifests be authored upstream (proposed back to the open Skills spec) or kept Portico-local with a `binding` extension namespace? Working assumption: Portico-local in V1; propose upstream after V1.
2. How much MCP client-side capability support belongs in V1 — sampling and roots specifically? Elicitation is required for the approval flow. Working assumption: V1 implements the host-facing **client capabilities** for elicitation only; sampling and roots land post-V1 unless a Phase 5 implementor finds a strong reason otherwise.
3. Is `Skill Pack` the right product name, or should the binding-extended Skills be called something distinct from open-spec Skills to avoid confusion in docs and tutorials? Working assumption: keep `Skill Pack` for now; revisit before V1 docs polish.

---

## 18. Repository structure

```
portico/
  cmd/
    portico/                 # main binary
      main.go
      cmd_serve.go
      cmd_dev.go
      cmd_validate.go        # validate manifests / config
  internal/
    config/                  # YAML loader, hot reload
    auth/
      jwt/                   # token validation, JWKS
      tenant/                # tenant context, resolution
      scope/                 # scope check helpers
    skills/
      manifest/              # types + JSON Schema
      source/                # SkillSource interface + LocalDir
      loader/                # validator + loader
      runtime/               # session enablement, exposure
    registry/                # MCP server registry (CRUD, hot reload)
    mcp/
      northbound/            # outward MCP endpoint (we are server)
        http/                # HTTP+SSE transport
        stdio/               # stdio transport (post-V1; northbound stdio is for embedding)
      southbound/            # inward MCP clients (we are client)
        stdio/
        http/
      protocol/              # MCP types if no SDK
      transports/            # shared transport helpers
    runtime/
      process/               # supervisor, Spawn abstraction
      session/               # session lifecycle
    catalog/
      resolver/              # effective catalog computation
      snapshots/             # stable per-session snapshots
      namespace/             # tool/resource namespacing
    policy/                  # risk classes, allow/deny, approvals
      approval/              # elicitation + fallback flow
    apps/                    # ui:// resource discovery, CSP
    audit/                   # event store + emit
    telemetry/               # OTel setup
    secrets/                 # credential vault
    server/
      api/                   # native REST API handlers
      mcpgw/                 # northbound MCP wiring
      ui/                    # embedded console handlers
    storage/
      sqlite/                # schema, migrations, queries
      ifaces/                # storage interfaces
  web/
    console/                 # SvelteKit project (adapter-static)
      package.json
      svelte.config.js
      vite.config.ts
      tsconfig.json
      src/
        app.html             # SvelteKit shell
        lib/
          tokens.css         # design tokens (CSS variables) — single swap point
          api.ts             # typed REST client
          components/        # local wrappers around component-library primitives
        routes/
          +layout.svelte
          +page.svelte       # /
          servers/+page.svelte
          skills/+page.svelte
          sessions/+page.svelte
      static/                # raw assets (favicon, fonts) copied verbatim
      build/                 # SvelteKit output, embedded by the Go binary
                             # generated; not committed (CI builds it)
  examples/
    servers/
      mock/                  # in-process mock MCP server for tests
      github/                # config snippet for github MCP
    skills/
      github.code-review/
      postgres.sql-analyst/
      linear.triage/
      filesystem.search/
  docs/
    rfc/
      RFC-001-Portico.md     # this file
    plans/
      phase-0-skeleton-tenant-foundation.md
      phase-1-mcp-gateway-core.md
      phase-2-registry-lifecycle.md
      phase-3-resources-prompts-mcp-apps.md
      phase-4-skills-runtime-virtual-directory.md
      phase-5-auth-policy-credentials-approval.md
      phase-6-catalog-snapshots-observability.md
    concepts/
    deployment/
    quickstart.md
  test/
    integration/             # cross-package integration tests
    fixtures/                # shared test data
  go.mod
  go.sum
  Makefile
  Dockerfile
  README.md
```

---

## 19. Success criteria

### 19.1 V0.1 success

V0.1 is successful if a 90-second screen recording shows:

- One command installs Portico.
- One config file registers the GitHub MCP server (dev tenant).
- One Skill Pack is loaded from the examples gallery.
- Claude Desktop connects and follows the Skill to review a real PR.
- Comments are blocked by policy until approved (via structured error or elicitation).
- The Console shows the session, catalog snapshot, and tool calls in real time.

### 19.2 V1 success (multi-tenant gateway)

V1 is successful if:

- **Multi-tenant operation**: at least two tenants can be configured with distinct registries, credentials, and skill catalogs; cross-tenant access is impossible by construction (verified by integration test).
- **Local dev**: a developer can run Portico locally with one config file (single tenant, dev mode).
- **Self-hosted prod**: an operator can run Portico self-hosted with two or more tenants, each authenticated via JWT.
- **Server breadth**: Portico can mount at least three MCP servers across stdio and HTTP transports.
- **Tool surface**: tools are namespaced and policy-filtered per tenant.
- **Full primitives**: resources, prompts, and resource templates are proxied through the gateway.
- **MCP Apps**: `ui://` resources are discovered, indexed, and surfaced with CSP enforcement.
- **Lifecycle**: stdio MCP servers are managed by Portico (spawn, idle, restart, crash recovery).
- **Credentials**: injected per user/session without exposing them to the client; OAuth token exchange works end-to-end for at least one provider.
- **UI**: shows servers, tools, Skills, sessions, tool calls, pending approvals, and audit events — all tenant-scoped.
- **Skills**: at least four reference Skill Packs ship in the examples gallery; a pack can be loaded, validated, exposed as `skill://` resources, and enabled per session.
- **Snapshots**: catalog snapshots are stable per session by default; live updates are explicit opt-in.
- **Approvals**: approval flow works via elicitation against an elicitation-aware host; falls back to structured error otherwise.
- **Observability**: OpenTelemetry traces span gateway, runtime, and Skill activations; schema fingerprinting detects upstream tool drift and emits an alert event.

### 19.3 Adoption signals

Beyond technical milestones, Portico is succeeding as an OSS project if:
- Contributors outside the original author submit Skill Packs to the example gallery.
- At least one external project depends on Portico's Skill Pack format.
- Issues are filed against real production use, not just demos.
- The README's first paragraph and the 90-second demo both stay accurate as the product grows.

---

## 20. Final recommendation

Proceed with **Portico** as the project name. Build V0.1 around the five-minute demo. Lead the narrative with Skills, not the gateway. Implement in Go. Multi-tenant from V1. Headless approvals. MCP-first skill virtual directory. Subprocess isolation in V1, container/sandbox post-V1.

Ship a single binary and an embedded UI. Defer everything that does not show up in the first 90 seconds of the demo recording.

The differentiator is the combination of:

> *MCP Gateway + Skill Runtime + Multi-Tenancy + Catalog Snapshots + UI + Policy*

...all behind one boring static binary that someone can run in under a minute.
