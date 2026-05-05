# Phase 6 — Catalog Snapshots & Observability

> Self-contained implementation plan. Builds on Phase 0–5. Final phase before V1 is declared done.

## Goal

Make the gateway's behavior **reproducible and inspectable**. Every session gets a persisted catalog snapshot (server set, tool list with schemas, prompt list, resource list, skill set, policies, credential strategies). Schema fingerprinting catches upstream drift and surfaces it as an alert. OpenTelemetry tracing spans every cross-component call so an operator can see "PR review session → tool call → policy → credential exchange → southbound → response" as one trace. The Console becomes a working session inspector that ties the audit trail (Phase 5), the snapshot, and the trace together. After Phase 6, V1 is shippable.

## Why this phase exists

Without snapshots, an audit event of "tool call X happened" is hard to interpret six months later — the catalog X belonged to has long since changed. Without drift detection, a downstream MCP server can silently change its tool schema and break policy assumptions (e.g. add a destructive parameter) without anyone noticing. Without tracing, debugging a hot path means stitching slog events together by hand. These three together close the gap from "works in demo" to "operable in production."

## Prerequisites

Phases 0–5 complete. Specifically:
- Audit store persists events with `trace_id`/`span_id` columns (Phase 5).
- Policy engine returns Decisions that reference a tool/server/skill (Phase 5).
- Tool aggregator already caches a per-session tool list for 60s (Phase 1).

## Deliverables

1. Snapshot service `internal/catalog/snapshots/` with create, get, diff, list operations; persists to `catalog_snapshots` table.
2. Schema fingerprinting in `internal/catalog/snapshots/fingerprint.go`.
3. Drift detector at `internal/catalog/snapshots/drift.go`; emits `schema.drift` audit event when an active snapshot's fingerprint diverges from current upstream state.
4. List-changed semantics: stable by default, opt-in `live` (Phase 3 wired the mux; Phase 6 binds it to snapshots).
5. OpenTelemetry tracing in `internal/telemetry/otel.go` — spans for session, request dispatch, policy, approval, credentials, southbound call, audit emit. OTLP exporter (gRPC + HTTP) configurable.
6. Trace context propagation across northbound and southbound boundaries (`traceparent` header).
7. Session inspector deepened: timeline view of trace + audit events; snapshot diff view; drift banner.
8. Snapshot APIs (`/v1/catalog/snapshots/*`).
9. CLI: `portico inspect-session <id>` outputs a structured dump for offline analysis.
10. Tests: snapshot stability across upstream changes, drift detection, list-changed mode behavior, OTel span hierarchy correctness, trace propagation.

## Acceptance criteria

1. Every session created via the northbound endpoint has a `catalog_snapshots` row created at session start, with `payload_json` containing the canonicalized effective catalog (tools list with schemas, resource list, prompt list, enabled skills, policies, credential strategies, server health hashes).
2. The snapshot is referenced by every audit event in the session via `catalog_snapshot_id`. Verified by integration test.
3. While a session is active, calling the same `tools/list` twice returns identical results even if a downstream server's tool list has changed in between (stable mode). Verified by mocking a list_changed mid-session.
4. Schema fingerprinting: each downstream server's `Tools` list is hashed deterministically. The hash is recorded on the snapshot. After session start, if a downstream's hash diverges, a `schema.drift` event is emitted with old/new hashes and a diff summary.
5. Drift event includes structured `diff: {added: [...], removed: [...], modified: [{tool, fields_changed}]}`.
6. OTel traces: `tools/call` produces a parent span `mcp.tool_call` with attributes `mcp.tool=<name>`, `tenant.id`, `session.id`, child spans `policy.evaluate`, `approval.flow` (if applicable), `credential.resolve`, `southbound.call`, plus a final span for the audit emit. Verified by an in-memory exporter test.
7. Trace context propagated southbound: HTTP MCP servers receive a `traceparent` header on every request; stdio MCP servers receive a `MCP_TRACEPARENT` env var per process and the dispatcher injects `_meta.traceparent` on each request.
8. Session inspector shows: (a) snapshot summary, (b) live tool calls with span timeline, (c) drift banner if any during the session, (d) link to snapshot diff page.
9. Snapshot diff: `GET /v1/catalog/snapshots/{a}/diff/{b}` returns a structured diff usable by tooling.
10. `portico inspect-session <id> --output json` produces a complete dump (snapshot + audit events + trace summary) usable for offline analysis.
11. V1 success criteria from RFC §19.2 all pass.

