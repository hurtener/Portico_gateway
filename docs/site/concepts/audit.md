# Audit

Every governed decision and write that flows through Portico produces a structured audit event. Tool calls, policy decisions, approval transitions, credential injections, vault mutations, operator CRUD actions, Code Mode executions, A2A dispatches, Virtual Key scope violations, and budget debits all land in the same append-only store, keyed by tenant, before any downstream request completes.

This page describes the audit pipeline: how events are structured, how the Redactor prevents credentials from reaching the on-disk log, how the SQLite-backed store handles burst traffic without blocking callers, and how the query and full-text search APIs expose the data to operators.

---

## Why every event goes through one path

The goal is answerable accountability: "who, in which tenant, called what, with what outcome, and when?" Portico enforces this by making the audit path non-optional. The `audit.Emitter` interface is the seam; the runtime wires a `FanoutEmitter` that writes to both the process logger and the persistent store. There is no code path that emits events around the fanout, and the Redactor runs inside the `Store.Emit` method — not at the call site — so a caller that forgets to pre-sanitize its payload is still protected.

The three non-negotiable properties:

1. `TenantID` is required on every event. The store drops any event that arrives with an empty `TenantID`.
2. The Redactor runs before persistence. Payloads that contain credential-shaped strings are scrubbed unconditionally.
3. Audit must not block the request path. The store is buffered; overflow drops the oldest queued event and records the loss as an `audit.dropped` event.

---

## Event structure

Every event is an instance of the same Go type, defined in `internal/audit/emitter.go`:

```go
type Event struct {
    Type       string         // dot-separated event type, e.g. "tool_call.complete"
    TenantID   string         // required; event is discarded without it
    SessionID  string         // empty for system-originated events
    UserID     string         // empty for system-originated events
    OccurredAt time.Time      // UTC; set by the emitter if zero at emit time
    TraceID    string         // W3C traceparent trace component, when present
    SpanID     string         // W3C traceparent span component, when present
    Payload    map[string]any // arbitrary JSON-serialisable context
}
```

`Payload` is the structured context for the event. For `tool_call.start` it carries `tool`, `server_id`, and an `args_summary`; for `approval.decided` it carries `approval_id`, `decision`, and `decided_by`. Values in `Payload` pass through the Redactor before reaching the database — the keys are preserved so operators can identify what was redacted.

IDs are ULID strings generated from the event's `OccurredAt` timestamp. This gives audit records a naturally sortable primary key, which the cursor-based pagination in the query API relies on (`id < ?` for "older than" paging).

---

## Event taxonomy

Events are defined as typed constants in `internal/audit/emitter.go`. The table below groups them by subsystem.

### Tool call lifecycle

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `tool_call.start` | MCP dispatcher | `tool`, `server_id`, `args_summary`, `skill_id` |
| `tool_call.complete` | MCP dispatcher | `duration_ms`, `result_size_bytes` |
| `tool_call.failed` | MCP dispatcher | `duration_ms`, `error_code`, `error_message` |

### Policy and access control

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `policy.allowed` | Policy engine | `tool`, `risk_class`, `requires_approval` |
| `policy.denied` | Policy engine | `tool`, `reason` |
| `policy.rule_changed` | Operator CRUD | `rule_id`, `priority`, `risk_class` |
| `policy.rule_deleted` | Operator CRUD | `rule_id` |
| `policy.dry_run` | Policy dry-run API | `tool`, `decision`, `reason` |
| `agent_profile.violation` | Profile gate (Phase 14) | `profile_id`, `tool`, `reason` |
| `vk.scope_violation` | Virtual Key gate | `vk_id`, `server_id`, `tool` |

### Approvals

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `approval.pending` | Approval flow | `approval_id`, `tool`, `risk_class` |
| `approval.decided` | Approval flow | `approval_id`, `decision`, `decided_by` |
| `approval.expired` | Approval flow | `approval_id` |
| `approval.replayed` | Approval flow | `approval_id`, `tool` |

### Credentials and vault

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `credential.injected` | Credential injector | `strategy`, `server_id` (no values) |
| `credential.exchange.success` | OAuth exchanger | `audience`, `ttl_s` |
| `credential.exchange.failed` | OAuth exchanger | `audience`, `error_code` |
| `vault.get` | Vault | `name` (no value) |
| `vault.put` | Vault (admin) | `name` |
| `vault.delete` | Vault (admin) | `name` |
| `vault.rotate_root` | Vault key rotation | `tenant_count` |
| `vault.rotate_root.aborted` | Vault key rotation | `reason`, `failed_at` |
| `secret.created` | Secrets CRUD | `name` |
| `secret.updated` | Secrets CRUD | `name` |
| `secret.deleted` | Secrets CRUD | `name` |
| `secret.rotated` | Secrets CRUD | `name` |
| `secret.reveal.issued` | Secrets CRUD | `name`, `expires_at` |
| `secret.reveal.consumed` | Secrets CRUD | `name` |

