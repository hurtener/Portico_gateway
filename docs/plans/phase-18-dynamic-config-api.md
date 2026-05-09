# Phase 18 — Dynamic Data-Plane Configuration API

> Self-contained implementation plan. Builds on Phase 14–17. Adds a structured, watchable, auditable CRUD API over the data-plane state (binds, listeners, routes, backends, security configuration). Optional Envoy ADS adapter so external orchestration tools can manage Portico without re-implementing the protocol.

## Goal

After Phase 18, the data-plane state has two equally first-class producers:

1. **`portico.yaml`** (static) — the existing source of truth for cold-start and human-edited deployments.
2. **The dynamic configuration API** — a structured CRUD surface plus a watch channel; operator tooling, GitOps controllers, and Phase 19's Kubernetes operator push state through it.

Both producers write to the same in-memory data-plane state object. Both go through the same validation, policy, audit, and security gating (Phase 17). The Console becomes a third client of the same API; nothing privileged.

The API is intentionally Envoy-adjacent in shape (resource types, named references, watch semantics) so the optional ADS adapter is small. Portico does *not* implement the full Envoy gRPC protocol set; it implements its own JSON CRUD + SSE watch surface and exposes an ADS-compatible projection for the subset that matters.

## Why this phase exists

Phase 14 introduced the data-plane state object. Phase 15–17 mutated it via YAML reload only. That works for human operators editing one box; it does not scale to:

- A GitOps workflow where a controller in a CI pipeline rolls out routes by template, with zero-downtime apply across a fleet.
- A Kubernetes operator (Phase 19) reconciling `Listener` and `Route` CRDs onto Portico instances.
- A federation deployment (Phase 19) where the leader pushes route changes to followers.
- An external automation suite (e.g. an internal developer portal) registering/deregistering backends as services come and go.

Any of these can today script `portico` invocations or YAML edits. None of them get a *transactional*, *validated*, *audited*, *watch-driven* surface. Phase 18 builds that surface once, so every later integration consumes it.

The phase also pays down a debt Phase 14 deliberately left: the read-only `/api/dataplane/*` endpoints from Phase 14 imply a write counterpart. Phase 18 ships it, behind the same validation and audit envelope as the rest of the control plane.

## Prerequisites

- Phase 14 substrate (binds/listeners/routes/backends as in-memory data-plane state).
- Phase 15 + 16 backend drivers (the API mutates routes that target them).
- Phase 17 security gating (config-change writes go through the same policy/scanner/drift surface as other state changes).
- Phase 5 audit + redactor (every write produces an audit event with the diff).

## Out of scope (explicit)

- **No full Envoy xDS implementation.** The optional ADS adapter projects a subset; full LDS/RDS/CDS/EDS parity is post-V2.
- **No CRD-style schema evolution machinery.** Resource schemas live in Go structs; non-additive changes require a versioned API path. We do not build an Open API CRD versioning layer.
- **No multi-version API.** The Phase 18 API ships at `v1`. A future v2 (post-V2) lives at `/api/dataplane/v2/...`; v1 stays.
- **No GraphQL.** REST + SSE only. GraphQL has been considered and explicitly rejected.
- **No webhook outputs.** Watch is consumed via SSE on a long-lived HTTP connection. Outbound webhooks (Portico calling external systems on changes) are post-V2.
- **No conflict resolution beyond optimistic concurrency.** Each resource carries a `version` integer; writes specify the expected version; mismatches return `409 conflict`. No CRDT, no last-writer-wins.

## Deliverables

1. **CRUD endpoints** under `/api/dataplane/v1/`:
   - `binds`, `listeners`, `routes`, `backends`, `bridges` (Phase 16)
   - `security/scanners`, `security/attestations`, `security/drift_gates`, `security/pins` (Phase 17)
   - Each resource: `GET` (list), `POST` (create), `GET /{id}` (read), `PUT /{id}` (update), `DELETE /{id}` (delete), `PATCH /{id}` (partial update via JSON Merge Patch).
