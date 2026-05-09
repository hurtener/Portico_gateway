# Phase 19 — Production Scale-Out

> Self-contained implementation plan. Builds on Phase 14–18. Lands the post-V1 production-readiness items the RFC §15 boundary deferred: Postgres-default storage, Redis-backed multi-instance coordination, Kubernetes operator + Helm chart, federation across instances, and container/microVM isolation modes for stdio MCP servers. Closes the V2 line.

## Goal

After Phase 19, an operator can deploy Portico in any of:

- **Single binary, SQLite** (V1 default) — unchanged, still supported.
- **Single binary, Postgres** — Postgres-backed storage with the same code paths and zero feature loss.
- **Multi-instance, Postgres + Redis** — N stateless Portico instances behind a load balancer, sharing Postgres for durable state and Redis for coordination (process-supervisor leasing, hot-reload notification, watch fan-out).
- **Kubernetes** — Helm chart + Operator that reconciles `Listener`, `Route`, `Backend`, `Tenant`, `SkillSource` CRDs onto a Portico fleet via the Phase 18 dynamic-config API.
- **Federated** — multiple Portico clusters in different regions / VPCs / trust boundaries, with controlled state replication for shared resources (e.g. a globally-published skill source) and strict isolation for tenant-scoped state.
- **Hardened isolation** — stdio MCP servers run in container or microVM sandboxes (`per_request` and `sidecar` runtime modes from RFC §6.3) with seccomp + landlock + cgroup limits.

Every combination preserves the V1 invariants: tenancy from day one, headless approvals, credentials behind the gateway, single binary as the artifact (the binary is what runs in containers; the operator is also a binary).

## Why this phase exists

The roadmap's V2 vision is "one gateway for all of a tenant's inbound traffic." That promise dies if Portico cannot run at production scale. The features Phase 19 lands are the ones a procurement team will list as table stakes for V2:

- Postgres-default for durable, backed-up state.
- Redis or equivalent for coordination so a 2-instance deployment is not silently inconsistent.
- Kubernetes-native deployment, because that is where most production tenants run.
- Federation, because organisations have multiple trust boundaries (per-region, per-business-unit, per-acquisition).
- Hardened isolation for stdio servers, because untrusted tools run untrusted code.

Each of these has a placeholder in earlier phases: the storage interface (Phase 0 §4.4), the supervisor's Spawn abstraction (RFC §6.3 says a sandboxed implementation drops in later), the data-plane API (Phase 18), federation hinted at in the V2 roadmap §6 risk register. Phase 19 lights them all up.

## Prerequisites

- Phase 14–18 complete. In particular:
  - Phase 18's dynamic-config API is the Kubernetes operator's writeable channel.
  - Phase 14's substrate is what the operator's CRDs project onto.
  - Phase 6's snapshot model is what federation replicates carefully.

## Out of scope (explicit)

- **No multi-cloud-native managed service.** Portico runs *on* Kubernetes; we do not build a hosted Portico offering in V2.
- **No automatic geo-routing.** Federation replicates state; it does not pick an instance for a client. DNS / GSLB is the operator's tool.
- **No CRDT-based merge for federation.** Federation is leader-based for shared resources and partition-based for tenant-scoped resources. CRDTs are post-V2.
- **No cross-cloud, cross-trust automatic key sharing.** Vault material does not cross trust boundaries. Each federated instance has its own vault; shared resources that need credentials reference the local vault on each instance.
- **No service mesh.** Same as Phase 15. Portico stays north-south.
- **No agentless monitoring of stdio MCP servers from the host OS.** Sandboxes are observed via Portico's existing telemetry pipeline (audit + spans + log capture). Host-level eBPF / sysdig integrations are post-V2.
- **No automatic Postgres schema migration tooling.** The migration shape from `internal/storage/sqlite/migrations/` extends to Postgres; operators run the migrations the same way (`portico migrate`). Online migrations / shadow tables are post-V2.

## Deliverables

### A. Postgres backend

