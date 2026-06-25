# Drift detection

Downstream MCP servers change. A server operator redeploys with a new tool, an upstream dependency adds a parameter to an existing tool's input schema, or a tool description is updated to clarify its behavior. In every case, the catalog that an active session was built against no longer matches the live state of the fleet.

Portico surfaces this divergence through drift detection: a background goroutine that continuously re-fingerprints the tool lists of active sessions and emits a structured `schema.drift` audit event whenever a server's live schema diverges from the hash recorded in the session's catalog snapshot.

## Why drift visibility matters

A catalog snapshot freezes the session's view of the tool fleet (see [Catalog & sessions](/concepts/catalog-and-sessions)). Stable mode — the default — means the session continues operating from that frozen view even if downstream state changes. Drift detection is what turns this silent stability into visible safety: you learn that the world has changed without the change disrupting a running workflow.

The audit trail for a session answers the question "what did tool X mean when it was called?" by carrying a `catalog_snapshot_id` on every event. Drift events extend that story: "tool X's schema changed during the session, here is the exact diff, and the session was still operating on the original snapshot when the divergence was detected."

Beyond observability, drift visibility is the prerequisite for enforcement. Phase 17 of Portico's roadmap introduces drift gates — policy rules that can **block** a drifted tool's calls until an operator confirms the change. The same `schema.drift` event that today surfaces an alert will, in that phase, feed a decision engine that can quarantine a server whose tool schema changed without authorization.

## How it works

### The Detector

The drift detector lives in `internal/catalog/snapshots/drift.go` as a `Detector` struct. It is wired at gateway startup and runs as a long-lived background goroutine that participates in the process's lifecycle through context cancellation and an explicit `Stop` method.

```go
// Pseudocode of the public API (internal/catalog/snapshots/drift.go)

type Detector struct { ... }

func NewDetector(service *Service, probe LiveProbe, log *slog.Logger, interval time.Duration) *Detector

// Start kicks off the detection loop. Returns immediately.
func (d *Detector) Start(ctx context.Context)

// Stop signals the loop to terminate and blocks until it exits. Idempotent.
func (d *Detector) Stop()

// Once runs a single drift sweep — useful in tests and operational tooling.
func (d *Detector) Once(ctx context.Context) error
```

On `Start`, a goroutine fires immediately — the first sweep runs before the ticker interval elapses, so a freshly booted gateway begins detecting drift right away. The goroutine exits when either the context passed to `Start` is cancelled or `Stop` is called. Both paths result in the goroutine being fully joined before `Stop` returns, so there are no goroutine leaks on shutdown.

The sweep interval defaults to 60 seconds. For large deployments where N active sessions × M servers would produce excessive `tools/list` fan-out, the interval is a tunable parameter passed to `NewDetector`.

### The sweep cycle

Each sweep executes these steps:

1. **Load active sessions.** The store returns sessions whose `ended_at` is NULL and whose `started_at` is within the last 24 hours. Sessions older than 24 hours are assumed abandoned and excluded to cap the query and downstream load.

2. **Group by tenant.** All sessions are grouped by `tenant_id` so each `(tenant, server)` pair is re-probed at most once per sweep, regardless of how many open sessions reference the same server.

3. **Re-fingerprint.** For each tenant, the detector calls `LiveProbe.ListTools`, which returns the current tool list keyed by server ID. Each tool list is hashed with `ServerToolsFingerprint`, which sorts the list by name before hashing to ensure that response ordering differences do not produce spurious drift signals.

4. **Compare.** For each active session, the detector loads the session's snapshot from the store. For each server entry in the snapshot, it compares `ServerInfo.SchemaHash` (the fingerprint recorded at snapshot creation time) against the freshly computed hash.

5. **Emit on divergence.** If the hashes differ, the detector builds a structured diff and emits a `schema.drift` audit event (described below).

6. **Update the fingerprint store.** Whether or not drift was detected, the latest fingerprint is upserted into the `schema_fingerprints` table keyed by `(tenant_id, server_id)`. This table lets the detector skip the full snapshot load on the next sweep for servers that have not changed.

### Deduplication

A single drift event is emitted once per `(session, server, new_hash)` triple. The detector maintains an in-memory `lastEmitted` map (protected by a mutex) keyed by `session_id|server_id`. Once a drift is surfaced, it will not re-fire on subsequent sweeps until the live hash changes again — for example, if the server is rolled back and then redeployed to a third state, the second change triggers a new event.

This means a deployment that locks in at a new schema version will produce exactly one `schema.drift` event for each affected session. It does not flood the audit log every 60 seconds for the lifetime of the session.

## Fingerprinting algorithm

Drift detection is grounded in deterministic SHA-256 fingerprints produced by `internal/catalog/snapshots/fingerprint.go`.