2. **Watch channel** at `/api/dataplane/v1/watch` (SSE). Operators specify a `?resource=routes,backends` query; events are emitted as `created` / `updated` / `deleted` with the after-state and the version. Initial connection sends a snapshot followed by the watermark; subsequent events are tagged with monotonic versions so a client can resume.
3. **Optimistic concurrency** — every resource has `metadata.version`; writes that don't match the current version return `409`. Watch events carry the new version.
4. **Validation pipeline** — every write is validated by the same code paths the YAML loader uses. Validation errors return `422` with JSON-Pointer-shaped details (the Phase 8 convention).
5. **Audit envelope** — every successful write emits an audit event of type `dataplane.config_changed` with `actor`, `resource_kind`, `resource_id`, `version_before`, `version_after`, and a `diff` (RFC 6902 JSON Patch). Redactor scrubs sensitive fields (e.g. egress_auth credentials).
6. **Policy gating** — writes are policy-evaluated. A policy rule can deny a write, require approval, or wrap a warning. Pre-existing approval flow (Phase 5) handles the queueing; pending writes appear in the existing approvals queue.
7. **Transactional bulk apply** — `POST /api/dataplane/v1/apply` accepts a multi-resource document (similar to a Kubernetes manifest) and applies all resources atomically: either every resource is applied or none. Used by the Kubernetes operator (Phase 19) to roll forward / back.
8. **YAML / API reconciliation** — the YAML loader still produces the cold-start state. After boot, the API takes over as the writeable channel. A `?source=yaml|api|all` filter on the read endpoints exposes provenance. A reload of the YAML file produces a structured diff event over the watch channel; existing API-managed resources are preserved unless the YAML re-declares them with a higher version (deterministic merge semantics, documented in §6.4).
9. **ADS adapter (optional, build-tag gated)** — `internal/dataplane/ads/` exposes the dataplane state via the Envoy ADS gRPC protocol for the `LDS`, `RDS`, `CDS` resource types. Operators who run an existing xDS-capable control plane (Istio, custom) can manage Portico through it.
10. **Console** — `/dataplane` becomes the editable surface. List pages gain `+ Add` CTAs and per-row Edit/Delete actions. The §4.5.1 operator UX gates apply. A "history" tab on every resource shows the audit events for that resource (reusing Phase 11's audit query).
11. **CLI** — `portico dataplane` subcommand: `list`, `get`, `apply -f file.yaml`, `delete`, `diff`, `watch`. Mirrors `kubectl` ergonomics where it's natural.
12. **Smoke** — `scripts/smoke/phase-18.sh` covers: create / read / update / delete a route via the API; conflict on stale version; watch receives the create event; bulk apply with one invalid resource rolls back all; audit event captures the diff with credentials redacted.

## Acceptance criteria

1. **CRUD round trip.** `POST /api/dataplane/v1/routes` with a valid spec creates the route; `GET` returns it; the new route resolves traffic immediately; `DELETE` removes it; `GET` returns 404.
2. **Validation parity with YAML.** A spec that the YAML loader would reject (e.g. unknown backend reference) is rejected by the API with a 422 + JSON-Pointer details.
3. **Optimistic concurrency.** Two concurrent `PUT`s with the same `metadata.version` produce one success and one 409; the failed client refreshes and retries.
4. **Watch — initial snapshot.** A new SSE consumer receives the current state of every requested resource type as `created` events, followed by a `watermark` event with the current version. Subsequent changes are streamed as they happen.
5. **Watch — resume.** A consumer that disconnects and reconnects with `?since=<version>` receives every event after that version; missing events (because the consumer was offline beyond the retention window) yield a `410 gone` and the consumer must re-snapshot.
6. **Audit diff.** Updating a route's `match.path_prefix` from `/billing/` to `/payments/` produces an audit event whose `diff` is `[{"op":"replace","path":"/match/path_prefix","value":"/payments/"}]`.
7. **Credential redaction.** Updating a backend's `egress_auth.vault_ref` value produces an audit event whose `diff` shows the field changed but does not include the value. (Vault refs are paths, not secrets, but the same redactor surface that handles MCP tool args applies here too.)
8. **Policy gating — deny.** A policy rule `deny if resource_kind=routes and actor.scope contains 'developer' and route.match.host == 'admin.example.com'` blocks the write with 403; an audit event records the deny.
9. **Policy gating — approval.** A policy rule `require_approval if resource_kind=backends and resource.driver=http_proxy and resource.config.upstreams[*].url ~ /prod/` queues the write in the approvals queue; the requesting client receives `202 accepted` with the approval id; on operator approval the write is applied and a watch event fires.
10. **Bulk apply atomicity.** `POST /apply` with three resources where the second is invalid returns 422 with details on the second resource only; resources one and three are not applied.
11. **YAML reconciliation.** Editing the on-disk `portico.yaml` to declare a route that the API has also created merges deterministically per §6.4; observed via the watch channel as zero, one, or two events depending on the merge outcome.
12. **ADS adapter.** With the build tag enabled, an Envoy ADS client (test fixture) successfully fetches `LDS`, `RDS`, `CDS` for the configured listeners/routes/backends. Subsequent updates push to the ADS client.
13. **Console parity.** Every resource type has a list page with `+ Add`, an edit form covering all spec fields, a delete confirmation, and a history tab. Playwright covers create + edit + delete for `routes` and `backends`.
14. **CLI parity.** `portico dataplane list routes`, `apply -f routes.yaml`, `delete route foo` work and emit the same audit events as the API. `portico dataplane watch routes` follows the SSE channel.
15. **Smoke gate.** `scripts/smoke/phase-18.sh` shows OK ≥ 22, FAIL = 0; prior phases' smokes still pass.
16. **Coverage.** `internal/dataplane/api/` ≥ 85%; `internal/dataplane/state/` ≥ 90% (the state object is the heart of correctness).

## Architecture

### 6.1 Package layout

```
internal/dataplane/
├── state/
│   ├── store.go             # in-memory state object (binds, listeners, routes, backends)
│   ├── version.go           # monotonic version counter + per-resource version
│   ├── snapshot.go          # immutable snapshot for watch fan-out
│   ├── diff.go              # RFC 6902 JSON Patch diffs
│   └── store_test.go
├── api/
│   ├── handlers_routes.go
│   ├── handlers_backends.go
│   ├── handlers_listeners.go
│   ├── handlers_binds.go
│   ├── handlers_security.go
│   ├── handlers_apply.go    # bulk apply endpoint
│   ├── handlers_watch.go    # SSE watch
│   └── shared.go            # shared validation, optimistic-concurrency, error shapes
├── reconcile/
│   ├── yaml_to_state.go     # YAML loader → state writes
│   └── merge.go             # YAML × API merge rules
└── ads/                     # build-tag: portico_ads
    ├── server.go            # Envoy ADS gRPC server
    └── translate.go         # state → Envoy resource projection
```

### 6.2 State object

```go
package state

type Store struct {
    mu       sync.RWMutex
    binds    map[string]*Bind
    listeners map[string]*Listener
    routes   map[string]*Route
    backends map[string]*Backend
    version  uint64
    perResource map[string]uint64  // resource_id → version
    subscribers []*subscriber
}

type ChangeEvent struct {
    Kind     string         // "created" | "updated" | "deleted"
    Resource string         // "routes" | "backends" | …
    ID       string
    Version  uint64
    After    any            // nil on delete
}
```

Reads are lock-free (snapshot-based); writes acquire the write lock briefly to swap the snapshot. Subscribers receive change events on a buffered channel; slow consumers are dropped after the documented threshold (drop-oldest with a `dataplane.subscriber_dropped` audit event, per `AGENTS.md` §5 concurrency rules).

### 6.3 Watch channel

SSE on `/api/dataplane/v1/watch?resource=routes,backends&since=<version>`:

- Initial response: `created` events for every existing matching resource, followed by a `watermark` event with the current version.
- Subsequent: `created` / `updated` / `deleted` events as they occur.
- Heartbeat: `: keepalive\n\n` every 15 s.
- Disconnection: client may reconnect with `?since=<last_version>`; if the gap exceeds retention (default 5 minutes of history), respond with `410 gone` and the client must re-snapshot.

### 6.4 YAML × API merge rules

The reconciliation algorithm:

1. **Cold start**: YAML is the only producer; every resource is owned by `source=yaml`.
2. **API write**: the resource is annotated `source=api` and version-bumped.
3. **YAML reload**:
   - For each resource in YAML:
     - If absent from state: create as `source=yaml`.
     - If present and `source=yaml` and content differs: update.
     - If present and `source=api`: warn (audit event), do nothing. The YAML is not authoritative for API-owned resources.
   - For each resource in state with `source=yaml`:
     - If absent from YAML: delete.
   - For each resource in state with `source=api`: untouched.

A resource's source can be flipped via `PATCH /{id}` with `metadata.source: yaml` or `metadata.source: api` (admin scope; explicit operator decision; audit-logged).

### 6.5 Policy gating for writes

Writes go through the policy engine before the state is mutated. The matcher surface for write rules:

- `resource_kind` (routes / backends / …)
- `resource.id`, `resource.driver`, `resource.match.*`
- `actor.tenant`, `actor.user`, `actor.scope`
- `change.kind` (create / update / delete)
- `change.diff[*]` (JSON-Pointer paths in the diff)

The action surface: `allow`, `deny`, `require_approval`, `wrap_warning`. Same vocabulary the rest of the policy engine uses.

### 6.6 ADS adapter

Build-tag-gated (`portico_ads`) so operators who do not need it pay zero binary size. Implements:

- `LDS` → projects each `state.Listener` as an Envoy `Listener` resource.
- `RDS` → projects each `state.Route` as an Envoy `RouteConfiguration` resource.
- `CDS` → projects each `state.Backend` as an Envoy `Cluster` resource (HTTP-proxy and gRPC-proxy backends only; MCP/A2A/LLM are not Envoy-native).
- ADS pushes are triggered by the same `state.ChangeEvent` channel as the SSE watch.

The adapter is one-way (state → ADS); we do not consume Envoy xDS configuration as input. That stays a future RFC.

## Configuration extensions

```yaml
dataplane_api:
  enabled: true                    # default true; can be disabled to lock the data plane
  watch:
    history_window: 5m
    max_subscribers: 64
  ads:
    enabled: false                 # only effective if built with -tags portico_ads
    bind: 127.0.0.1:18000
```

## REST APIs

The full surface is too large to list per row; the shape is uniform:

- `GET    /api/dataplane/v1/{resource}` — list (filter, sort, paginate)
- `POST   /api/dataplane/v1/{resource}` — create
- `GET    /api/dataplane/v1/{resource}/{id}` — read
- `PUT    /api/dataplane/v1/{resource}/{id}` — replace (requires `metadata.version`)
- `PATCH  /api/dataplane/v1/{resource}/{id}` — JSON Merge Patch (requires `metadata.version`)
- `DELETE /api/dataplane/v1/{resource}/{id}` — delete (requires `metadata.version` query param)
- `POST   /api/dataplane/v1/apply` — bulk transactional apply
- `GET    /api/dataplane/v1/watch` — SSE
- `GET    /api/dataplane/v1/{resource}/{id}/history` — audit events for one resource

Resource kinds: `binds`, `listeners`, `routes`, `backends`, `bridges`, `security/scanners`, `security/attestations`, `security/drift_gates`, `security/pins`.

Standard error envelope: `{"error":"<code>","message":"...","details":{}}`. Common codes: `not_found`, `conflict`, `invalid`, `forbidden`, `approval_required`.

## Implementation walkthrough

1. **State store.** `internal/dataplane/state.Store` — in-memory, versioned, subscribable. Tests cover concurrent writes, slow-consumer drop, snapshot consistency.
2. **CRUD endpoints — routes first.** Validate, version, audit-log, mutate state. Smoke proves a round trip.
3. **Watch endpoint.** SSE with snapshot → live → keepalive → resume semantics.
4. **Remaining resource kinds.** Backends, listeners, binds, bridges, security/*. Same shape.
5. **Bulk apply.** Transactional; error returns details on the first invalid resource only.
6. **YAML reconciliation.** Implement merge rules; audit any conflict.
7. **Policy gating.** Hook into the policy engine; queue approval-required writes.
8. **CLI.** `portico dataplane …` subcommands.
9. **Console editable surfaces.** Promote read-only screens to editable; add history tab; Playwright for create/edit/delete.
10. **ADS adapter (build-tag).** Behind `portico_ads`; integration test with a stub Envoy client.
11. **Smoke + perf.** `phase-18.sh`; perf gate (write latency p99 ≤ 50 ms, read latency p99 ≤ 5 ms at 100 r/s).

## Test plan

Unit:

- `TestStore_CreateRead`
- `TestStore_OptimisticConcurrency_Conflict`
- `TestStore_Watch_InitialSnapshot`
- `TestStore_Watch_LiveEvents`
- `TestStore_Watch_SlowConsumerDrop`
- `TestStore_Snapshot_Immutable`
- `TestAPI_RouteCreate_Valid`
- `TestAPI_RouteCreate_RejectsUnknownBackend`
- `TestAPI_RoutePatch_AppliesMergePatch`
- `TestAPI_BulkApply_AtomicRollback`
- `TestAPI_Watch_ResumeFromVersion`
- `TestAPI_Watch_GoneAfterRetentionWindow`
- `TestAPI_Audit_DiffShape`
- `TestAPI_Audit_RedactsSecrets`
- `TestPolicy_DenyWrite`
- `TestPolicy_RequireApprovalForWrite`
- `TestReconcile_YAML_OwnedDoesNotOverwriteAPI`
- `TestADS_TranslatesListenerToEnvoy`
- `TestADS_PushesOnStateChange`

Integration:

- `TestE2E_DataPlaneAPI_RouteRoundTrip_TrafficResolves`
- `TestE2E_DataPlaneAPI_DeleteRoute_StopsResolution`
- `TestE2E_DataPlaneAPI_BulkApply_KubernetesShape`
- `TestE2E_DataPlaneAPI_PolicyDeny_BlockedAndAudited`
- `TestE2E_DataPlaneAPI_ApprovalQueue_PendingWritesApply`
- `TestE2E_DataPlaneAPI_YAMLReload_DoesNotClobberAPIResources`
- `TestE2E_DataPlaneAPI_ADS_EnvoyClientRoundTrip` (build-tag-gated)

## Common pitfalls

1. **Watch fan-out blocking writers.** Subscriber channels are buffered; full channels drop oldest with an audit event; writers never block on subscribers. Test `TestStore_Watch_SlowConsumerDrop` enforces.
2. **Version monotonicity across restarts.** The `state.version` counter persists in SQLite; restart resumes from the persisted value. A test asserts the version does not regress on restart.
3. **JSON Merge Patch surprises.** Merge Patch deletes keys with `null`. Document this clearly in the API reference and add an explicit test.
4. **Bulk apply that mutates state mid-transaction.** Bulk apply validates *all* resources first against the current state, then applies them atomically. No partial state mutation; no observable interleaving.
5. **YAML × API merge confusion.** The §6.4 algorithm is deterministic but subtle. Document with examples and run integration tests for every merge case (yaml-only, api-only, conflict).
6. **ADS push storms.** Coalesce updates: if the state changes 50 times in 100 ms, push once with the final state, not 50 times. Coalescing window is configurable.
7. **Policy write rules that disable themselves.** A `deny` rule on `resource_kind: dataplane_api` writes locks the data plane. There must always be an `admin` escape: writes by an `admin`-scope JWT bypass deny rules but still go through audit. Tests assert.
8. **Schema evolution mid-flight.** Adding a new optional field to a resource is fine. Removing or renaming a field is a breaking change requiring an API version bump. The Phase 18 surface is `v1` forever; v2 is a separate path.

## Hand-off to Phase 19

Phase 19 inherits the dynamic-config API as the integration seam for:

- The Kubernetes operator (Phase 19 watches CRDs and writes to `/api/dataplane/v1/apply`).
- Federation (the leader's writes propagate to followers' API endpoints; followers' watches drive local state).
- Multi-instance hot-reload (Redis pub/sub triggers each instance to refresh from the leader's API).

The `metadata.source` provenance field gains `federation` as a value when Phase 19 lands; the merge rules extend accordingly.
