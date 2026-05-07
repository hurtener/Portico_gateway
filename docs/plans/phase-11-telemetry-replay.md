# Phase 11 — Telemetry Replay & Time-Travel Inspector

> Self-contained implementation plan. Builds on Phase 0–10.

## Goal

Turn Portico into a time-travel debugger. Every session already produces audit events (Phase 5), spans (Phase 6), drift events (Phase 6), and policy decisions (Phase 9). Today an operator triaging an incident reads each in isolation. Phase 11 unifies them into one timeline, with scrub + replay built in.

The deliverables:

- A **session inspector** that lays the trace waterfall, audit log, drift events, and policy decisions on a single horizontal timeline.
- A **scrubber** that reveals state at an arbitrary `t` (catalog, active skill, vault references in scope, last tool result).
- A **bundle exporter** that packages a session — spans, audit, drift, policy, redaction-safe payloads — into a portable archive an operator can attach to a ticket or share with another Portico instance for offline analysis.
- A **bundle importer** that opens an exported archive in a read-only inspector view (no live system needed).
- **One-click replay** for any tool call within a session, reusing Phase 10's playback machinery.

After Phase 11, "what happened in session XYZ?" is one URL away. After-the-fact debugging stops requiring grep across logs.

## Why this phase exists

The user's goal: "Telemetry visualization first-class for the replay, then we extend with the LLM gateway." Phase 10 exposed a playground that runs *new* calls; Phase 11 lets the operator replay *old* sessions. Together they cover before-the-fact (playground) and after-the-fact (replay) debugging.

A second motivator: the OTel exporter ships spans to an external collector, but Portico needs a self-contained view that works without a third-party Grafana / Honeycomb / Tempo instance. The `inspect-session` CLI shipped in Phase 6 dumps JSON; Phase 11 turns that JSON into a navigable UI.

## Prerequisites

Phases 0–10 complete. Specifically:

- Per-session catalog snapshot binding (Phase 6) — each session has a known snapshot id.
- OTel exporter with batch SDK + W3C TraceContext propagator (Phase 6).
- Audit store + redactor (Phase 5) — events carry `session_id`, `trace_id`, `span_id`, `tool`, `tenant_id`.
- Schema-drift detector (Phase 6) — events tagged `schema.drift`.
- Policy decision events (Phase 9) — `policy.allowed` / `policy.denied` / `policy.required_approval` carry the rule id + matched conditions.
- `inspect-session` CLI (Phase 6) — same data model the UI consumes.
- Phase 10 components: `SpanTree.svelte`, `EvalTree.svelte`, the correlation bundle shape, the playback machinery.
- Phase 7 component library: `Tabs`, `Drawer`, `Modal`, `Toast`, `Skeleton`, `CodeBlock`.

## Deliverables

1. **Self-contained span store** — Portico already exports spans to OTel, but it doesn't store them itself. Phase 11 introduces `internal/telemetry/spanstore/` — a SQLite-backed span store that the OTel exporter writes to *in addition to* the configured external collector. Configurable retention (default 7 days) per tenant.
2. **Unified session bundle API** — `GET /api/sessions/{sid}/bundle` returns the full bundle: session row + snapshot + spans + audit events + drift events + policy decisions + approvals. JSON. The `inspect-session` CLI rewrites to consume this endpoint when run against a live binary; falls back to direct SQLite when run offline.
3. **Inspector route** — `/sessions/[id]/inspect`. Three-pane layout: timeline (top), span tree + audit + policy/drift tabs (centre), state-at-time scrubber (right). Keyboard navigable.
4. **Timeline component** — `web/console/src/lib/components/Timeline.svelte`. Horizontal lanes for each subsystem (spans, audit, drift, policy). Click a row to open the centre detail. Brushable selector for time range.
5. **State-at-time scrubber** — pick `t` (slider or click on the timeline). Right pane shows: active catalog snapshot at `t`, last tool result before `t`, vault refs requested before `t` (names only, never plaintext), active approval requests, pending elicitations.
6. **Bundle exporter** — `POST /api/sessions/{sid}/export`. Produces a `.portico-bundle.tar.gz` with deterministic structure: `manifest.json`, `spans.jsonl`, `audit.jsonl`, `drift.jsonl`, `policy.jsonl`, `approvals.jsonl`, `snapshot.json`. Redaction is canonical (uses Phase 5 redactor). Optional encryption: pass an `--encrypt` flag and the bundle is age-encrypted with a recipient key.
7. **Bundle importer** — `POST /api/sessions/import` accepts a bundle and registers it under a synthetic `imported:<bundle_id>` session id. The inspector renders it identically to a native session; live calls are not possible (read-only).
8. **Replay-from-inspector** — every tool call within a session has a "Replay" button that hands off to Phase 10's playground. The replay starts in the same snapshot the original call ran against; the inspector links the original ↔ replay run.
9. **Cross-session pivots** — clicking a `tool` cell jumps to all sessions in the last 24h that called the same tool. Clicking a `tenant_id` cell pivots to that tenant's session list.
10. **Audit search** — server-side full-text search over `audit_events` per tenant. The inspector grows a search box that filters the timeline live; the existing `/audit` page reuses the same backend.