### Tool-list fingerprint

`ServerToolsFingerprint(tools []protocol.Tool) string` produces the fingerprint the drift detector compares against. It:

1. Copies the tool list and sorts it by tool name.
2. Encodes the sorted list as canonical JSON (see below).
3. Returns the hex-encoded SHA-256 of the result.

Two tool lists with the same logical content always produce the same hash regardless of the order in which the upstream server returned them.

### Per-tool fingerprint

`ToolFingerprint(t ToolInfo) string` hashes a single tool's full resolved view:

| Field | Hashed? |
|---|---|
| `namespaced_name` | Yes |
| `server_id` | Yes |
| `description` | Yes |
| `input_schema` | Yes |
| `annotations` | Yes |
| `risk_class` | Yes |
| `requires_approval` | Yes |
| `skill_id` | Yes |

This means a change in the upstream tool's description, a new required parameter in `input_schema`, or a change in its MCP annotation block all produce a different hash — and therefore a different diff entry.

### Canonical JSON

All fingerprints are built over canonical JSON produced by `internal/catalog/snapshots/canonical.go`. The encoding rules are:

- Object keys sorted ascending (byte-wise).
- `null` values omitted from objects (`{"x": null}` is identical to `{}`).
- Lists keep the caller-supplied order (callers sort by stable key before passing).
- Integer-valued floats rendered without a decimal point (`1.0` becomes `1`).
- Structs round-trip through `encoding/json` then through the canonical encoder, ensuring Go struct field ordering never influences the output.

Determinism is non-negotiable here: the same logical tool must always produce the same fingerprint across Go versions, restarts, and map iteration orders.

### Overall snapshot fingerprint

`OverallFingerprint(s *Snapshot) string` hashes the entire snapshot by combining all per-server and per-tool fingerprints. The snapshot's own ID and `created_at` are deliberately excluded so two snapshots built from an identical catalog state produce the same `overall_hash`. This lets operators detect with certainty whether anything changed between two sessions: if both sessions' snapshots share an `overall_hash`, the catalog was identical when both were created.

## The `schema.drift` event

When the drift detector finds divergence, it emits an audit event with type `schema.drift`. The event is routed through the same audit emitter pipeline as all other events, including any configured redactors and sinks.

```json
{
  "type": "schema.drift",
  "tenant_id": "acme",
  "session_id": "sess_3VkQ...",
  "payload": {
    "snapshot_id": "snap_AAAA...",
    "server_id":   "github",
    "old_hash":    "c1b9f4e2...",
    "new_hash":    "f7e22d8a...",
    "diff": {
      "tools": {
        "added": [
          "github.search_code"
        ],
        "removed": [],
        "modified": [
          {
            "name":           "github.create_review_comment",
            "fields_changed": ["input_schema"],
            "old_hash":       "8f3a...",
            "new_hash":       "2c10..."
          }
        ]
      }
    }
  }
}
```

**Event fields:**

| Field | Description |
|---|---|
| `payload.snapshot_id` | The immutable snapshot the session was built from. |
| `payload.server_id` | The downstream server whose tool list diverged. |
| `payload.old_hash` | The `ServerInfo.SchemaHash` recorded in the snapshot. |
| `payload.new_hash` | The freshly computed fingerprint. |
| `payload.diff.tools.added` | Namespaced tool names present live but absent in the snapshot. |
| `payload.diff.tools.removed` | Namespaced tool names present in the snapshot but absent live. |
| `payload.diff.tools.modified` | Tools present in both but with a different hash; `fields_changed` lists which fields changed. |

The `fields_changed` field on a modified tool can contain any subset of: `input_schema`, `description`, `annotations`, `risk_class`, `requires_approval`, `skill_id`.

## Effect on the session

Drift events are observability signals, not state changes. The session that triggered the event continues operating from its original snapshot:

- `tools/list` continues to return the snapshot-pinned tool list.
- `tools/call` continues to dispatch to the snapshot-recorded server configuration.
- No error is returned to the calling agent.

To pick up the new schema, the agent must start a new session (which will snapshot the live catalog) or opt into live mode before the drift occurs. See [Catalog & sessions](/concepts/catalog-and-sessions) for the stable-vs-live mode trade-off.

::: info Protecting long-running workflows
This behavior is intentional. An agentic workflow that began with a known tool surface should not be silently interrupted mid-run by a server redeployment. The drift event notifies the platform operator; the workflow completes using the catalog it was designed against.
:::

## Snapshot diff API

Beyond the background detector, the snapshot service exposes an on-demand diff endpoint that operators and the Playground use to compare any two snapshots:

```http
GET /v1/catalog/snapshots/{snapshot_a_id}/diff/{snapshot_b_id}
Authorization: Bearer <tenant-jwt>
```

Response:

