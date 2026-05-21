# Phase 18 — Dynamic Configuration API (GitOps + Watch over existing CRUD)

> Self-contained implementation plan. Builds on Phases 14 (Agent Profiles), 15.5 (Virtual Keys + Budgets + Cache), 16 (A2A), and 17 (security policies). **Reshaped 2026-05-12** when the V2 line dropped the Envoy-shaped substrate. This phase no longer ships a Listener/Route/Backend CRUD or an Envoy ADS adapter; it ships a structured, watchable, auditable CRUD over the **Portico resource model** (Profiles, VKs, Teams, Customers, Budgets, Servers, Skills, Policies, A2A peers, Security configs).

## Goal

After Phase 18, every operator-visible resource Portico exposes has two equally first-class producers:

1. **`portico.yaml`** (static) — the existing source of truth for cold-start and human-edited deployments.
2. **The dynamic configuration API** — a structured CRUD surface plus a watch channel; operator tooling, GitOps controllers, and Phase 19's Kubernetes operator push state through it.

Both producers write to the same in-memory state. Both go through the same validation, policy, audit, and security gating (Phase 17). The Console becomes a third client of the same API; nothing privileged.

Crucially, the **resources** the API targets are Portico's own (Agent Profile, Virtual Key, Team, Customer, Budget, MCP Server, Skill, Skill Source, Policy Rule, A2A Peer, Security Config). There is no Listener / Route / Backend resource type — those don't exist (see [v2-roadmap-agentgateway-parity.md](./v2-roadmap-agentgateway-parity.md) §0 for the substrate pivot). There is **no Envoy ADS adapter**.

The API is intentionally Kubernetes-adjacent in shape (resource types, named references, watch semantics, optimistic concurrency, bulk apply) so Phase 19's K8s operator is a thin translator, not a full reimplementation.

## Why this phase exists

Phase 14 introduced Agent Profiles. Phase 15.5 introduced VKs + budgets + cache. Phase 16 added A2A peers. Phase 17 added security configs. All of these are operator-facing resources today reachable via `portico.yaml` reload + Console + REST CRUD endpoints scattered across `/api/*`. That works for human operators editing one box. It does not scale to:

- A GitOps workflow where a controller in a CI pipeline rolls out Profile/VK/Budget templates, with zero-downtime apply across a fleet.
- A Kubernetes operator (Phase 19) reconciling `AgentProfile` / `VirtualKey` / `Budget` / `Server` / `Skill` / `Policy` CRDs onto Portico instances.
- A federation deployment (Phase 19) where a leader pushes resource changes to followers.
- An external automation suite (e.g. an internal developer portal) registering/deregistering resources as the org chart changes.

Any of these can today script `portico` invocations or YAML edits. None gets a *transactional*, *validated*, *audited*, *watch-driven* surface. Phase 18 builds that surface once, so every later integration consumes it.

This phase also unifies the per-resource CRUD endpoints introduced piecemeal in Phases 0–17 into a coherent `/api/v1/{resource}/...` shape, plus a shared watch channel.

## Prerequisites

- Phase 14 (Agent Profile resource + repo).
- Phase 15.5 (VK, Team, Customer, Budget, Cache config resources + repos).
- Phase 16 (A2A Peer resource + repo).
- Phase 17 (Security policies, attestation configs, drift gates + repos).
- Phase 5 audit + redactor (every write produces an audit event with the diff).
- Phase 5 policy engine (write authorisation goes through policy).

## Out of scope (explicit)