## Architecture

```
                      [northbound POST initialize]
                                  |
                                  v
         +---------------------------------------------------+
         | SnapshotService                                    |
         |   create()                                         |
         |   resolveEffectiveCatalog()                        |
         |   fingerprintEachServer()                          |
         |   persist()                                        |
         +-------------------+--------------------------------+
                             |
                             v  (snapshot_id stamped on session)
+--------------------------------------------------+
| Dispatcher                                       |
|   tools/list -> from snapshot in stable mode    |
|   tools/call -> policy/cred/dispatch (Phase 5)  |
|   audit events carry snapshot_id + trace_id      |
+----------------+---------------------------------+
                 |
                 v
+--------------------------------------------------+
| DriftDetector (background)                       |
|   periodic recompute of upstream fingerprints    |
|   compare with active snapshots                  |
|   emit schema.drift on divergence                |
+--------------------------------------------------+

+--------------------------------------------------+
| OTel Provider                                    |
|   in-process tracer + OTLP exporter              |
|   propagator: traceparent (W3C)                  |
+--------------------------------------------------+
```

## Package layout

```
internal/catalog/snapshots/
  snapshot.go            // types
  service.go             // create / get / list / diff
  fingerprint.go         // canonical hashing
  drift.go               // background detector
  service_test.go
  drift_test.go
internal/telemetry/
  otel.go                // tracer setup, exporter wiring
  propagation.go         // traceparent handling
  attrs.go               // semantic attribute keys
  otel_test.go
internal/mcp/southbound/
  trace_inject.go        // inject _meta.traceparent / env var
internal/server/api/
  handlers_snapshots.go
internal/server/mcpgw/
  dispatcher.go          // augment with snapshot reads + spans
cmd/portico/
  cmd_inspect_session.go
web/console/src/routes/
  sessions/[id]/+page.svelte         # session inspector
  snapshots/[id]/+page.svelte        # snapshot view
  snapshots/[a]/diff/[b]/+page.svelte
test/integration/
  snapshot_e2e_test.go
  drift_e2e_test.go
  trace_e2e_test.go
```

## Snapshot model

```go
// internal/catalog/snapshots/snapshot.go
package snapshots

type Snapshot struct {
    ID         string         // ULID
    TenantID   string
    SessionID  string         // ID of session it was generated for
    CreatedAt  time.Time
    Servers    []ServerInfo
    Tools      []ToolInfo
    Resources  []ResourceInfo
    Prompts    []PromptInfo
    Skills     []SkillInfo
    Policies   PoliciesInfo
    Credentials []CredentialInfo
    OverallHash string                  // deterministic across all sub-fingerprints
}

type ServerInfo struct {
    ID            string
    DisplayName   string
    Transport     string
    RuntimeMode   string
    SchemaHash    string                // hash of tools list at snapshot time
    Health        string
}

type ToolInfo struct {
    NamespacedName string                // e.g. github.get_pull_request
    ServerID       string
    Description    string
    InputSchema    json.RawMessage
    Annotations    *protocol.ToolAnnotations
    RiskClass      string                // resolved (server default + skill override)
    RequiresApproval bool
    Hash           string                // sha256 of canonical JSON
    SkillID        string                // origin if from skill required_tools (else "")
}

type ResourceInfo struct {
    URI         string
    UpstreamURI string
    ServerID    string
    MIMEType    string
}

type PromptInfo struct {
    NamespacedName string
    ServerID       string
    Arguments      []protocol.PromptArgument
}

type SkillInfo struct {
    ID            string
    Version       string
    EnabledForSession bool
    MissingTools  []string
}

type PoliciesInfo struct {
    AllowList []string
    DenyList  []string
    ApprovalTimeout time.Duration
    DefaultRiskClass string
}

type CredentialInfo struct {
    ServerID string
    Strategy string
    SecretRefs []string  // names only, no values
}
```

