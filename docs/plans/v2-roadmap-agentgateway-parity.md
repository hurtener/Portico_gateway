# Portico V2 — Bifrost-Shaped Agentic Gateway Roadmap

> **Status:** Draft, 2026-05-09. Materially revised 2026-05-12.
> **Scope:** Multi-phase roadmap (Phases 13–19, with sub-phases 13.5 and 15.5) describing the technical path from V1 (end of Phase 12) to a multi-tenant agentic data plane that is SOTA against `bifrost` on LLM-engine governance and against `agentgateway` on MCP/A2A protocol breadth — without copying the parts of either that enterprise customers don't buy.
> **2026-05-12 pivot:** The original V2 line proposed agentgateway-parity: an Envoy-shaped `Bind / Listener / Route / Backend` substrate (Phase 14) and a general-purpose HTTP/gRPC reverse proxy (Phase 15). After enterprise-deployment review, that direction was rejected. Customers do not replace their existing HTTP gateway (Kong/Envoy/Istio/ALB) with an agentic gateway — they consolidate *agentic* traffic. The substrate and the proxy were therefore retired in favour of a Bifrost-shaped, consumer-centric model with **Agent Profiles** as the headline primitive. The old plan files are retained for git-archaeology purposes (Phase 15 with a "DEFERRED" header; Phase 14 renamed and rewritten).
> **Status of constituent phase plans:** `phase-13` was rewritten 2026-05-12 to swap `liter-llm` (CGo issues) for the Bifrost Go SDK. `phase-13.5` (MCP Code Mode) and `phase-15.5` (Semantic Cache + Virtual Keys + Hierarchical Budgets) were authored 2026-05-12. `phase-14-agent-profiles` was authored 2026-05-12 to replace the substrate plan. `phase-18` was reshaped 2026-05-12 to drop the Envoy ADS adapter and the Listener/Route/Backend resource types. `phase-15` is post-V2. Each plan is binding for its phase. Where this roadmap and a phase plan disagree, the **phase plan wins**; where the phase plan and the RFC disagree, the RFC wins (per `AGENTS.md` §15).

---

## 0. TL;DR

Portico V1 (Phases 0–12) is a **multi-tenant MCP gateway with a Skill Pack runtime**. V1.5 (Phase 13) adds an LLM gateway alongside it, now backed by **Bifrost** (Apache 2.0, pure-Go, CGo-free — `github.com/maximhq/bifrost/core`) as the inference engine. V1.6 (Phase 13.5) adds MCP Code Mode. Both ship as a single Go binary on a fixed `:8080` listener; routing surface is hard-coded by path prefix (`/mcp`, `/v1/*`, `/api/*`, `/*`).

V2 makes Portico **the agentic gateway enterprises actually buy**:

| Phase | Headline |
|-------|----------|
| 13 (rewrite) | LLM gateway on Bifrost engine + `custom_openai` provider template catalog |
| 13.5 (new) | MCP Code Mode (Starlark sandbox + virtual `.pyi` catalog; ~50% token savings) |
| **14 (rewrite)** | **Agent Profiles** — first-class consumer binding (servers/tools/skills/models/scopes/VKs) |
| **15.5 (new)** | Semantic cache + Virtual Keys + hierarchical budgets (VK → Team → Customer → Tenant) |
| 16 | A2A as a peer protocol to MCP (same listener, same envelope) |
| 17 | Tool-poisoning defence (attestation, drift gates, scanning, supply-chain pinning) |
| 18 (reshaped) | GitOps + watch channel over existing CRUD (Agent Profiles, VKs, Servers, Skills, Policies) |
| 19 | Production scale-out (Postgres / Redis / Kubernetes / federation / sandboxed runtime) |

**Deferred / not in V2:**
- Phase 15 as written (HTTP/gRPC reverse proxy for arbitrary microservices). Customers keep their existing HTTP gateway.
- Envoy-shaped Bind/Listener/Route/Backend substrate. The original Phase 14 substrate plan was retired 2026-05-12.
- xDS / Envoy ADS adapter. The Phase 18 dynamic-config API now targets Portico's own resource model only.

