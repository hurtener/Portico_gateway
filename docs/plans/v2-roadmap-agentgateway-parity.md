# Portico V2 — Agentgateway-Class Roadmap

> **Status:** Draft, 2026-05-09. Authored as a strategic planning document.
> **Scope:** Multi-phase roadmap (Phases 14–19) describing the technical path from V1.5 (end of Phase 13) to a multi-tenant, agentic-native data plane that matches `agentgateway` on capability while keeping Portico's Skill Pack runtime and tenant model as the moat.
> **Status of constituent phase plans:** `phase-14` through `phase-19` are skeleton plans authored alongside this roadmap. Each is binding for its phase and follows the structure established by `phase-0` through `phase-13`. Where this roadmap and a phase plan disagree, the **phase plan wins**; where the phase plan and the RFC disagree, the RFC wins (per `AGENTS.md` §15).

---

## 0. TL;DR

Portico V1 (Phases 0–12) is a **multi-tenant MCP gateway with a Skill Pack runtime**. V1.5 (Phase 13) adds an LLM gateway alongside it. Both ship as a single Go binary on a fixed `:8080` listener whose routing surface is hard-coded: `/mcp`, `/v1/*` (Phase 13), `/api/*`, `/*` (Console).

`agentgateway` reframes the same problem as a **full HTTP/gRPC + agentic proxy** with explicit **Bind → Listener → Route → Backend** dataflow. It can put microservice APIs and agentic traffic behind one gateway with shared policy, security, and observability. That dataflow shape — not the protocols themselves — is what makes it feel like a peer to Envoy / Kong / Tyk while still understanding MCP and A2A semantics.

V2 closes the gap **without abandoning what V1 built**:

1. **Phase 14** — refactor the boot path so the existing surfaces sit on a real `Listener / Route / Backend` substrate (no behavior change for tenants).
2. **Phase 15** — turn Portico into a real HTTP and gRPC reverse proxy on top of that substrate.
3. **Phase 16** — add A2A (Agent-to-Agent) as a peer protocol to MCP, including bridges.
4. **Phase 17** — harden against tool poisoning, supply-chain compromise, and prompt injection in tool descriptions/results.
5. **Phase 18** — expose the data plane via a structured, watchable configuration API (an xDS-shaped surface).
6. **Phase 19** — production scale-out: Postgres-default, Redis coordination, Kubernetes operator, federation, container/microVM isolation.

The **Skill Pack runtime stays the moat** through all of this. Every other gateway that competes on protocol breadth still does not bind Skills to MCP servers, tools, policies, entitlements, and UI resources. V2 makes the gateway under that runtime large enough to be the only gateway in a tenant's environment.

---

## 1. Why this roadmap exists

After Phase 13, a buyer evaluating Portico against `agentgateway` will hear:

> "Agentgateway is an open-source, agentic-native full HTTP and gRPC proxy, not just an 'AI sidecar.' You can put microservice APIs and agentic traffic behind one gateway with shared policies, security, and observability."

Portico's honest answer today is: *we are an MCP gateway and Skill runtime; for HTTP/gRPC microservice traffic you keep your existing API gateway*. That is a defensible answer for a tightly scoped V1, but it is not a winning answer in a procurement conversation where the customer is consolidating gateways.

The strategic question is: **does Portico stay a specialised MCP+Skills gateway and integrate with whatever HTTP gateway the customer already runs, or does it grow into the single gateway the customer puts all of their inbound traffic through?**

This roadmap commits to the second answer. Reasoning:

- The single biggest cost driver in MCP-era infrastructure is **session and protocol awareness** — long-lived JSON-RPC, server-initiated SSE, per-session tool virtualisation. Once you have built that substrate (Phases 0–12 did), adding "also speaks plain HTTP" is significantly cheaper than building MCP-awareness on top of an existing HTTP gateway.
- The **policy / audit / vault / telemetry / approval flow** that V1 built is gateway-shape from day one. There is no reason it should not also evaluate a route to a billing microservice, an A2A peer, or an LLM call. The data structures already model `(tenant, session, request, decision, evidence)` generically; the protocol is the only variable.
- The **operator UX work** (Phases 7, 9, 10.5–10.9) gave Portico a Console that already surfaces servers, skills, tenants, secrets, audit, snapshots, and approvals. Adding HTTP routes and backends to that Console is a smaller lift than building a Console from scratch on top of a different gateway.
- **One artifact to ship and run.** Operators want one binary, one DB, one configuration model, one RBAC model, one log stream, one trace store. Portico already optimises for that. V2 protects that property — adding listeners and protocols, not adding deployable processes.