## Acceptance criteria

1. Every span produced by the OTel exporter is also persisted in `spanstore`. Verified by an integration test that runs a tool call, then reads `spanstore` and the OTel mock collector — counts match exactly.
2. `GET /api/sessions/{sid}/bundle` returns a complete, well-formed bundle for any session in the retention window. Latency for a typical session (≤ 100 audit events, ≤ 50 spans): ≤ 200 ms on a warm DB.
3. `/sessions/[id]/inspect` renders the full timeline within ≤ 1.5 s for a session with 200 events. The timeline supports click, scrub, and zoom (mouse wheel + pinch on trackpad).
4. State-at-time scrubber correctly reconstructs catalog + last result + vault refs at any `t`. Verified by an integration test that times the audits, scrubs to two distinct `t`s, and asserts the right state is shown.
5. Bundle exporter produces a deterministic archive: same session → same bytes (modulo signing). Bundle is reproducible across runs and platforms; a checksum in `manifest.json` is the integrity gate.
6. Bundle importer accepts a bundle from another Portico instance (or from this one) and presents it read-only. Tries to import a tampered bundle (modified after checksum) → typed error `bundle_corrupt`.
7. Replay-from-inspector hands off cleanly to the playground, lands on the right snapshot, runs the call, and links the replay run back to the original session. Drift detection works.
8. Audit search returns results within ≤ 500 ms for a single tenant with 100k events on the index.
9. Smoke: `scripts/smoke/phase-11.sh` covers bundle, inspect, export, import, replay-from-inspector, audit search. SKIP for unimplemented; OK ≥ 9 by phase close.
10. Coverage: ≥ 75% across new packages.
11. Cross-tenant isolation: `tenantA` cannot fetch a bundle, export a session, or import into `tenantB`. Integration test asserts.
12. Bundle redaction: payloads in the exported archive go through Phase 5's redactor; an integration test deliberately seeds a credential-shaped payload and asserts it is redacted in the export.

## Architecture

```
internal/telemetry/
├── spanstore/
│   ├── store.go                  # interface
│   ├── sqlite/
│   │   ├── store.go              # SQLite-backed implementation
│   │   └── store_test.go
│   └── exporter_hook.go          # tee from OTel exporter into spanstore
└── otel_impl.go                  # extended to wire the spanstore tee

internal/sessionbundle/
├── bundle.go                     # Bundle assembler (reads spans+audit+drift+policy+approvals+snapshot)
├── exporter.go                   # writes the .portico-bundle.tar.gz
├── importer.go                   # reads + verifies + registers a bundle as imported:<id>
├── canonical.go                  # canonical JSONL writers (deterministic ordering)
└── crypto.go                     # optional age encryption (post-V1 decryption hook reserved)

internal/audit/
└── search.go                     # full-text indexer + query (FTS5 over audit_events)

internal/server/api/
├── sessions_inspect.go           # /api/sessions/{sid}/{bundle,export,import}
└── audit_search.go               # /api/audit/search

internal/storage/sqlite/migrations/
├── 0011_spanstore.sql
└── 0012_audit_fts.sql

cmd/portico/
└── cmd_inspect_session.go        # rewritten to consume /api/sessions/{sid}/bundle (with offline fallback)

web/console/src/
├── lib/components/
│   ├── Timeline.svelte
│   ├── TimelineLane.svelte
│   ├── BundleSummary.svelte
│   └── StateAtTime.svelte
└── routes/sessions/
    ├── +page.svelte              # extended: search + cross-session pivots
    ├── [id]/+page.svelte         # existing summary; gains "Open inspector" CTA
    └── [id]/inspect/+page.svelte # the time-travel UI
```