1. **`internal/storage/postgres/`** — driver implementing the `Backend` interface defined by the §4.4 reference pattern. Pure Go via `jackc/pgx`. CGo-free.
2. **Postgres-flavoured migrations** — every existing migration in `internal/storage/sqlite/migrations/` has a Postgres counterpart in `internal/storage/postgres/migrations/`. Schemas are identical where possible; Postgres-only features (e.g. `BIGSERIAL`, `JSONB`) are used where they materially help.
3. **Driver dispatch by config** — `cfg.Storage.Driver` selects `sqlite` (default) or `postgres`. Connection string in `cfg.Storage.DSN`. Factory dispatch follows the §4.4 reference.
4. **Cross-driver test suite** — integration tests parametrised over both drivers; CI runs both legs.

### B. Redis coordination

5. **`internal/coord/redis/`** — driver behind a new `internal/coord/ifaces` interface. Capabilities: distributed leases (process-supervisor leadership for `per_tenant` modes when multiple instances might race), pub/sub for hot-reload notifications, watch fan-out across instances, optimistic-lock counter for the dataplane state version.
6. **`internal/coord/single`** — fallback driver for single-instance deployments. No-op leases, in-process pub/sub.
7. **Driver dispatch by config** — `cfg.Coordinator.Driver` selects `single` (default) or `redis`. Connection string in `cfg.Coordinator.DSN`.
8. **Multi-instance integration test** — `test/integration/multi_instance_test.go` boots two Portico processes against shared Postgres + Redis, asserts: (a) leader election for `per_tenant` supervisor races yields exactly one process per (tenant, server), (b) a write to instance A's data-plane API propagates to instance B's state via Redis pub/sub within 1 s, (c) audit events from both instances are queryable via either's REST API.

### C. Kubernetes operator + Helm chart

9. **`deploy/helm/portico/`** — Helm chart for the Portico binary (Deployment, Service, ConfigMap, Secret, optional Ingress).
10. **`deploy/helm/portico-operator/`** — Helm chart for the operator binary.
11. **`cmd/portico-operator/`** — operator binary (separate from `cmd/portico` to keep it optional). Watches CRDs, reconciles to Portico instances via the Phase 18 dynamic-config API.
12. **CRDs**: `Listener`, `Route`, `Backend`, `Bridge`, `Tenant`, `SkillSource`, `SecurityPolicy`. Schemas mirror the Phase 18 API resource shapes.
13. **Operator features**: idempotent reconcile, drift detection (CRD vs. Portico state), status-subresource updates with health, owner-reference cleanup on CRD delete, finalisers for graceful tenant deletion.
14. **kustomize overlays** — `deploy/kustomize/{base,dev,prod}` for non-Helm operators.

### D. Federation

15. **`internal/federation/`** — protocol + implementations for cross-instance state sync. Two scopes:
    - **Tenant-scoped resources**: never federated. Each instance is authoritative for the tenants it hosts.
    - **Shared resources**: optionally federated. Includes `SkillSource` definitions (so a globally-published skill source is consistent across regions), `SecurityPolicy` templates, and the `Tenant` directory (so an authentication request to instance A can identify a tenant whose home is instance B and route accordingly).
16. **Federation transport**: HTTPS-pull with signed manifests. The leader publishes a signed snapshot at `/api/federation/v1/snapshot`; followers pull on a schedule. No leader-push; followers retry on failure.
17. **Cross-tenant isolation in federation** — federation messages cannot carry tenant-scoped data. Compile-time + integration tests assert.
18. **Federation Console screens** — `/federation/peers`, `/federation/peers/[id]`, replication lag dashboard.

### E. Hardened isolation

19. **`internal/runtime/sandbox/`** — Spawn implementations for `container` and `microvm` modes. Container mode uses runc (rootless if available); microVM mode uses Firecracker (Linux only, optional behind a build tag).
20. **Runtime modes added**: `per_request` (cold start per call, pre-existing in RFC §6.3 deferred list), `sidecar` (container per (tenant, server)).
21. **Resource limits** — cgroup memory + CPU quota, seccomp profile (curated allow-list), landlock filesystem allow-list, no-network unless declared.
22. **Tenant policy gate** — sandbox modes opt-in per tenant per server; default behaviour for existing servers is unchanged (subprocess mode with optional seccomp/landlock).

## Acceptance criteria

### A. Postgres

1. **Schema parity.** Every Phase 0–18 schema exists in `internal/storage/postgres/migrations/`. `portico migrate --driver postgres` succeeds against a fresh Postgres 15 instance.
2. **Behaviour parity.** Every cross-driver integration test passes against Postgres.
3. **Performance.** Postgres p99 latency for catalog snapshot read is ≤ 1.5× SQLite for the same fixture (Postgres is fundamentally networked).
4. **Migration ordering.** Migrations are forward-only and version-tracked, mirroring SQLite policy. `AGENTS.md` §9 rule applies.