The snapshot's `payload_json` is the canonical serialization of this struct (sorted maps, no whitespace). `OverallHash = sha256(payload_json)`.

## Service

```go
// internal/catalog/snapshots/service.go
package snapshots

type Service struct {
    db        *sqlite.DB
    registry  *registry.Registry
    sup       *process.Supervisor
    skills    *runtime.Catalog
    enable    *runtime.Enablement
    policy    *policy.Engine
    audit     audit.Emitter
    log       *slog.Logger
}

func (s *Service) Create(ctx context.Context, sessionID, tenantID, userID string) (*Snapshot, error)
func (s *Service) Get(ctx context.Context, id string) (*Snapshot, error)
func (s *Service) List(ctx context.Context, tenantID string, q ListQuery) ([]*Snapshot, string, error)
func (s *Service) Diff(ctx context.Context, idA, idB string) (*Diff, error)

type Diff struct {
    Tools struct {
        Added    []string
        Removed  []string
        Modified []ModifiedTool
    }
    Resources struct {
        Added   []string
        Removed []string
    }
    Prompts struct { /* same shape as Tools */ }
    Skills  struct { /* same shape as Tools */ }
}

type ModifiedTool struct {
    Name           string
    FieldsChanged  []string  // e.g. ["input_schema", "annotations.destructiveHint"]
    OldHash        string
    NewHash        string
}
```

`Create` flow:
1. Resolve all servers enabled for the tenant.
2. For each server, call `client.ListTools()`, `ListResources()`, `ListPrompts()` (use the same client the dispatcher will use, with the same credentials).
3. Compute per-tool risk class via policy engine.
4. Filter by skill enablement and tenant entitlements.
5. Compute fingerprints.
6. Build the Snapshot struct; serialize with deterministic JSON (sorted keys, no extra whitespace) using `encoding/json` + a sort step.
7. Persist via `catalog_snapshots` table. Stamp the `sessions.snapshot_id` column.
8. Emit `snapshot.created` audit event.

If any per-server list fails, the snapshot still creates with the remaining servers; failures are recorded in `payload_json.warnings`.

## Fingerprinting

```go
// internal/catalog/snapshots/fingerprint.go
package snapshots

func ServerFingerprint(tools []protocol.Tool) string  // sha256 of canonical JSON of sorted tools
func ToolFingerprint(t protocol.Tool) string          // per-tool hash
func ResourcesFingerprint(rs []protocol.Resource) string
func PromptsFingerprint(ps []protocol.Prompt) string
```

Canonicalization rules:
- Lists sorted by stable key (`name` for tools/prompts, `uri` for resources).
- Object keys sorted alphabetically.
- No insignificant whitespace.
- `null` values omitted from objects (so `{"x": null}` ≡ `{}`).

The whole snapshot has `OverallHash = sha256(canonical JSON of all sub-hashes)`.

## Drift detector

```go
// internal/catalog/snapshots/drift.go
package snapshots

type Detector struct {
    db        *sqlite.DB
    service   *Service
    sup       *process.Supervisor
    audit     audit.Emitter
    log       *slog.Logger
    interval  time.Duration  // default 60s
}

func (d *Detector) Run(ctx context.Context)
```

Loop:
1. List active sessions (sessions.ended_at IS NULL).
2. For each, get its snapshot.
3. For each server in the snapshot, fetch live tool list (cheap; uses the existing client).
4. Compare hashes. If different, build a Diff. Emit `schema.drift` event on the session, including diff payload.
5. Sleep `interval`.

Drift event:

