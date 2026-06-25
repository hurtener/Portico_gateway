# Catalog & sessions

Portico aggregates tools, resources, and prompts from every registered MCP server in your fleet. To make that aggregation safe and reproducible, it binds the view to a session: at the moment a session starts, the entire catalog is frozen into an immutable **catalog snapshot**. Every subsequent call within that session reads from the snapshot, not from live upstream state. The snapshot also anchors the audit trail — every persisted event references it so you can reconstruct exactly what a tool meant when it was called, months after the original session ended.

## Overview

When an AI client connects and begins an MCP session, Portico performs four operations atomically:

1. **Aggregates** the catalog across all enabled downstream MCP servers for the tenant.
2. **Namespaces** every tool, prompt, and resource so names from different servers never collide.
3. **Fingerprints** each server's tool list and the entire snapshot with deterministic SHA-256 hashes.
4. **Persists** the snapshot to the `catalog_snapshots` table, stamping its ID onto the session row.

After that point the session operates in **stable mode** by default: downstream `notifications/tools/list_changed`, `notifications/resources/list_changed`, and `notifications/prompts/list_changed` notifications are absorbed by the gateway rather than forwarded. The session's tool list does not change beneath the client. If a downstream server is redeployed mid-session and its schema changes, the session continues to see the snapshot it started with — and the drift detector surfaces the divergence as a `schema.drift` audit event.

::: info Lazy creation
The snapshot is created on the **first catalog-touching call** (`tools/list`, `resources/list`, `prompts/list`, or `tools/call`), not at connection time. A session that only exchanges ping/initialize never pays the snapshot creation cost, and `sessions.snapshot_id` remains NULL until the first list call.
:::

## Namespacing

Because a single tenant may register many MCP servers — each with its own flat tool namespace — Portico qualifies every name before exposing it northbound.

### Tools and prompts

The format is `{server_id}.{tool_name}`:

```
github.get_pull_request
jira.create_issue
postgres.run_query
```

Server IDs must match `^[a-z0-9][a-z0-9_-]{0,31}$`. The split is always on the **first dot**, so tool names that themselves contain dots are preserved intact (`github.pr.review` is server `github`, tool `pr.review`).

### Resources

Resource URIs follow a scheme-rewrite convention so they survive round-trips through MCP clients:

| Upstream scheme | Namespaced form |
|---|---|
| `ui://...` | `ui://{server_id}/{rest}` |
| `file://...` | `mcp+server://{server_id}/file/{path}` |
| `https://host/path` | `mcp+server://{server_id}/https/{authority}/{path}` |
| `http://host/path` | `mcp+server://{server_id}/http/{authority}/{path}` |
| Any other scheme | `mcp+server://{server_id}/raw/{base64url(original)}` |

The gateway restores the original URI before forwarding `resources/read` to the owning server. The snapshot records both the namespaced URI (`uri`) and the upstream URI (`upstream_uri`) for transparency.

## Catalog snapshot structure

A snapshot captures everything the session is entitled to see at creation time. The fields map directly to `internal/catalog/snapshots/snapshot.go`:

```json
{
  "id": "snap_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
  "tenant_id": "acme",
  "session_id": "sess_...",
  "created_at": "2025-11-01T09:00:00Z",
  "overall_hash": "3a7f...",
  "servers": [
    {
      "id": "github",
      "display_name": "GitHub MCP",
      "transport": "stdio",
      "runtime_mode": "per_session",
      "schema_hash": "c1b9...",
      "health": "healthy"
    }
  ],
  "tools": [
    {
      "namespaced_name": "github.get_pull_request",
      "server_id": "github",
      "description": "Fetch a pull request by number",
      "input_schema": { ... },
      "risk_class": "read",
      "requires_approval": false,
      "hash": "8f3a..."
    }
  ],
  "resources": [ ... ],
  "prompts": [ ... ],
  "skills": [
    {
      "id": "code-review",
      "version": "1.2.0",
      "enabled_for_session": true,
      "missing_tools": []
    }
  ],
  "policies": {
    "allow_list": [],
    "deny_list": ["postgres.drop_table"],
    "approval_timeout": 120000000000,
    "default_risk_class": "write"
  },
  "credentials": [
    {
      "server_id": "github",
      "strategy": "header",
      "secret_refs": ["github-token"]
    }
  ],
  "warnings": []
}
```

**Key fields:**

- `overall_hash` — SHA-256 over canonical JSON of all sub-fingerprints. Two independently-built snapshots over identical state hash equal, so you can detect whether the catalog changed between sessions.
- `servers[].schema_hash` — SHA-256 over the sorted tool list from that server at snapshot time. The drift detector compares this against the live fingerprint.
- `tools[].hash` — Per-tool hash covering namespaced name, description, input schema, annotations, resolved risk class, approval requirement, and owning skill.
- `tools[].risk_class` and `tools[].requires_approval` — Policy-resolved values, not upstream values. The snapshot records what the policy engine decided at creation time.
- `credentials[].secret_refs` — Secret names only, never values.
- `warnings` — Populated when a server fails to respond during snapshot creation. The snapshot still persists; the failing server is omitted from the tool and resource lists.