### B. Coordination

5. **Single-instance fallback.** `cfg.Coordinator.Driver: single` (or absent) preserves Phase 18 behaviour exactly.
6. **Multi-instance leader election.** Two instances racing to start a `per_tenant` MCP server result in exactly one process; the loser registers as observer.
7. **Multi-instance hot reload.** A YAML edit on instance A is visible on instance B within 1 s when Redis coordination is enabled.
8. **Watch fan-out across instances.** A POST to instance A's `/api/dataplane/v1/routes` produces a watch event on a subscriber connected to instance B.

### C. Kubernetes

9. **Helm install.** `helm install portico ./deploy/helm/portico` brings up a single-replica deployment that passes all Phase 1–13 smokes against a Postgres + Redis fixture.
10. **Operator install.** `helm install portico-operator ./deploy/helm/portico-operator` deploys the operator. Creating a `Route` CRD reconciles to the Portico instance via the Phase 18 API; a smoke check confirms the route resolves traffic.
11. **CRD lifecycle.** Deleting a `SkillSource` CRD triggers the operator's finaliser, which calls Portico's API to remove the source; only then does the CRD finalise.
12. **Status subresource.** Each CRD's `.status` reflects the actual Portico state (synced / pending / error). A reconcile-loop test asserts status converges within 5 s.
13. **CRD schemas validate.** OpenAPI v3 schemas reject malformed manifests at `kubectl apply` time.

### D. Federation

14. **Shared skill source replicates.** A `SkillSource` declared as `federation.shared: true` on the leader appears on every follower within `replication_interval` (default 30 s).
15. **Tenant-scoped resource does not replicate.** Even if mistakenly marked shared, tenant-scoped data is filtered out before publishing; an audit event records the rejected attempt.
16. **Signed snapshot verification.** Followers verify the leader's snapshot signature against a configured trust root; an unsigned or wrong-signed snapshot is refused.
17. **Replication lag observable.** `/api/federation/peers` returns `last_pulled_at`, `last_pull_status`, `lag_seconds` per peer. The Console dashboard surfaces this.

### E. Isolation

18. **Container mode round trip.** A stdio MCP server registered with `runtime: { mode: sidecar, sandbox: container }` spawns under runc, serves `tools/list` correctly, and is terminated when the supervisor decides.
19. **microVM mode round trip (Linux + build tag).** Same with Firecracker, behind `portico_microvm` build tag.
20. **Resource-limit enforcement.** A sandboxed server that allocates beyond `cgroup.memory_limit` is OOM-killed and the supervisor records `runtime.oom_killed` audit event.
21. **Seccomp denial.** A sandboxed server attempting a denied syscall (e.g. `mount`) is killed; audit event captures the denied syscall.
22. **No-network default.** A sandboxed server has no network access unless `runtime.allow_network: true`. Tested by attempting an outbound HTTP call from inside the sandbox.

### Common gates

23. **Smoke gate.** `scripts/smoke/phase-19.sh` shows OK ≥ 30, FAIL = 0; prior phases' smokes still pass.
24. **Coverage.** New packages (`internal/storage/postgres`, `internal/coord/redis`, `internal/federation`, `internal/runtime/sandbox`, `cmd/portico-operator`) ≥ 80%.
25. **Single-binary invariant preserved for `cmd/portico`.** The operator is a separate binary by design; the gateway itself remains single-binary, CGo-free.

## Architecture

### 6.1 Storage seam (Postgres lands)

The §4.4 storage seam, established in Phase 0 around SQLite, lights up its second driver. Production code paths are unchanged; the driver-dispatching factory picks Postgres when configured. Cross-driver test parametrisation enforces that nothing in the calling code accidentally depends on SQLite-only behaviour.

### 6.2 Coordination seam

A new seam at `internal/coord/ifaces`:

```go
type Coordinator interface {
    AcquireLease(ctx context.Context, key string, ttl time.Duration) (Lease, error)
    Subscribe(ctx context.Context, channel string) (<-chan Event, error)
    Publish(ctx context.Context, channel string, payload []byte) error
    NextVersion(ctx context.Context, key string) (uint64, error)
}
```