```json
{
  "type": "schema.drift",
  "tenant_id": "acme",
  "session_id": "sess_123",
  "catalog_snapshot_id": "cat_abc",
  "server_id": "github",
  "old_hash": "...",
  "new_hash": "...",
  "diff": {
    "tools": {
      "added":   ["github.search_code"],
      "removed": [],
      "modified": [{"name":"github.create_review_comment","fields_changed":["input_schema"]}]
    }
  }
}
```

The session does NOT silently switch to the new schema — it keeps using the snapshot. To pick up changes, the client must initiate a new session OR opt into live mode.

If `schema.drift` events outnumber some threshold (configurable, default 5/min for one server), promote to a tenant-level alert in the Console.

## List-changed semantics

Phase 3 wired the mux. Phase 6 gives it teeth:

- **Stable mode** (default): downstream `notifications/{tools,resources,prompts}/list_changed` does NOT propagate. Audit event `list_changed_suppressed` includes a hint that drift detection will compare on the next interval.
- **Live mode**: downstream notification IS forwarded. The dispatcher creates a *new snapshot* for the session and updates `sessions.snapshot_id`. The old snapshot is preserved (immutable). The audit chain follows the new snapshot ID for subsequent events.

Live mode opt-in via initialize params:

```json
"experimental": {
  "portico/listChanged": "live"
}
```

Or via tenant config default (`list_changed.default_mode: live`).

## OpenTelemetry

### Setup

```go
// internal/telemetry/otel.go
package telemetry

type Config struct {
    Enabled       bool
    ServiceName   string  // default "portico"
    Exporter      string  // "otlp_grpc" | "otlp_http" | "stdout" | "none"
    OTLPEndpoint  string
    OTLPHeaders   map[string]string
    SampleRate    float64 // 0..1; default 1.0 (everything)
    ResourceAttrs map[string]string
}

func Init(ctx context.Context, cfg Config, log *slog.Logger) (shutdown func(context.Context) error, err error)
```

`Init` registers the OTel TracerProvider globally. `shutdown` flushes pending spans on process exit.

### Spans

| Span name              | Attributes                                                                   |
|------------------------|------------------------------------------------------------------------------|
| `mcp.session`          | `session.id`, `tenant.id`, `user.id`                                         |
| `mcp.request`          | `mcp.method`, `mcp.request_id`                                              |
| `mcp.tool_call`        | `mcp.tool`, `mcp.server_id`, `mcp.skill_id`, `tenant.id`, `session.id`      |
| `policy.evaluate`      | `policy.allow`, `policy.reason`, `policy.requires_approval`, `policy.risk_class` |
| `approval.flow`        | `approval.id`, `approval.elicit`, `approval.outcome`                        |
| `credential.resolve`   | `credential.strategy`, `credential.cache_hit`                               |
| `southbound.call`      | `mcp.server_id`, `mcp.transport`, `peer.url` (HTTP only)                    |
| `audit.emit`           | `audit.type`                                                                 |
| `snapshot.create`      | `snapshot.id`, `snapshot.servers`, `snapshot.tools_count`                   |
| `snapshot.drift_check` | `snapshot.id`, `drift.detected`                                             |

Semantic attribute keys defined once in `internal/telemetry/attrs.go` to avoid drift.

### Propagation

```go
// internal/telemetry/propagation.go
func ExtractFromHTTP(r *http.Request) context.Context  // reads traceparent
func InjectIntoHTTP(ctx context.Context, h http.Header) // writes traceparent
func ExtractFromMCPMeta(meta json.RawMessage) context.Context
func InjectIntoMCPMeta(ctx context.Context, meta map[string]any) // adds traceparent
```

Northbound: extract on POST, inject into outgoing notifications (`_meta.traceparent`) so clients can correlate.

Southbound:
- HTTP transport: inject `traceparent` header on each request.
- Stdio transport: inject `MCP_TRACEPARENT` env var at process start (best-effort; downstream may ignore). Also inject `_meta.traceparent` on each request payload — most modern MCP servers preserve `_meta`.

### Sampling

V1 default is 1.0 (record everything). Operators can lower for high-traffic deployments. A future post-V1 enhancement: tail-based sampling on errors only.