```json
{
  "tools": {
    "added":    ["github.search_code"],
    "removed":  [],
    "modified": [
      {
        "name":           "github.create_review_comment",
        "fields_changed": ["input_schema"],
        "old_hash":       "8f3a...",
        "new_hash":       "2c10..."
      }
    ]
  },
  "resources": { "added": [], "removed": [] },
  "prompts":   { "added": [], "removed": [] },
  "skills":    { "added": [], "removed": [] }
}
```

The diff is computed by `DiffSnapshots` in `internal/catalog/snapshots/fingerprint.go`, the same function the detector uses internally. Both snapshots must belong to the requesting tenant; a cross-tenant diff returns `403`.

## Storage

The `schema_fingerprints` table (created in migration `0006_snapshots_extended.sql`) stores the latest known fingerprint per `(tenant_id, server_id)` alongside a `seen_at` timestamp and a tool count:

```sql
CREATE TABLE IF NOT EXISTS schema_fingerprints (
    tenant_id    TEXT NOT NULL,
    server_id    TEXT NOT NULL,
    seen_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    hash         TEXT NOT NULL,
    tools_count  INTEGER NOT NULL,
    PRIMARY KEY (tenant_id, server_id, hash)
);

CREATE INDEX IF NOT EXISTS idx_fingerprints_server
    ON schema_fingerprints(tenant_id, server_id, seen_at DESC);
```

The drift detector calls `Store.UpsertFingerprint` after each sweep, regardless of whether drift was found. This gives operators a queryable history of fingerprint transitions per server — useful for reconstructing when a schema change was first seen, even if no session was active at that moment.

## Operational load

The sweep is designed to be cheap when the fleet is stable:

- **Coalescing:** The same `(tenant, server)` pair calls `ListTools` at most once per sweep, even if dozens of active sessions reference the same server.
- **Abandonment cutoff:** Sessions older than 24 hours are not checked, bounding the worst-case fan-out to the number of non-expired sessions.
- **Fingerprint comparison before snapshot load:** The detector compares the freshly computed hash against the value in the session's snapshot server entry. If hashes are equal, no further work is done for that server.

For large deployments, tune the sweep interval upward. A 5-minute interval may be appropriate for a fleet with hundreds of sessions and dozens of servers; the trade-off is a longer detection lag.

## The path to drift enforcement (Phase 17)

The current implementation is observability-only: drift is detected and reported, but not blocked. Phase 17 introduces drift gates that extend this behavior with policy-driven enforcement.

A drift gate matches on properties of the drift event — `drift.kind` (tool added, tool removed, schema changed), `source.id`, tenant — and applies one of three actions:

| Action | Behavior |
|---|---|
| `allow` | Snapshot updates proceed as today. |
| `audit_only` | Snapshot updates; a `security.drift_gated` event is recorded. |
| `block` | Snapshot does not update. Tool calls against the affected server return a `gateway.drift_blocked` typed error until an operator approves the change via the security API. |

The `block` action is the primary defense against tool-poisoning attacks: a tool that changes its schema (description, input schema, annotations) without operator awareness cannot silently become active in new sessions. The operator reviews the diff, confirms intent, and explicitly approves the update.

This matters because drift detection covers not only benign redeployments but also adversarial schema changes. A tool that was registered with a narrow, safe input schema could, if its upstream implementation is compromised, change its description to include instructions that cause an LLM calling it to behave differently — without the schema fingerprint changing at all. Drift enforcement with description scanning (also Phase 17) closes this gap.

::: warning Not yet enforced
Drift enforcement is a planned capability documented in `docs/plans/phase-17-tool-poisoning-defense.md`. The current release emits `schema.drift` events only. No tool calls are blocked on drift.
:::

## Offline inspection

The `inspect-session` CLI command includes drift events in its output, making it possible to reconstruct the drift history of a session without a live server:

```bash
portico inspect-session <session_id> \
  --output json \
  --dsn "file:./data/portico.db?mode=ro"
```

The `drift_events` array in the output lists every `schema.drift` event recorded for the session, including the full diff payload. See [CLI reference](/reference/cli) for full flag documentation.

## Related

- [Catalog & sessions](/concepts/catalog-and-sessions) — how snapshots are created and what stable vs. live mode means for active sessions
- [Audit](/concepts/audit) — how `schema.drift` events flow through the audit pipeline and can be queried
- [Observability](/concepts/observability) — OTel spans emitted during snapshot creation and drift sweeps
- [Playground](/concepts/playground) — interactive session inspector with a drift banner and snapshot diff viewer
- [Security model](/concepts/security-model) — how drift detection fits into Portico's broader defense-in-depth posture
- [MCP registry](/concepts/mcp-registry) — server registration, health tracking, and how servers are resolved into a snapshot