Drivers: `single` (in-process) and `redis`. Production code (process supervisor, dataplane state store, hot-reload watcher) talks to this interface.

### 6.3 Operator architecture

```
+----------------------+       +------------------------+
|  Kubernetes API      | <---  |  portico-operator      |
|  (CRDs)              | --->  |  (controller-runtime)  |
+----------------------+       +-----------+------------+
                                           |
                                           | HTTPS / Phase 18 API
                                           v
                               +------------------------+
                               |  Portico instance(s)   |
                               +------------------------+
```

The operator never reaches into Portico's storage. It is a Phase 18 API consumer. This means the operator works against any Portico binary that ships Phase 18, including non-Kubernetes deployments (an operator could run on a developer laptop and reconcile a remote Portico).

### 6.4 Federation model

```
                 +----------+
                 |  Leader  |   /api/federation/v1/snapshot (signed)
                 +----+-----+
                      |
        +-------------+-------------+
        |             |             |
        v             v             v
   +--------+    +--------+    +--------+
   |Follower|    |Follower|    |Follower|
   +--------+    +--------+    +--------+
```

Followers pull on a schedule. The snapshot contains only resources marked `federation.shared: true`. Each follower applies the snapshot to its local state via the same Phase 18 internal write paths (with `metadata.source: federation`).

Tenant-scoped resources never appear in the snapshot. An assertion at the snapshot-publication boundary enforces this; an integration test deliberately tries to publish a tenant-scoped resource and asserts the rejection.

### 6.5 Sandbox model

The supervisor's `Spawn(spec)` abstraction (RFC §6.3) gains driver dispatch:

```go
type Spawner interface {
    Spawn(ctx context.Context, spec SpawnSpec) (Process, error)
}

// Drivers:
// internal/runtime/spawn/subprocess  (Phase 2 default)
// internal/runtime/spawn/container   (Phase 19, runc-based, rootless when possible)
// internal/runtime/spawn/microvm     (Phase 19, Firecracker, build-tag-gated)
```

Per-server runtime config picks the driver. Default for existing configurations remains `subprocess` (no behaviour change without operator opt-in).

## Configuration extensions

```yaml
storage:
  driver: postgres                       # default sqlite
  dsn: postgres://portico:***@db/portico

coordinator:
  driver: redis                          # default single
  dsn: redis://redis:6379/0

federation:
  role: leader                           # leader | follower | none
  peers:
    - id: us-east
      url: https://us-east.portico.example.com
      trust_root: /etc/portico/federation-root.pem
  snapshot:
    interval: 30s
    signing_key: /etc/portico/federation-signing.key

runtime:
  default_spawner: subprocess
  per_server:
    - id: github
      runtime:
        mode: sidecar
        sandbox: container
        resource_limits: { memory: 512Mi, cpu_quota: 0.5 }
        allow_network: true
        seccomp_profile: default
        landlock_paths: [/tmp/github-mcp, ro:/etc/ssl/certs]
```

All sub-blocks are optional; defaults preserve V1.5 behaviour.

## REST APIs

Federation:

| Method | Path                                     | Scope    | Returns                          |
|--------|------------------------------------------|----------|----------------------------------|
| GET    | `/api/federation/peers`                  | admin    | peer list with replication state |
| POST   | `/api/federation/peers/{id}/refresh`     | admin    | 202                              |
| GET    | `/api/federation/v1/snapshot`            | federation-pull token | signed snapshot     |

Operator-facing API surface is Phase 18's; no new endpoints from the operator side.

## Implementation walkthrough

This is the largest V2 phase; it ships in three sub-phases that can land in separate PRs.

### Sub-phase 19a — Postgres + coordination

1. Postgres driver + migrations.
2. Coordinator interface + single-instance driver (no-op).
3. Redis driver.
4. Cross-driver and multi-instance integration tests.
5. Smoke updates: phase-19a.sh covers Postgres parity + multi-instance hot reload.

### Sub-phase 19b — Kubernetes operator

6. CRD definitions + OpenAPI schemas.
7. Operator binary (controller-runtime).
8. Reconciliation logic (CRD ↔ Phase 18 API).
9. Helm charts (gateway + operator).
10. Smoke updates: phase-19b.sh covers helm install + CRD round trip.

