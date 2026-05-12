# Phase 16 — A2A Protocol Support

> Self-contained implementation plan. Builds on Phase 14 substrate and Phase 15 reverse-proxy machinery. Adds Agent-to-Agent (A2A) as a peer protocol to MCP: northbound listener for inbound A2A traffic, `a2a_peer` backend driver for outbound A2A calls, agent-card discovery surfaced through Portico's existing catalog model, and bridges between A2A skills/tasks and MCP tools where useful.

## Goal

After Phase 16, Portico is fluent in two agentic wire protocols: MCP (V1) and A2A. Both share the V2 envelope (tenancy → JWT → policy → audit → tracing → backend dispatch). Operators can:

1. Expose Portico as an A2A endpoint that other agents can call (northbound listener).
2. Register external A2A peers as backends, route to them, attach egress credentials, observe traffic in the audit/trace/Console pipeline.
3. Discover agent cards (A2A's analog of MCP's `tools/list`) for any registered peer; surface the discovered tasks/skills in the catalog alongside MCP tools.
4. Optionally bridge: an MCP `tools/call` that names an A2A-backed task transparently dispatches over A2A; an inbound A2A request that names an MCP-backed tool transparently dispatches over MCP.

The phase pins to a published A2A spec version (the same way `internal/mcp/protocol/types.go` pins MCP). Spec bumps are a future RFC, not an in-phase change.

## Why this phase exists

`agentgateway` lists A2A as a first-class protocol. A buyer comparing Portico against agentgateway after Phases 14–15 will ask: "what about A2A?" The answer cannot be "soon" if the agentic gateway story is to land. Phase 16 makes A2A a peer to MCP in Portico's protocol stack.