::: warning Passthrough credentials
When a server is configured with `auth.passthrough: true`, every credential that flows through receives an explicit `credential.passthrough` audit event alongside `credential.injected`. This is intentional: passthrough is an exception to Portico's default token-exchange model and must leave a clear trace. See [Credentials Vault](/concepts/credentials-vault) for the full policy.
:::

### Operator CRUD

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `server.created` | Console / API | `server_id` |
| `server.updated` | Console / API | `server_id` |
| `server.deleted` | Console / API | `server_id` |
| `server.restarted` | Console / API | `server_id`, `reason` |
| `tenant.created` | Admin API | `tenant_id` |
| `tenant.updated` | Admin API | `tenant_id` |
| `tenant.archived` | Admin API | `tenant_id` |
| `tenant.purged` | Admin API | `tenant_id` |

### Code Mode

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `code_mode.execution_started` | Code Mode runtime | `execution_id`, `tool_call_count` |
| `code_mode.execution_completed` | Code Mode runtime | `execution_id`, `tool_call_count`, `duration_ms` |
| `code_mode.execution_failed` | Code Mode runtime | `execution_id`, `error_code` |
| `code_mode.execution_suspended` | Code Mode runtime | `execution_id`, `approval_id` |
| `code_mode.unsafe_denied` | Static gate | `execution_id`, `rule` |

Tool calls issued from inside a Code Mode sandbox emit the standard `tool_call.*` events through the identical governed envelope — there is no separate code-mode tool-call type.

### A2A dispatch

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `a2a.dispatch` | A2A outbound dispatcher | `peer_id`, `method`, `task_id` (when applicable) |

### Infrastructure

| Type | Emitted by | Key payload fields |
|------|------------|-------------------|
| `audit.dropped` | Store overflow handler | `dropped_count` |

---

## The Redactor

Every payload passes through `audit.Redactor` before it is serialised to the database. The Redactor applies two layers of scrubbing, in order:

**1. Structural redaction — sensitive key names**

If a map key matches one of the canonical sensitive names (case-insensitive), the entire value is replaced with `[REDACTED:key=<keyname>]` regardless of the value's shape. This catches credentials passed as structured maps even when the regex set would miss them. The canonical sensitive key set (from `internal/audit/redact.go`) is:

```
token, secret, password, passwd, api_key, apikey, authorization,
auth, access_token, refresh_token, session_token, client_secret,
secret_key, private_key, credential, credentials
```

**2. Pattern redaction — known credential shapes**

String values that survive structural redaction are scanned against seven compiled regex patterns. Each match is replaced by `[REDACTED:<label>]` where the label names the pattern that fired:

| Label | Pattern matches |
|-------|----------------|
| `bearer` | `Bearer <20+ char token>` |
| `basic_auth` | `Basic <16+ char base64>` |
| `github_pat` | `gh[pousr]_<36+ chars>` |
| `aws_access_key` | `AKIA<16 uppercase alphanumerics>` |
| `slack_token` | `xox[abprs]-<10+ chars>` |
| `jwt` | Three-segment `eyJ…` base64url JWT |
| `private_key` | PEM `-----BEGIN … PRIVATE KEY-----` blocks |

The patterns are intentionally conservative: they require enough length or a specific prefix to avoid false positives on ordinary English text. The 20-character floor on Bearer tokens is what prevents phrases like "send a Bearer hello" from being redacted.

Keys are always preserved in the output. `[REDACTED:key=secret]` tells an operator the field was present and was redacted by the structural rule; `[REDACTED:bearer]` tells them a bearer token appeared in a string value. Neither leaks the secret.

**Custom redactors**

`NewRedactor(patterns ...UserPattern)` constructs a Redactor from caller-supplied patterns plus the canonical structural key set. Operators who need to redact an additional credential family (for example, a vendor-specific token format) can pass extra rules without replacing the defaults. The replacement format is always `[REDACTED:<Label>]` — callers do not control it, so on-disk audit records stay parseable.

---

## The store

`audit.Store` in `internal/audit/store.go` is a SQLite-backed sink. Events arrive from `Emit` calls and are placed on a bounded in-memory channel. A single worker goroutine drains the channel and writes to the database in batches.

### Buffering and batching