## SQL DDL

### Migration 0011 — span store

```sql
CREATE TABLE IF NOT EXISTS spans (
    tenant_id    TEXT NOT NULL,
    session_id   TEXT,
    trace_id     TEXT NOT NULL,
    span_id      TEXT NOT NULL,
    parent_id    TEXT,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,          -- 'internal' | 'server' | 'client' | 'producer' | 'consumer'
    started_at   TEXT NOT NULL,
    ended_at     TEXT NOT NULL,
    status       TEXT NOT NULL,          -- 'unset' | 'ok' | 'error'
    status_msg   TEXT,
    attrs_json   TEXT NOT NULL,          -- canonical JSON of attribute set
    events_json  TEXT NOT NULL,          -- canonical JSON of timed events (limited)
    PRIMARY KEY (tenant_id, trace_id, span_id)
);

CREATE INDEX IF NOT EXISTS idx_spans_session   ON spans(tenant_id, session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_spans_trace     ON spans(tenant_id, trace_id, started_at);
CREATE INDEX IF NOT EXISTS idx_spans_started   ON spans(tenant_id, started_at);
```

### Migration 0012 — audit FTS5

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS audit_events_fts USING fts5(
    type, summary, payload_json,
    content='audit_events',
    content_rowid='rowid'
);

-- Maintain the index on insert/update/delete via triggers.
CREATE TRIGGER IF NOT EXISTS audit_events_ai AFTER INSERT ON audit_events BEGIN
    INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
    VALUES (new.rowid, new.type, COALESCE(new.summary, ''), COALESCE(new.payload_json, ''));
END;

CREATE TRIGGER IF NOT EXISTS audit_events_ad AFTER DELETE ON audit_events BEGIN
    INSERT INTO audit_events_fts(audit_events_fts, rowid, type, summary, payload_json)
    VALUES ('delete', old.rowid, old.type, COALESCE(old.summary, ''), COALESCE(old.payload_json, ''));
END;

CREATE TRIGGER IF NOT EXISTS audit_events_au AFTER UPDATE ON audit_events BEGIN
    INSERT INTO audit_events_fts(audit_events_fts, rowid, type, summary, payload_json)
    VALUES ('delete', old.rowid, old.type, COALESCE(old.summary, ''), COALESCE(old.payload_json, ''));
    INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
    VALUES (new.rowid, new.type, COALESCE(new.summary, ''), COALESCE(new.payload_json, ''));
END;
```

`audit_events.summary` is a Phase-11-introduced denormalised column (added by an `ALTER TABLE`) carrying a one-line redacted summary so the FTS doesn't need to scan full payloads at query time.

## Public types

```go
// internal/telemetry/spanstore/store.go

type Span struct {
    TenantID    string
    SessionID   string
    TraceID     string
    SpanID      string
    ParentID    string
    Name        string
    Kind        string
    StartedAt   time.Time
    EndedAt     time.Time
    Status      string
    StatusMsg   string
    Attrs       map[string]any
    Events      []SpanEvent
}

type SpanEvent struct {
    Name      string
    Timestamp time.Time
    Attrs     map[string]any
}

type Store interface {
    Put(ctx context.Context, batch []Span) error
    QueryBySession(ctx context.Context, tenantID, sessionID string) ([]Span, error)
    QueryByTrace(ctx context.Context, tenantID, traceID string) ([]Span, error)
    Purge(ctx context.Context, before time.Time) (int64, error)
}
```

```go
// internal/sessionbundle/bundle.go

type Bundle struct {
    Manifest Manifest
    Session  SessionRow
    Snapshot json.RawMessage      // the bound catalog snapshot
    Spans    []spanstore.Span
    Audit    []audit.Event
    Drift    []audit.Event        // type=schema.drift
    Policy   []audit.Event        // type=policy.*
    Approvals []ApprovalRow
}

type Manifest struct {
    Schema       string    `json:"schema"`           // "portico-bundle/v1"
    BundleID     string    `json:"bundle_id"`
    SessionID    string    `json:"session_id"`
    TenantID     string    `json:"tenant_id"`        // present on export
    GeneratedAt  time.Time `json:"generated_at"`
    Range        Range     `json:"range"`
    Counts       Counts    `json:"counts"`
    Checksum     string    `json:"checksum"`         // sha256 of canonical jsonls
    Encrypted    bool      `json:"encrypted"`
}