A2A and MCP are intentionally similar in spirit (long-lived, JSON-RPC-shaped, capability-based) but different in primitives (A2A speaks of `tasks` and `agent cards`; MCP speaks of `tools`, `resources`, `prompts`). Adding A2A is therefore both a substrate exercise (does Phase 14 hold up under a second JSON-RPC wire shape?) and a model-design exercise (how do A2A primitives map onto Portico's existing catalog?).

The bridge work is what makes A2A worth shipping in the same gateway as MCP. Without it, A2A and MCP are just two unrelated traffic classes that happen to share auth and audit. With it, an operator can expose a single namespaced catalog where some tools resolve over MCP and others over A2A, and the calling agent does not need to know — exactly the vendor-lock-in argument Portico is built to defeat.

## Prerequisites

- Phase 14 substrate (`Bind / Listener / Route / Backend`) landed.
- Phase 15 `http_proxy` + `grpc_proxy` (templates for `a2a_peer`).
- Phase 5 vault and OAuth machinery (A2A peer auth reuses them).
- Phase 6 telemetry (A2A spans use the same exporter).
- Catalog snapshots (Phase 6) — extended to include A2A tasks/skills.
- Approval flow (Phase 5) — extended to wrap A2A task invocations.

## Out of scope (explicit)

- **No A2A as the *only* protocol.** Portico continues to ship MCP first. A2A is additive.
- **No A2A protocol extensions.** Portico implements the published spec; experimental extensions stay out.
- **No A2A-specific UI vocabulary.** A2A tasks/skills surface in the existing catalog vocabulary as `kind: a2a_task` rows alongside `kind: mcp_tool` rows. No new top-level navigation entry.
- **No multi-A2A-version bridging.** If two peers speak different A2A versions, the operator must run two separate backends and route accordingly. Auto-translation is post-V2.
- **No replacement of MCP-side primitives with A2A-side ones.** Skill Packs continue to bind against MCP tools; A2A tasks are exposed in parallel. A future Skill Pack v2 may bind A2A tasks; that is post-V2 and gated behind an RFC.

## Deliverables

1. **`internal/a2a/protocol/`** — wire types for the pinned A2A spec version. Same shape as `internal/mcp/protocol/`: types in `types.go`, methods in `methods.go`, errors in `errors.go`, capabilities in `capabilities.go`, agent cards in `agent_cards.go`. Single source of truth — no other package defines A2A messages.
2. **`internal/a2a/northbound/http/`** — northbound A2A transport. HTTP+SSE the same way MCP does it. The listener kind that uses this lives at `internal/dataplane/listeners/a2a/`.
3. **`internal/a2a/southbound/`** — A2A client implementation. Mirrors `internal/mcp/southbound/` shape: `southbound.Client` interface in `types.go`, an HTTP client in `http/`, a manager that pools clients per (tenant, peer) in `manager/`.
4. **`internal/dataplane/listeners/a2a/`** — A2A listener kind. Decodes A2A wire shape, applies the route table, and dispatches.
5. **`internal/dataplane/backends/a2a_peer/`** — backend driver. Wraps `southbound.Client`. Same retry/health/transform shape as `http_proxy` (Phase 15).
6. **Agent-card ingestion** — when an `a2a_peer` backend is registered, Portico fetches the peer's agent card on a schedule and persists it in the catalog. The catalog gains a `discovered_capabilities` view per backend.
7. **Catalog extension** — `catalog/snapshots` (Phase 6) extends to include A2A tasks/skills as first-class catalog rows. The snapshot's drift detector treats agent-card changes the same way it treats MCP `list-changed` events.
8. **MCP↔A2A bridges** — opt-in, per-route. The `mcp` listener can be configured with a "bridge" route that dispatches a `tools/call` for a named tool to an A2A backend. The `a2a` listener can be configured with a bridge route that dispatches a task to an MCP backend. Bridges live in `internal/dataplane/bridges/` (one file per bridge direction).
9. **Skill Pack metadata extension** — Skill manifests gain an optional `a2a:` block describing A2A tasks the skill needs (mirror of `required_tools`). Phase 16 *parses* the new field but does not *enforce* it; enforcement is a Skill Pack v2 work item out of scope here.
10. **Approval flow integration** — A2A task invocations that match a `requires_approval` policy go through the same elicitation + structured-error flow as MCP tool calls. The approval payload schema gains an `a2a_task` discriminator alongside `mcp_tool`.
11. **Console screens** — `/a2a/peers` (list), `/a2a/peers/[id]` (agent card view, recent calls, health), `/a2a/peers/new` (create-peer form). Catalog rows for A2A tasks render in the existing snapshot/catalog views with a `[A2A]` badge.
12. **Smoke** — `scripts/smoke/phase-16.sh` covers the inbound A2A `tasks/list` and a unary task invocation; the outbound peer registration + task dispatch; and one bridge end-to-end (MCP `tools/call` → A2A peer).
13. **Mock A2A peer** — `examples/servers/mock/cmd/mocka2a/` for integration tests.

## Acceptance criteria

1. **Inbound A2A `agent_card` and `tasks/list` work.** A standard A2A client can fetch Portico's agent card and list available tasks.
2. **Outbound peer registration.** A `POST /api/a2a/peers` registers a peer; Portico fetches the peer's agent card; the discovered tasks appear in the catalog within 5 s.
3. **Outbound task dispatch.** An inbound A2A request that targets a registered peer's task dispatches successfully; the response is returned verbatim; a span records `a2a.peer_id`, `a2a.task_id`, `a2a.duration_ms`.
4. **Approval flow.** A task with risk class `external_side_effect` triggers the approval flow; elicitation is sent to the calling A2A client when capability negotiation succeeds; otherwise a structured `approval_required` error is returned.
5. **Per-tenant peer isolation.** Tenant A's registered peer is not visible to tenant B. Cross-tenant queries fail.
6. **Egress auth.** A peer configured with `egress_auth: bearer_from_vault` attaches the bearer to outbound A2A calls. The inbound A2A `Authorization` is not forwarded.
7. **MCP↔A2A bridge — outbound side.** A bridge route configured as `mcp tool github.code-review.run → a2a peer research-agent.task code-review` dispatches the MCP tool call as an A2A task and returns the A2A result formatted as an MCP `tools/call` response.
8. **MCP↔A2A bridge — inbound side.** A bridge route configured as `a2a task billing.refund → mcp tool billing.refund` dispatches the A2A task as an MCP tool call and returns the MCP result formatted as an A2A task response.
9. **Drift detection.** When the peer's agent card changes between two snapshots (a task added, removed, or schema-changed), Portico emits a `catalog.drift` event with `kind: a2a_task` and the diff.
10. **Server-initiated SSE.** A2A server-initiated events (status updates on long-running tasks) flow back through the listener's SSE channel and reach the calling client. Tested against the mock A2A peer's `slow_task`.
11. **Spec pinning.** `internal/a2a/protocol/version.go::SpecVersion` is the single source of truth; bumping it is an RFC change.
12. **No CGo introduced.** Build is `CGO_ENABLED=0`. Binary size delta vs. Phase 15 ≤ +15 MB.
13. **Smoke gate.** `scripts/smoke/phase-16.sh` shows OK ≥ 14, FAIL = 0; prior phases' smokes still pass.
14. **Coverage.** `internal/a2a/...` ≥ 80%; `internal/dataplane/backends/a2a_peer/` ≥ 85%; `internal/dataplane/bridges/` ≥ 85%.

## Architecture

### 6.1 Package layout

```
internal/a2a/
├── protocol/
│   ├── version.go           # SpecVersion = "..."
│   ├── types.go
│   ├── methods.go
│   ├── errors.go
│   ├── capabilities.go
│   └── agent_cards.go
├── northbound/http/
│   ├── transport.go
│   └── server_initiated.go
└── southbound/
    ├── types.go             # southbound.Client interface
    ├── http/
    │   └── client.go
    └── manager/
        └── pool.go

internal/dataplane/
├── listeners/a2a/
│   └── listener.go
├── backends/a2a_peer/
│   └── backend.go
└── bridges/
    ├── mcp_to_a2a.go        # outbound MCP → A2A bridge
    └── a2a_to_mcp.go        # outbound A2A → MCP bridge

internal/catalog/
└── snapshots/
    └── a2a.go               # extension: A2A tasks in snapshots

internal/server/api/
├── handlers_a2a_peers.go
└── handlers_a2a_peers_test.go
```

### 6.2 Wire-type discipline

`internal/a2a/protocol` is the single source of truth for A2A wire shapes. Other packages import it; nothing else defines A2A message structs. This mirrors the rule for MCP types in `AGENTS.md` §13. The forbidden-practices list gains an entry: "Adding a third place to define A2A message types."

### 6.3 Catalog model

Catalog rows already carry `kind` (`mcp_tool`, `mcp_resource`, `mcp_prompt`, `mcp_app`). Phase 16 adds `a2a_task` and `a2a_skill`. Snapshot diffs treat new/removed/changed `a2a_task` rows the same way they treat MCP tools.

The catalog row's `provider` field gains a discriminator: an MCP tool's provider is the registered MCP server; an A2A task's provider is the registered A2A peer. Per-session enablement, policy matching, and audit attribution all key off `(provider_kind, provider_id)`.

### 6.4 Bridges

A bridge is a route on listener X that dispatches to backend Y of a different protocol family. The bridge code translates the protocol envelope:

- **MCP→A2A**: `tools/call(name, args)` → `tasks/invoke(task_id, parameters)` based on a route-defined name mapping. The result's content type is preserved; if the A2A response is a structured payload, it is wrapped in an MCP `content[]` array as appropriate.
- **A2A→MCP**: `tasks/invoke` → `tools/call`. Same translation in reverse.

Bridges are explicit and per-route. There is no automatic bridging discovery in Phase 16; the operator declares each bridge route. (Auto-bridging based on naming conventions is a Phase 17/18 ergonomics question.)

### 6.5 Approval flow extension

The approval payload Phase 5 emits gains an explicit shape:

```json
{
  "kind": "approval_required",
  "subject": { "discriminator": "a2a_task", "task_id": "...", "peer_id": "...", "args_summary": "..." },
  "risk_class": "...",
  "rationale": "..."
}
```

(The MCP shape has the same envelope with `discriminator: "mcp_tool"`.) Hosts that implement elicitation render either; hosts that don't surface the structured error and let the user decide.

## Configuration extensions

```yaml
# A2A peer backend
backends:
  - name: research-agent
    driver: a2a_peer
    config:
      url: https://research-agent.example.com/a2a
      agent_card_refresh: 5m
      egress_auth: { kind: bearer_from_vault, vault_ref: secrets/research-agent }
      retry: { attempts: 2, per_try_timeout: 5s }
      health: { interval: 10s }

# A2A listener
listeners:
  - name: a2a-public
    bind: tls
    protocol: a2a
    routes:
      - match: { path_prefix: /a2a }
        backend: a2a-aggregator   # an a2a_peer-like backend that wraps Portico's own task router

# MCP→A2A bridge route
listeners:
  - name: default-mcp
    bind: default
    protocol: mcp
    routes:
      - match: { jsonrpc_method: tools/call, tool_name: github.code-review.run }
        bridge:
          to: a2a_peer
          backend: research-agent
          task: code-review
      - match: { path_prefix: /mcp }
        backend: mcp-aggregator
```

The `bridge:` field on a route is mutually exclusive with `backend:`; validation enforces.

## REST APIs

| Method | Path                              | Body / params                | Returns                          |
|--------|-----------------------------------|------------------------------|----------------------------------|
| GET    | `/api/a2a/peers`                  | -                            | array                            |
| POST   | `/api/a2a/peers`                  | peer spec                    | 201 + peer                       |
| GET    | `/api/a2a/peers/{id}`             | -                            | peer + agent card                |
| PUT    | `/api/a2a/peers/{id}`             | peer spec                    | 200 + peer                       |
| DELETE | `/api/a2a/peers/{id}`             | -                            | 204                              |
| POST   | `/api/a2a/peers/{id}/refresh-card`| -                            | 202                              |
| GET    | `/api/a2a/peers/{id}/health`      | -                            | health snapshot                  |

## Implementation walkthrough

1. **Wire types (`internal/a2a/protocol`).** Define and unit-test against the pinned spec.
2. **Mock A2A peer.** `examples/servers/mock/cmd/mocka2a/` returns a deterministic agent card and serves a few tasks (one fast, one slow with SSE updates).
3. **Northbound listener.** Implement the A2A listener kind; wire to the Phase 14 substrate; smoke a `tasks/list` against the mock as the source of truth (Portico itself acts as a passthrough until the agent-card aggregator lands).
4. **`a2a_peer` backend.** Implement the driver; reuse Phase 15 retry/health/transform shapes.
5. **Agent-card aggregator.** Fetch peers' cards on a schedule; persist; expose via REST + Console.
6. **Catalog extension.** Wire `kind: a2a_task` rows into snapshots; drift detector picks up changes.
7. **Bridges.** MCP→A2A first (outbound), then A2A→MCP (inbound). Each bridge has its own integration test against `mockmcp` + `mocka2a`.
8. **Approval flow extension.** Update payload shape; wire approval engine to recognise A2A subjects.
9. **REST + Console.** CRUD endpoints + create form + peer detail page; Playwright spec.
10. **Smoke + microbench.** `phase-16.sh`; perf gate (A2A request overhead within ±5% of MCP).

## Test plan

Unit:

- `TestA2AProtocol_AgentCard_RoundTrip`
- `TestA2AProtocol_TasksList_RoundTrip`
- `TestA2AProtocol_TasksInvoke_Unary`
- `TestA2ASouthbound_HTTPClient_Connect`
- `TestA2ASouthbound_PoolReusesConnection`
- `TestA2APeerBackend_Dispatch`
- `TestA2APeerBackend_RetryOnUnavailable`
- `TestBridge_MCPtoA2A_TranslatesArgsAndResult`
- `TestBridge_A2AtoMCP_TranslatesArgsAndResult`
- `TestApproval_A2ATask_ElicitationPayloadShape`

Integration:

- `TestE2E_A2A_NorthboundTasksList`
- `TestE2E_A2A_OutboundTaskDispatch_Roundtrip`
- `TestE2E_A2A_AgentCardIngestion_AppearsInCatalog`
- `TestE2E_A2A_Drift_DetectsTaskAddedRemoved`
- `TestE2E_A2A_ServerInitiatedSSE_DeliversUpdates`
- `TestE2E_Bridge_MCPtoA2A_GithubCodeReviewRoute`
- `TestE2E_Bridge_A2AtoMCP_BillingRefundRoute`
- `TestE2E_A2A_PolicyDeny_BlocksTask`
- `TestE2E_A2A_MultiTenantPeerIsolation`

## Common pitfalls

1. **Defining A2A types in two places.** §13 forbidden-practices entry. Single source of truth in `internal/a2a/protocol`.
2. **Letting A2A bypass the V2 envelope.** A2A is just another protocol on the substrate. Tenancy/JWT/policy/audit/tracing apply identically.
3. **Treating bridges as automatic.** Bridges are explicit, per-route, validated. Auto-bridging based on name guessing is a footgun.
4. **Forgetting drift detection for A2A.** A peer that quietly changes its agent card is the A2A version of MCP tool drift. Catalog snapshots must catch it.
5. **Approval-payload schema drift.** The discriminator (`mcp_tool` / `a2a_task`) is the seam. Adding a new subject kind without updating the approval renderer breaks the host's UX.
6. **Pinning to a moving spec.** `SpecVersion` is a constant. RFC bump first; code change after.

## Hand-off to Phase 17

Phase 17 inherits A2A as a second tool surface that needs poisoning defence. Every defence Phase 17 implements (schema attestation, drift gates, prompt-injection scanning) applies to both `mcp_tool` and `a2a_task` rows in the catalog. The discriminator the catalog already carries is the seam for that.