- **No Listener / Route / Backend resource types.** They don't exist in Portico (substrate dropped 2026-05-12). This phase does not introduce them via the API either.
- **No Envoy ADS / xDS adapter.** Originally part of this phase; dropped 2026-05-12. Portico does not project state via xDS.
- **No full CRD-style schema evolution machinery.** Resource schemas live in Go structs; non-additive changes require a versioned API path. We do not build an OpenAPI CRD versioning layer; we use one versioned API path (`/api/v1/...`) and additive evolution only within it.
- **No multi-version API.** The Phase 18 API ships at `v1`. A future `v2` (post-V2) would live at `/api/dataplane/v2/...`; `v1` stays.
- **No GraphQL.** REST + SSE only. Explicitly rejected.
- **No webhook outputs.** Watch is consumed via SSE on a long-lived HTTP connection. Outbound webhooks (Portico calling external systems on changes) are post-V2 (though Phase 15.5 has a narrow webhook for budget-critical events; that's different).
- **No conflict resolution beyond optimistic concurrency.** Each resource carries a `version` integer; writes specify the expected version; mismatches return `409 conflict`. No CRDT, no last-writer-wins.

## Deliverables

1. **Versioned API namespace** — `/api/v1/{resource}/...` for every resource type. The pre-Phase-18 endpoints (`/api/agent-profiles`, `/api/governance/virtual-keys`, etc.) become aliases / redirects to the new shape so older callers continue to work.
2. **Resource registry** — `internal/dataplane/api/registry.go` maps resource kind names (`AgentProfile`, `VirtualKey`, `Team`, `Customer`, `Budget`, `Server`, `Skill`, `SkillSource`, `Policy`, `A2APeer`, `SecurityPolicy`, `AttestationConfig`, `DriftGate`, `PinnedSource`, `CacheConfig`) to their repos, validators, and policy bundles. Adding a future resource type is a registry registration.
3. **CRUD per resource** — `GET` (list), `POST` (create), `GET /{id}` (read), `PUT /{id}` (update), `DELETE /{id}`, `PATCH /{id}` (JSON Merge Patch).
4. **Watch channel** — `GET /api/v1/watch?resource=<kinds>` (SSE). Events: `created` / `updated` / `deleted` with after-state and version. Initial snapshot followed by a watermark; events tagged with monotonic versions so consumers can resume from a `?since=<version>` parameter.
5. **Optimistic concurrency** — every resource has `metadata.version`; writes that don't match the current version return `409`. Watch events carry the new version.
6. **Validation pipeline** — every write is validated by the same code paths the YAML loader uses. Validation errors return `422` with JSON-Pointer-shaped details (the Phase 8 convention).
7. **Audit envelope** — every successful write emits an audit event of type `resource.changed` with `actor`, `resource_kind`, `resource_id`, `version_before`, `version_after`, and a `diff` (RFC 6902 JSON Patch). Redactor scrubs sensitive fields (e.g. VK secrets, cache credentials, vault refs).
8. **Policy gating on writes** — writes are policy-evaluated. A policy rule can deny a write, require approval, or wrap a warning. The pre-existing approval flow (Phase 5) handles the queueing; pending writes appear in the existing approvals queue.
9. **Transactional bulk apply** — `POST /api/v1/apply` accepts a multi-resource document and applies all resources atomically: either every resource is applied or none. Used by the Kubernetes operator (Phase 19) to roll forward / back.
10. **YAML / API reconciliation** — the YAML loader still produces the cold-start state. After boot, the API takes over as the writeable channel. A `?source=yaml|api|all` filter on read endpoints exposes provenance. A reload of the YAML file produces a structured diff event over the watch channel; existing API-managed resources are preserved unless the YAML re-declares them with a higher version (deterministic merge semantics, §6).
11. **Console** — every existing CRUD page (Agents, Servers, Skills, Tenants, VKs, Teams, Customers, Budgets, Cache, A2A Peers, Security) gains a "History" tab with the audit events for that resource and a "Source" badge (yaml | api). The §4.5.1 operator UX gates already apply to all CRUD pages from prior phases — Phase 18 doesn't introduce new screens, it enriches the detail pages.
12. **CLI** — `portico apply -f file.yaml`, `portico diff -f file.yaml`, `portico delete <kind>/<name>`, `portico watch <kinds>`, `portico get <kind>/<name> -o yaml`. Mirrors `kubectl` ergonomics where it's natural.
13. **Smoke** — `scripts/smoke/phase-18.sh` covers: create / read / update / delete via the API for each resource kind (parametrised); conflict on stale version; watch receives the create event; bulk apply with one invalid resource rolls back all; audit event captures the diff with credentials redacted.

## Acceptance criteria

1. **CRUD round trip for every resource kind.** Parametrised test: for each kind in {AgentProfile, VirtualKey, Team, Customer, Budget, Server, Skill, SkillSource, Policy, A2APeer, SecurityPolicy, AttestationConfig, DriftGate, PinnedSource, CacheConfig}, POST → GET → PUT → DELETE → GET 404. The new resource resolves traffic immediately (Profile applies on next request; VK applies on next call; etc.).
2. **Validation parity with YAML.** A spec that the YAML loader would reject (e.g. unknown Profile binding to a non-existent Server) is rejected by the API with a 422 + JSON-Pointer details.
3. **Optimistic concurrency.** Two concurrent `PUT`s with the same `metadata.version` produce one success and one 409; the failed client refreshes and retries.
4. **Watch — initial snapshot.** A new SSE consumer receives the current state of every requested resource type as `created` events, followed by a `watermark` event with the current version. Subsequent changes are streamed as they happen.
5. **Watch — resume.** A consumer that disconnects and reconnects with `?since=<version>` receives every event after that version; missing events (because the consumer was offline beyond the retention window) yield a `410 gone` and the consumer must re-snapshot.
6. **Audit diff.** Updating an Agent Profile's `allowed_mcp_servers` from `[github, jira]` to `[github, jira, slack]` produces an audit event whose `diff` is a JSON Patch capturing the addition. Updating a VK's allowlists similarly.
7. **Credential redaction.** Updating a backend's `egress_auth.vault_ref` value (when Phase 15 is eventually revived — for now, updating any vault-backed field on a Server or A2A Peer) produces an audit event whose `diff` shows the field changed but does not include the value. The redactor surface that handles MCP tool args applies here.
8. **Policy gating — deny.** A policy rule `deny if resource_kind=AgentProfile and actor.scope contains 'developer' and resource.name starts_with 'admin-'` blocks the write with 403; an audit event records the deny.
9. **Policy gating — approval.** A policy rule `require_approval if resource_kind=VirtualKey and resource.attached_to_profile contains 'prod'` queues the write in the approvals queue; the requesting client receives `202 accepted` with the approval id; on operator approval the write is applied and a watch event fires.
10. **Bulk apply atomicity.** `POST /api/v1/apply` with three resources where the second is invalid returns 422 with details on the second resource only; resources one and three are not applied. The audit log records a single `apply.rolled_back` event with the offending resource.
11. **YAML reconciliation.** Editing the on-disk `portico.yaml` to declare an Agent Profile that the API has also created merges deterministically per §6; observed via the watch channel as zero, one, or two events depending on the merge outcome. The integration test covers all three outcomes.
12. **Aliasing of pre-Phase-18 endpoints.** Calling the pre-existing `/api/agent-profiles` succeeds and is functionally identical to `/api/v1/agent-profiles`. Documented as legacy aliases.
13. **Console history tab.** Every CRUD detail page shows a "History" tab with the audit events for that resource (chronological, redacted) plus a "Source" chip (yaml | api). Playwright covers it.
14. **CLI parity.** `portico apply -f file.yaml` applies the same resources as the API and emits the same audit events. `portico watch agent-profiles` follows the SSE channel. `portico diff -f file.yaml` shows what would change without applying.
15. **Smoke gate.** `scripts/smoke/phase-18.sh` shows OK ≥ 18, FAIL = 0; prior phases' smokes still pass.
16. **Coverage.** `internal/dataplane/api/` ≥ 85%; `internal/dataplane/state/` ≥ 90% (the state object is the heart of correctness).

## Architecture

### Package layout

```
internal/dataplane/api/
├── api.go                     # versioned routing setup (/api/v1/...)
├── registry.go                # resource kind ↔ repo/validator/policy map
├── handlers.go                # generic CRUD handler factory
├── watch.go                   # SSE watch channel + replay buffer
├── apply.go                   # transactional bulk apply
├── reconcile.go               # YAML × API merge rules
├── alias.go                   # pre-Phase-18 endpoint compatibility
└── api_test.go

internal/dataplane/state/
├── state.go                   # in-memory aggregate state (one snapshot across all kinds)
├── version.go                 # monotonic version counter + replay buffer
├── events.go                  # event emission for the watch channel
└── state_test.go

cmd/portico/
├── cmd_apply.go               # `portico apply -f`
├── cmd_diff.go                # `portico diff -f`
├── cmd_get.go                 # `portico get <kind>/<name>`
└── cmd_watch.go               # `portico watch <kinds>`

web/console/src/lib/api/
└── watch.ts                   # typed SSE client for the watch channel

(All existing CRUD pages already exist from prior phases. Phase 18 adds:)
web/console/src/lib/components/HistoryTab.svelte    # reusable history tab for any resource detail page
web/console/tests/history.spec.ts
```

### Resource model

Every resource registered in `internal/dataplane/api/registry.go` provides:

```go
type Resource struct {
    Kind         string                  // "AgentProfile", "VirtualKey", ...
    APIPath      string                  // "/api/v1/agent-profiles", ...
    AliasPaths   []string                // ["/api/agent-profiles"] for back-compat
    Repo         CRUDRepo                // tenant-scoped repo (List/Get/Put/Delete)
    Validator    func(any) error         // same validator the YAML loader uses
    Differ       func(before, after any) JSONPatch
    Redactor     func(JSONPatch) JSONPatch
    PolicyBundle string                  // policy rules that apply to writes on this kind
}
```

The CRUD handler factory turns a `Resource` into a `chi.Router` mounted at its `APIPath` + alias paths. Validation, audit, policy, and watch-emission are wired uniformly.

### State + watch

A single in-memory `State` object holds the current aggregate of every resource kind. Writes mutate it under a per-kind mutex; reads are lock-free (CoW snapshot). Every successful write bumps a monotonic global `version` counter and appends an event to a bounded ring buffer (default 1024 events, configurable). SSE consumers tail the ring; consumers behind by more than the ring's capacity get `410 gone`.

The watch channel emits events grouped by resource kind (filtered by `?resource=…`) and serialised as JSON-per-line:

```
event: created
data: {"version": 4711, "kind": "AgentProfile", "id": "01H...", "after": {...}}

event: updated
data: {"version": 4712, "kind": "AgentProfile", "id": "01H...", "before_version": 4711, "after": {...}, "diff": [...]}

event: deleted
data: {"version": 4713, "kind": "AgentProfile", "id": "01H...", "before": {...}}

event: watermark
data: {"version": 4713}
```

### YAML × API reconciliation (§6)

Both producers write into the same `State`. The merge rule on reload of `portico.yaml`:

1. Compute the YAML's declared resource set per kind.
2. For each declared resource, compare against the current `State`:
   - If the resource does not exist → create it with source: `yaml`.
   - If the resource exists with source: `yaml` and content differs → update.
   - If the resource exists with source: `api` and YAML declares it explicitly with a higher `version` → take the YAML side; emit `updated` event.
   - If the resource exists with source: `api` and YAML does not declare it → leave it alone (the operator chose to manage it via API).
3. For each previously-yaml-managed resource that's no longer in the YAML → delete it; emit `deleted` event.

The integration test (`TestE2E_DataPlane_API_YAMLReconciliation_*`) covers all four cases.

### Bulk apply

`POST /api/v1/apply` accepts:

```yaml
apiVersion: portico/v1
kind: List
items:
  - kind: AgentProfile
    metadata: { name: "support-eu", version: 3 }
    spec: { ... }
  - kind: VirtualKey
    metadata: { name: "support-eu-prod", version: 1 }
    spec: { ... }
  - kind: Budget
    metadata: { name: "support-eu-monthly", version: 1 }
    spec: { ... }
```

Apply runs in a single transactional pass. Either every resource validates + applies, or none does. Partial failures return 422 with a `failed_at` index and the original list intact in `State`.

## REST API

```
# Versioned generic shape (one row per registered kind):
GET    /api/v1/{kind-plural}                      # list, paginated
POST   /api/v1/{kind-plural}                      # create
GET    /api/v1/{kind-plural}/{name-or-id}         # read
PUT    /api/v1/{kind-plural}/{name-or-id}         # update (version-matched)
PATCH  /api/v1/{kind-plural}/{name-or-id}         # partial update (JSON Merge Patch)
DELETE /api/v1/{kind-plural}/{name-or-id}

# Watch + apply + provenance:
GET    /api/v1/watch?resource=<kinds-csv>&since=<version>   # SSE
POST   /api/v1/apply                                          # transactional bulk
GET    /api/v1/{kind-plural}?source=<yaml|api|all>            # provenance filter

# Legacy aliases (kept for back-compat through V2):
GET    /api/agent-profiles    → /api/v1/agent-profiles
GET    /api/governance/virtual-keys → /api/v1/virtual-keys
... (one alias entry per pre-Phase-18 endpoint)
```

## CLI

```bash
portico apply -f resources.yaml                  # transactional bulk apply
portico apply -f dir/                            # apply every YAML in a directory (still transactional)
portico diff  -f resources.yaml                  # show what would change without applying
portico get   <kind>/<name> -o yaml              # current state, YAML-shaped
portico delete <kind>/<name>
portico watch <kinds-csv>                        # follow the SSE channel
portico edit  <kind>/<name>                      # opens $EDITOR; PUT on save
```

## Implementation walkthrough

### Step 1 — Resource registry + generic CRUD handler

Define the `Resource` shape. Register every existing resource kind. The generic handler factory turns a `Resource` into mounted `chi.Router` endpoints. Each kind's repo is already implemented by its phase (P14 for AgentProfile, P15.5 for VK/Team/Customer/Budget/Cache, etc.); Phase 18 wraps them.

### Step 2 — Versioning + audit + redactor wiring

Every write goes through: validate → policy → bump version → persist → emit audit (redacted) → emit watch event. Tests cover each layer in isolation, plus end-to-end.

### Step 3 — State + watch channel

`State` is the in-memory aggregate. Watch channel tails its event ring. Initial-snapshot replay is the materialisation of every registered repo's `List`. Resume is a ring lookup with `410` fallback.

### Step 4 — Legacy aliases

For each pre-Phase-18 endpoint (`/api/agent-profiles`, `/api/governance/*`, `/api/llm/cache/*`, `/api/a2a/peers`, `/api/security/*`), add an alias route that 308-redirects (or transparent-proxies) to the canonical `/api/v1/...` path. Documented.

### Step 5 — YAML × API reconciliation

The YAML loader gains an `ApplyToState(state *State) error` method. On reload, the merge rule (§6) executes; events emit. Integration tests cover all four merge cases.

### Step 6 — Bulk apply

Transactional bulk: parse list, validate all, policy-check all, then commit all under one State lock or roll back the whole batch.

### Step 7 — CLI

`portico apply` reads YAML, posts to `/api/v1/apply`. `portico diff` does the same but uses a dry-run query param. `portico watch` consumes SSE.

### Step 8 — Console history tab

Reusable `HistoryTab.svelte` component pulls audit events for `(kind, id)` from the Phase 11 audit query API. Mounted on every resource detail page. Source chip rendered next to the resource name.

### Step 9 — Smoke

`scripts/smoke/phase-18.sh`:
- POST a Profile via `/api/v1/agent-profiles` → 201.
- GET → 200.
- PUT with stale version → 409.
- PUT with matching version → 200.
- Watch subscription receives the updated event.
- Bulk apply with three resources (one invalid) → 422; assert State unchanged.
- YAML reload that declares a previously-API-only Profile with higher version → State takes YAML side.
- Legacy alias path `/api/agent-profiles` works.

OK ≥ 18 by phase close, FAIL = 0.

## Test plan

### Unit

- `internal/dataplane/api/registry_test.go` — registry registration + lookup + duplicate detection.
- `internal/dataplane/api/handlers_test.go`
  - `TestCRUDHandler_HappyPath`
  - `TestCRUDHandler_ValidationError_Returns422`
  - `TestCRUDHandler_PolicyDeny_Returns403`
  - `TestCRUDHandler_ApprovalRequired_Returns202`
  - `TestCRUDHandler_OptimisticConcurrency`
- `internal/dataplane/state/state_test.go`
  - `TestState_VersionMonotonic`
  - `TestState_EventRing_Bounded`
  - `TestState_CoWSnapshot_NoTearingReads`
- `internal/dataplane/api/watch_test.go`
  - `TestWatch_InitialSnapshot`
  - `TestWatch_StreamingEvents`
  - `TestWatch_Resume_FromSince`
  - `TestWatch_Resume_BeyondRing_Returns410`
- `internal/dataplane/api/apply_test.go`
  - `TestApply_AllValid_AllApplied`
  - `TestApply_OneInvalid_NoneApplied`
  - `TestApply_AuditTrailRecordsRollback`
- `internal/dataplane/api/reconcile_test.go`
  - `TestReconcile_YAMLDeclaresNew`
  - `TestReconcile_YAMLOverridesAPI_HigherVersion`
  - `TestReconcile_APIOnlyResource_Preserved`
  - `TestReconcile_YAMLRemoval_DeletesPreviouslyDeclared`
- `internal/dataplane/api/alias_test.go` — every legacy alias resolves to the canonical handler.

### Integration

- `TestE2E_DataPlane_API_CRUDAllKinds` — parametrised across every registered kind.
- `TestE2E_DataPlane_API_OptimisticConcurrency_Conflict`
- `TestE2E_DataPlane_API_Watch_RealTime`
- `TestE2E_DataPlane_API_Apply_RollsBackOnInvalid`
- `TestE2E_DataPlane_API_YAMLReconciliation_AllFourCases`
- `TestE2E_DataPlane_API_Audit_DiffWithRedaction`
- `TestE2E_DataPlane_API_Policy_DenyAndApproval`
- `TestE2E_DataPlane_API_LegacyAliases_StillWork`
- `TestE2E_DataPlane_API_CrossTenantIsolation`

### Frontend tests

- Playwright: history tab renders audit events on every resource detail page (covered via one test that exercises a Profile + a VK + a Server + a Skill, asserting the pattern is uniform).

### Smoke

`scripts/smoke/phase-18.sh` — listed above. OK ≥ 18.

### Coverage gates

- `internal/dataplane/api/` ≥ 85%.
- `internal/dataplane/state/` ≥ 90% (correctness-critical).

## Common pitfalls

- **Reintroducing Listener/Route/Backend via the generic registry.** The registry's job is to wrap the resource kinds Portico actually has. Adding a "Listener" kind here brings back the substrate we explicitly dropped. §13 forbidden practice (updated by this phase).
- **State ring too small.** A consumer offline for more than the ring's window can't resume. Default 1024 events; configurable. Document; the consumer's `410 gone` recovery path is to re-snapshot.
- **Event tearing on concurrent writes.** Two writes to different kinds proceed in parallel under per-kind mutexes; their event ordering is monotonic only by version, not by wall clock. Watch consumers must order by version, not arrival.
- **Diff redaction missing a field.** A new resource kind that adds a sensitive field needs a redactor entry. The Phase 18 generic handler reads the redactor from the Resource registration; the §13 forbidden practice (updated) is "registering a kind without a redactor for fields that match the audit-secret regex set."
- **YAML loader path drift.** The YAML × API reconciliation depends on the YAML loader producing equivalent shapes to the API handler. Test parity matters; the parametrised "validation parity with YAML" criterion covers this.
- **Bulk apply non-atomic across kinds.** A bulk apply that crosses kinds (Profile + VK + Budget) holds the per-kind mutexes in a deterministic order to avoid deadlock; the integration test exercises high-concurrency apply to catch ordering bugs.
- **Alias confusion.** Pre-Phase-18 endpoints are aliases, not parallel implementations. Operators who edit the alias mapping by hand can desync; the §13 forbidden practice (updated) is "implementing a parallel CRUD path for a registered kind outside the generic handler."
- **Approval-queue flooding.** A policy requiring approval on every Profile write floods the approvals UI during a GitOps reconcile. Phase 18 supports a per-policy `approval_batch: true` mode where many pending writes from one actor are presented as a single approval card (batched diff); the operator approves or rejects the whole batch atomically.
- **Watch back-pressure.** A slow SSE consumer can stall the ring's broadcast goroutine. Per-connection bounded channel with drop-oldest policy + an `audit.dropped` event on overflow. Never block other consumers.

## Out of scope (recap)

- Listener / Route / Backend resource types (substrate dropped 2026-05-12).
- Envoy ADS / xDS adapter (dropped 2026-05-12).
- GraphQL.
- Outbound webhooks (except Phase 15.5's budget-critical narrow case).
- CRDT / last-writer-wins conflict resolution.
- Multi-version API (`v2`+ is post-V2).
- A resource-kind plugin system (kinds are Go-coded; adding one requires a code change + a registry entry).

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-18.sh` shows OK ≥ 18, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. Docs site gains `/docs/concepts/dynamic-config-api`, `/docs/how-to/gitops-with-portico`, `/docs/reference/api-v1`.
5. `AGENTS.md` §13 forbidden practices updated:
   - "Adding a Listener / Route / Backend resource kind to the dynamic config registry."
   - "Implementing a parallel CRUD path for a registered kind outside the generic handler."
   - "Registering a resource kind without a redactor declared for fields that may carry secrets."
6. RFC-001 updated with a Dynamic Configuration API section.
7. `docs/plans/README.md` index updated.

## Hand-off to Phase 19

Phase 19 inherits:

- The `/api/v1/...` shape — the Kubernetes operator translates CRDs (one CRD per registered resource kind) to API calls. **The CRD set is**: `AgentProfile`, `VirtualKey`, `Team`, `Customer`, `Budget`, `Server`, `Skill`, `SkillSource`, `Policy`, `A2APeer`, `SecurityPolicy`, `AttestationConfig`, `DriftGate`, `PinnedSource`, `CacheConfig`. **Notably absent**: `Listener`, `Route`, `Backend`, `Bridge` — these were dropped with the substrate pivot.
- The watch channel — federation in Phase 19 consumes it for shared-resource replication (with tenant-scope filters that drop tenant-scoped resources at the boundary).
- The bulk apply — the K8s operator uses it for reconcile-from-CRD-list to roll forward/back atomically.
- The audit diff — federation messages carry signed diffs derived from the same Patch shape.