type Range struct { From, To time.Time }
type Counts struct { Spans, Audit, Drift, Policy, Approvals int }

func Load(ctx context.Context, tenantID, sessionID string) (*Bundle, error)
```

```go
// internal/sessionbundle/exporter.go

type ExportOptions struct {
    Encrypt        bool
    RecipientKey   string   // age recipient
    OmitPayloads   bool     // drop payload_json fields entirely
}

func Export(ctx context.Context, b *Bundle, w io.Writer, opt ExportOptions) error
```

```go
// internal/sessionbundle/importer.go

type ImportResult struct {
    SyntheticSessionID string  // "imported:<bundle_id>"
    BundleID           string
    Range              Range
    Counts             Counts
}

func Import(ctx context.Context, tenantID string, r io.Reader) (*ImportResult, error)
```

```go
// internal/audit/search.go

type SearchQuery struct {
    Q          string
    From, To   time.Time
    SessionID  string
    Type       string
    Tenant     string
    Limit      int      // default 100, max 1000
    Cursor     string   // opaque
}

type SearchResult struct {
    Events []Event
    Next   string       // cursor for next page
}

func Search(ctx context.Context, q SearchQuery) (SearchResult, error)
```

## REST API

```
GET    /api/sessions/{sid}/bundle                 → JSON Bundle
POST   /api/sessions/{sid}/export                 → application/octet-stream (.portico-bundle.tar.gz)
POST   /api/sessions/import                       → multipart upload; returns ImportResult
GET    /api/sessions/imported                     → list imported bundles for tenant

GET    /api/spans?session_id=…&trace_id=…         → query spanstore
GET    /api/audit/search?q=…&from=…&to=…&type=…   → audit search

POST   /api/sessions/{sid}/calls/{cid}/replay     → hand-off to Phase 10 playback; returns Run
```

The bundle export endpoint streams; the importer accepts up to 100 MB by default (configurable per tenant).

## CLI

```bash
# inspect-session rewritten to consume the bundle endpoint when a live URL is provided
portico inspect-session <sid> --output json
portico inspect-session <sid> --base-url http://localhost:8080 --token "$JWT"
portico inspect-session --bundle ./incident.portico-bundle.tar.gz   # offline mode

