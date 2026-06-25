# Roadmap

Portico ships as a single version line. There are no separate open-core and enterprise editions;
every capability described here is in the same static binary. The implementation is organized into
numbered phases. Each phase has binding acceptance criteria and named test cases documented in
`docs/plans/`. A phase is not considered done until every criterion passes, coverage targets are
met, and the pre-commit preflight (build + boot + HTTP smoke) runs clean.

This page summarizes what has shipped, what is in progress, and what is planned through the 1.0
launch.

---

## Version milestones

| Milestone | Content | Status |
|-----------|---------|--------|
| **V1 core** | Phases 0–11: gateway engine, registry, skills, auth, policy, vault, observability, Console CRUD, playground, telemetry replay | Shipped |
| **V1.5** | Phase 13: LLM gateway | Shipped |
| **V1.6** | Phase 13.5: MCP Code Mode | Shipped |
| **V2** | Phases 14–16 + 15.5: Agent Profiles, semantic cache, Virtual Keys, hierarchical budgets, A2A | Shipped |
| **V2 hardening** | Phases 17–19: tool-poisoning defense, dynamic config API, production scale-out | Upcoming |
| **Launch** | Phase 20 + Phase 12: productization verification pass, onboarding/distribution, signed release | Upcoming |

::: info Build sequencing
All phases are built before anything launches. The sequence is: 13 → 13.5 → 14 → 15.5 → 16 → 17
→ 18 → 19 → 20 (productization) → 12 (onboarding/distribution/launch, dead last). Phase 12's
first-run wizard and signed multi-arch release ship only after Phase 20's adversarial verification
pass confirms every load-bearing claim against the live binary.
:::

---

## Shipped

### Phase 0 — Skeleton and tenant foundation

Repository structure, `portico.yaml` config loader, tenant context threading through
`context.Context`, JWT authentication with a dev-mode bypass, SQLite storage with numbered
forward-only migrations, Console shell, and the core CLI subcommands (`serve`, `dev`,
`validate`). Every tenant-scoped table includes `tenant_id NOT NULL`; every storage method that
touches one takes an explicit `tenantID` parameter. This multi-tenancy invariant is enforced
from the first line of code and never relaxed.

### Phase 1 — MCP gateway core

MCP protocol types, northbound HTTP+SSE transport, southbound stdio and HTTP clients, tool
aggregation with per-server namespacing, and the central dispatcher. The northbound/southbound
split — and the interface boundary between them — is established here and never broken.

See [MCP Gateway](/concepts/mcp-gateway), [Northbound](/concepts/mcp-northbound), and
[Southbound](/concepts/mcp-southbound).

### Phase 2 — Registry and process lifecycle

Dynamic per-tenant MCP server registry, a full process supervisor, and five runtime isolation
modes: `shared_global`, `per_tenant`, `per_user`, `per_session`, and `remote_static`. Environment
variable interpolation from vault references. Log capture. See [MCP Registry](/concepts/mcp-registry).

### Phase 3 — Resources, prompts, and MCP Apps

Resources, resource templates, and prompts forwarded over the northbound channel. MCP Apps
(`ui://` resources) with a deny-by-default Content Security Policy. Multiplexed
`list_changed` notifications.

### Phase 4 — Skill Packs and virtual directory

Skill manifest format with JSON Schema validation, the `SkillSource` interface and its
`LocalDir` driver, a virtual directory exposed as `skill://` resources and prompts, per-session
enablement, and four reference Skill Packs shipped in `examples/skills/`. See
[Skill Packs](/concepts/skill-packs) and [Skill Sources](/concepts/skill-sources).

### Phase 5 — Auth, policy, credentials, and approval

AES-256-GCM vault, OAuth 2.0 token exchange (RFC 8693), three credential injection strategies
(environment variable, header, OAuth exchange), a policy engine with typed risk classes, the
headless approval flow via MCP `elicitation/create` with a structured JSON-RPC error fallback,
and a persisted, redacted audit store. See [Credentials Vault](/concepts/credentials-vault),
[OAuth Token Exchange](/concepts/oauth-token-exchange), [Policy](/concepts/policy), and
[Approvals](/concepts/approvals).

### Phase 6 — Catalog, snapshots, and observability

Per-session catalog snapshots, schema fingerprinting, drift detection (a background detector
that emits `catalog.drift` events when a downstream server's tool schema changes between
snapshots), and OpenTelemetry tracing end-to-end including `traceparent` propagation to stdio
servers. See [Catalog and Sessions](/concepts/catalog-and-sessions), [Drift Detection](/concepts/drift-detection),
and [Observability](/concepts/observability).

### Phase 7 — Console design system

Token-driven design system for the operator Console: light and dark modes, a component library
built on Skeleton, self-hosted Inter / JetBrains Mono / Newsreader, brand placement, and a full
accessibility pass. All visual properties flow through `web/console/src/lib/tokens.css`; raw
literals in `.svelte` files are a build-blocking lint error. See [Console](/concepts/console).

### Phase 8 — Skill sources as first-class resources