## Fingerprinting

All hashes use the same deterministic canonical JSON algorithm:

- Object keys sorted ascending (byte-wise).
- `null` values omitted from objects (`{"x": null}` is identical to `{}`).
- Lists sorted by stable key before hashing (tool name, resource URI, prompt name).
- Integer-valued floats rendered as integers (`1.0` → `1`).

This means the same logical catalog always produces the same hash regardless of map iteration order or server response ordering. The `OverallFingerprint` function in `internal/catalog/snapshots/fingerprint.go` deliberately excludes the snapshot's own ID and `created_at` field so two snapshots created at different times from the same catalog state produce the same `overall_hash` — useful for detecting when nothing actually changed across sessions.

## Stable vs. live mode

Portico separates two concerns: **what the session sees** (frozen at snapshot time) and **whether the client is notified about upstream changes** (controlled by the list-changed mode).

| Mode | Behavior |
|---|---|
| **Stable** (default) | Downstream `list_changed` notifications are absorbed. The session's tool list is locked to the snapshot. Drift is surfaced via background `schema.drift` events, not mid-session tool-list changes. |
| **Live** | Downstream `list_changed` notifications are forwarded to the client. The dispatcher creates a new snapshot for the session on each notification and updates `sessions.snapshot_id`. The old snapshot is preserved. |

To request live mode, include the following in your `initialize` request:

```json
{
  "experimental": {
    "portico/listChanged": "live"
  }
}
```

Operators can also set the tenant-level default in `portico.yaml`. Live mode is appropriate for interactive sessions where the client reacts to server redeployments; stable mode is appropriate for automated agent workflows where tool-list changes mid-run would be disruptive.

::: warning Live-mode churn
A noisy downstream server that emits `list_changed` events rapidly can generate many snapshots. Portico caps snapshot refresh rate per session (default: at most one new snapshot per 30 seconds). Excess events are logged and emitted as audit events but do not create additional snapshots.
:::

## Agent Profile projection

When an Agent Profile is bound to the request — via a Virtual Key or an explicit `X-Agent-Profile-ID` header — the session's catalog view is further filtered at dispatch time.

The profile's `allowed_servers` and `allowed_tools` lists act as an additional gate on top of the tenant-level snapshot:

- **`tools/list`** returns only tools the profile permits (`AllowsTool`).
- **`tools/call`** rejects with a typed `agent_profile_violation` error if the requested tool is outside the profile's surface.

The snapshot itself is built against the full tenant catalog; the profile projection is applied per-request in the dispatcher, not baked into the snapshot. This means a single snapshot can serve clients with different profiles, and rotating or updating an Agent Profile does not require rebuilding the snapshot.

See [Agent Profiles](/concepts/agent-profiles) for the full surface definition.

## Drift detection

A background `Detector` goroutine (in `internal/catalog/snapshots/drift.go`) periodically re-fingerprints the tool lists of every active session's servers and compares the result against the `schema_hash` recorded in the snapshot. When a hash diverges, it emits a `schema.drift` audit event:

```json
{
  "type": "schema.drift",
  "tenant_id": "acme",
  "session_id": "sess_...",
  "payload": {
    "snapshot_id": "snap_...",
    "server_id": "github",
    "old_hash": "c1b9...",
    "new_hash": "f7e2...",
    "diff": {
      "tools": {
        "added":    ["github.search_code"],
        "removed":  [],
        "modified": [
          {
            "name": "github.create_review_comment",
            "fields_changed": ["input_schema"],
            "old_hash": "...",
            "new_hash": "..."
          }
        ]
      }
    }
  }
}
```

The detector deduplicates: once a drift event has been emitted for a `(session, server, new_hash)` tuple, it will not re-fire until the live hash changes again. Sessions older than 24 hours are skipped (assumed abandoned). The same (tenant, server) pair is fingerprinted at most once per sweep interval regardless of how many active sessions reference it.

::: info Session is not updated
The session does not automatically pick up the new schema. It continues to operate from its snapshot. To pick up the change, the agent must initiate a new session, or opt into live mode before the change arrives.
:::

The drift detector's sweep interval defaults to 60 seconds and can be tuned for large deployments where N sessions × M servers would otherwise create excessive `tools/list` fan-out.

See [Drift detection](/concepts/drift-detection) for the full observability story.

## REST API

### Catalog resolve

```http
POST /v1/catalog/resolve
Authorization: Bearer <tenant-jwt>
Content-Type: application/json

{"session_id": "sess_..."}
```

Creates and returns a snapshot for the supplied session. If the binder already holds an in-memory snapshot for the session, a new one is created and persisted (this is how the smoke harness anchors the audit trail before firing tool calls).

### Snapshot endpoints