# new export/import CLIs
portico session export <sid> --out ./incident.portico-bundle.tar.gz
portico session export <sid> --encrypt --recipient age1...
portico session import ./incident.portico-bundle.tar.gz
```

## Console screens

### `/sessions` (extended)

`Table` gains a search box and tag filters; clicking a row opens the existing summary. Each row gets a **Inspect** button (kbd: `Enter`) that jumps to `/sessions/[id]/inspect`.

### `/sessions/[id]/inspect`

Three-pane layout:

- **Top (Timeline, ~140 px)**: `Timeline.svelte` with lanes:
  - Spans (waterfall-shaded)
  - Audit (dotted markers)
  - Drift (red triangles)
  - Policy (filled circles, colour by decision)
  - Approvals (hollow rectangles)
  - Brush selector for zoom; mouse wheel zooms in/out at the cursor.
- **Centre**: Tabs:
  - **Trace** — `SpanTree` rooted at the session's first span, durations, attribute filter.
  - **Audit** — virtualised list with redacted payload preview; click → expand in a Drawer.
  - **Policy** — `EvalTree` per decision; chain across multiple decisions.
  - **Drift** — list of drift events with diff snippets.
  - **Approvals** — table of approval requests + outcomes.
- **Right (StateAtTime, ~360 px)**:
  - Time pin (`t`) — drag the timeline cursor or pick from a list of "interesting moments" (errors, drifts, denials).
  - "At t" panel: active snapshot, last tool result before `t`, vault refs in scope, pending elicitations, active approvals.
  - "Replay this call" CTA that hands off to Phase 10.

Header: PageHeader with session id (copy), tenant, snapshot id, started/ended timestamps, status (badge). Right-aligned action group: Export, Refresh, Open in CLI (copies the `inspect-session` command).

### Imported bundle view

Same layout as a live session, but a banner at top: "Imported bundle — read-only. Generated `<at>` from tenant `<tenant_id>`. Replay disabled." The Replay button is hidden.

### `/audit`

Search box + lane filters. Searching pivots the existing audit list to FTS-backed results.

## Implementation walkthrough

### Step 1 — Span store

Implement `internal/telemetry/spanstore/sqlite/`. The OTel exporter wraps the configured external exporter with a fan-out that also writes to the local store. Buffered, drop-oldest on overflow (default buffer: 1024 spans), audit event on first drop in a window. Retention purge runs hourly.

### Step 2 — Bundle assembler

`internal/sessionbundle/bundle.go::Load` runs the SQL reads the existing `inspect-session` CLI does, plus the new spanstore query. Canonical JSONL writers use Phase 6's `canonical.go` for stable byte order. The bundle's `manifest.checksum` is sha256 of the concatenated canonical bytes; the exporter writes the manifest *first* with the checksum populated, so the importer can verify before deserialising the rest.

### Step 3 — Exporter + importer

Standard tar+gz. Stream-friendly; writer never buffers the whole bundle in memory. Optional age encryption uses `filippo.io/age`. The importer registers the bundle under `imported:<bundle_id>` in a new `imported_sessions` table; the existing session reads consult this table when the synthetic prefix is present.

### Step 4 — Audit FTS

Migration 0012 adds the FTS5 virtual table and triggers. `internal/audit/search.go::Search` runs typed queries; the response is paginated with cursor-based pagination so a noisy tenant doesn't OOM the API.

### Step 5 — Inspector frontend

`Timeline.svelte` renders SVG (cheap, snapshot-able). Lanes are a fixed grid; events render as positioned rects/circles/triangles. Hover shows a tooltip with the event row; click pins selection. Brushing fires a Svelte store event consumed by the centre pane.

`StateAtTime.svelte` reconstructs the state by replaying the audit log up to `t` (audit events are the source of truth for what state Portico saw at that point). Catalog binding is the snapshot; tool result is the most recent `tool_call.completed` payload.

### Step 6 — Replay glue

The "Replay this call" button POSTs `/api/sessions/{sid}/calls/{cid}/replay`, which materialises a `Case` (Phase 10) on the fly and runs `Replay`. The Phase 10 playback emits a Run that is linked back to the original session through `playground_runs.case_id` IS NULL but `playground_runs.session_id` carrying both the new session id and a `replay_of_session_id` attribute on the first span.

### Step 7 — CLI rewrite

`cmd/portico/cmd_inspect_session.go` keeps its current offline path (`--dsn` mode) and adds two more: `--base-url` + `--token` for live mode, and `--bundle` for archive mode. The shared core renders the same shape so all three paths produce identical output.

### Step 8 — Smoke + tests

`scripts/smoke/phase-11.sh` exercises bundle, export, import, audit search, replay handoff. Integration tests cover the spanstore tee, bundle determinism, redaction, and cross-tenant isolation.

## Test plan

### Unit

- `internal/telemetry/spanstore/sqlite/store_test.go`
  - `TestStore_PutAndQueryBySession`.
  - `TestStore_QueryByTrace_OrdersByStart`.
  - `TestStore_Purge_RespectsBefore`.
  - `TestStore_TenantIsolation`.
- `internal/telemetry/spanstore/exporter_hook_test.go`
  - `TestExporterHook_Tees_NoSpanLost`.
  - `TestExporterHook_DropsOldestOnOverflow_AuditEventEmitted`.
- `internal/sessionbundle/bundle_test.go`
  - `TestLoad_HappyPath`.
  - `TestLoad_NotFound`.
  - `TestLoad_TenantIsolation`.
- `internal/sessionbundle/exporter_test.go`
  - `TestExport_DeterministicBytes`.
  - `TestExport_RedactsPayloads`.
  - `TestExport_OptionalEncryption_RoundTrip`.
- `internal/sessionbundle/importer_test.go`
  - `TestImport_HappyPath`.
  - `TestImport_TamperedChecksum_Refused`.
  - `TestImport_RegistersSyntheticSession`.
- `internal/audit/search_test.go`
  - `TestSearch_ByText`.
  - `TestSearch_ByTimeRange`.
  - `TestSearch_TenantIsolation`.
  - `TestSearch_PaginationCursor`.

### Integration (`test/integration/replay/`)

- `TestE2E_SpanCounts_Match` — run a tool call; spanstore count == OTel mock count.
- `TestE2E_Bundle_RoundTrip` — export → import → bundle reads identical to the original.
- `TestE2E_BundleEncryption_RoundTrip`.
- `TestE2E_Inspector_StateAtTime` — known sequence of events; scrub to two `t`s; assert state.
- `TestE2E_ReplayFromInspector_LinksOriginal`.
- `TestE2E_AuditSearch_Performance` — 100k events; query returns in < 500 ms.
- `TestE2E_TenantIsolation_BundlesAndSearch`.

### Frontend tests

- `web/console/tests/inspect.spec.ts` (Playwright): scrubbing the timeline updates the state pane; replay button hands off; imported bundle hides replay.

### Smoke

`scripts/smoke/phase-11.sh`:
- GET bundle
- POST export → assert tar header + manifest schema
- POST import → assert ImportResult
- GET imported sessions
- GET audit search
- POST replay → assert Run created
- skip_if_404 for unimplemented surfaces

OK ≥ 9 by phase close, FAIL = 0.

### Coverage gates

- `internal/telemetry/spanstore`: ≥ 80%.
- `internal/sessionbundle`: ≥ 80%.
- `internal/audit/search.go`: ≥ 75%.

## Common pitfalls

- **Span tee back-pressure on the OTel hot path.** The spanstore writer must NEVER block the OTel exporter. Bounded buffer + drop-oldest is mandatory; a span that lands in the external collector but not in the local store is acceptable, the inverse is not (we'd be lying about what was exported).
- **Bundle determinism across platforms.** Map iteration in Go is non-deterministic. The canonical writers MUST sort keys; bundle bytes must hash identically on Linux/macOS/Windows.
- **Redaction misses nested fields.** The Phase 5 redactor walks JSON; ensure the bundle exporter passes payloads through it before serialising. Test with a deliberately-nested credential payload.
- **FTS5 unicode tokenisation.** Default tokeniser drops non-ASCII; for non-English error messages the search misses. Use `unicode61` tokeniser.
- **State reconstruction with out-of-order audit events.** Some clock skew is normal; the reconstruction sorts by `(occurred_at, sequence)` where `sequence` is a per-tenant monotonic counter we already write. Don't rely on `occurred_at` alone.
- **Imported session writes leaking into live tenant data.** An imported bundle is read-only. The session registry must reject writes (tool_call, audit insert) against synthetic session ids; integration test required.
- **Timeline rendering cost.** SVG with 10k events is slow; throttle to a maximum of 2k visible events per lane and aggregate the rest into a "+12 more" badge that expands on click.
- **Cross-tab inspector state.** Two tabs open on the same session pollute each other's pinned `t`. Each tab keeps its own state in the URL hash so reload restores it.
- **Bundle size bloat.** A long-running session with thousands of audit events produces a large archive; gz compression mitigates but operators should be able to truncate. `--from` / `--to` flags on `portico session export` clip the time range.
- **Encryption recipient management.** Bundles encrypted to a key the operator has lost are unrecoverable. The export endpoint logs the recipient fingerprint; operators should keep a recovery key in a separate vault.

## Out of scope

- **Continuous profiling.** OTel logs + metrics + traces; profiling is a separate Phase 14 idea.
- **Distributed traces across multiple Portico instances.** V1 ships single-instance; cross-instance replay is post-V1.
- **AI-assisted incident triage.** "Summarise this session" is post-V1; depends on the LLM gateway.
- **Per-user RBAC over bundles.** Today the gating is `audit:read` + tenant scope; finer-grained ACLs over bundles are post-V1.
- **Replaying entire sessions automatically.** Phase 10/11 can replay a single tool call; "rerun this whole session" is post-V1 and dangerous (irreversible side effects).
- **Live trace streaming during the call.** The inspector reads from spanstore, which is updated post-call (after the OTel batch flush). For sub-second insight during a call, use the Phase 10 playground correlation rail.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-11.sh` shows OK ≥ 9, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. CLI `inspect-session` parity test: same input session produces byte-identical output across the three modes (offline / live / bundle).
5. PR description references RFC §"Observability" + this plan; lists retention defaults and the FTS index size estimate.
6. README at repo root mentions session inspector + bundle export as a V1 feature.

## Hand-off to Phase 12

Phase 12 (Onboarding & Distribution → V1 ship) inherits:

- A self-contained observability stack that runs without an external collector — important for the first-run experience.
- Bundle export as the canonical "send this to support" workflow.
- A polished `/sessions/[id]/inspect` page that doubles as the demo when first-run users want to see what Portico does.

Phase 12's first task: ship a first-run wizard that walks an operator from `portico dev` to "I have a tenant, a server, and an authored skill". The inspector and bundle export are the closing flourish — Phase 12 runs the wizard against fixture data and the inspector renders it cleanly, sealing the V1 demo loop.
