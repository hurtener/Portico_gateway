# Concepts overview

Portico is a multi-tenant MCP gateway and Skill runtime, shipped as a single static Go binary.
It speaks **MCP** and **A2A** outward to agents and AI clients, speaks MCP inward to a managed
fleet of downstream MCP servers, and exposes an OpenAI-compatible **LLM gateway** over a
configurable set of providers — all through one governed, audited, session-aware control plane.

This page is a map of the product. Each section below links to the concept page that covers its
area in depth.

::: info Reading order
If you are new to Portico, start with [Architecture](/concepts/architecture) and
[Multi-tenancy](/concepts/multi-tenancy) to establish the foundations, then follow the group
that matches what you are trying to do.
:::

---

## Foundation

These two pages describe the structural decisions everything else builds on.

**[Architecture](/concepts/architecture)**
The layered shape of Portico: how a request flows from an AI client through auth and tenant
resolution, through the policy engine, into the process supervisor, and back. Covers the
component map, runtime modes (`shared_global`, `per_tenant`, `per_user`, `per_session`), and
how the binary's HTTP listener serves MCP, A2A, LLM, and the operator Console from one port.

**[Multi-tenancy](/concepts/multi-tenancy)**
Tenant identity flows through every layer, from the JWT's `tenant` claim to every row of
storage. Explains the tenant model, how the synthetic `dev` tenant works, the `tenants:` config
block, and the isolation guarantees — process-level, credential, storage, and audit — that make
it safe to run multiple organisations on one Portico instance.

---

## Consumer governance

Portico's governance layer sits between the identity boundary (JWT / Virtual Key) and every
downstream surface. The five concepts below compose into a single, consistent enforcement model.

**[Agent Profiles](/concepts/agent-profiles)**
The named, tenant-scoped object operators actually think in. An Agent Profile binds a logical
consumer to a curated subset of MCP servers, tools, Skill Packs, LLM model aliases, and scopes.
It is the single source of truth for consumer entitlement: every gate (`tools/list`, `tools/call`,
`/v1/models`, the Skills runtime) reads the resolved profile and nowhere else. A principal with no
profile bound sees the tenant's full surface (back-compat).

**[Virtual Keys](/concepts/virtual-keys)**
A Portico-side credential (`pk-portico-<id>.<secret>`) that sub-divides a tenant into per-app,
per-developer, or per-environment slots. Each key carries its own scopes, server/model allowlists,
an optional bound Agent Profile, and a budget parent. Portico stores only a per-VK `salt` +
`HMAC-SHA256(salt, secret)` — never the raw secret. The secret is returned once at create or
rotate and never again.

**[Hierarchical Budgets](/concepts/hierarchical-budgets)**
Spend and usage caps that nest: **Virtual Key → Team → Customer → Tenant**. Each budget is a
`(scope_kind, metric, period, limit)` tuple; the pre-call check walks the chain most-specific to
least and the lowest level that would exceed fires first. The post-call reconcile debits every
applicable level in a single transaction. Threshold crossings at 80%, 95%, and 100% emit
debounced audit events.

**[Authentication](/concepts/authentication)**
How Portico validates identity on every northbound request. Covers Bearer JWT validation
(asymmetric algorithms only: RS256/384/512, ES256/384/512), the required JWT claims (`tenant`,
`sub`, `exp`), Virtual Key resolution at the same auth boundary, dev-mode short-circuiting, and
the propagation of the resolved identity into every downstream context.

**[Policy](/concepts/policy)**
The policy engine that runs after identity resolution and before any tool dispatch. Explains
policy rule structure, risk classes (`read`, `write`, `destructive`, `external_side_effect`,
`sensitive_read`), the approval flow trigger, and how policies are scoped per tenant, per server,
and per Skill Pack binding.

---

## MCP Gateway

Portico presents one MCP endpoint to the outside world and manages a fleet of downstream MCP
servers on the inside. These five pages cover the full northbound-to-southbound path.

**[MCP Gateway](/concepts/mcp-gateway)**
The overview: how Portico aggregates namespaced tools, resources, and prompts from many
downstream servers into one MCP surface for clients. Covers namespace collision resolution, the
full MCP spec support (sampling, roots, elicitation, list-changed, cancellation, progress,
resource templates, MCP Apps), and the request path from northbound socket to southbound client.

**[MCP Northbound](/concepts/mcp-northbound)**
The HTTP transport Portico exposes to AI clients: Streamable HTTP and SSE. Explains the session
lifecycle, `initialize`/`initialized` handshake, capability negotiation, and the mechanism by
which Portico can send server-initiated requests (elicitation, list-changed notifications) back
to connected clients.