## Session inspector

`web/console/src/routes/sessions/[id]/+page.svelte`:
- Header: session ID, tenant, user, started, snapshot link, drift banner if any.
- Tabs (use the component-library Tabs):
  - **Trace**: timeline rendered with a small SVG component reading from a Svelte store.
  - **Audit**: filterable event list.
  - **Snapshot**: rendered summary; link to diff with previous session of same tenant.
  - **Approvals**: pending and decided.
  - **Errors**: failures only.

Drift banner: if any `schema.drift` event for this session, show red banner at top with link to `/snapshots/{id}/diff/{current}`.

## CLI

```
portico inspect-session <session_id> [--output json|table] [--include-trace] [--since=...]
```

Output (JSON shape):

```json
{
  "session": {...},
  "snapshot": {...},
  "audit_events": [...],
  "approvals": [...],
  "drift_events": [...],
  "trace_summary": {
    "total_spans": 42,
    "duration_ms": 1840,
    "errors": 0
  }
}
```

## External APIs

```
POST   /v1/catalog/resolve
       Body: {"session_id": "..."}
       → 200 {snapshot}            (creates a snapshot for the active session)

GET    /v1/catalog/snapshots/{id}
       → 200 {snapshot}

GET    /v1/catalog/snapshots
       Query: ?tenant_id=&since=&limit=
       → 200 [{snapshot summary}]

GET    /v1/catalog/snapshots/{a}/diff/{b}
       → 200 {diff}

# Session inspector data sources
GET    /v1/sessions/{id}/snapshot         → 200 {snapshot}
GET    /v1/sessions/{id}/audit_events     → 200 [{event}]    (filtered to session)
GET    /v1/sessions/{id}/spans            → 200 [{span}]      (from in-memory ring; OTel exporter is canonical)
```

## SQLite migration

```sql
-- 0006_snapshots_extended.sql
ALTER TABLE catalog_snapshots ADD COLUMN overall_hash TEXT;

CREATE TABLE IF NOT EXISTS schema_fingerprints (
    tenant_id    TEXT NOT NULL,
    server_id    TEXT NOT NULL,
    seen_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    hash         TEXT NOT NULL,
    tools_count  INTEGER NOT NULL,
    PRIMARY KEY (tenant_id, server_id, hash)
);

CREATE INDEX IF NOT EXISTS idx_fingerprints_server ON schema_fingerprints(tenant_id, server_id, seen_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (6);
```

## Implementation walkthrough

### Step 1: Snapshot types + service

Implement `Service.Create`. Test that the same input produces the same `OverallHash` (determinism is non-negotiable).

### Step 2: Fingerprinting

Canonical JSON marshaller using a recursive sorter. Include benchmark to keep an eye on cost — snapshots are created once per session but fingerprint comparisons happen at the drift interval × server count.

### Step 3: Drift detector

Background goroutine started by the server bootstrap with the supervisor's lifetime. Skip sessions older than 24h (assume abandoned).

### Step 4: Bind snapshots to sessions

`session_registry.Create` (Phase 1) now calls `Service.Create` after the initialize handshake completes. `sessions.snapshot_id` populated.

### Step 5: Stable tools/list

Modify `mcpgw.Dispatcher.handleToolsList`:
- Stable mode: read `Session.SnapshotID` → load snapshot → return its `Tools`. Do not fan out.
- Live mode: fan out as before (Phase 1 path). On every fan-out, update the session's snapshot with a new ID; emit `snapshot.refreshed`.

### Step 6: OTel wiring

Tracer initialized at startup. Span creation in:
- `northbound/http/transport.go::ServeHTTP` → `mcp.request` (parent).
- Dispatcher per-method → `mcp.tool_call` etc.
- Policy engine → `policy.evaluate`.
- Approval flow → `approval.flow`.
- Injectors → `credential.resolve`.
- Southbound `CallTool` → `southbound.call`.
- Audit emit → `audit.emit`.

Use `otel.Tracer("portico")` once per package; cache.

