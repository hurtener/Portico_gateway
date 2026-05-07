# Phase 10 — Interactive MCP Playground

> Self-contained implementation plan. Builds on Phase 0–9.

## Goal

Land a first-class **playground** inside the Console: an interactive surface where an operator picks a tenant + session shape, browses the live catalog (servers / tools / resources / prompts / skills), composes a tool call (form-driven from each tool's JSON Schema), executes it, and watches the streamed JSON-RPC + SSE response with live trace, audit, drift, and policy decisions correlated. Saved test cases and one-click replays make the playground the canonical pre-flight tool for new servers, new skills, and policy changes.

This is the surface that turns Portico from "engine + dashboards" into "engine + dashboards + a place to actually use it." It rides on top of every previous phase: registry CRUD (Phase 9), authored skills (Phase 8), policy dry-run (Phase 9), audit + redactor (Phase 5), trace propagation (Phase 6).

## Why this phase exists

User feedback after Phase 6 named this directly: "MCP Servers configs, test flight from the UI with a playground, check on the skills, add more skills, management in general." Without a playground, an operator validates a new server by writing a curl script or asking an external client to connect. With one, they see the catalog the system actually exposes, send a tool call exactly the way an agent would, and watch the policy decisions, credential injections, and trace events in real time.

The playground also doubles as the seam for Phase 11 (telemetry replay) and Phase 13 (LLM gateway) — both reuse the same call-shape, the same streaming UI, and the same trace inspector. Building it once now means those phases inherit the surface instead of cloning it.

## Prerequisites

Phases 0–9 complete. Specifically:

- Northbound HTTP+SSE transport handles JSON-RPC + streamed responses (Phase 1).
- Per-session catalog snapshot machinery (Phase 6) — the playground binds to a snapshot at session start.
- Audit store + redactor (Phase 5) emit events the playground subscribes to.
- OTel trace propagation (Phase 6) — the playground reads spans for the call.
- Policy dry-run (Phase 9) — the playground reuses the eval-tree renderer.
- Phase 7 component library: `Form` primitives, `Tabs`, `CodeBlock`, `Badge`, `StatusDot`, `Skeleton`, `Toast`, `Drawer`.
- Phase 9 entity activity + permission scopes — the playground is gated behind `playground:execute` (separate from `servers:write`).

## Phase 9 carry-overs (must land before or alongside Phase 10)

These three items shipped in Phase 9 as documented deviations because the right home for them is the supervisor + playground work. Phase 10 absorbs them as first-class deliverables; do not lose them.

1. **Supervisor per-process log ring buffer + live SSE tail.** Phase 9 shipped the `/api/servers/{id}/logs` route as a stub (auth-gated, `text/event-stream`, info event on connect). Phase 10 lands the supervisor side: a bounded ring buffer per stdio process + an SSE writer that streams `since=…` to the route. The playground's "Output" panel reuses the same SSE parser the catalog client uses, so the supervisor change unblocks both the operator log viewer on `/servers/{id}` AND the playground's streaming output. Coverage gate: ≥ 80% on the new ring-buffer + SSE writer. Smoke: extend `scripts/smoke/phase-10.sh` to assert `GET /api/servers/{id}/logs?since=…` returns at least one log line for a server that has just emitted output.
2. **Approval-gate middleware for destructive Phase 9 verbs.** Phase 9 wired the `/v1/approvals` UI but did NOT route the destructive Phase 9 verbs (`DELETE /api/servers/{id}` with sessions, `DELETE /api/admin/tenants/{id}`, `POST /api/admin/tenants/{id}/purge`, `POST /api/admin/secrets/rotate-root`, `DELETE /api/admin/secrets/{name}` when an injector references it) through `internal/policy/approval.Flow.Run`. Phase 10 lands `internal/server/api/middleware/approval_gate.go` with the 202+poll pattern: handler returns `202 Accepted { approval_request_id }`, Console polls `/v1/approvals/{id}` until decided, then re-issues the original verb. The playground's Audit tab is the test surface — running a destructive call from the playground must surface the approval prompt and the eventual decision in the timeline.
3. **The Phase 9 integration tests deferred to smoke.** Three tests named in the Phase 9 plan ship in `test/integration/console_crud/` here:
   - `TestE2E_PolicyEdit_HotReload` — add a deny rule via REST; next tool call denied without restart.
   - `TestE2E_DestructiveDelete_RequiresApproval` — DELETE a server with a live session; assert `202 approval_required`; approve via the flow; DELETE succeeds.
   - `TestE2E_ServerCRUD_NoRestart` — POST `/api/servers`; tool call lands; PATCH env override; restart; tool call still lands with new env.
   These are the canonical proofs that the Phase 9 surface holds; the playground's smoke covers the same behaviours from the UI side.

## Deliverables

1. **Playground route** at `/playground` — full-page surface; not a tab inside another resource.
2. **Session bootstrap** — operator picks tenant (admin) or uses their own; picks a runtime mode override if allowed; the playground opens an MCP session against the gateway with a synthetic JWT minted server-side, scoped to `playground:execute` for the duration of the session. Session id is visible and copyable.
3. **Catalog browser** — left rail with grouped lists: servers → tools (with namespace `<server>.<tool>`), resources, prompts, skills (split by source: local / git / http / authored). Each entry shows last-snapshot fingerprint + drift indicator; clicking selects it for execution.
4. **Tool call composer** — auto-generated form from the tool's JSON Schema (`tools/list` + `inputSchema`). Operator fills fields; raw JSON tab toggles for power users; validation runs locally via Ajv with the same schema the gateway uses server-side.
5. **Resource fetcher** — pick a `resources/read` URI, optionally with template variables, see the bytes returned (text → CodeBlock with syntax detection; binary → "binary, N bytes" + download).
6. **Prompt previewer** — pick `prompts/get`, fill argument fields, see the rendered messages array.
7. **Skill runner** — list skills the active session sees; running one binds it for the rest of the playground session and the catalog filters down to the skill's allowed surface (existing Phase 4 virtual directory).
8. **Streaming output** — JSON-RPC response panel updates incrementally; SSE chunks render as a stream, error frames inline. Pretty-print + raw toggle.
9. **Live correlation rail** — right rail with Tabs:
   - **Trace** — span tree for the current call (root → southbound → tool-internal where present), durations, attributes, error status.
   - **Audit** — events emitted during the call (tool_call.dispatched, policy.allowed/denied, credential.injected, audit.dropped, etc.).
   - **Policy** — eval-tree (the same renderer used by the policy dry-run page).
   - **Drift** — schema-drift events that fired during the call.
10. **Saved cases** — operator names a call ("happy path", "rejects empty args", etc.) and saves it. Cases live per tenant; share-link copies a URL the operator can DM. Test cases support tagging + grouping.
11. **One-click replay** — pick any saved case (or any session from `/sessions`) and re-run it. Replay binds to the **same snapshot id** the original session used (Phase 6) so drift is detectable: replay diffs against original.
12. **Permission gating** — `playground:execute` required to run any call. `playground:save` to save test cases. Read-only operators can browse but not execute.

## Acceptance criteria

1. Opening `/playground` mints a synthetic JWT, opens an MCP session, fetches the live catalog, and renders the catalog browser within ≤ 1.5 s on a warm DB.
2. Tool call composer correctly generates a form for every tool the system exposes (servers, skills, MCP Apps). Required fields are marked, defaults are applied, oneOf/anyOf surfaces a tab strip.
3. Executing a tool call streams the response in real time. Network tab shows a single JSON-RPC over SSE channel; UI does not poll.
4. The Trace tab renders a flame-graph-shaped span tree for the call with sub-second latency. Timings, attributes, status, and links match what the OTel exporter receives.
5. The Audit tab surfaces every event the audit redactor emitted during the call, with the same redaction the persistent store sees.
6. The Policy tab shows the eval tree (same component as the dry-run page on `/policy/dry-run`).
7. The Drift tab shows any `schema.drift` events that fired during the call.
8. Saved cases survive a binary restart (persisted) and reload exactly: tool, args, and (optionally) snapshot id.
9. Replay against a snapshot that has drifted surfaces a "drift detected" banner with a diff view of the changed schema; the call still executes against the live snapshot.
10. Smoke: `scripts/smoke/phase-10.sh` exercises the catalog fetch, a happy-path tool call, the audit/trace/policy correlation endpoints, save+list+replay. SKIP for unimplemented; OK ≥ 10 by phase close.
11. Coverage: ≥ 75% on the new packages.
12. UI: every interaction works on a 1280×720 viewport without horizontal scroll. All interactive elements are keyboard-reachable; screen reader announces tab changes.

## Architecture

```
internal/server/api/
├── playground.go                 # session bootstrap + saved-case CRUD
├── playground_correlation.go     # /api/playground/sessions/{sid}/{trace|audit|policy|drift}
└── playground_replay.go

internal/playground/
├── session.go                    # synthetic JWT minting + session lifecycle
├── correlation.go                # collates spans + audit + policy decisions per session
├── snapshot_binding.go           # binds a session to a specific catalog snapshot
└── playback.go                   # executes a saved case; emits drift bundle

internal/storage/sqlite/migrations/
└── 0010_playground_cases.sql

web/console/src/
├── lib/
│   ├── playground/
│   │   ├── client.ts             # browser-side MCP client (JSON-RPC over fetch+SSE)
│   │   ├── form.ts               # JSON Schema → form descriptor
│   │   ├── stream.ts             # SSE parser
│   │   └── correlation.ts        # subscriber for trace/audit/policy/drift
│   └── components/
│       ├── SchemaForm.svelte     # Schema-driven form
│       ├── SpanTree.svelte       # OTel span tree renderer
│       └── EvalTree.svelte       # policy eval tree (extracted from /policy/dry-run)
└── routes/playground/
    ├── +page.svelte              # catalog + composer + output + correlation
    ├── cases/+page.svelte        # saved-cases list
    ├── cases/[id]/+page.svelte   # case detail + replay
    └── sessions/[id]/+page.svelte # replay an arbitrary session
```

The browser-side `client.ts` is the second MCP client in the codebase (the first being the southbound clients in `internal/mcp/southbound/`). It is a *subset* — it only speaks the methods the playground needs (`initialize`, `tools/list`, `tools/call`, `resources/list`, `resources/read`, `prompts/list`, `prompts/get`, `notifications/*`). It does not implement server-side surfaces.

## SQL DDL (migration 0010)

```sql
-- Saved playground test cases.
CREATE TABLE IF NOT EXISTS playground_cases (
    tenant_id   TEXT NOT NULL,
    case_id     TEXT NOT NULL,                 -- ULID
    name        TEXT NOT NULL,
    description TEXT,
    kind        TEXT NOT NULL,                 -- 'tool_call' | 'resource_read' | 'prompt_get'
    target      TEXT NOT NULL,                 -- '<server>.<tool>' | uri | prompt name
    payload     TEXT NOT NULL,                 -- canonical JSON of the call shape
    snapshot_id TEXT,                          -- optional pin
    tags        TEXT NOT NULL DEFAULT '[]',    -- canonical JSON array
    created_at  TEXT NOT NULL,
    created_by  TEXT,
    PRIMARY KEY (tenant_id, case_id)
);
CREATE INDEX IF NOT EXISTS idx_playground_cases_tenant_created
    ON playground_cases(tenant_id, created_at DESC);

-- Run history for cases (and ad-hoc executions).
CREATE TABLE IF NOT EXISTS playground_runs (
    tenant_id    TEXT NOT NULL,
    run_id       TEXT NOT NULL,                -- ULID
    case_id      TEXT,                         -- NULL for ad-hoc
    session_id   TEXT NOT NULL,
    snapshot_id  TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    ended_at     TEXT,
    status       TEXT NOT NULL,                -- 'running'|'ok'|'error'|'denied'
    drift_detected INTEGER NOT NULL DEFAULT 0,
    summary      TEXT,                         -- short text for the case list
    PRIMARY KEY (tenant_id, run_id)
);
CREATE INDEX IF NOT EXISTS idx_playground_runs_lookup
    ON playground_runs(tenant_id, started_at DESC);
```

Existing `audit_events` and the OTel span store remain canonical; `playground_runs` is a thin index.

## Public types

```go
// internal/playground/session.go

type SessionRequest struct {
    TenantID         string
    ActorUserID      string
    SnapshotID       string  // "" → bind to current
    RuntimeOverride  string  // "" | "shared_global" | "per_session" — admin-gated
    Scopes           []string
}

type Session struct {
    ID         string
    TenantID   string
    SnapshotID string
    Token      string         // synthetic JWT, lifetime bounded
    ExpiresAt  time.Time
}

func StartSession(ctx context.Context, req SessionRequest) (*Session, error)
func EndSession(ctx context.Context, tenantID, sessionID string) error
```

```go
// internal/playground/correlation.go

type CorrelationFilter struct {
    SessionID string
    Since     time.Time   // for incremental fetches
}

type Bundle struct {
    Spans   []SpanNode
    Audits  []audit.Event
    Policy  []policy.DryRunResult
    Drift   []audit.Event   // typed: schema.drift
}

type SpanNode struct {
    SpanID     string
    ParentID   string
    Name       string
    StartedAt  time.Time
    EndedAt    time.Time
    Status     string
    Attributes map[string]string
}

func Get(ctx context.Context, tenantID string, f CorrelationFilter) (*Bundle, error)
```

```go
// internal/playground/playback.go

type Case struct {
    ID         string
    Name       string
    Kind       string                // tool_call | resource_read | prompt_get
    Target     string
    Payload    json.RawMessage       // canonical
    SnapshotID string                // optional pin
    Tags       []string
}

func Replay(ctx context.Context, tenantID string, c Case) (*Run, error)

type Run struct {
    ID             string
    SessionID      string
    SnapshotID     string
    Status         string             // running|ok|error|denied
    DriftDetected  bool
    Summary        string
    StartedAt      time.Time
    EndedAt        time.Time
}
```

## REST API

```
POST   /api/playground/sessions                          → start a playground session
DELETE /api/playground/sessions/{sid}                    → end (graceful)
GET    /api/playground/sessions/{sid}/catalog            → snapshot-bound catalog
POST   /api/playground/sessions/{sid}/calls              → enqueue a tool/resource/prompt call
GET    /api/playground/sessions/{sid}/calls/{cid}/stream → SSE: response chunks
GET    /api/playground/sessions/{sid}/correlation        → trace+audit+policy+drift bundle
                                                            (?since= for incremental polling)

GET    /api/playground/cases                             → list saved cases
POST   /api/playground/cases                             → save a case
GET    /api/playground/cases/{id}                        → fetch
PUT    /api/playground/cases/{id}                        → update
DELETE /api/playground/cases/{id}                        → delete
POST   /api/playground/cases/{id}/replay                 → run + record a Run; returns Run
GET    /api/playground/cases/{id}/runs                   → run history

GET    /api/playground/runs/{run_id}                     → run detail (status, drift, summary)
GET    /api/playground/runs/{run_id}/correlation         → re-fetch the correlation bundle for that run
```

The `calls` and `stream` endpoints are JSON-RPC + SSE-shaped — they wrap the existing northbound transport so the browser-side client doesn't have to mint its own framing. Calls run **as if** an external agent connected; policy, credentials, audit, drift all behave identically.

## Console screens

### `/playground`

Layout (1440×900 reference, scales down):

- **Top bar**: tenant picker (admin-only), session pill (ID + "End"), snapshot dropdown (current vs. pinned), drift badge.
- **Left rail (320 px)**: catalog browser. Sections: Servers (collapsed by default; expand reveals tools), Resources, Prompts, Skills (sub-grouped: Local / Git / HTTP / Authored). Search box at top. Drift badges per item.
- **Centre (flex)**: composer + output stacked vertically.
  - Composer (~40% height): `SchemaForm` for the selected entry; "Run" button (large primary); "Raw JSON" tab; "Save as case" overflow.
  - Output: streamed response. Pretty / Raw toggle. Truncate-with-expand for huge payloads. "Copy" / "Download" actions.
- **Right rail (380 px)**: correlation `Tabs` — Trace / Audit / Policy / Drift. Each tab's content updates live as the call streams. Auto-scrolls.

Empty state: when no entry is selected, centre shows the architectural illustration (Phase 7 EmptyState) + a "Pick a tool to get started" hint.

### `/playground/cases`

`Table` of saved cases. Columns: name, kind, target, last run status, tags, created. Row click → detail. Filters: tag chips + a search box.

### `/playground/cases/[id]`

Read-only summary at top (name, description, kind, target, payload preview). "Replay" button. Run history below: list of past Runs with timestamps, status, drift indicator, links to the correlation bundle.

### `/playground/sessions/[id]`

Replay an arbitrary `/sessions` row — the existing Sessions page gets a "Replay in playground" link that lands here. Body: same shape as case detail, but fed from the session's bound snapshot + audit log.

## Implementation walkthrough

### Step 1 — Migration + repos

Land migration 0010. Add repos for `playground_cases` and `playground_runs`. Round-trip tests cover the canonical-JSON round trip on `payload` and `tags`.

### Step 2 — Synthetic JWT minter

`internal/playground/session.go::StartSession` mints a JWT signed with the gateway's *internal* signing key (a key dedicated to playground tokens; rotated on the same cadence as the JWT verification keys). The token carries:

- `sub: playground:<actor_user_id>`
- `tenant: <tenant_id>`
- `scope: ["playground:execute"]` (plus the actor's other scopes, capped to read-only equivalents — never escalate)
- `exp: now + 30 min`
- `meta.playground_session: <sid>`

The gateway's existing JWT validator accepts these tokens because they're signed with an issuer the validator already trusts. Audit events emitted during a playground session carry `meta.playground_session` so `/playground` can filter them.

### Step 3 — Snapshot binding

`internal/playground/snapshot_binding.go` wires the session to a specific catalog snapshot. Default behaviour: bind to the live snapshot for the tenant; on operator pick, bind to a historical one (Phase 6 keeps them).

For replay against a drifted snapshot, the binding compares fingerprints between the saved case's pinned snapshot and the live snapshot. If they differ, the playback returns the drift bundle (Phase 6 schema-drift detector reused) and the UI surfaces a banner. The actual call still executes against the live snapshot.

### Step 4 — Browser-side MCP client

`web/console/src/lib/playground/client.ts` speaks JSON-RPC over `fetch` for non-streamed verbs and over SSE for `tools/call`. Reuses the SSE parser from `web/console/src/lib/playground/stream.ts`. Token from the session bootstrap. `client.ts` does NOT speak server-initiated requests (elicitation) — the playground catches `elicitation/create` and routes the operator to `/approvals` in a new tab; resuming the call is a Phase 11 enhancement.

### Step 5 — Schema-driven form

`web/console/src/lib/playground/form.ts` walks a JSON Schema 2020-12 tree and emits a flat form descriptor (`{kind: 'string'|'integer'|'object'|'array'|'oneOf'|'enum', path, label, required, validators, …}`). `SchemaForm.svelte` renders it using the Phase 7 input primitives. Validation hooks into `ajv` on the browser side.

Edge cases:

- `oneOf`/`anyOf` → tab strip.
- `additionalProperties: true` → "+ Add custom field" affordance.
- `enum` → `Select`.
- `format: uri/email/uuid` → typed input with regex.
- Recursive schemas → expand on demand, capped at depth 5 (UI sanity).

### Step 6 — Streaming output

The "Run" button POSTs `/api/playground/sessions/{sid}/calls` and immediately opens an SSE connection to `/calls/{cid}/stream`. Each SSE event renders incrementally. JSON-RPC errors render in a danger panel; partial results render in a success panel. Stream end emits a final summary line.

### Step 7 — Correlation rail

The right rail polls `/api/playground/sessions/{sid}/correlation?since=…` every 500 ms while a call is in-flight, every 5 s otherwise. Spans render via `SpanTree.svelte` (a self-contained recursive component); audit events render via a virtualised list; policy via `EvalTree.svelte` extracted from the Phase 9 `/policy/dry-run` page.

`SpanTree` is the seed for Phase 11's full replay UI — keep it self-contained and well-tested.

### Step 8 — Saved cases CRUD

Standard CRUD against `playground_cases`. Save flow: from the composer, "Save as case" opens a `Modal` with name + description + tag chips; submits to `POST /api/playground/cases`. The case's `payload` is the canonical JSON of the call.

### Step 9 — Replay

`internal/playground/playback.go::Replay` opens a fresh session bound to the case's pinned snapshot (or live if none). Executes the call. Records a `Run` row. If the snapshot fingerprint drifted vs. live, sets `drift_detected = 1` and records a per-call diff in the audit store (`schema.drift` event tagged with `playground_run_id`).

### Step 10 — Smoke + tests

`scripts/smoke/phase-10.sh` covers: bootstrap, catalog fetch, an ad-hoc tool call, save case, replay, and a deliberate-drift replay (using a fixture). Integration tests cover the streamed call lifecycle, the synthetic JWT lifetime, and the correlation correctness.

### Step 11 — Phase 9 carry-overs

Pick this up at the start of the phase; it unblocks the streaming output panel and the destructive-verb tests:

- `internal/runtime/process/log_ring.go` — bounded per-process stdout/stderr ring buffer (default 1 MB, configurable). Subscriber API returns a `<-chan LogLine` with cancellation through `ctx`. Wire it into the supervisor's process spawn path so every started process feeds the ring.
- Wire the ring into `Registry.Logs(ctx, tenantID, serverID, since)`. Replace the closed-channel placeholder added in Phase 9.
- `internal/server/api/middleware/approval_gate.go` — chi middleware factory `NewApprovalGate(flow, audit, deps)` that wraps a handler and returns `202 Accepted { approval_request_id }` for destructive verbs. The middleware is mounted in `router.go` only on the explicit verb list above (do NOT blanket-wrap `DELETE`).
- `test/integration/console_crud/` — the three named tests above. Each uses the existing `examples/servers/mock` fixture for the in-flight tool call.

## Test plan

### Unit

- `internal/playground/session_test.go`
  - `TestStartSession_MintsScopedJWT`.
  - `TestStartSession_RuntimeOverride_AdminOnly`.
  - `TestStartSession_RejectsExpired`.
- `internal/playground/snapshot_binding_test.go`
  - `TestBinding_DefaultsToLive`.
  - `TestBinding_PinnedToHistorical`.
  - `TestBinding_DriftDetected_OnReplay`.
- `internal/playground/correlation_test.go`
  - `TestCorrelation_BundlesAllChannels`.
  - `TestCorrelation_Since_FiltersIncrementally`.
  - `TestCorrelation_Redaction_Honored`.
- `internal/playground/playback_test.go`
  - `TestPlayback_HappyPath`.
  - `TestPlayback_DriftDetected_StillExecutes`.
  - `TestPlayback_PolicyDeniesCallsAreRecorded`.

### Integration (`test/integration/playground/`)

- `TestE2E_Playground_HappyPath` — start session, list catalog, run a tool, assert audit + trace + policy events match.
- `TestE2E_Playground_StreamingResponse` — large response chunked over SSE, all chunks received and ordered.
- `TestE2E_Playground_PolicyDenied` — call a tool gated by a deny rule; UI reflects the denial; audit recorded.
- `TestE2E_Playground_TenantIsolation` — operator in tenantA cannot read tenantB's cases.
- `TestE2E_Playground_Replay_AgainstDrift` — pin a snapshot; mutate the catalog; replay; drift bundle returned, call still ran.
- `TestE2E_Playground_GoroutineLeak` — start/end 100 sessions in a loop; goroutine count returns to baseline.

### Frontend tests

- `web/console/tests/playground.spec.ts` (Playwright)
  - Catalog renders within 1.5 s.
  - Schema form validates required fields client-side.
  - SSE response renders incrementally.
  - Trace/audit/policy/drift tabs populate.
  - Save case + replay round-trip.

### Smoke

`scripts/smoke/phase-10.sh` covers: session bootstrap, catalog fetch, tool call (assert SSE chunked), correlation pull, case save, case replay. SKIP for unimplemented; OK ≥ 10 by phase close.

### Coverage gates

- `internal/playground`: ≥ 80%.
- `internal/server/api/playground*.go`: ≥ 80%.

## Common pitfalls

- **Synthetic JWT scope creep.** Easy mistake: copy the operator's full scope set into the playground token. Don't — cap to read-only equivalents + `playground:execute`. A playground token must never grant `tenants:admin` or `secrets:write`.
- **SSE keep-alive.** Long-running tool calls without output get killed by the browser's "no data" heartbeat. Emit a comment line every 15 s.
- **Form generation infinite recursion.** A self-referencing schema (`{$ref: "#"}`) blows the stack. Cap recursion depth and render a "↻ recursive — expand on demand" placeholder.
- **Correlation polling cost.** Polling `/correlation` every 500 ms during a call is fine; doing it every 500 ms when no call is in-flight is waste. Backoff to 5 s after `last_event_age > 2 s`.
- **Stream parse fragility.** SSE events can split mid-JSON. Buffer until newline + JSON-parse, never per-event.
- **Replay against deleted servers.** A saved case for a server that was deregistered should fail gracefully (Run.status = "error", summary = "server_unavailable") — never crash the playground.
- **Cross-tab session collision.** Two playground tabs sharing one session step on each other. Each tab gets its own session (cheap: session creation is one DB row + one JWT mint).
- **Browser-side caching of catalog.** Stale catalog after a Phase 9 server CRUD or a Phase 8 publish. Subscribe to `notifications/list_changed` and refetch.
- **Drift bundle bloat.** Replays that fan against a dramatically-drifted snapshot can produce huge diff payloads. Cap the per-run drift bundle at 256 KB; truncate with a "diff truncated, see /snapshots/{a}/diff/{b}" link.

## Out of scope

- **Multi-step orchestration.** The playground runs one call at a time. Chained tool calls (the Skill runtime already handles them) require a "scenario" abstraction — post-V1.
- **Mock server mode.** The playground talks to live servers. A Phase 14+ idea: spin up an in-process mock with stubbed responses for offline UI work.
- **Public share-links.** Saved-case URLs are tenant-scoped; they don't work outside the tenant. A Phase 12+ idea is signed share-links that anyone in the same tenant can open.
- **Server-initiated requests (elicitation) inside the playground.** Phase 5's elicitation flow is observed-only here; resuming an elicitation call is a Phase 11 (replay) enhancement.
- **Performance benchmarking.** Latency observability lands in the Trace tab; a "run this 100×" load mode is post-V1.
- **AI-assisted form filling.** "Pretend you're an agent — fill this form" is a fun idea but post-V1; it depends on Phase 13 (LLM gateway).

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-10.sh` shows OK ≥ 10, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. Frontend Playwright `playground.spec.ts` passes; bundle size delta vs. Phase 9 within +60 KB gzipped.
5. PR description references RFC §"Operator surface" + this plan; lists the catalog + correlation invariants the playground depends on.
6. README at repo root mentions the playground in the V1 features paragraph.

## Hand-off to Phase 11

Phase 11 (Telemetry replay) inherits:

- `SpanTree.svelte`, `EvalTree.svelte`, the SSE stream parser, the correlation bundle shape.
- Saved-case + replay machinery for the post-incident "rerun this exact session" workflow.
- The synthetic JWT pattern for read-only replays.

Phase 11's first task: extend `/sessions/[id]` into a full time-travel inspector that scrubs through every span, audit event, and drift event in a session, and lets the operator one-click replay any tool call within it (using Phase 10's playback). The `Bundle` shape becomes the canonical surface for "what happened in this session" — Phase 11 grows it with replay diffs and exportable bundles.