### Sub-phase 19c — Federation + isolation

11. Federation snapshot publish + pull.
12. Signed-snapshot verification.
13. Container spawner (runc).
14. microVM spawner (Firecracker, build-tag).
15. Smoke updates: phase-19c.sh covers federation round trip + sandboxed spawn.

Each sub-phase is independently shippable. Phase 19 is "done" when all three have landed and the combined `scripts/smoke/phase-19.sh` passes.

## Test plan

Postgres / coordination:

- `TestStorage_Postgres_AllOperations_Parity`
- `TestStorage_Postgres_Migrations_FreshDB`
- `TestCoord_Redis_LeaseAcquireRenewExpire`
- `TestCoord_Redis_PubSub_DeliveryAcrossInstances`
- `TestE2E_MultiInstance_LeaderElection_PerTenantSpawn`
- `TestE2E_MultiInstance_HotReload_Propagates`
- `TestE2E_MultiInstance_DataPlaneAPI_Watch_FansOut`

Kubernetes:

- `TestE2E_Helm_InstallGateway_PassesPriorSmokes`
- `TestE2E_Operator_RouteCRD_RoundTrip`
- `TestE2E_Operator_SkillSourceCRD_FinaliserCleanup`
- `TestE2E_Operator_StatusSubresource_Converges`
- `TestE2E_Operator_DriftDetection_RecoversFromManualChange`

Federation:

- `TestE2E_Federation_LeaderPublishesSignedSnapshot`
- `TestE2E_Federation_FollowerVerifiesAndApplies`
- `TestE2E_Federation_RejectsBadSignature`
- `TestE2E_Federation_NeverPublishesTenantScopedResource`
- `TestE2E_Federation_ReplicationLagMetric`

Isolation:

- `TestE2E_Sandbox_Container_RoundTrip`
- `TestE2E_Sandbox_Container_OOMKilled_AuditEvent`
- `TestE2E_Sandbox_Container_SeccompDenial_AuditEvent`
- `TestE2E_Sandbox_Container_NoNetworkByDefault`
- `TestE2E_Sandbox_microVM_RoundTrip` (build-tag, Linux only)

## Common pitfalls

1. **SQLite-flavoured SQL leaking into shared queries.** Cross-driver tests catch most of these. The fix is a typed query layer per driver where divergence is unavoidable; everything else uses standard SQL.
2. **Redis as a hard dependency.** The single-instance fallback must remain frictionless. CI runs both `single` and `redis` legs to prevent inadvertent Redis-only paths.
3. **Operator that bypasses Phase 18 validation.** The operator is a Phase 18 API consumer. It does not have a back door. Tests assert that every reconcile is observable as a `dataplane.config_changed` audit event.
4. **CRD schema drift.** When a Phase 14–18 resource gains a field, the operator's CRD must gain it too. CI cross-references via a generator that produces CRDs from the Phase 18 OpenAPI surface.
5. **Federation that leaks tenant data.** The §6.4 boundary test is non-negotiable. A snapshot-publication boundary helper is the single function that gates publication; deleting that helper or bypassing it is a §13 forbidden practice.
6. **Container spawner that runs as root in production.** Default to rootless; document the trade-off; integration tests cover both modes.
7. **Firecracker that depends on a host-side daemon.** The build-tag isolation is real. Operators on non-Linux platforms get a clear "microVM not available on this build" error rather than a silent fallback.
8. **Operator writing to multiple Portico instances inconsistently.** When federation is on, the operator targets the leader; followers receive via federation. When federation is off, the operator may target any instance and Redis coordination handles the rest.

## Hand-off — V2 ships

V2 is feature-complete with Phase 19. The artifacts that ship as **V2.0**:

- `bin/portico` — the gateway binary, single-process, all V2 features available subject to configuration.
- `bin/portico-operator` — the Kubernetes operator binary.
- `deploy/helm/{portico,portico-operator}` — Helm charts.
- `deploy/kustomize/...` — overlays.
- `docs/v2/` — the V2 documentation set (built on Phase 12's docs surface).
- Multi-arch container images (linux/amd64, linux/arm64) for both binaries.
- Signed release artifacts (cosign).

Post-V2 territory: model-side defences, cross-cloud key federation, multi-region active-active with quorum, hosted SaaS Portico, agent framework integrations. None of these are pre-planned; they are negotiated when queued.