### Step 7: Trace propagation

`InjectIntoMCPMeta` writes `traceparent` and (if present) `tracestate` into the outgoing request's `_meta`. `ExtractFromMCPMeta` reads them on the way back. For stdio, also set `MCP_TRACEPARENT` env var at process spawn (Phase 2's spawner takes a `cmd.Env`).

### Step 8: Session inspector

The inspector reads from SQLite (audit + snapshots + approvals) and queries an in-memory span ring buffer. The ring is a per-session bounded buffer (size 256) populated by an OTel `SpanProcessor` callback — exists for UI immediacy when the OTLP exporter is async/external.

### Step 9: CLI

`cmd_inspect_session.go` reads from SQLite directly (read-only DB open). Output is structured for piping into `jq`.

## Test plan

### Unit

- `internal/catalog/snapshots/service_test.go`
  - `TestCreate_DeterministicHash` — same registry/skills produce same OverallHash.
  - `TestCreate_StableUnderUnsortedInput` — change order of registry list, hash unchanged.
  - `TestCreate_TenantA_DoesNotIncludeTenantBData`
  - `TestDiff_AddedRemovedModified` — synthetic snapshots with controlled changes.
  - `TestDiff_NoChange_EmptyDiff`

- `internal/catalog/snapshots/fingerprint_test.go`
  - `TestServerFingerprint_Stable`
  - `TestServerFingerprint_DifferentToolOrder_SameHash`
  - `TestServerFingerprint_OneToolChange_DifferentHash`

- `internal/catalog/snapshots/drift_test.go`
  - `TestDetector_NoDrift_NoEvent`
  - `TestDetector_DriftDetected_EmitsEvent`
  - `TestDetector_RemovedSession_NoLongerChecked`

- `internal/telemetry/otel_test.go`
  - `TestInit_StdoutExporter` — verify a span emitted lands in the writer.
  - `TestSpanHierarchy_ToolCall` — using in-memory exporter, assert parent-child relationships.
  - `TestPropagation_RoundTrip` — inject + extract; expect identical context.

### Integration

- `test/integration/snapshot_e2e_test.go`
  - `TestE2E_SnapshotCreatedAtSessionStart`
  - `TestE2E_StableMode_ToolsListUnchangedAfterUpstreamChange`
  - `TestE2E_LiveMode_ToolsListChangesWithUpstream`
  - `TestE2E_AuditEventsCarrySnapshotID`

- `test/integration/drift_e2e_test.go`
  - `TestE2E_DownstreamSchemaChange_EmitsDrift`
  - `TestE2E_DownstreamRemovedTool_EmitsDriftRemoval`
  - `TestE2E_DriftBannerInUI` — UI test using a headless browser harness OR an HTML smoke check.

- `test/integration/trace_e2e_test.go`
  - `TestE2E_TraceContextEndToEnd` — initiate request with `traceparent` header; downstream HTTP mock inspects request and asserts `traceparent` present + same trace_id; final audit event has same trace_id.
  - `TestE2E_StdioDownstream_TraceparentInMeta` — stdio mock echoes `_meta.traceparent`; assert match.

## Common pitfalls