The roadmap deliberately **overengineers the substrate (Phase 14)** so that adding HTTP, gRPC, A2A, and future protocols is a driver-shaped change rather than a refactor. The §4.4 extensibility-seam pattern from `AGENTS.md` is the rule the substrate is designed around.

---

## 2. What `agentgateway` actually is, and the gap

### 2.1 Their dataflow

```
Bind  →  Listener  →  Route  →  Backend
:8080    /mcp        /tools/*   github-mcp
         /api        /billing   billing-svc.cluster.local:80
         /a2a        /agents/*  research-agent.example.com
```

A **Bind** is `host:port + TLS context`. A **Listener** is a logical surface attached to a bind, with a protocol type (`http`, `mcp`, `a2a`, `grpc`). **Routes** are matching rules (path, method, header, host, JSON-RPC method body) that dispatch to **Backends** (concrete upstream services with health, retry, lb, auth strategy).

This is the Envoy / Kong shape, except the Listener and Route layers know about MCP and A2A.

### 2.2 Their five capability claims and our current standing

| Capability                                                                 | Portico V1.5 status                                                                 |
|----------------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| Stateful JSON-RPC sessions with long-lived connections                     | ✅ Northbound HTTP+SSE in Phase 1; server-initiated correlator in Phase 5            |
| Session fan-out across multiple MCP servers                                | ✅ Aggregator + namespacing in Phase 1, snapshots in Phase 6                         |
| Bidirectional: servers can push events (SSE) to clients                    | ✅ `internal/mcp/northbound/http/server_initiated.go`                                |
| Protocol-aware routing that understands JSON-RPC message bodies            | ✅ Dispatcher inspects `method` / `params` to route to namespaced server             |
| Dynamic tool virtualization on a per-client basis                          | 🟡 Snapshots are per-session; per-client virtualisation depends on Phase 5 scopes    |
| Multiplexing & fan-out                                                     | ✅ Built into the aggregator                                                         |
| Server-initiated events                                                    | ✅ See above                                                                         |
| Protocol negotiation (graceful upgrade/fallback)                           | 🟡 Capability negotiation present; transport-level fallback (WebSocket etc.) absent  |
| Per-session authorization                                                  | ✅ Phase 5 scopes + policy engine                                                    |
| Tool poisoning protection                                                  | 🟡 Drift detection (Phase 6) present; signing/attestation/scanning absent            |
| **General HTTP and gRPC reverse proxy**                                    | ❌ Not a feature                                                                     |
| **A2A protocol**                                                           | ❌ Not a feature                                                                     |
| **Multiple listeners / dynamic routes**                                    | ❌ One fixed listener, hard-coded route table                                        |
| **xDS-style dynamic configuration API**                                    | ❌ Hot-reload from YAML only                                                         |

The four ❌ items are V2 work. The two 🟡 items get hardened along the way.

### 2.3 What we have that they do not (and we keep)

These are non-negotiable through V2 — they are why a tenant chooses Portico over a generic agentic gateway:

1. **Skill Pack runtime.** Manifests, JSON-Schema validator, virtual directory, source drivers, per-session enablement, MCP-primitive surfacing. Phase 4 + Phase 8.
2. **Multi-tenancy from day one.** `tenant_id` on every row, every span, every audit event, every process key. RFC §6.2.
3. **Catalog snapshots.** Stable per-session view with drift detection and reproducibility for audit. Phase 6.
4. **Headless approval flow.** Elicitation + structured-error fallback; the host renders, Portico does not. RFC §6.6.
5. **Vault-backed credential injection** with OAuth token exchange and explicit, audited passthrough. Phase 5.
6. **Tenant-aware process supervisor** with `shared_global / per_tenant / per_user / per_session / remote_static` modes. Phase 2.
7. **Operator Console** that already covers registry, skills, tenants, secrets, policy, snapshots, audit, approvals, playground. Phases 7–10.

V2's job is to make all of these apply to **every** request that crosses the gateway — REST, gRPC, A2A, MCP, LLM — not just MCP.

---

## 3. V2 architecture sketch

