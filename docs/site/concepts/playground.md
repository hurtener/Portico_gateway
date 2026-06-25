# Playground

The Playground is an interactive operator surface built into the [Console](/concepts/console) at `/playground`. It lets you send real MCP calls — tool calls, resource reads, and prompt previews — against a live tenant catalog, and watch every layer of the gateway respond in real time: the JSON-RPC stream, trace spans, audit events, policy decisions, and schema-drift signals. Saved test cases and one-click replay make it the canonical pre-flight tool for validating new server registrations, skill changes, and policy edits before agents encounter them.

## How a session works

Opening `/playground` immediately bootstraps a session. This is not a UI abstraction — it is a real MCP session against the gateway, governed by the same machinery every other connection uses.

### Synthetic JWT

The server mints a short-lived RS256 JWT signed with a key dedicated to playground tokens. The token is returned in the session response and used by the browser-side client for every subsequent request within that session.

The JWT payload carries:

```jsonc
{
  "iss": "portico-playground",
  "aud": ["portico"],
  "sub": "playground:<actor_user_id>",
  "tenant": "<tenant_id>",
  "scope": ["playground:execute", ...],  // capped — see below
  "iat": 1719200000,
  "exp": 1719201800,   // 30-minute lifetime by default
  "meta": {
    "playground_session": "psn_01J0...",
    "snapshot_id": "",           // empty → live snapshot
    "runtime_override": ""
  }
}
```

**Scope capping** is enforced server-side. No matter what scopes the operator requests, the playground token is limited to a fixed allowlist:

| Scope | Grants |
|---|---|
| `playground:execute` | Required. Run any call in the session. |
| `playground:save` | Save test cases. |
| `servers:read` | Browse registered servers. |
| `skills:read` | Browse available skill packs. |
| `policy:read` | Read policy rules (dry-run output). |
| `snapshots:read` | Read catalog snapshots. |

Scopes such as `tenants:admin` and `secrets:write` are unconditionally stripped. The playground can never escalate the operator's effective permissions.

The playground signing key's public half is registered with the gateway's JWT validator at boot so these tokens are accepted on the same northbound transport every real agent uses. This means [policy](/concepts/policy), credential injection, and [audit](/concepts/audit) all behave identically whether the caller is the playground or an external agent.

### Snapshot binding

When a session opens, it binds to a catalog snapshot. By default it binds to the live snapshot — a point-in-time fingerprint of every tool, resource, and prompt the tenant's registered servers expose. Operators can also pin a historical snapshot by passing `snapshot_id` in the session bootstrap request.

The session ID and the bound snapshot ID are both visible in the Console's top bar throughout the session.

See [Catalog and sessions](/concepts/catalog-and-sessions) for the snapshot model.

### Session lifecycle

```
POST /api/playground/sessions        → 201  { id, token, expires_at, snapshot_id, ... }
GET  /api/playground/sessions/{sid}  → 200  session detail (or 404 if expired)
DELETE /api/playground/sessions/{sid} → 204  graceful end
```

Each browser tab holds its own session. Sessions expire after 30 minutes of wall-clock time; the Console detects an expired session (404 on the GET) and mints a fresh one automatically.

## The catalog browser

The left rail of `/playground` shows the live catalog for the session's bound snapshot, organised into four sections:

- **Servers** — collapsed by default. Expanding a server reveals its tools in `<server>.<tool>` namespace form.
- **Resources** — URI-addressable resources exposed by registered servers.
- **Prompts** — named prompt templates with argument fields.
- **Skills** — skill packs visible in the session, sub-grouped by source type (local / git / HTTP / authored).

Each entry shows the last-snapshot fingerprint and a drift badge when the item's schema has changed since the snapshot was taken. Selecting any entry loads its form in the centre composer.

If the session was opened with a pinned historical snapshot, the catalog reflects that snapshot's surface, not the current live state.

## Issuing calls

### Tool call composer

Selecting a tool renders a form generated from the tool's JSON Schema as returned by `tools/list`. Required fields are marked, default values are applied, and validation runs client-side before the call is dispatched. The "Raw JSON" tab accepts a hand-written arguments object for cases where the schema alone doesn't cover the call shape.

Pressing "Run" issues the call in two steps:

```
POST /api/playground/sessions/{sid}/calls
     Body: { "kind": "tool_call", "target": "<server>.<tool>", "arguments": { ... } }
     → 202  { "call_id": "...", "session_id": "...", "status": "accepted" }
```

The browser then opens an SSE connection to stream the response:

```
GET /api/playground/sessions/{sid}/calls/{cid}/stream
    Accept: text/event-stream
```