**The Skill Pack runtime and the V1 multi-tenant chassis are still the moat.** V2 makes the consumer-side primitive (Agent Profile) and the cost/governance primitives (VK, hierarchical budget, semantic cache) first-class on top of them.

---

## 1. Why this roadmap exists (and why this shape)

After V1.5, a buyer evaluating Portico has two natural reference points:

- **`agentgateway`** for protocol breadth — full HTTP/gRPC + agentic proxy with an Envoy-shaped substrate.
- **`bifrost`** for LLM-engine governance — VKs, hierarchical budgets, semantic caching, MCP code mode, sub-100 μs overhead.

The market-research question we tested at V2 planning: *which of those two shapes does an enterprise customer actually adopt?*

Enterprise reality (validated against Santiago's hands-on deployment experience, 2026-05-12):

1. **Customers don't replace their HTTP gateway.** They have Kong/Envoy/Istio/ALB already, with policies their security and platform teams own. Asking them to put microservice traffic through a new agentic gateway is a non-starter.
2. **Customers do consolidate agentic traffic.** "Which agents can call which MCP servers / Skills / LLMs, with which budget, under which policy" is a real coordination problem — and there's no incumbent. This is the gap Portico fills.
3. **Consumer-side gating is the actual UX they want.** Operators think in terms of agents and what each agent is allowed to reach. They do not think in terms of binds, listeners, routes, and backends. An Envoy-shaped Console makes them re-learn their org chart in network-engineer vocabulary.
4. **`bifrost`'s VK + allowlist model matches the mental model.** But Bifrost's surface is flat (VK has a model allowlist; MCP clients are registered separately; the two never reconcile into a single "Agent" object). Portico can do better by making the Profile the first-class object that combines all of it.

So V2 commits to a **Bifrost-shaped consumer-centric model with Portico-specific upgrades**:

- **Agent Profile** as the named, tenant-scoped consumer-binding primitive (Phase 14).
- **Virtual Keys** as the credential lifecycle (`pk-portico-*`), attached to Profiles, with hierarchical budgets (Phase 15.5).
- **Semantic cache** + **MCP Code Mode** as cost-reduction primitives that operators *opt into* per Profile.
- **A2A** as a peer protocol to MCP (Phase 16), reachable through the same listener and the same envelope — no parallel substrate.
- **Tool-poisoning defence** (Phase 17) and **GitOps + watch** (Phase 18) and **production scale-out** (Phase 19) ride on the existing CRUD surfaces.

What we explicitly do *not* commit to:

- **No HTTP/gRPC reverse proxy.** Phase 15 is deferred indefinitely. If a customer engagement re-opens it, the Phase 15 file is the starting point.
- **No Listener/Route/Backend substrate.** The original Phase 14 plan is gone. Portico has one listener on one bind by default; multi-bind TLS termination is a small config addition handled inline in Phase 14 (Agent Profiles) where it fits, not its own substrate phase.
- **No xDS / Envoy ADS adapter.** Phase 18 targets Portico's resource model only.

---

## 2. What `agentgateway` and `bifrost` actually do — and the gap we close

### 2.1 The two reference points side by side

| Capability                                                                 | agentgateway | bifrost | Portico V1.5 status                                                                 | Lands in    |
|----------------------------------------------------------------------------|--------------|---------|--------------------------------------------------------------------------------------|-------------|
| Stateful MCP JSON-RPC sessions, long-lived connections                     | ✅           | ✅      | ✅ Phase 1 northbound HTTP+SSE; Phase 5 server-initiated correlator                  | done         |
| Session fan-out across multiple MCP servers                                | ✅           | ✅      | ✅ Phase 1 aggregator + namespacing; Phase 6 snapshots                               | done         |
| Server-initiated SSE events                                                | ✅           | ✅      | ✅                                                                                   | done         |
| Protocol-aware MCP routing                                                 | ✅           | ✅      | ✅ Dispatcher inspects `method` / `params`                                          | done         |
| Per-session authorisation                                                  | ✅           | ✅      | ✅ Phase 5 scopes + policy engine                                                    | done         |
| Multi-tenant from day one                                                  | 🟡           | 🟡      | ✅ V1 invariant                                                                       | done         |
| **General HTTP and gRPC reverse proxy**                                    | ✅           | ❌      | ❌ Not a feature, **and not in V2** (Phase 15 deferred)                              | deferred     |
| **Envoy-shaped Bind/Listener/Route/Backend substrate**                     | ✅           | ❌      | ❌ Not a feature, **and not in V2** (original Phase 14 substrate dropped)            | dropped      |
| **xDS-style dynamic configuration API**                                    | ✅           | ❌      | ❌ Not a feature, **and not in V2** (Phase 18 watches Portico resources only)        | dropped      |
| Tool poisoning protection                                                  | 🟡           | 🟡      | 🟡 Phase 6 drift detection; full defence in P17                                      | P17          |
| Unified OpenAI-compatible LLM API                                          | ✅           | ✅      | ✅ P13 on Bifrost engine                                                              | done         |
| 23+ native LLM providers                                                   | ❌ (~6)      | ✅      | ✅ P13 via Bifrost                                                                    | done         |
| OpenAI-compatible custom-provider templates (DeepSeek, internal vLLM, …)   | partial       | ✅       | ✅ P13 via Bifrost custom-provider hook                                              | done         |
| Automatic failover + weighted key routing                                  | 🟡           | ✅      | ✅ P13 via Bifrost                                                                    | done         |
| Semantic caching across providers                                          | ❌           | ✅      | P15.5                                                                                | P15.5        |
| **MCP Code Mode** (virtualised tool surface)                               | ❌           | ✅      | P13.5                                                                                | P13.5        |
| **Virtual Keys** (sub-tenant credentials)                                  | ❌           | ✅      | P15.5                                                                                | P15.5        |
| **Hierarchical budgets** (VK → Team → Customer → Tenant)                  | ❌           | ✅      | P15.5                                                                                | P15.5        |
| **Agent Profiles** (first-class consumer binding)                          | ❌           | partial | **Phase 14 (new)**                                                                    | **P14**       |
| A2A peer protocol                                                          | ✅           | ❌      | P16                                                                                  | P16          |
| Skill Pack runtime                                                         | ❌           | ❌      | ✅ V1                                                                                 | done         |
| Headless approval flow                                                     | ❌           | ❌      | ✅ V1                                                                                 | done         |
| Webhook on budget critical                                                 | ❌           | ✅      | P15.5 (optional)                                                                     | P15.5        |

Three observations:

- **agentgateway's HTTP/gRPC and substrate columns are real differentiation in their target market (the "consolidate all gateways" pitch), but enterprise agentic deployments do not buy that pitch.** We deliberately leave that column to them.
- **bifrost's governance columns are the right shape for what enterprises want from an LLM gateway.** We adopt them (Phase 15.5).
- **The Agent Profile row is where Portico does what neither does — combining consumer-binding across MCP + Skills + LLM + VK into one first-class object.** Bifrost has VK allowlists but not a unified profile; agentgateway has substrate but no consumer abstraction.

### 2.2 What `bifrost` has that we deliberately do NOT copy

We use Bifrost as a **Go library** (`github.com/maximhq/bifrost/core`) — never as a sidecar. Specifically we do NOT adopt:

- **Bifrost's HTTP-mode admin surface** (its own Console, its own admin REST, its own VK store, its own MCP client registration UI). Our Console and `/api/governance/*` are authoritative for tenants.
- **Bifrost's tenant-flat VK store.** Ours is tenant-scoped from V1; cross-tenant VK reuse would weaken §6 of `AGENTS.md`.
- **Bifrost's Agent Mode auto-execute defaults.** Our headless approval flow (Phase 5) stays the V1 invariant.

We adopt: Bifrost's engine (provider routing, custom-provider hook, semantic-cache primitive optionally, fallback). We authored: Portico's surfaces (REST API, Console, audit, redaction, governance, Agent Profiles) on top.

### 2.3 What we have that neither has (and we keep)

Non-negotiable through V2:

1. **Skill Pack runtime.** Manifests, JSON-Schema validator, virtual directory, source drivers, per-session enablement, MCP-primitive surfacing. Phase 4 + Phase 8.
2. **Multi-tenancy from day one.** `tenant_id` on every row, every span, every audit event, every process key. RFC §6.2.
3. **Catalog snapshots.** Stable per-session view with drift detection and reproducibility for audit. Phase 6.
4. **Headless approval flow.** Elicitation + structured-error fallback; the host renders, Portico does not. RFC §6.6.
5. **Vault-backed credential injection** with OAuth token exchange and explicit, audited passthrough. Phase 5.
6. **Tenant-aware process supervisor** with `shared_global / per_tenant / per_user / per_session / remote_static` modes. Phase 2.
7. **Operator Console** that already covers registry, skills, tenants, secrets, policy, snapshots, audit, approvals, playground. Phases 7–10.

V2's job is to make all of these apply to **every** request that crosses the gateway — MCP, A2A, LLM, plus the Agent Profile, VK, and budget primitives that gate them.

---

## 3. V2 architecture sketch (post-pivot)

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Portico Binary                              │
│                                                                          │
│  ┌────────────── Control Plane (V1 + Phase 14 + Phase 15.5) ─────────┐  │
│  │  Tenancy │ Auth (JWT + Virtual Keys) │ Vault │ Policy │ Approvals │  │
│  │  Audit │ Telemetry │ Registry │ Skills │ Snapshots │ Catalog       │  │
│  │  **Agent Profiles** │ **Budgets** │ Semantic Cache │ Console       │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                  │                                       │
│                                  ▼                                       │
│  ┌──────────── Northbound (one listener; optional TLS sibling) ──────┐  │
│  │   /mcp           → MCP gateway (P1+P3+P4)                          │  │
│  │   /a2a           → A2A gateway (P16)                               │  │
│  │   /v1/*          → LLM gateway on Bifrost engine (P13)             │  │
│  │   /api/*         → REST control plane (incl. /api/agent-profiles) │  │
│  │   /              → Console SPA                                      │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                  │                                       │
│                                  ▼                                       │
│  ┌────────────────── Southbound (MCP/A2A/LLM Providers) ──────────────┐  │
│  │   MCP servers (stdio + HTTP) │ A2A peers │ LLM providers (Bifrost) │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────┘
```

**Key shape:**

- **One listener by default** (`:8080`), optional TLS sibling (`:8443`) for production. Routing is path-prefix only; no Envoy-shaped substrate.
- **Every request — MCP, A2A, LLM — passes through the same middleware chain**: tenant resolution → JWT or VK resolution → **Profile resolution (Phase 14)** → policy → audit → tracing → approval routing → dispatch → response → audit close → tracing close.
- **Profile resolution is the new step.** It runs once after auth and writes the profile (or "no profile") into the request context. Every downstream handler — MCP dispatcher, LLM handler, Skills runtime — reads it through the same helper.

### 3.1 What changes from Phase 13's shape

Three additions, no rewires:

1. **Profile resolver** in the request middleware (Phase 14).
2. **VK auth strategy** in JWT middleware (Phase 15.5) — recognises `pk-portico-*` bearers, resolves to (tenant, profile, scopes, budgets).
3. **Semantic cache** in the LLM gateway handler (Phase 15.5) — pre-dispatch lookup, post-dispatch store.

The MCP dispatcher and LLM handler from Phase 1–13 are untouched in shape. They learn to read `profile.* allowlists` from context where today they read `scope.*` allowlists.

### 3.2 What Phase 14 explicitly does *not* introduce

- No Envoy-shaped data-plane substrate. The original Phase 14 plan is retired.
- No multi-listener / multi-protocol bind topology. The single-listener path-prefix shape from V1 stays.
- No HTTP/gRPC reverse proxy. Phase 15 is deferred indefinitely.
- No xDS / Envoy ADS adapter. Phase 18 watches Portico resources only.

---

## 4. Phase order (V2)

| #     | Plan                                                                                                | Phase summary                                                                                                                                                                              |
|-------|-----------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 13    | [phase-13-llm-gateway.md](./phase-13-llm-gateway.md) *(2026-05-12 rewrite)*                          | LLM gateway on the **Bifrost** engine (pure Go, Apache 2.0). 23 native providers; `custom_openai` provider type with a curated template catalog (DeepSeek, Together, internal vLLM, httpbun) closing the agentgateway gap. OpenAI-compatible northbound; MCP tool-use bridge; per-tenant quotas; engine seam via `internal/llm/engine/ifaces`. |
| 13.5  | [phase-13.5-mcp-code-mode.md](./phase-13.5-mcp-code-mode.md) *(new 2026-05-12)*                       | MCP Code Mode: four meta-tools + Starlark sandbox over a virtual `.pyi` catalog; ~50% token savings on multi-tool sessions; bound to our Snapshots + Policy + Approval + Audit envelope. |
| **14**    | **[phase-14-agent-profiles.md](./phase-14-agent-profiles.md) *(new 2026-05-12 — replaces the retired Envoy substrate plan)*** | **Agent Profiles — first-class consumer binding. A Profile binds a logical agent to allowed MCP servers + tools + Skill Packs + LLM aliases + scopes + N attached Virtual Keys. The dispatcher, snapshot, skills runtime, and LLM gateway all read Profile from the request context. Replaces the composition of Phase 5 scopes + Phase 6 snapshot scoping + Phase 4 Skill enablement + Phase 15.5 VK MCP allowlists with one CRUD surface.** |
| 15    | [phase-15-http-grpc-proxy.md](./phase-15-http-grpc-proxy.md) *(DEFERRED 2026-05-12)*                  | **Post-V2.** General-purpose HTTP/gRPC reverse proxy. Retained for reference if a future customer engagement re-opens the "consolidate all gateways" pitch.                                |
| 15.5  | [phase-15.5-semantic-cache-and-virtual-keys.md](./phase-15.5-semantic-cache-and-virtual-keys.md) *(new 2026-05-12)* | Semantic cache (Weaviate/Redis/Qdrant pluggable) + Virtual Keys (`pk-portico-*`) attached to Agent Profiles + hierarchical budgets (VK → Team → Customer → Tenant).                       |
| 16    | [phase-16-a2a-protocol.md](./phase-16-a2a-protocol.md)                                              | A2A as a peer protocol to MCP — same listener, same envelope, same Profile resolver. No separate substrate. Catalog rows for A2A tasks/skills. Opt-in MCP↔A2A bridges.                     |
| 17    | [phase-17-tool-poisoning-defense.md](./phase-17-tool-poisoning-defense.md)                          | Schema attestation, drift gates (block, not just detect), description + result prompt-injection scanning, supply-chain digest pinning. Applies to both MCP and A2A surfaces.              |
| 18    | [phase-18-dynamic-config-api.md](./phase-18-dynamic-config-api.md) *(reshaped 2026-05-12)*           | GitOps + watch channel over **existing** CRUD surfaces (Agent Profiles, VKs, Servers, Skills, Policies, Budgets). Bulk apply with transactional rollback. Resource versioning. No Envoy ADS adapter; no Listener/Route/Backend resources. |
| 19    | [phase-19-production-scale-out.md](./phase-19-production-scale-out.md)                              | Postgres-default, Redis coordination, Kubernetes operator + Helm chart, federation across instances, sandboxed stdio runtime modes. CRDs map to Agent Profile / VK / Server / Skill, not Listener/Route/Backend. |

### 4.1 Why this order

- **13 (Bifrost rewrite) first** — every subsequent LLM-side phase (13.5, 15.5) inherits the engine seam. Doing it once with the right library means the seam is the abstraction every subsequent phase plugs into.
- **13.5 (Code Mode) before 14** — Code Mode is a presentation layer on the MCP listener and a high-leverage cost-saver to ship early. It also exercises Approval flow under a new shape (continuation-based suspension), surfacing latent bugs before Phase 14 introduces Profile resolution.
- **14 (Agent Profiles) before 15.5** — VKs attach to Profiles; budgets attach to VKs and to Profile hierarchy levels. Building Profiles first means VKs land into the right home from day one.
- **15.5 before 16** — A2A inbound auth and consumer-side gating ride on the Profile resolver and the VK auth strategy. Ship those first so A2A doesn't get a separate auth path.
- **16 before 17** — tool-poisoning defence applies to both MCP and A2A tool surfaces; building it once for both is cheaper than retrofitting.
- **17 before 18** — the dynamic config API is a privileged write surface. Land tool-level defence before letting external GitOps controllers push state.
- **18 before 19** — federation and multi-instance coordination are dramatically simpler when there is already one structured channel for resource state changes.

### 4.2 Each phase respects the existing contracts

Every V2 phase preserves:

- **All `AGENTS.md` rules**, including §4.1 preflight gate, §4.2 phase implementor contract, §4.4 extensibility seams, §4.5 frontend conventions, §4.5.1 operator UX gates.
- **Multi-tenancy invariants** (§6 of `AGENTS.md`).
- **Security non-negotiables** (§7).
- **One source of truth** for MCP types (`internal/mcp/protocol`) — A2A gets its own equivalent at `internal/a2a/protocol`; nothing shadows MCP types.
- **The §13 forbidden-practices list** is updated (not relaxed) by each phase.

---

## 5. Cross-cutting design rules for V2 (post-pivot)

### 5.1 The Profile is the consumer

A request's identity is `(tenant, principal, profile)` where principal is the authenticated user/agent (from JWT) or the resolved VK owner. Profile drives the surface (which servers/tools/skills/models). If a request arrives without an attached Profile, the resolver assigns a synthesised "default profile" (the tenant's full surface) so existing V1/V1.5 clients continue to work. **Operators opt into restriction by creating a real Profile and binding their VK(s) to it.**

### 5.2 Every request inherits the V1+P14 envelope

A request's lifecycle is:

```
1.  TLS termination (if listener.tls)            (V1 + a small P14 helper for TLS-on-sibling-bind)
2.  Tenant resolution                            (Phase 0)
3.  JWT or Virtual Key validation                (Phase 0 / Phase 15.5)
4.  **Profile resolution**                       (Phase 14 — writes profile into ctx)
5.  Policy evaluation                            (Phase 5; reads profile)
6.  Approval-flow short-circuit if required      (Phase 5)
7.  Budget pre-check (VK → Team → Customer)     (Phase 15.5; reads profile)
8.  Tracing span open                            (Phase 6)
9.  Audit event open                             (Phase 5)
10. **Cache lookup (LLM path only)**             (Phase 15.5)
11. Dispatch (MCP / A2A / LLM)
12. Response
13. Cache store (LLM path only, on miss)         (Phase 15.5)
14. Audit event close (with redaction)           (Phase 5)
15. Tracing span close                           (Phase 6)
16. Budget reconcile (VK → Team → Customer)     (Phase 15.5)
```

Steps 1–9 and 14–16 are protocol-agnostic. **Adding a new protocol (A2A in Phase 16) means adding step 11 for that protocol; nothing else moves.** This is the litmus test for whether the Phase 14 Profile resolver is shaped right.

### 5.3 No new substrate primitives

V2 introduces:

- **Agent Profile** (Phase 14)
- **Virtual Key** (Phase 15.5)
- **Team, Customer, Budget** (Phase 15.5)
- **Semantic Cache config** (Phase 15.5)

V2 does **not** introduce:

- Bind / Listener / Route / Backend (Envoy-shaped substrate). Dropped 2026-05-12.
- HTTP / gRPC backend drivers. Dropped 2026-05-12.
- Bridge resource type. (A2A↔MCP bridges in Phase 16 are configured *on the Profile*, not as a separate "bridge" resource.)

### 5.4 Configuration evolves additively

Existing `portico.yaml` files keep working. V2 adds new top-level optional blocks:

```yaml
agent_profiles: [...]   # Phase 14
virtual_keys:    [...]  # Phase 15.5
customers:       [...]  # Phase 15.5
teams:           [...]  # Phase 15.5
budgets:         [...]  # Phase 15.5
cache:                  # Phase 15.5
  driver: redis|weaviate|qdrant|inmem|none
  ttl: 5m
  threshold: 0.85
```

No `binds:` / `listeners:` / `backends:` blocks. The single-listener shape from V1/V1.5 stays. A small optional `tls:` block on the existing server config covers TLS-on-sibling-bind for production deployments.

### 5.5 Dynamic config never bypasses the static guarantees (Phase 18, reshaped)

When Phase 18 lands the dynamic configuration API, **every change still goes through the same validation, policy, and audit stack as a YAML reload.** The API targets Portico's resource types (Profile, VK, Team, Customer, Budget, Server, Skill, Policy, SecurityPolicy). It does **not** expose Listener/Route/Backend (those don't exist). It does **not** speak xDS. Envoy ADS adapter was dropped 2026-05-12.

### 5.6 The Console keeps up

Every V2 phase that adds a tenant-visible concept gets Console screens **in the same phase**. The §4.5.1 operator UX gates apply. The new headline route is **`/agents`** (Phase 14) — the Console's primary navigation centre of gravity shifts from "servers" to "agents" because that's how operators actually think.

### 5.7 The smoke surface keeps up

Every V2 phase ships its `scripts/smoke/phase-N.sh` per `AGENTS.md` §4.2. Earlier phases' smokes keep passing. A phase that adds endpoints without a smoke check is rejected.

---

## 6. Risk register (post-pivot)

| Risk                                                                                       | Mitigation                                                                                                                                                  |
|--------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Customers expect HTTP-proxy capabilities and Phase 15 isn't there                          | Document the position explicitly in our concept docs: "Portico is an agentic gateway, not an HTTP gateway." Phase 15 file is retained; revival is a customer-driven RFC. |
| Phase 14 Profile resolution adds latency on the hot path                                   | Microbenchmark: Profile resolution must add < 1 ms p95 (in-memory LRU keyed by VK + profile_id, invalidated on revoke). Acceptance criterion in Phase 14.   |
| Profile + VK + Scope + Snapshot allowlist semantics become ambiguous                       | Intersection semantics, documented and tested: most-restrictive layer wins. Profile is the headline; VK/Scope/Snapshot may further restrict but never relax. |
| A2A spec churns                                                                            | Phase 16 pins to a published spec version (same pattern as `internal/mcp/protocol/types.go`). Spec bumps are RFC changes.                                   |
| Tool poisoning defence creates false positives on legitimate MCP tools                     | Phase 17 ships every defence in `audit-only` mode first; `enforce` mode is opt-in per profile (or per tenant).                                              |
| Dynamic config API (Phase 18) becomes a privileged-write attack surface                    | Phase 18 requires `admin` scope + per-tenant rate limits; every write is policy-evaluated and audit-logged with the diff; signed-write mode optional.        |
| Federation in Phase 19 reintroduces the cross-tenant leakage class                         | Phase 19 carries a federation-specific cross-tenant integration test suite; tenants are first-class in every federated message; profiles are tenant-scoped. |
| The Profile resolver becomes a god-object                                                  | Strict interface: `resolver.Resolve(ctx, principal) (Profile, error)`. Profile is a read-only DTO. Behaviour lives in the consumers (dispatcher, handler).  |

---

## 7. V1 / V1.5 / V1.6 / V2 boundary

| Boundary       | What ships                                                                                                                                                      |
|----------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **V1** (end of Phase 12) | Multi-tenant MCP gateway, Skill runtime, Console, observability, conformance. Single `:8080` listener.                                              |
| **V1.5** (end of Phase 13) | + LLM gateway northbound on **Bifrost** engine (23 native providers + `custom_openai` template catalog), tool-use bridging, provider/model registry, per-tenant quotas, costs. |
| **V1.6** (end of Phase 13.5) | + MCP Code Mode: virtualised tool surface + Starlark sandbox.                                                                                  |
| **V2** (end of Phase 15.5) | + **Agent Profiles** + Semantic cache + Virtual Keys + hierarchical budgets. Portico now matches Bifrost on LLM governance, has a primitive neither Bifrost nor agentgateway has (unified Agent Profile), and explicitly does not match agentgateway on HTTP/gRPC proxy. |
| **V2.5** (end of Phase 19) | + A2A, tool-poisoning defence, GitOps/watch over existing CRUD, production scale-out (Postgres / Redis / Kubernetes / federation).             |

V2 is **additive**: a V1/V1.5/V1.6 deployment continues to work against a V2 binary if the operator does not create any Agent Profiles, Virtual Keys, or Budgets. Without Profiles, every authenticated request gets the synthesised default profile (full tenant surface) — exactly today's behaviour.

---

## 8. Things deliberately NOT in V2

Items that stay deferred:

- **HTTP / gRPC reverse proxy for arbitrary microservices.** Phase 15 is deferred indefinitely. Customers keep their existing HTTP gateway.
- **Envoy-shaped Bind/Listener/Route/Backend substrate.** Dropped 2026-05-12. Portico runs one listener; the routing is path-prefix.
- **xDS / Envoy ADS adapter.** Dropped 2026-05-12. Phase 18 targets Portico's own resource types.
- **Workflow engine for non-AI workloads.** Portico is an *agentic* gateway; it does not become an iPaaS.
- **A planner / agent framework.** RFC §5 still applies.
- **A model fine-tuning surface.** Phase 13's LLM gateway delivers inference, not training.
- **Hosted SaaS Portico.** The roadmap protects self-host as the first-class deployment shape.
- **Replacement of MCP, A2A, or Skills with a Portico-proprietary protocol.** All specs stay open.
- **A built-in identity provider.** Portico continues to consume external IDPs via JWT/JWKS. Phase 19 may add SSO conveniences for the Console; the IDP itself stays external.
- **A built-in service mesh.** Portico is a north-south agentic gateway, not east-west. Mesh-class features stay out of scope.
- **Running Bifrost's HTTP-mode admin surface.** We use Bifrost as a Go library only.
- **A Python interpreter inside Code Mode.** Starlark (Python subset) is intentional; full Python would re-introduce CGo and import surface (Phase 13.5).
- **A tenant-exposed vector store.** Phase 15.5's vector backend is internal to the semantic cache.
- **Automated Virtual Key rotation cron.** Phase 15.5 ships manual rotation.
- **Cross-tenant cache sharing.** Even when two tenants ask identical prompts, they get separate cache entries. Multi-tenancy is the V1 invariant.

If a future phase plan tries to bring any of these in, that is an RFC change first.

---

## 9. How to read the V2 phase plans

Each `phase-N` plan in V2 follows the same structure established by the V1 plans:

- **Goal** — one paragraph, what tenant-visible thing exists at the end.
- **Why this phase exists** — the strategic argument; references this roadmap.
- **Prerequisites** — exact phases consumed.
- **Out of scope (explicit)** — list what reviewers will otherwise scope-creep into the PR.
- **Deliverables** — numbered list, each maps to acceptance.
- **Acceptance criteria** — numbered, testable, gates the phase.
- **Architecture** — package layout, interfaces, data flow.
- **Configuration extensions** — additive YAML, with defaults for backward compatibility.
- **REST APIs** — request/response shapes and status codes.
- **MCP / A2A / wire-protocol shapes** — where the phase touches a wire protocol.
- **Implementation walkthrough** — ordered steps, each landable as a single commit.
- **Test plan** — named tests, coverage targets.
- **Common pitfalls** — what the reviewer will catch.
- **Hand-off to next phase** — what the next phase inherits and starts on.

A phase is **done** when (a) all acceptance criteria pass, (b) coverage targets met, (c) `scripts/smoke/phase-N.sh` shows OK ≥ acceptance count and FAIL = 0, (d) prior phases' smokes still pass, (e) Console screens for new tenant-visible concepts ship in the same PR per §4.5.1.

---

## 10. Update cadence for this roadmap

This file is the planning anchor for V2. After each V2 phase ships:

1. Move that phase's row in §4 from "planned" to "shipped" with the merge commit hash.
2. Update §2.1 / §3 if the architecture shape evolved during implementation.
3. Update §6 risk register if a risk materialised or a new one became visible.
4. Append any V2-derived rules into `AGENTS.md` §5.x cross-cutting rules.

The roadmap is allowed to change between phases. The discipline is to change it in a PR, not to silently drift. Two material changes are already documented:

- **2026-05-12 — LLM engine swap.** `liter-llm` → Bifrost. Documented in this file's frontmatter and in Phase 13's revision header.
- **2026-05-12 — Substrate pivot.** Envoy-shaped Phase 14 + HTTP/gRPC proxy Phase 15 → Agent Profiles primitive + Phase 15 deferred + Phase 18 reshaped. Documented in this file's frontmatter and in the affected phase plans.