| Tunable | Default | Override |
|---------|---------|---------|
| Channel depth | 4096 events | `WithBufferSize(n)` |
| Batch size | 100 events | `WithBatchSize(n)` |
| Flush interval | 200 ms | `WithBatchInterval(d)` |

The worker flushes when it accumulates `batchSize` events or when the ticker fires, whichever comes first. Each flush opens a single database transaction, prepares a single statement, and executes it for every event in the batch — one round-trip per flush rather than one per event.

On shutdown, `Store.Stop()` signals the worker and waits for it to finish draining the channel before returning. Events in flight at the time of a graceful shutdown are flushed before the process exits.

### Overflow: drop-oldest

When the channel is full, `Emit` does not block the caller. Instead it discards the oldest queued event (head of the channel) and enqueues the new one. The count of dropped events accumulates in an atomic counter; on each flush the worker reads and resets the counter and, if non-zero, persists an `audit.dropped` event under tenant `_system`. This gives operators a way to detect backpressure in the audit log itself.

::: tip Monitoring for drops
Query for `type=audit.dropped` with a `since` parameter matching your monitoring window. A non-zero `dropped_count` in the payload indicates that audit events were lost due to backpressure. Sustained drops typically indicate that the batch interval is too long for the ingest rate, or that the SQLite write path is saturated.
:::

### Database schema

The `audit_events` table is defined in migration `0001_init.sql`:

```sql
CREATE TABLE IF NOT EXISTS audit_events (
    id           TEXT PRIMARY KEY,      -- ULID, sortable by time
    tenant_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    session_id   TEXT,
    user_id      TEXT,
    occurred_at  TEXT NOT NULL,         -- RFC3339Nano, UTC
    trace_id     TEXT,
    span_id      TEXT,
    payload_json TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_time
    ON audit_events(tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_type
    ON audit_events(tenant_id, type, occurred_at DESC);
```

Migration `0012_audit_search.sql` adds a `summary` column (a one-line redacted preview of the payload, capped at 256 characters and built from scalar payload values at insert time) and an FTS5 virtual table:

```sql
ALTER TABLE audit_events ADD COLUMN summary TEXT NOT NULL DEFAULT '';

CREATE VIRTUAL TABLE IF NOT EXISTS audit_events_fts USING fts5(
    type,
    summary,
    payload_json,
    content='audit_events',
    content_rowid='rowid',
    tokenize='unicode61'
);
```

Three triggers keep the FTS index in sync with the base table on insert, update, and delete. The `unicode61` tokenizer makes non-ASCII error messages and tool names searchable without additional configuration.

---

## Query API

### List events

```http
GET /v1/audit/events
```

Requires a valid tenant JWT. Returns events for the calling tenant only.

**Query parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `type` | string | Comma-separated list of event types to include |
| `since` | RFC3339 | Return events at or after this timestamp |
| `until` | RFC3339 | Return events before this timestamp |
| `limit` | integer | Page size (default 100, max 1000) |
| `cursor` | string | Opaque cursor from a previous response |

**Response**

```json
{
  "events": [
    {
      "type": "tool_call.complete",
      "tenant_id": "acme",
      "session_id": "01HQ...",
      "user_id": "user@acme.example",
      "occurred_at": "2026-06-24T14:32:01.123456789Z",
      "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
      "span_id": "00f067aa0ba902b7",
      "payload": {
        "tool": "github.create_review_comment",
        "duration_ms": 142,
        "result_size_bytes": 312
      }
    }
  ],
  "next_cursor": "01HQ..."
}
```

Events are returned in reverse chronological order (newest first). Pass `next_cursor` as `cursor` to retrieve the next page. An empty `next_cursor` indicates there are no further events matching the query.

**Admin cross-tenant queries**

A JWT with the `admin` scope may pass `tenant_id=*` to aggregate across all tenants. No other path exists for cross-tenant reads. This is enforced at the storage layer, not only the handler. See [Multi-tenancy](/concepts/multi-tenancy) for the full isolation model.

### Full-text search

```http
GET /api/audit/search
```

Queries the FTS5 index built from the `type`, `summary`, and `payload_json` columns.

**Query parameters**

| Parameter | Type | Description |
|-----------|------|-------------|
| `q` | string | FTS expression. Supports terms, `term*` prefix, `AND`/`OR`/`NOT`, and quoted phrases. Empty `q` is a plain list with the remaining filters applied. |
| `session_id` | string | Filter to a specific session |
| `type` | string | Exact event type match |
| `from` | RFC3339 | Lower bound on `occurred_at` |
| `to` | RFC3339 | Upper bound on `occurred_at` |
| `limit` | integer | Page size (default 100, max 1000) |
| `cursor` | string | Opaque cursor from a previous response |