Skill sources elevated to first-class REST and Console CRUD: Git and HTTP drivers join
`LocalDir`, hot-reload propagates changes to all active sessions, a validation pipeline returns
JSON Pointer errors for manifest problems. See [Skill Sources](/concepts/skill-sources).

### Phase 9 — Console CRUD

Full operator Console for servers, tenants, secrets, and the policy rule editor. Hot-reload
wires every save to the running process. Destructive actions go through the approval flow.
Permission scopes enforced at every form boundary.

### Phase 10 — Interactive playground

Catalog browser, schema-driven tool-call composer, streamed response panel, and live
correlation of trace, audit, policy, and drift events for the same call. Saved test cases and
replay. See [Playground](/concepts/playground).

### Phase 11 — Telemetry replay

Self-contained span store, session bundle exporter/importer, a time-travel inspector with a
state-at-time scrubber, cross-session pivots, full-text audit search, and replay-from-inspector.

### Phase 13 — LLM gateway

An OpenAI-compatible northbound LLM API (`/v1/*`) on the same listener as MCP, backed by a
pure-Go, Apache-2.0 LLM engine. Per-tenant provider and model registry with weighted routing and
fallback. Vault-backed API keys. Tool-use bridging between the LLM API and registered MCP tools.
Quota enforcement and cost telemetry. An OpenAI conformance test suite. See
[LLM Gateway](/concepts/llm-gateway), [LLM Providers](/concepts/llm-providers), and
[LLM Routing](/concepts/llm-routing).

### Phase 13.5 — MCP Code Mode

Four meta-tools (`listToolFiles`, `readToolFile`, `getToolDocs`, `executeToolCode`) over a
virtual `.pyi` catalog generated from JSON Schema. The execution sandbox is a hardened Starlark
runtime: no file I/O, no network, no subprocess, no `import`, no `load`. In-sandbox tool
calls traverse the identical governed path as direct `tools/call` — the same policy engine, the
same approval flow, the same audit record. Approval-suspend via Starlark continuations: a call
that triggers an approval gate suspends and resumes after the operator responds, with no
connection held open. Per-session opt-in; existing clients see no change.

See [Code Mode](/concepts/code-mode) and [Code Mode Savings](/concepts/code-mode-savings).

### Phase 14 — Agent Profiles

Agent Profiles are the V2 consumer-binding primitive. A Profile binds a logical agent (or
operator, or CI pipeline) to an allowed set of MCP servers, tools, Skill Packs, LLM model
aliases, and scopes, and attaches N Virtual Keys. Profiles are enforced in a resolver middleware
that runs before every MCP `tools/list`, `tools/call`, LLM `/v1/*`, prompt, resource, and
`skill://` dispatch. A principal with no bound Profile receives a synthesised default Profile
that grants the full tenant surface, preserving backward compatibility.

See [Agent Profiles](/concepts/agent-profiles).

### Phase 15.5 — Semantic cache, Virtual Keys, and hierarchical budgets

Three governance primitives shipped together because they compose:

- **Semantic cache** — an LLM response cache keyed first by tenant, supporting exact-hash
  matching today and a pluggable vector-similarity backend (the `§4.4` interface seam) for
  semantic deduplication. `Cache-Control` and `x-bf-cache-*` headers give callers bypass
  control. See [Semantic Cache](/concepts/semantic-cache).
- **Virtual Keys** — `pk-portico-<id>.<secret>` credentials attached to Agent Profiles. The
  secret is shown once at create/rotate and never stored; only a `salt` + `HMAC-SHA256(salt,
  secret)` pair is persisted. Provider and model allowlists on each VK constrain which LLM
  endpoints it can reach. See [Virtual Keys](/concepts/virtual-keys).
- **Hierarchical budgets** — spending limits at four levels: VK → Team → Customer → Tenant.
  A pre-check finds the most-specific binding before dispatching; a post-call reconcile debits
  all levels atomically. Configurable warning thresholds at 80, 95, and 100 percent.
  See [Hierarchical Budgets](/concepts/hierarchical-budgets).

### Phase 16 — A2A peer protocol

A2A runs on the same single listener as MCP (`GET /a2a/.well-known/agent.json` for discovery,
`POST /a2a` for task dispatch), behind the same authentication envelope, and enforced by the
same Agent Profile resolver. Portico registers external A2A peers, ingests their agent cards,
and surfaces discovered tasks as catalog rows. The MCP→A2A bridge — configured per Agent Profile
— translates an inbound `tools/call` into an outbound A2A `message/send` and returns the result
to the calling agent. Agent card aggregation publishes a combined card reflecting all registered
peer capabilities.

See [A2A](/concepts/a2a) and [A2A Bridges](/concepts/a2a-bridges).

---

## Upcoming

### Phase 17 — Tool-poisoning defense

Phase 6 detects schema drift; Phase 17 enforces a response to it. Four defenses, each with
`audit_only` (default) and `enforce` modes:

- **Schema attestation** — registered MCP servers, A2A peers, and skill sources can carry an
  operator-configured signature (static public key or Sigstore-shaped verification). Portico
  verifies it on registration and on every drift event.
- **Drift gates** — instead of emitting a `catalog.drift` event and continuing, a configured
  gate can block the snapshot update and fail affected tool calls with a typed error until an
  operator confirms the change.