Each event on the stream carries a typed frame:

```jsonc
// event: chunk
{ "type": "chunk", "data": { ...partial JSON-RPC result... } }

// event: end
{ "type": "end", "data": { ...final result... } }

// event: error
{ "type": "error", "data": { "code": -32000, "message": "..." } }
```

A keep-alive comment is emitted every 15 seconds to prevent the browser from closing idle connections during long-running tool calls. The output panel renders chunks as they arrive and collapses them into a final pretty-printed document on stream end. A "Raw" toggle shows the unformatted SSE frames.

The call traverses the full gateway stack: the dispatcher resolves the `<server>.<tool>` target, credential injectors attach secrets, the policy engine evaluates rules, and the southbound client issues the call to the downstream server. Nothing is short-circuited for playground sessions.

### Resource fetcher

Selecting a resource (`kind: resource_read`) and pressing "Run" issues a `resources/read` via the same session call path. Template variables in the URI (RFC 6570 form) are surfaced as form fields. Binary responses display as "binary, N bytes" with a download link; text responses render in a syntax-detected code block.

### Prompt previewer

Selecting a prompt (`kind: prompt_get`) and filling its argument fields issues a `prompts/get` call. The response renders the messages array — the sequence of role/content pairs the prompt template produces — so operators can verify template output without writing client code.

## The correlation rail

The right rail updates in real time as a call runs. It fetches the correlation bundle from:

```
GET /api/playground/sessions/{sid}/correlation?since=<RFC3339>
```

The `since` parameter enables incremental polling: while a call is in flight the Console polls every 500 ms; once the call ends and no new events have arrived for two seconds, the interval backs off to five seconds.

The bundle contains four categories of signal, all sourced from the [audit](/concepts/audit) log filtered by `playground_session` claim:

### Trace tab

A span tree built from `tool_call.start`, `tool_call.complete`, and `tool_call.failed` audit events. Each node shows name, start time, duration, status, and key attributes. The tree is structured as root → southbound call → tool-internal spans where present.

```jsonc
// Span node shape
{
  "span_id": "...",
  "parent_id": "...",
  "name": "filesystem.read_file",
  "started_at": "2026-06-24T12:00:00.123Z",
  "ended_at":   "2026-06-24T12:00:00.456Z",
  "status": "ok",           // ok | error | running
  "attributes": {
    "tenant_id": "acme",
    "tool": "filesystem.read_file"
  }
}
```

### Audit tab

Every audit event emitted during the call, in emission order. Events are passed through the same redactor the persistent audit store sees — no unredacted tool arguments appear in the playground output.

See [Audit](/concepts/audit) for the full event taxonomy.

### Policy tab

`policy.allowed` and `policy.denied` events rendered as a structured decision list showing the matched rule, the decision, and the reason string. The same component used by the `/policy/dry-run` page renders these decisions, so the display is consistent across operator surfaces.

### Drift tab

`schema.drift` events that fired during the call. A drift event is emitted whenever the southbound snapshot detects that a tool's `inputSchema` or a resource's MIME type has changed relative to the session's bound snapshot.

See [Drift detection](/concepts/drift-detection) for the drift model.

## Saved test cases

Any call can be saved as a named test case. From the composer's overflow menu, "Save as case" opens a dialog where the operator names the case, adds a description, and assigns tags. The case stores the exact call shape (kind, target, arguments) and optionally pins the current snapshot ID.

```
POST /api/playground/cases
Body:
{
  "name": "happy path — read config",
  "kind": "tool_call",
  "target": "filesystem.read_file",
  "payload": { "path": "/etc/app/config.yaml" },
  "snapshot_id": "snap_01J0...",   // optional; omit to replay against live
  "tags": ["smoke", "filesystem"]
}
```

Cases are scoped to the tenant and visible to every operator in that tenant who holds `playground:save`. They are stored in the `playground_cases` table and survive binary restarts.

### Browsing and filtering cases

`/playground/cases` lists all saved cases for the tenant in a table with columns for name, kind, target, last run status, tags, and creation date. Tag chips and a search box narrow the list. Clicking a row opens the case detail page at `/playground/cases/{id}`.

### Replay

The case detail page shows a read-only summary of the case and its full run history. Pressing "Replay" invokes:

```
POST /api/playground/cases/{id}/replay
→ 200  { "id": "run_01J0...", "session_id": "psn_01J0...", "snapshot_id": "...", "status": "running", ... }
```

The replay engine opens a fresh session, binds it to the case's pinned snapshot (or the live snapshot if none was pinned), executes the call through the full gateway stack, and records a `Run` row. Every replay is logged in the run history under `GET /api/playground/cases/{id}/runs`.