**[MCP Southbound](/concepts/mcp-southbound)**
The client layer Portico uses to talk to downstream MCP servers. Covers the `southbound.Client`
interface, the stdio transport (subprocess lifecycle, env injection, log capture), the HTTP
transport, per-tenant and per-session process ownership, and crash recovery.

**[MCP Registry](/concepts/mcp-registry)**
The per-tenant directory of registered MCP servers — their transport config, runtime mode,
credential references, and health state. Explains server registration (`POST /api/servers`),
the Console's Servers list, hot reconfiguration, and how the registry feeds the catalog resolver.

**[Catalog & Sessions](/concepts/catalog-and-sessions)**
How Portico resolves the effective tool/resource/prompt catalog for a session — the intersection
of client entitlement, tenant subscription, policy, user scopes, and healthy servers — and takes
a stable snapshot. Explains catalog snapshots (what they freeze, why they exist), schema
fingerprinting, drift detection, and the per-session state machine.

---

## LLM Gateway

Portico exposes an OpenAI-compatible `/v1` surface over a configurable set of providers, behind
the same governance envelope as the MCP gateway.

**[LLM Gateway](/concepts/llm-gateway)**
The OpenAI-compatible surface: `POST /v1/chat/completions`, `POST /v1/embeddings`,
`GET /v1/models`. Explains how model aliases decouple client code from provider details, how
Agent Profile and Virtual Key enforcement applies (`allowed_model_aliases`, `vk_scope_violation`),
and how LLM calls flow through the budget pre-check and post-call reconcile.

**[Providers](/concepts/llm-providers)**
How to register LLM providers in `portico.yaml`, map them to model aliases, and configure
fallback chains. Covers the provider interface (a `§4.4` extensibility seam), the embedded
pure-Go, Apache-2.0 LLM engine Portico ships for local inference, and external provider
registration.

**[Routing](/concepts/llm-routing)**
The per-alias routing table: primary provider, fallback chain, load-balancing strategy, and the
conditions that trigger fallover. Explains how routing interacts with Virtual Key model
allowlists and Agent Profile `allowed_model_aliases`.

**[Semantic Cache](/concepts/semantic-cache)**
An optional, pluggable cache in front of the LLM gateway. Drivers include `inmem` (development),
`redis` (production exact-hash), and embedding-similarity backends. Cache keys are tenant-first
by construction — cross-tenant sharing is impossible. Hits are served before quota and budget
checks (they are free). Clients opt out per request via `Cache-Control: no-store` /
`Cache-Control: no-cache`.

---

## A2A

A2A (Agent-to-Agent) is the second agentic wire protocol Portico speaks, on the same listener as
MCP, through the same governance envelope.

**[A2A](/concepts/a2a)**
How Portico acts as an A2A endpoint (serving its own agent card and a JSON-RPC 2.0 dispatch
endpoint at `POST /a2a`), how operators register external A2A peers, and how the governed
dispatch path works: Agent Profile entitlement check → egress credential injection from the
vault → southbound client → audit. The agent card at `GET /a2a/.well-known/agent.json`
aggregates skills discovered from registered peers.

**[A2A Bridges](/concepts/a2a-bridges)**
Bridges let an Agent Profile declare that a named MCP `tools/call` is transparently dispatched
to an A2A peer task. The calling agent continues using a tool it already knows; the work runs on
a remote agent. The bridge traverses the same governed envelope as any direct call — there is
no separate fast path.

---

## Skills & Code Mode

The Skills layer turns raw MCP tools into governed, documented, policy-aware workflows. Code Mode
is an alternative tool-presentation mode that reduces token spend on large catalogs.

**[Skill Packs](/concepts/skill-packs)**
Versioned packages that conform to the open Skills spec (instructions, resources, prompts) and
add Portico-specific binding metadata: server dependencies, required and optional tools, risk
classifications, approval triggers, and an optional `ui://` resource for inline rendering. Skill
Packs are exposed through standard MCP primitives so any compliant client benefits.

**[Skill Sources](/concepts/skill-sources)**
How Portico loads Skill Packs: the source interface (a `§4.4` extensibility seam), the `LocalDir`
driver that ships in V1, and the post-V1 path toward `Git`, `OCI`, and `HTTP` sources. Covers
manifest validation, version management, and how the virtual `skill://` resource directory is
built.