- **Description scanning** — tool, resource, prompt, and A2A task descriptions are scanned at
  catalog admission time for prompt-injection patterns (instruction phrases, hidden Unicode,
  suspicious URLs). Findings at `critical` severity block admission in `enforce` mode.
- **Result scanning** — tool and task result bodies are scanned before being returned to the
  calling agent. Suspicious results are wrapped with a structured warning.
- **Supply-chain pinning** — skill sources gain content-addressing: a SHA-256 digest is recorded
  on first load; later fetches that produce a different digest fail closed.

All defenses apply uniformly to MCP and A2A, using the catalog `kind` discriminator as the seam.

### Phase 18 — Dynamic configuration API

A structured, watchable, auditable CRUD surface over the full Portico resource model (Agent
Profiles, Virtual Keys, Teams, Customers, Budgets, MCP Servers, Skills, Skill Sources, Policies,
A2A Peers, Security Configs). A watch channel lets tooling observe resource changes in real time.
Bulk apply with transactional rollback. GitOps controllers and the Phase 19 Kubernetes operator
consume this API rather than parsing `portico.yaml` directly.

The existing `/api/*` endpoints are unified under a `/api/v1/{resource}/...` shape. The Console
becomes a first-class client of the same API — no privileged back channel.

### Phase 19 — Production scale-out

Lights up the extensibility seams that earlier phases reserved but did not activate:

- **Postgres-default storage** — the storage interface (Phase 0) gains a Postgres driver.
  SQLite remains supported; Postgres becomes the recommended production backend.
- **Redis coordination** — multi-instance deployments share Postgres for durable state and Redis
  for process-supervisor leasing, hot-reload fan-out, and watch channel distribution. N stateless
  Portico instances behind a load balancer is a supported topology.
- **Kubernetes operator + Helm chart** — a custom controller reconciles
  `AgentProfile`, `VirtualKey`, `Team`, `Customer`, `Budget`, `Server`, `Skill`, `SkillSource`,
  `Policy`, `A2APeer`, and `Tenant` CRDs onto a Portico fleet via the Phase 18 dynamic-config
  API.
- **Federation** — multiple Portico clusters in separate regions or trust boundaries, with
  controlled replication for shared resources and strict isolation for tenant-scoped state.
- **Hardened subprocess isolation** — `per_request` and `sidecar` runtime modes that run stdio
  MCP servers inside container or microVM sandboxes with seccomp, landlock, and cgroup limits.

### Phase 20 — Productization (pre-launch verification)

An adversarial verification pass over the entire product before launch. Fan-out checks confirm
that every load-bearing claim is true against the running binary: REST and MCP surfaces, Console
UX flows, observability pipelines, and documentation consistency. Findings land as normal PRs;
top findings receive regression locks that run in CI.

### Phase 12 — Onboarding, distribution, and launch (dead last)

First-run wizard, `portico init`, in-Console help system, embedded docs site, OpenAPI extractor,
`make release` producing signed multi-arch binaries (`linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`), and the MCP conformance suite. Phase 12 is sequenced last, after Phase 20's
verification pass, so the artifact a public announcement points at reflects the complete product.

---

## Post-launch (not pre-planned)

Items the RFC explicitly defers beyond the V2 launch boundary. Each has a reserved interface or
hook so it is additive when picked up.

| Item | Notes |
|------|-------|
| Async approval channels | Slack, email, and ticketing integrations for the approval flow |
| OCI skill source | HTTP and Git skill sources ship in Phase 8; OCI is a later driver |
| Alternative auth backends | mTLS, SSO direct; the JWT validator interface accepts pluggable backends |
| Sub-tenant (per-user) RBAC | The tenant model is a floor; sub-tenant rows can be added |
| Cross-instance distributed tracing | Phase 11 covers single-instance; federation adds the cross-instance path |
| Hosted SaaS | A managed deployment of the open-source binary |
| Fine-tuning and multimodal LLM APIs | The LLM gateway northbound interface has reserved extension points |
| Semantic vector-store drivers | The semantic cache seam (Phase 15.5) accepts pluggable Weaviate and Qdrant drivers as a fast-follow |
| Visual skill manifest builder | A drag-drop manifest editor in the Console |
| Mobile-first Console layouts | The design system is responsive-ready; mobile-optimized layouts are post-launch polish |

---

## Related

- [Concepts overview](/concepts/) — the full concept index
- [Architecture](/concepts/architecture) — how the components fit together
- [RFC-001](/reference/rfc) — the product intent and design decisions that govern all phases
- [MCP Gateway](/concepts/mcp-gateway) — northbound and southbound MCP
- [Agent Profiles](/concepts/agent-profiles) — the V2 consumer-binding primitive
- [A2A](/concepts/a2a) — the second agentic wire protocol
- [Code Mode](/concepts/code-mode) — token-efficient tool execution in a hardened sandbox
- [LLM Gateway](/concepts/llm-gateway) — the OpenAI-compatible LLM API surface
- [Security Model](/concepts/security-model) — the security invariants Portico enforces