**Drift detection on replay.** When the case has a pinned `snapshot_id`, the replay engine compares the pinned snapshot's `overall_hash` against the live snapshot's `overall_hash`. If they differ, the run records `drift_detected: true` and the Console displays a banner identifying which tools, resources, or prompts changed. The call still executes against the live snapshot — drift detection is informational, not blocking.

```jsonc
// Run shape
{
  "id": "run_01J0...",
  "case_id": "case_01J0...",
  "session_id": "psn_01J0...",
  "snapshot_id": "snap_01J0...",
  "status": "ok",            // running | ok | error | denied
  "drift_detected": true,
  "summary": "completed",
  "started_at": "2026-06-24T12:00:00Z",
  "ended_at":   "2026-06-24T12:00:02Z"
}
```

A `status` of `denied` means the [policy](/concepts/policy) engine blocked the call. The audit and policy tabs in the correlation rail still populate so the operator can inspect which rule matched.

## Replaying an arbitrary session

Any past session visible on the Sessions page can be replayed in the playground via the "Replay in playground" link. This opens `/playground/sessions/{id}` with the same session's bound snapshot and the last call it issued pre-loaded in the composer. This surface is the seed for post-incident investigation: reconstruct the exact catalog surface an agent saw and reissue calls that produced unexpected results.

Correlation for a past run is available at:

```
GET /api/playground/runs/{run_id}/correlation
```

## Permission scopes

| Scope | Required for |
|---|---|
| `playground:execute` | Open a session, issue any call, replay a case |
| `playground:save` | Save, update, or delete test cases |

Read-only operators (no `playground:execute`) can browse the catalog browser and inspect saved cases but cannot issue calls. The "Run" and "Replay" buttons are disabled and the session bootstrap is skipped.

The `runtime_override` field in the session bootstrap request — which lets an operator force a `per_session` or `shared_global` runtime mode — requires the `admin` scope. Standard operators are bound to the tenant's configured default.

## Full REST surface

```
POST   /api/playground/sessions
GET    /api/playground/sessions/{sid}
DELETE /api/playground/sessions/{sid}
GET    /api/playground/sessions/{sid}/catalog
POST   /api/playground/sessions/{sid}/calls
GET    /api/playground/sessions/{sid}/calls/{cid}/stream
GET    /api/playground/sessions/{sid}/correlation

GET    /api/playground/cases
POST   /api/playground/cases
GET    /api/playground/cases/{id}
PUT    /api/playground/cases/{id}
DELETE /api/playground/cases/{id}
POST   /api/playground/cases/{id}/replay
GET    /api/playground/cases/{id}/runs

GET    /api/playground/runs/{run_id}
GET    /api/playground/runs/{run_id}/correlation
```

All endpoints require a valid JWT with at least `playground:execute`. Case-writing endpoints additionally require `playground:save`. See the [REST API reference](/reference/rest-api) for full request/response schemas.

## Tenant isolation

Playground sessions and saved cases are strictly tenant-scoped. A session created in tenant A is invisible to tenant B. The session bootstrap request can only target the operator's own tenant unless the JWT carries the `admin` scope. The API validates this at the handler layer before the session is created.

Test case data lives in the `playground_cases` and `playground_runs` tables, both keyed by `(tenant_id, case_id)` and `(tenant_id, run_id)` respectively. No query path touches records outside the requesting tenant.

## What the playground is not

- **A load testing tool.** The playground issues one call at a time. A bulk or concurrent run mode is post-V1.
- **A mock environment.** Calls reach live downstream servers. There is no offline stub mode where responses are fabricated by the gateway.
- **An approval flow UI.** If a tool call triggers an approval (the policy engine returns `approval_required`), the playground surfaces the approval prompt and links to the `/approvals` page. Completing the approval and continuing the same call from the playground is a future enhancement. See [Approvals](/concepts/approvals).

## Related

- [Console](/concepts/console) — the operator UI the playground is part of.
- [Catalog and sessions](/concepts/catalog-and-sessions) — snapshot mechanics underpinning session binding and replay drift detection.
- [Audit](/concepts/audit) — the event store the correlation rail queries.
- [Drift detection](/concepts/drift-detection) — schema-drift events surfaced in the Drift tab and on replay.
- [Policy](/concepts/policy) — policy decisions visible in the Policy tab and reflected in run status.
- [MCP Gateway](/concepts/mcp-gateway) — the gateway stack the playground calls traverse.
- [Skill Packs](/concepts/skill-packs) — skill-scoped catalog views in the playground session.
- [REST API reference](/reference/rest-api) — full schema for all playground endpoints.