**[Code Mode](/concepts/code-mode)**
An opt-in session mode where a client orchestrates tools by writing Starlark snippets instead
of receiving the full namespaced catalog. The client sees four meta-tools (`mcp.listToolFiles`,
`mcp.readToolFile`, `mcp.getToolDocs`, `mcp.executeToolCode`); tool calls inside a snippet
traverse the identical governance envelope as direct `tools/call` — no shortcuts, no bypasses.

**[Code Mode — Token Savings](/concepts/code-mode-savings)**
How Portico estimates the tokens saved per `executeToolCode` execution: the catalog that never
shipped to the context window plus collapsed round-trip overhead, minus the code and result cost.
The formula is deterministic and surfaced per execution and rolled up per tenant on the
observability dashboard.

**[Approvals](/concepts/approvals)**
The headless approval flow that fires when a tool call hits a policy that requires it. Portico
uses MCP `elicitation/create` when the host supports it and falls back to a structured
`approval_required` JSON-RPC error otherwise. Pending approvals are persisted in SQLite and
visible read-only in the Console. Covers the approval lifecycle, the configurable timeout, and
the replay semantics when an approval resolves.

---

## Credentials and security

Agents never receive downstream tokens. Credentials live behind Portico's vault and are injected
per request at the transport layer.

**[Credentials Vault](/concepts/credentials-vault)**
The encrypted, per-tenant store for downstream credentials: API keys, OAuth tokens, service
account secrets. The vault is file-backed in V1 (AES-GCM encryption, master key from
`PORTICO_VAULT_KEY`), behind a `§4.4` interface that admits future backends. Covers the vault
CLI (`portico vault put|get|delete|list|rotate-key`), credential injectors (Authorization header,
environment variable, credential shim), and the rule that agents never see broad tokens.

**[OAuth Token Exchange](/concepts/oauth-token-exchange)**
How Portico implements RFC 8693 token exchange so downstream servers receive scoped, short-lived
tokens derived from the inbound principal's identity — rather than the inbound token itself.
Covers the exchange flow, how exchange results are cached per `(tenant, server, user)`, and the
`auth.passthrough: true` escape hatch (opt-in, audited).

**[Security Model](/concepts/security-model)**
The full threat model: tool-poisoning mitigations, path traversal prevention in manifest loading,
CSP enforcement on `ui://` resources, secret redaction in audit payloads, the JWT algorithm
allowlist (asymmetric only — no HS\* or `none`), the subprocess command allowlist for stdio
servers, and the cross-tenant isolation guarantees.

---

## Observability

Every governed decision Portico makes is recorded and traceable.

**[Observability](/concepts/observability)**
Structured logging (`log/slog`, JSON in production), OpenTelemetry traces that span the gateway
and downstream calls, and the `slog`-based telemetry attributes that appear on every span:
`tenant_id`, `request_id`, `trace_id`, `session_id`, `server_id`, `tool`. Covers the OTel
exporter configuration and how trace context propagates into stdio servers via environment.

**[Audit](/concepts/audit)**
The structured event stream that records every tool call, policy decision, Skill activation,
credential injection, budget threshold crossing, and approval state change. Audit events carry
`tenant_id`, go through the redactor before emission (no unredacted tool arguments in the log),
and are queryable per tenant via the REST API or the Console's Audit Log screen.

**[Drift Detection](/concepts/drift-detection)**
How Portico detects when a downstream server's tool schema changes from the fingerprint stored
in the session's catalog snapshot. Explains the fingerprinting algorithm, the `schema_drift`
audit event, the optional operator alert, and how drift interacts with snapshot stability
guarantees.

**[Playground](/concepts/playground)**
The Console's interactive tool-call surface. Operators and developers can issue governed
`tools/call` requests against any registered server, inspect the full response and audit event,
and test approval flows — all inside the auth and policy envelope of their tenant.

**[Console](/concepts/console)**
The embedded SvelteKit operator UI served directly from the Portico binary. Covers the page
structure (Servers, Agents, Governance, Skill Catalog, Sessions, Audit Log, Playground), the
auth model (same JWT / Virtual Key as the REST API), and the fact that the Console is compiled
into the binary at build time — no separate process, no CDN dependency.

---

## Related

- [Getting started](/getting-started/) — install Portico and connect your first MCP server in
  under ten minutes.
- [RFC-001](/reference/rfc) — the design RFC that specifies every product decision in this
  document's source of truth.
- [Guides](/guides/) — task-oriented walkthroughs for deployment, provider management, profile
  creation, Skill Pack authoring, and more.
- [Configuration reference](/reference/configuration) — the full `portico.yaml` schema.
- [REST API reference](/reference/rest-api) — every endpoint with request and response shapes.