### 3.1 The data plane after Phase 14

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Portico Binary                              │
│                                                                          │
│   ┌────────────────── Control Plane (unchanged shape) ─────────────────┐ │
│   │  Tenancy │ Auth │ Vault │ Policy │ Approvals │ Audit │ Telemetry  │ │
│   │  Registry │ Skills │ Snapshots │ Catalog Resolver │ Console        │ │
│   └────────────────────────────────────────────────────────────────────┘ │
│                                  │                                       │
│                                  ▼                                       │
│   ┌─────────────────────────── Data Plane ──────────────────────────────┐│
│   │                                                                     ││
│   │   ┌── Bind :8080 ─────────────────────────────────────────────────┐ ││
│   │   │  Listener "default-mcp"   (proto=mcp)    →  routes_mcp[]      │ ││
│   │   │  Listener "default-rest"  (proto=http)   →  routes_rest[]     │ ││
│   │   │  Listener "default-a2a"   (proto=a2a)    →  routes_a2a[]      │ ││
│   │   │  Listener "default-llm"   (proto=openai) →  routes_llm[]      │ ││
│   │   │  Listener "console"       (proto=spa)    →  embedded SPA      │ ││
│   │   └───────────────────────────────────────────────────────────────┘ ││
│   │                                                                     ││
│   │   ┌── Bind :8443 (TLS) ───────────────────────────────────────────┐ ││
│   │   │  …same listeners, terminated TLS                              │ ││
│   │   └───────────────────────────────────────────────────────────────┘ ││
│   │                                                                     ││
│   └─────────────────────────────────────────────────────────────────────┘│
│                                  │                                       │
│                                  ▼                                       │
│   ┌────────────────────────── Backends ─────────────────────────────────┐│
│   │  mcp_stdio │ mcp_http │ http_proxy │ grpc_proxy │ a2a_peer │ llm   ││
│   └─────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────┘
```

**The key invariants**:

- **Every backend driver is registered through the §4.4 seam pattern**: interface in `internal/dataplane/backends/ifaces`, drivers in `internal/dataplane/backends/<driver>`, blank-imported from `cmd/portico`. The control plane never imports a concrete backend.
- **Every request — regardless of listener protocol — passes through the same middleware chain**: tenant resolution → JWT → policy → audit → telemetry → approval routing → backend dispatch → response → audit close → telemetry close.
- **The MCP surface is just one listener** in the new world; Phase 13's `/v1/*` is just another listener; the Console SPA is just another listener.

### 3.2 What Phase 14 explicitly does *not* change

- The MCP wire shape (`internal/mcp/protocol`) is untouched.
- The Skill Pack runtime is untouched.
- The vault, policy, approval, audit, telemetry, snapshot, and registry packages are untouched.
- The `portico.yaml` shape is **additive**: existing configs continue to work; a default listener stanza is synthesised when the operator does not specify one.
- The single-binary, CGo-free, pure-Go invariant is untouched.

This is a refactor on the *boot and dispatch* path, not a rewrite. The bar is "Phase 12 / 13 acceptance criteria still pass against the V2 binary, byte for byte where reasonable."

---

## 4. Phase order (V2)

| #  | Plan                                                                       | Phase summary                                                                                                                                                                              |
|----|----------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 14 | [phase-14-listener-route-backend-substrate.md](./phase-14-listener-route-backend-substrate.md) | Formalise `Bind / Listener / Route / Backend`. Migrate every existing surface (`/mcp`, `/v1`, `/api`, Console) onto the new substrate. No new protocols; no new tenant-visible features.    |
| 15 | [phase-15-http-grpc-proxy.md](./phase-15-http-grpc-proxy.md)               | Add `http_proxy` and `grpc_proxy` backend drivers. Routes can target arbitrary REST / gRPC services. Per-route policy, auth strategy, audit, telemetry; upstream TLS, health, retry, circuit-break, load-balance. |
| 16 | [phase-16-a2a-protocol.md](./phase-16-a2a-protocol.md)                     | A2A (Agent-to-Agent) northbound + southbound. `a2a_peer` backend. Agent-card discovery surfaced through the existing catalog model. Bridges between A2A and MCP where useful.            |
| 17 | [phase-17-tool-poisoning-defense.md](./phase-17-tool-poisoning-defense.md) | Schema attestation, drift gates (block, not just detect), prompt-injection scanning of tool descriptions and results, supply-chain pinning for skill sources, optional sigstore-style signing. |
| 18 | [phase-18-dynamic-config-api.md](./phase-18-dynamic-config-api.md)         | Structured CRUD over listeners / routes / backends with watch semantics; optional Envoy ADS bridge. Hot-reload from YAML stays; YAML becomes one of several producers of dataplane state. |
| 19 | [phase-19-production-scale-out.md](./phase-19-production-scale-out.md)     | Postgres-default, Redis coordination, Kubernetes operator + Helm chart, federation across instances, container/microVM isolation modes. The post-V1 list from the RFC §15 boundary lands. |

### 4.1 Why this order

- **14 before 15** — adding HTTP proxying before formalising the substrate is how gateways grow into spaghetti. Pay the substrate cost once.
- **15 before 16** — HTTP is a vastly larger feature surface than A2A and exposes more substrate edge cases. Get those bugs out before the new wire protocol.
- **16 before 17** — tool poisoning defence has to apply to MCP *and* A2A tool surfaces; building it once for both is cheaper than retrofitting.
- **17 before 18** — the dynamic configuration API is a privileged write surface. Land tool-level defence before letting external systems push data-plane state.
- **18 before 19** — federation and multi-instance coordination are dramatically simpler when there is already one structured channel for data-plane state changes.

### 4.2 Each phase respects the existing contracts

Every V2 phase preserves:

- **All `AGENTS.md` rules**, including §4.1 preflight gate, §4.2 phase implementor contract, §4.4 extensibility seams, §4.5 frontend conventions.
- **Multi-tenancy invariants** (§6 of `AGENTS.md`).
- **Security non-negotiables** (§7).
- **One source of truth** for MCP types (`internal/mcp/protocol`) — A2A gets its own equivalent; nothing shadows MCP types.
- **The §13 forbidden-practices list** is updated, not relaxed, by each phase.

---

## 5. Cross-cutting design rules for V2

These are binding for every V2 phase and will be added to `AGENTS.md` when Phase 14 lands.

### 5.1 Listeners are not routes

A listener owns the wire (transport, TLS, protocol decoder). A route owns the semantic dispatch (this method+path → that backend, with this policy and audit envelope). Mixing the two is the fastest path to a configuration model nobody can reason about. Phase 14 enforces the separation in code (different packages, different interfaces, different config sub-schemas) and in the Console.

### 5.2 Every route inherits the V1 envelope

A route's request lifecycle is:

```
1.  TLS termination (if listener.tls)
2.  Tenant resolution                         (Phase 0)
3.  JWT validation + scope extraction         (Phase 0/5)
4.  Per-route auth-strategy injection         (Phase 5/15)
5.  Policy evaluation                         (Phase 5)
6.  Approval-flow short-circuit if required   (Phase 5)
7.  Tracing span open                         (Phase 6)
8.  Audit event open                          (Phase 5)
9.  Backend dispatch                          (Phase 14 + driver)
10. Backend response
11. Per-route response transformation         (Phase 15)
12. Audit event close (with redaction)        (Phase 5)
13. Tracing span close                        (Phase 6)
14. Quotas / cost ledger update               (Phase 13/15)
```

Steps 1–3 and 5–14 are protocol-agnostic — they do not care whether the request was MCP, HTTP, gRPC, A2A, or LLM. **Adding a new protocol means adding step 4 and step 9 for that protocol; nothing else moves.** This is the litmus test for whether Phase 14's substrate is shaped right.

### 5.3 Backends are drivers, not subclasses

Each backend type (`mcp_stdio`, `mcp_http`, `http_proxy`, `grpc_proxy`, `a2a_peer`, `llm`) is a driver that satisfies the `backends.Driver` interface. Drivers self-register from `init()`, and the factory dispatches by name — same shape as `internal/storage` and `internal/skills/source`. **No production code outside `cmd/portico` and the driver's own tests imports a concrete driver.**

### 5.4 Configuration evolves additively

Existing `portico.yaml` files keep working. A V2 config without a `listeners:` block synthesises:

```yaml
listeners:
  - name: default
    bind: { host: 127.0.0.1, port: 8080 }
    routes:
      - { match: { path_prefix: /mcp     }, backend: mcp-aggregator }
      - { match: { path_prefix: /v1      }, backend: llm-gateway     }   # if Phase 13 enabled
      - { match: { path_prefix: /api     }, backend: control-plane   }
      - { match: { path_prefix: /        }, backend: console-spa     }
```

The synthesis is documented in Phase 14. The operator can override any part of it without re-declaring the rest.

### 5.5 Dynamic config never bypasses the static guarantees

When Phase 18 lands the dynamic configuration API, **every change still goes through the same validation, policy, and audit stack as a YAML reload.** Validation does not skip because a write came in over the API. Policy decisions about who may push routes are themselves policy rules. Audit captures the actor, the diff, and the resulting state.

### 5.6 The Console keeps up

Every V2 phase that adds a tenant-visible concept (HTTP routes, A2A peers, attestation signatures) gets Console screens **in the same phase**. The §4.5.1 operator UX gates apply. A backend type that the operator can create gets a `+ Add` CTA. Forms cover the full plan-defined surface. Playwright spec ships with the route.

### 5.7 The smoke surface keeps up

Every V2 phase ships its `scripts/smoke/phase-N.sh` per `AGENTS.md` §4.2. Earlier phases' smokes keep passing. A phase that adds endpoints without a smoke check is rejected.

---

## 6. Risk register

These are the risks the V2 roadmap inherits, with mitigations.

| Risk                                                                                       | Mitigation                                                                                                                                                  |
|--------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Phase 14 refactor breaks Phase 1–13 acceptance criteria                                    | Phase 14 acceptance is "every prior phase's smoke and `make preflight` still pass." Phase 14 does not ship until that gate is green.                        |
| HTTP proxy introduces a generic CVE surface (HRS, request smuggling, header injection)     | Phase 15 mandates `net/http` standard server with strict header validation; reject ambiguous transfer encodings; integration tests for known smuggling shapes. |
| A2A spec churns                                                                            | Phase 16 pins to a published spec version (the same way `internal/mcp/protocol/types.go` pins MCP). Spec bumps are a future RFC, not an in-phase change.    |
| Tool poisoning defence creates false positives that break legitimate MCP tools             | Phase 17 ships every defence in `audit-only` mode first; `enforce` mode is opt-in per route or per server, configurable per tenant.                          |
| Dynamic config API becomes a privileged-write attack surface                               | Phase 18 requires `admin` scope + per-tenant rate limits; every write is policy-evaluated and audit-logged with the diff; signed-write mode optional.        |
| Federation in Phase 19 reintroduces the cross-tenant leakage class                         | Phase 19 carries a federation-specific cross-tenant integration test suite; tenants are first-class in every federated message.                              |
| The substrate makes the binary slow                                                        | Each phase carries a microbenchmark gate: median request latency may not regress more than 5% vs. Phase 13 baseline at fixed traffic shape.                 |

---

## 7. V1 / V1.5 / V2 boundary

| Boundary       | What ships                                                                                                                                                      |
|----------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **V1** (end of Phase 12) | Multi-tenant MCP gateway, Skill runtime, Console, observability, conformance. Single `:8080` listener.                                              |
| **V1.5** (end of Phase 13) | + LLM gateway northbound, tool-use bridging, provider/model registry, quotas, costs.                                                              |
| **V2** (end of Phase 19) | Multi-listener, multi-protocol gateway: REST + gRPC + MCP + A2A + LLM. Tool poisoning defence. Dynamic data-plane config. Production scale-out (Postgres / Redis / Kubernetes / federation). |

V2 is *additive*: a V1 deployment continues to work against the V2 binary if the operator does not enable any new listeners, backends, or scale-out modes. Phase 14 preserves backward compatibility by construction; subsequent phases default new features off.

---

## 8. Things deliberately NOT in V2

Items that stay deferred:

- **Workflow engine for non-AI workloads.** Portico is still an *agentic* gateway; it does not become an iPaaS.
- **A planner / agent framework.** RFC §5 still applies.
- **A model fine-tuning surface.** Phase 13's LLM gateway delivers inference, not training.
- **Hosted SaaS Portico.** The roadmap protects self-host as a first-class deployment shape; hosted is a separate product decision.
- **Replacement of MCP or Skills with a Portico-proprietary protocol.** Both specs stay open; Portico remains a runtime *for* them.
- **A built-in identity provider.** Portico continues to consume external IDPs via JWT/JWKS. Phase 19 may add SSO conveniences for the Console; the IDP itself stays external.
- **A built-in service mesh.** Portico is a north-south gateway, not east-west. Mesh-class features (mTLS between every pod, sidecar injection, traffic mirroring) stay out of scope.

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
2. Update §2.2 / §3.1 if the substrate shape evolved during implementation.
3. Update §6 risk register if a risk materialised or a new one became visible.
4. Append any V2-derived rules into `AGENTS.md` §5.x cross-cutting rules.

The roadmap is allowed to change between phases. The discipline is to change it in a PR, not to silently drift.