```http
GET /v1/catalog/snapshots
    ?since=<RFC3339>
    &limit=<int>
    &cursor=<opaque>
```

Returns a paginated list of snapshots for the authenticated tenant, newest first.

```http
GET /v1/catalog/snapshots/{id}
```

Returns a single snapshot by ID. Returns `404 not_found` if the snapshot does not exist or belongs to a different tenant.

```http
GET /v1/catalog/snapshots/{a}/diff/{b}
```

Returns the structured diff between two snapshots. The response shape mirrors `snapshots.Diff`:

```json
{
  "tools": {
    "added":    ["github.search_code"],
    "removed":  [],
    "modified": [...]
  },
  "resources": {"added": [], "removed": []},
  "prompts":   {"added": [], "removed": []},
  "skills":    {"added": [], "removed": []}
}
```

### Session snapshot

```http
GET /v1/sessions/{session_id}/snapshot
```

Returns the in-memory snapshot currently bound to an active session (via the `SnapshotBinder`). Returns `404` if no snapshot has been materialised yet for the session.

## Session bundles and the inspector

A **session bundle** is a portable, verifiable archive of everything Portico knows about one session. The `internal/sessionbundle` package assembles it from five stores:

| File in bundle | Contents |
|---|---|
| `manifest.json` | Schema version, bundle ID, counts, SHA-256 checksum |
| `session.json` | Session row |
| `snapshot.json` | Bound catalog snapshot |
| `spans.jsonl` | Telemetry spans, sorted by start time |
| `audit.jsonl` | Non-drift, non-policy audit events |
| `drift.jsonl` | `schema.drift` events |
| `policy.jsonl` | `policy.*` events |
| `approvals.jsonl` | Approval records |

Bundles are written as deterministic tar.gz archives (gzip header timestamp zeroed, USTAR format, canonical JSON per stream). Two exports of the same session produce byte-identical stream content — the checksum in `manifest.json` covers all stream files and is verified on import before any records are deserialised.

### Exporting

```http
GET /v1/sessions/{session_id}/bundle
```

Returns the bundle as a `application/gzip` download.

To strip sensitive payload maps (for regulated-industry sharing):

```http
GET /v1/sessions/{session_id}/bundle?omit_payloads=true
```

### Importing

```http
POST /v1/sessions/import
Content-Type: application/gzip

<bundle bytes>
```

The importer verifies the checksum, rewrites tenant and session IDs to a synthetic form (`imported:{bundle_id}`), and registers the bundle under the importing tenant. The Console inspector renders imported bundles identically to live sessions. The runtime's write paths reject any session ID with the `imported:` prefix, so an imported bundle can never be written to.

### Offline CLI

The `inspect-session` subcommand reads SQLite directly without booting a server:

```bash
portico inspect-session <session_id> \
  --output json \
  --dsn "file:./data/portico.db?mode=ro"
```

Output:

```json
{
  "session":       { "id": "...", "tenant_id": "...", "snapshot_id": "..." },
  "snapshot":      { ... },
  "audit_events":  [ ... ],
  "approvals":     [ ... ],
  "drift_events":  [ ... ],
  "trace_summary": { "total_events": 42, "errors": 0 }
}
```

A `--since <RFC3339>` flag filters audit and drift events. The trace summary is derived from the persisted audit events; full span data is available only if an OTLP exporter was configured.

## Multi-tenancy

Every snapshot row carries `tenant_id NOT NULL`. The snapshot store filters all reads with `WHERE tenant_id = ?` drawn from the request's JWT context — a snapshot from tenant `acme` is invisible to tenant `beta` even if both IDs are known. The `sessionbundle` importer enforces the same rule: it cross-checks the decoded session row's `tenant_id` against the importing tenant before registering anything.

## Storage and retention

Snapshots are persisted to the `catalog_snapshots` table (migration `0006_snapshots_extended.sql`). The `schema_fingerprints` table stores the latest per-server hash alongside a `seen_at` timestamp and a tools count, allowing the drift detector to skip the SQLite snapshot read on the happy path.

V1 does not auto-delete snapshots. For long-running deployments, prune manually:

```sql
DELETE FROM catalog_snapshots
WHERE created_at < datetime('now', '-30 days');
```

Keep at least the snapshots referenced by open sessions and recent audit events, since `catalog_snapshot_id` is the audit trail's anchor.

## Related

- [Agent Profiles](/concepts/agent-profiles) — how profiles filter the per-session tool projection
- [Drift detection](/concepts/drift-detection) — full details on schema drift monitoring and alerting
- [MCP Gateway](/concepts/mcp-gateway) — the dispatcher that reads from snapshots for stable tool lists
- [Playground](/concepts/playground) — interactive session inspector and snapshot diff viewer in the Console
- [Audit](/concepts/audit) — how snapshot IDs are carried on every audit event
- [Observability](/concepts/observability) — OTel spans emitted during snapshot creation and drift sweeps