- **Deterministic JSON**: `encoding/json` does not sort map keys deterministically across all Go versions; use a custom canonical marshaller. The package `internal/catalog/snapshots/fingerprint.go` should own this; do not reuse for general API serialization (that is allowed to be non-canonical).
- **Snapshot bloat**: a snapshot with 200 tools, each with a kilobyte schema, is ~200KB. Multiply by sessions and by retention and SQLite grows fast. Compress `payload_json` with zstd at insert; keep recent N uncompressed for speed (configurable).
- **Snapshot retention**: do not auto-delete in V1. Operators run `DELETE FROM catalog_snapshots WHERE created_at < datetime('now','-30 days')` manually if needed. Document.
- **Drift detector load**: with N sessions × M servers, each interval makes N×M `tools/list` calls. For V1 single-instance scale this is fine; document the upper bound and recommend tuning `interval` for large deployments. Coalesce: same (tenant, server) pair fingerprinted at most once per interval (cache).
- **Trace context size**: don't shovel arbitrary context into spans. Limit attribute counts and sizes (OTel SDK already enforces, but keep your own discipline).
- **Stable mode regression risk**: a client that depends on `notifications/list_changed` may misbehave in stable mode. Document explicitly: stable mode is default; clients that need live updates must opt in.
- **Span end on error path**: every `defer span.End()` is non-negotiable. Use `tracer.Start` + immediate `defer`. Audit a few hand-written spans to ensure error paths don't drop them.
- **Snapshot creation cost on initialize**: an MCP session that just wants to ping shouldn't pay snapshot creation. Defer snapshot creation until the first call that needs the catalog (`tools/list`, `resources/list`, `prompts/list`, `tools/call`). Until then, `sessions.snapshot_id` is NULL and an "early call" snapshot creation runs synchronously on first need.
- **Live-mode mid-session snapshot churn**: a noisy downstream emitting list_changed every second creates 60 snapshots/min/session. Cap snapshot refresh rate per session (default: max 1/30s). Excess events log + audit only.

## Out of scope

- Persistent span storage (post-V1; OTLP exporter is the canonical sink).
- Distributed tracing across multiple Portico instances (post-V1; multi-instance coordination is itself post-V1).
- Anomaly detection on traces or audit (post-V1).
- Tail-based sampling (post-V1).
- Schema evolution helpers (auto-update tenant policies on tool addition): post-V1.
- Snapshot signing for tamper-evidence: post-V1.

## Done definition

1. All acceptance criteria pass.
2. Coverage ≥ 80% for `internal/catalog/snapshots`, `internal/telemetry`.
3. End-to-end demo (V1 success demo):
   ```bash
   ./bin/portico serve --config examples/prod-portico.yaml &
   # Use a real MCP client with a JWT for tenant 'acme'
   # Initiate a session, list tools, call a tool that triggers approval
   # Observe in Console:
   #   - Session inspector shows the snapshot
   #   - Audit log shows the chain of events
   #   - OTLP exporter received traces (verified via Jaeger or stdout)
   #   - After modifying the downstream's tool list, drift event appears
   ```
4. RFC §19.2 V1 success criteria all pass:
   - Multi-tenant operation with isolation: yes.
   - Local dev: yes.
   - Self-hosted prod with two tenants: yes.
   - 3+ MCP servers across stdio + HTTP: yes.
   - Tools namespaced + policy-filtered per tenant: yes.
   - Resources, prompts, templates proxied: yes.
   - MCP Apps with CSP: yes.
   - Stdio lifecycle managed: yes.
   - Credentials injected per user/session; OAuth exchange end-to-end: yes.
   - UI: yes (servers, tools, skills, sessions, approvals, audit).
   - 4 reference Skill Packs ship: yes.
   - Skill loaded, validated, exposed, enabled: yes.
   - Snapshots stable by default + opt-in live: yes.
   - Approvals via elicitation + fallback: yes.
   - OTel traces span gateway + runtime + skills: yes.
   - Schema drift detected: yes.

## Hand-off to V1 release

After Phase 6, V1 is feature-complete. Pre-release work (not a phase, but a checklist):

- README polish: quickstart aligned with V0.1 demo.
- Quickstart docs.
- Example `portico.yaml` for prod with comments.
- A `make release` target producing tagged binaries for linux-{amd64,arm64} and darwin-{amd64,arm64}.
- Versioned binary publication (GitHub Releases).
- Versioned Docker image at `ghcr.io/hurtener/portico`.
- Conformance test suite that boots Portico against a real upstream MCP server and runs the V1 success criteria.
- Public V0.1 demo recording (90s).

Post-V1 backlog already named: Postgres-default with migrations, K8s artifacts, Redis multi-instance, sidecar/per_request runtime, quotas, async approvals, container/microVM isolation, Git/OCI/HTTP skill sources, alternative auth backends, hosted SaaS evaluation.