**Response**

Same shape as the list endpoint: `{"events": [...], "next_cursor": "..."}`.

**FTS expression examples**

```
# All events whose summary mentions a specific server
github.create*

# Approval events that were denied
approval denied

# Redacted credential events in a specific session
REDACTED:bearer
```

The query handler normalises plain terms into quoted phrases before passing to FTS5, so a query like `create_review_comment` does not silently drop the underscore. A query that already contains a `"` or starts with `(` is treated as a raw FTS expression and passed through unchanged.

When `q` is empty the handler skips the FTS join and runs a plain SELECT against the base table, avoiding any FTS index cost on pure-filter queries.

---

## Entity activity projection

The `entity_activity` table (migration `0009_console_crud.sql`) is a denormalised projection over `audit_events`, written by the audit fanout. It records one row per CRUD event against a named entity — servers, tenants, secrets, policies — carrying the actor's `user_id`, a one-line `summary`, and a `diff_json` blob of the before/after values.

```http
GET /api/{kind}/{id}/activity
```

Where `kind` is `servers`, `tenants`, `secrets`, or similar. Returns up to 50 rows (configurable via `?limit=`) in reverse chronological order. The underlying `audit_events` table remains canonical; the projection is what the Console "Activity" tab renders for each resource detail page.

**Structure per row**

```json
{
  "event_id": "01HQ...",
  "occurred_at": "2026-06-24T14:30:00Z",
  "actor_user_id": "operator@acme.example",
  "summary": "server updated: enabled=true",
  "diff": {
    "raw": "{\"before\":{...},\"after\":{...}}"
  }
}
```

The projection is not a full audit substitute — it is a convenience surface for the operator console. For compliance purposes, query `audit_events` directly via `GET /v1/audit/events`.

---

## Budget and approval events

Approval and budget events carry enough context to reconstruct a decision chain without re-reading the original policy state.

An `approval.pending` event records the `approval_id`, `tool`, and `risk_class`. If the operator resolves the approval out-of-band, the subsequent `approval.decided` event carries `decided_by` (the user ID of the operator or the string `"agent"` if decided via elicitation). An `approval.replayed` event marks a tool call that was granted via an existing approval within the configured replay window.

Budget pre-checks and post-call reconciliations do not produce explicit audit events at the `tool_call.*` level; the debit is captured in the hierarchical budget store and is visible via the budget query API. A future budget-specific event type is reserved but not yet defined.

---

## Security and tenant isolation

The audit subsystem enforces the same tenant isolation as the rest of Portico:

- `Store.Emit` discards any event with an empty `TenantID`. This is checked before the Redactor runs, so a missing tenant cannot be papered over by payload manipulation.
- `Store.Query` requires a non-empty `TenantID` and returns an error otherwise. The query SQL filters on `tenant_id = ?`; there is no "all tenants" default.
- The `admin` JWT scope is the only path to cross-tenant queries, and that path is explicitly documented in [Multi-tenancy](/concepts/multi-tenancy).
- Secret values never appear in audit payloads. Vault operations record only the secret `name`, never the value. The Redactor provides a second layer of defence if a caller accidentally includes a value.
- Full tool call arguments are never persisted raw. Call sites pass a pre-summarised `args_summary` string, not the full argument map.

---

## Console audit page

The Console exposes the audit log at the `/audit` route. A search box drives the `GET /api/audit/search` endpoint; a type filter and time-range picker narrow the results. Events are paginated with 50 rows per page using the cursor from the API response.

Clicking an event opens a drawer with the full payload, the resolved `[REDACTED:*]` markers visible where scrubbing fired, and a link to the originating session's inspector view when `session_id` is present.

The session inspector (`/sessions/[id]/inspect`) integrates the audit log as one of five timeline lanes — alongside spans, drift events, policy decisions, and approvals — so an operator can see where in the execution timeline a specific event occurred. See [Observability](/concepts/observability) for the full inspector description.

---

## Related

- [Policy](/concepts/policy) — how `policy.allowed`, `policy.denied`, and `policy.rule_changed` events originate
- [Approvals](/concepts/approvals) — the full lifecycle of `approval.pending`, `approval.decided`, and `approval.replayed`
- [Credentials Vault](/concepts/credentials-vault) — `vault.*` and `credential.*` event semantics
- [Observability](/concepts/observability) — how audit events appear in the session inspector timeline
- [Multi-tenancy](/concepts/multi-tenancy) — tenant isolation guarantees at the audit store layer
- [Security Model](/concepts/security-model) — the broader security posture the audit trail supports
