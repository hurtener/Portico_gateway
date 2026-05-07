# Phase 9 — Console CRUD: Servers, Tenants, Secrets, Policy

> Self-contained implementation plan. Builds on Phase 0–8.

## Goal

Make the Console a first-class operations surface. Today the Console reads almost everything but writes very little — every server registration, tenant creation, secret rotation, or policy rule edit goes through `portico.yaml` + a binary restart, which the user explicitly called out as defeating the UX. After Phase 9 the operator runs Portico once and manages the system from the browser:

- **Servers** — register, edit, enable/disable, restart, view live logs from a single screen.
- **Tenants** — create, archive, configure runtime mode + quotas + JWT issuer.
- **Secrets** — vault entries CRUD + rotation, with credential reference picker that the rest of the UI consumes.
- **Policy** — rules editor with risk-class assignment, dry-run evaluator, change history.

Every write is hot-reloadable, audit-logged, multi-tenant scoped, and protected by a write-scoped JWT. None of these are V2 ideas — they're the V1 promise. Phase 9 closes the gap between "engine works" and "operator can run it."

## Why this phase exists

User feedback after Phase 6: "our system is not allowing to add servers or skills for example from the UI, right? That's something that is basic for these kind of systems."

The engine already knows how to do everything Phase 9 needs:
- The registry has a hot-reload path (Phase 2) and the SQLite store treats servers as runtime data, not config.
- The vault is keyed by `(tenant, name)` and the injectors look up by reference (Phase 5).
- The policy engine reads from YAML today but the rule shape is a structured tree that maps cleanly to a SQLite table (Phase 5).
- The audit store + redactor (Phase 5) are ready to record every CRUD event.

Phase 9 wires those engines to a write API and a Console surface. Skills CRUD landed in Phase 8. This phase covers the rest of the operator surface.

## Prerequisites

Phases 0–8 complete. Specifically:

- Tenant context flows via `tenant.MustFrom(ctx)` and `tenant_id` is on every relevant table (Phase 0).
- Process supervisor restart hooks exist (`internal/runtime/process/supervisor.go`, Phase 2).
- Registry hot reload is wired to fsnotify on the YAML side (Phase 2) — Phase 9 redirects it to a SQL-backed source of truth.
- Vault CRUD CLI exists (`portico vault put|get|delete|list|rotate-key`, Phase 5) — Phase 9 ports it to REST.
- Policy engine evaluates rules with risk classes (Phase 5).
- Audit store + redactor (Phase 5) accept arbitrary event types.
- Phase 7 component library: `Form` primitives, `Modal`, `Drawer`, `Toast`, `Table`, `Tabs`, `CodeBlock`, `KeyValueGrid`, `Skeleton`.
- Phase 8 CRUD pattern (REST + audit + hot-reload + Modal-form) — copy the shape.

## Deliverables

1. **Servers CRUD** — `POST/PUT/PATCH/DELETE /api/servers`, plus `POST /api/servers/{id}/restart`, `GET /api/servers/{id}/logs?since=…`, `GET /api/servers/{id}/health`. Full-form Console screens for create/edit. Live tail via SSE.
2. **Tenants CRUD** — `POST/PUT/DELETE /api/admin/tenants` (admin scope only), Console screens at `/admin/tenants`. Includes runtime mode, quotas (request/min, concurrent sessions, audit retention days), JWT issuer + JWKS URL.
3. **Secrets CRUD** — `POST/PUT/DELETE /api/admin/secrets`, `POST /api/admin/secrets/{name}/rotate`. Console form picks AES-GCM AAD context, ties to credential injectors. Reveal-on-demand for one-time copy (operator must re-auth or re-confirm).
4. **Policy editor** — `GET/PUT /api/policy/rules`, `POST /api/policy/dry-run`. Console editor at `/policy` with structured form + raw YAML toggle + dry-run pane that takes a tool call shape and shows the evaluation tree.
5. **Hot-reload everywhere** — every CRUD write emits an internal event that the registry / vault / policy engine listens to. No restart required for any of these surfaces. Documented "restart-required" list shrinks to zero for these four areas.
6. **Risky-action approvals** — destructive actions (delete server with running sessions, rotate vault root key, delete tenant, bulk-disable policy rule) go through the existing approval flow (Phase 5). Operator UI surfaces "Approval required" per CLAUDE.md §1 (no separate approval UI; emit `elicitation/create`).
7. **Audit + observability** — every write produces a structured audit event (`server.created`, `server.updated`, `secret.rotated`, `policy.rule_changed`, `tenant.created`, etc.) with a redacted before/after diff. OTel span per write request.
8. **Form validation parity** — server-side validation is canonical; the Console pre-validates with the same JSON Schema for instant feedback. The validator is a single function reused by REST + UI.
9. **Permission model** — write scopes (`servers:write`, `secrets:write`, `policy:write`, `tenants:admin`) checked in middleware. Read-only operators see disabled buttons + a tooltip; the API still rejects on the server side.
10. **Activity log + diff view** — every write surface gets an "Activity" tab showing the last N changes (who, when, before/after diff) sourced from the audit store.

## Acceptance criteria

1. An operator can register a new MCP server from the Console; the server enters the supervisor without a binary restart and starts processing requests within ≤ 2 s.
2. Editing a server's command/args/env restarts the affected process(es) and surfaces the new run-state in the Console within ≤ 3 s. Sessions with in-flight tool calls drain on a configurable grace window (default 10 s) before the restart.
3. Deleting a server with running sessions requires explicit confirmation. The supervisor terminates affected processes; sessions surface a typed JSON-RPC error code (`server_unavailable`) rather than hanging.
4. An operator can create a new tenant from `/admin/tenants`, set its JWT issuer + JWKS URL, and the new tenant immediately accepts JWTs at the gateway. Cross-tenant isolation invariants (CLAUDE.md §6) are preserved — verified by integration test.
5. Vault CRUD: create, read (metadata only — never the plaintext on list), update value (creates a new version), rotate root key (re-encrypts every value), delete. Reveal value works only via a separate `POST /reveal` endpoint that emits an audit event with the operator's identity and a one-shot grant token.
6. Policy editor: structured form persists as canonical YAML; raw YAML editor accepts the same content and round-trips byte-for-byte after canonicalisation; dry-run renders the evaluation tree (which rules matched, which lost on priority, the final risk class).
7. Hot reload: editing a policy rule from the Console takes effect on the next tool call; running sessions see the new ruleset without a restart.
8. Approval required: destructive actions emit a `elicitation/create` request the operator approves in their MCP client (or via the existing approval Console screen). The action waits up to a configurable timeout before failing closed.
9. Audit: every write surface emits a structured event with `tenant_id`, `user_id`, `before`, `after` (redacted via the Phase 5 redactor). The Activity tab on each entity reads the matching slice.
10. Smoke: `scripts/smoke/phase-9.sh` exercises every new endpoint. SKIP for unimplemented; OK ≥ 16 by phase close.
11. Coverage: ≥ 75% on every new package. ≥ 80% on `internal/server/api/servers.go`, `secrets.go`, `policy.go`, `tenants.go`.
12. Permission model: a `read-only` JWT cannot create/update/delete; the API returns `403 forbidden` with a typed `permission_denied` error code; the Console hides write affordances.

## Architecture

```
internal/server/api/
├── servers.go             # CRUD + restart + logs (SSE)
├── tenants.go             # CRUD (admin only)
├── secrets.go             # CRUD + rotate + reveal
├── policy.go              # rules CRUD + dry-run
├── audit_activity.go      # /api/{entity}/{id}/activity helper
└── middleware/
    ├── permission.go      # scope checks
    └── approval_gate.go   # routes destructive verbs through Phase 5 approval

internal/registry/
├── registry.go            # gains `Apply(ctx, mut)` for CRUD-driven mutations
├── watcher.go             # listens on registry mutation channel; fans into supervisor
└── …

internal/secrets/vault/
├── crud.go                # CRUD over the existing AES-GCM store
├── rotate.go              # root-key rotation (re-encrypt every value)
└── reveal.go              # gated plaintext fetch with audit hook

internal/policy/
├── store.go               # NEW: SQLite-backed rule store (alongside YAML compatibility)
├── editor.go              # validate + canonicalise + diff
└── dryrun.go              # evaluate against a synthetic tool call

internal/storage/sqlite/migrations/
└── 0009_console_crud.sql

web/console/src/routes/
├── servers/
│   ├── +page.svelte
│   ├── new/+page.svelte
│   ├── [id]/+page.svelte
│   └── [id]/edit/+page.svelte
├── admin/
│   ├── tenants/+page.svelte
│   ├── tenants/new/+page.svelte
│   ├── tenants/[id]/+page.svelte
│   ├── secrets/+page.svelte (extended)
│   └── secrets/[name]/+page.svelte
└── policy/
    ├── +page.svelte
    └── dry-run/+page.svelte
```

The "everything from YAML, watch with fsnotify" model that Phase 2 used for registry stays available as a fallback. Phase 9's source of truth becomes the SQLite tables, with YAML import/export as a one-way migration helper (an operator can `portico import --config portico.yaml` to seed). After Phase 9 the recommended deployment ships a minimal `portico.yaml` (only listener + JWT + storage paths) and operates everything else from the Console.

## SQL DDL (migration 0009)

```sql
-- Most relevant tables already exist (servers, tenants, vault entries,
-- audit_events). Phase 9 adds policy rules + a tenants metadata extension.

-- Tenants now carry runtime configuration.
ALTER TABLE tenants ADD COLUMN runtime_mode TEXT NOT NULL DEFAULT 'shared_global';
ALTER TABLE tenants ADD COLUMN max_concurrent_sessions INTEGER NOT NULL DEFAULT 16;
ALTER TABLE tenants ADD COLUMN max_requests_per_minute INTEGER NOT NULL DEFAULT 600;
ALTER TABLE tenants ADD COLUMN audit_retention_days INTEGER NOT NULL DEFAULT 30;
ALTER TABLE tenants ADD COLUMN jwt_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN jwt_jwks_url TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN status TEXT NOT NULL DEFAULT 'active'; -- 'active'|'archived'

-- Policy rules persisted (one row per rule; ordering by priority + idx).
CREATE TABLE IF NOT EXISTS tenant_policy_rules (
    tenant_id    TEXT NOT NULL,
    rule_id      TEXT NOT NULL,            -- stable slug, unique per tenant
    priority     INTEGER NOT NULL,         -- lower = earlier
    enabled      INTEGER NOT NULL DEFAULT 1,
    risk_class   TEXT NOT NULL,            -- 'low'|'medium'|'high'|'sensitive'
    conditions   TEXT NOT NULL,            -- canonical JSON (matchers)
    actions      TEXT NOT NULL,            -- canonical JSON (allow/deny/require_approval)
    notes        TEXT,
    updated_at   TEXT NOT NULL,
    updated_by   TEXT,
    PRIMARY KEY (tenant_id, rule_id)
);
CREATE INDEX IF NOT EXISTS idx_tenant_policy_rules_priority ON tenant_policy_rules(tenant_id, priority, rule_id);

-- Server runtime overrides table — config that changes more frequently
-- than the static server row (env, restart attempts, transient flags).
CREATE TABLE IF NOT EXISTS tenant_servers_runtime (
    tenant_id      TEXT NOT NULL,
    server_id      TEXT NOT NULL,
    env_overrides  TEXT NOT NULL DEFAULT '{}',   -- canonical JSON
    enabled        INTEGER NOT NULL DEFAULT 1,
    last_restart_at TEXT,
    last_restart_reason TEXT,
    PRIMARY KEY (tenant_id, server_id),
    FOREIGN KEY (tenant_id, server_id) REFERENCES servers(tenant_id, id) ON DELETE CASCADE
);

-- Activity log materialised view of audit_events for Console UI lookups.
-- This is a denormalised projection; the audit store remains canonical.
CREATE TABLE IF NOT EXISTS entity_activity (
    tenant_id     TEXT NOT NULL,
    entity_kind   TEXT NOT NULL,           -- 'server'|'tenant'|'secret'|'policy_rule'
    entity_id     TEXT NOT NULL,
    event_id      TEXT NOT NULL,
    occurred_at   TEXT NOT NULL,
    actor_user_id TEXT,
    summary       TEXT NOT NULL,
    diff_json     TEXT,
    FOREIGN KEY (event_id) REFERENCES audit_events(id)
);
CREATE INDEX IF NOT EXISTS idx_entity_activity_lookup
    ON entity_activity(tenant_id, entity_kind, entity_id, occurred_at DESC);
```

## Public types

```go
// internal/registry/registry.go (extension)

type Mutation struct {
    Op      MutOp           // Create | Update | Delete | Restart
    Server  ServerMutation  // populated for Create/Update; ID + Reason for others
    Reason  string
    ActorID string
}

type Registry interface {
    // Existing read methods …

    Apply(ctx context.Context, tenantID string, m Mutation) error
    Restart(ctx context.Context, tenantID, serverID string, reason string) error
    Logs(ctx context.Context, tenantID, serverID string, since time.Time) (<-chan LogLine, error)
}
```

```go
// internal/policy/editor.go

type RuleSet struct {
    Rules []Rule
}

type Rule struct {
    ID         string
    Priority   int
    Enabled    bool
    RiskClass  string // low|medium|high|sensitive
    Conditions Conditions
    Actions    Actions
    Notes      string
    UpdatedAt  time.Time
    UpdatedBy  string
}

type Conditions struct {
    Match struct {
        Tools     []string `json:"tools,omitempty"`
        Servers   []string `json:"servers,omitempty"`
        Tenants   []string `json:"tenants,omitempty"`
        ArgsExpr  string   `json:"args_expr,omitempty"` // CEL or jq-style expression
        TimeRange struct {
            From string `json:"from,omitempty"`
            To   string `json:"to,omitempty"`
        } `json:"time_range,omitempty"`
    }
}

type Actions struct {
    Allow             bool     `json:"allow,omitempty"`
    Deny              bool     `json:"deny,omitempty"`
    RequireApproval   bool     `json:"require_approval,omitempty"`
    LogLevel          string   `json:"log_level,omitempty"`   // override for this rule
    AnnotateRiskClass string   `json:"annotate,omitempty"`
}

func Validate(r Rule) error
func Canonicalise(rs RuleSet) ([]byte, error)
```

```go
// internal/policy/dryrun.go

type ToolCallShape struct {
    TenantID string
    Server   string
    Tool     string
    Args     map[string]any
    Now      time.Time
}

type DryRunResult struct {
    MatchedRules []RuleMatch
    LosingRules  []RuleMatch
    FinalAction  Actions
    FinalRisk    string
}

type RuleMatch struct {
    RuleID   string
    Priority int
    Reason   string // human-readable why it matched
}

func DryRun(ctx context.Context, rules RuleSet, call ToolCallShape) DryRunResult
```

```go
// internal/secrets/vault/reveal.go

type RevealToken struct {
    Token     string    // one-shot, expires in 60s
    ExpiresAt time.Time
}

func (v *Vault) IssueRevealToken(ctx context.Context, tenant, name, actorID string) (RevealToken, error)
func (v *Vault) ConsumeReveal(ctx context.Context, token string) (string /* plaintext */, error)
```

## REST API

All endpoints under `/api/...` require a JWT. `tenants:admin` for `/api/admin/tenants/*`. `secrets:write` for vault writes. `policy:write` for policy writes. `servers:write` for registry writes.

### Servers

```
GET    /api/servers                         → list (existing, extended with status fields)
POST   /api/servers                         → register
GET    /api/servers/{id}                    → fetch
PUT    /api/servers/{id}                    → replace
PATCH  /api/servers/{id}                    → partial update (env overrides, enabled)
DELETE /api/servers/{id}                    → deregister + terminate
POST   /api/servers/{id}/restart            → restart with reason
GET    /api/servers/{id}/logs?since=…       → SSE stream of the last N seconds
GET    /api/servers/{id}/health             → live health check
GET    /api/servers/{id}/activity?limit=…   → entity activity log
```

### Tenants (admin scope)

```
GET    /api/admin/tenants
POST   /api/admin/tenants
GET    /api/admin/tenants/{id}
PUT    /api/admin/tenants/{id}
DELETE /api/admin/tenants/{id}              → archives (status='archived'); destructive purge separate
POST   /api/admin/tenants/{id}/purge        → wipe all tenant data; admin + approval-required
GET    /api/admin/tenants/{id}/activity
```

### Secrets

```
GET    /api/admin/secrets                   → list metadata only (name, version, created_at, updated_at)
POST   /api/admin/secrets                   → create (tenant, name, value, AAD context, ttl)
GET    /api/admin/secrets/{name}            → metadata + version history
PUT    /api/admin/secrets/{name}            → update value (new version)
DELETE /api/admin/secrets/{name}            → delete (audit; version history retained 30d then purged)
POST   /api/admin/secrets/{name}/rotate     → re-encrypt with the current root key
POST   /api/admin/secrets/{name}/reveal     → returns RevealToken; client follows up with GET /reveal/{token}
GET    /api/admin/secrets/reveal/{token}    → one-shot plaintext
GET    /api/admin/secrets/{name}/activity   → entity activity log
POST   /api/admin/secrets/rotate-root       → admin + approval-required; re-encrypts every value
```

### Policy

```
GET    /api/policy/rules                    → ordered list
PUT    /api/policy/rules                    → replace whole ruleset (canonical)
POST   /api/policy/rules                    → append a rule
PUT    /api/policy/rules/{id}               → update one rule
DELETE /api/policy/rules/{id}               → remove
POST   /api/policy/dry-run                  → evaluate a synthetic tool call against the live ruleset
GET    /api/policy/activity                 → ruleset history
```

Error shape uniform with §"Errors on the wire". `permission_denied`, `validation_failed`, `conflict`, `approval_required`, `not_found` are the new typed slugs.

## Console screens

### `/servers`

`Table` with columns `id`, `name`, `transport`, `status`, `last_seen`, `actions`. Add button opens `/servers/new`. Row click → `/servers/{id}`. Status uses `Badge` (`running` green, `restarting` info, `stopped` warning, `errored` danger).

### `/servers/new`

`Form` (Phase 7 primitives). Top section: id, name, transport (stdio/http/sse). Conditional fields per transport (command + args for stdio, URL for http/sse). Env table editor. Credential reference picker (drop-down sourced from `/api/admin/secrets`). "Save & start" submits the form and waits for the supervisor to acknowledge before navigating to the detail view.

### `/servers/{id}`

Tabs:
- **Overview** — KeyValueGrid + status + last health-check.
- **Logs** — live tail via SSE; "Pause" / "Resume" / "Download last 5 minutes".
- **Tools** — read-only list of tools the server publishes (existing).
- **Activity** — audit-driven activity log, before/after diffs.
- **Configuration** — read-only dump; "Edit" jumps to `/servers/{id}/edit`.

Action buttons (right-aligned in PageHeader): Restart, Disable/Enable, Delete (destructive variant; opens approval flow).

### `/admin/tenants`

`Table` (admin-only; Console hides the route otherwise). Columns: id, name, runtime mode, status, sessions, requests/min. Detail page surfaces JWT issuer/JWKS URL with a copy button. Create wizard: 3 steps — identity (id, name), runtime (mode + quotas), auth (JWT issuer + JWKS URL).

### `/admin/secrets`

Extended from the Phase 5 minimal view. CRUD via Modal forms. Reveal is a separate two-click flow (operator → "Reveal" → re-confirm modal → one-shot copy). Rotate-root is in the page header behind an approval-gated button.

### `/policy`

Layout:

- Left: rule list, drag-handle to reorder priority (PATCH on drop).
- Centre: rule editor (structured form); raw YAML toggle in PageHeader.
- Right: dry-run sidebar — input fields for tool call shape, "Run" button, evaluation tree output.

Save is staged (no live writes on every keystroke). "Discard" / "Save" buttons in PageHeader. On save, a Toast confirms; the live policy engine picks up immediately.

### `/policy/dry-run`

Standalone page for the dry-run flow when an operator wants more screen real estate. Same input fields, same eval tree, plus a JSON view of the result.

## Implementation walkthrough

### Step 1 — Migration + repo extensions

Land migration `0009_console_crud.sql`. Extend the SQLite repos:

- `internal/storage/sqlite/repo_servers.go` — gains the runtime-overrides surface.
- `internal/storage/sqlite/repo_tenants.go` — write CRUD.
- `internal/storage/sqlite/repo_policy_rules.go` — new.
- `internal/storage/sqlite/repo_entity_activity.go` — new (writers + readers).

Round-trip tests cover every method.

### Step 2 — Policy SQL store + canonicaliser

Implement `internal/policy/store.go` reading/writing `tenant_policy_rules`. Canonicaliser at `internal/policy/editor.go::Canonicalise` reuses `internal/catalog/snapshots/canonical.go` for stable JSON.

Existing YAML loader stays as a one-shot import (`portico import --config portico.yaml`) but the engine reads from the SQL store at runtime. Migration command emits a per-tenant ruleset diff.

### Step 3 — Vault CRUD + rotate-root

`internal/secrets/vault/crud.go` exposes the methods the CLI already uses, returning structured types instead of stdout. `internal/secrets/vault/rotate.go` implements root-key rotation: generates a new key from `PORTICO_VAULT_KEY_NEXT`, re-encrypts each value, swaps the active key, archives the previous key for a configurable grace period. Reveal-token + audit-event surface lands.

### Step 4 — Registry + supervisor wiring

`internal/registry/registry.go::Apply` is the canonical entry point for mutations. Internally:

1. Validate the mutation against the existing schema (Phase 2 server config).
2. Persist to SQLite in a transaction.
3. Emit a structured event on the registry mutation channel.
4. The watcher fans the event out to the supervisor (which spawns / restarts / kills processes) and to the catalog updater.
5. Audit event written.

Restart with grace: the supervisor stops accepting new tool calls on the affected process(es), waits up to `grace_seconds` for in-flight calls to complete, then SIGTERM → SIGKILL on timeout. Restart reason recorded.

### Step 5 — REST handlers + middleware

Each handler reads tenant + actor from context, validates, calls into the corresponding internal package, emits audit, returns the canonical entity shape. `internal/server/api/middleware/permission.go` walks the JWT scopes; misconfigured scope produces `permission_denied`.

`approval_gate.go` middleware intercepts destructive verbs:

- DELETE `/servers/{id}` if any session is currently using it
- DELETE `/admin/tenants/{id}` (always)
- POST `/admin/tenants/{id}/purge`
- POST `/admin/secrets/rotate-root`
- DELETE `/admin/secrets/{name}` if a credential injector currently references it

It emits an `elicitation/create` request through the Phase 5 approval flow and waits up to `approval_timeout` (default 5 min). On approval the handler proceeds; on denial or timeout, returns `403 approval_required` with a typed error.

### Step 6 — Activity log writer

A small fanout in `internal/audit/audit.go` (Phase 5) writes denormalised rows into `entity_activity` for events that target an entity. The `/activity` endpoints read the projection. The original audit table remains canonical and unmodified.

### Step 7 — Console screens

One route per resource, following the Phase 8 pattern. Forms use the Phase 7 component library. The dry-run pane on `/policy` calls `POST /api/policy/dry-run` on every input change (debounced 300 ms). All writes show a `Toast` and an inline Activity row in the Drawer.

Permission gating: the layout reads the JWT scopes once on boot and passes them via a Svelte store to every page. Buttons that would call a write endpoint render disabled with a tooltip when the scope is missing.

### Step 8 — Hot-reload glue

- Registry: existing fsnotify watcher swapped for a SQL change-listener that fires on every `Apply`.
- Vault: cached encrypted entries invalidated by a per-tenant generation counter that increments on every CRUD write.
- Policy: engine subscribes to the policy store's `Watch()` channel and rebuilds its rule tree on every event.

All of these are the seams Phase 2 + Phase 5 already reserved; the work here is connecting them to the new mutation paths.

### Step 9 — Smoke + tests

`scripts/smoke/phase-9.sh` covers every endpoint. Integration tests (next section) cover hot-reload, isolation, and approval flow.

## Test plan

### Unit

- `internal/registry/apply_test.go`
  - `TestApply_Create_StartsSupervisor`.
  - `TestApply_Update_ScheduleRestart`.
  - `TestApply_Delete_DrainsSessions`.
  - `TestApply_RejectsInvalidConfig`.
- `internal/policy/editor_test.go`
  - `TestValidate_RuleShape`.
  - `TestCanonicalise_StableAcrossOSEncodings`.
  - `TestRuleSet_OrderedByPriority`.
- `internal/policy/dryrun_test.go`
  - `TestDryRun_HappyPath_Allow`.
  - `TestDryRun_DenyOverridesAllow`.
  - `TestDryRun_PriorityWins`.
  - `TestDryRun_TimeRangeMatch`.
- `internal/secrets/vault/rotate_test.go`
  - `TestRotateRoot_ReencryptsEveryValue`.
  - `TestRotateRoot_KeepsArchivedKeyDuringGrace`.
  - `TestRotateRoot_AbortsOnPartialFailure`.
- `internal/secrets/vault/reveal_test.go`
  - `TestReveal_TokenSingleUse`.
  - `TestReveal_TokenExpires`.
  - `TestReveal_AuditEventEmitted`.
- `internal/server/api/{servers,tenants,secrets,policy}_test.go`
  - Per endpoint: happy-path, permission denied, validation failure, audit event emitted.

### Integration (`test/integration/console_crud/`)

- `TestE2E_ServerCRUD_NoRestart` — boot Portico; POST `/api/servers`; tool call lands; PATCH env override; restart; tool call still lands with new env.
- `TestE2E_TenantCreate_AcceptsJWT` — create tenant with mock JWKS; mint a token; gateway accepts it for the new tenant.
- `TestE2E_SecretRotate_DoesNotBreakInjectors` — rotate-root with the credential injector under load; no failed tool calls; injectors transparently use the new key.
- `TestE2E_PolicyEdit_HotReload` — add a deny rule via REST; next tool call denied without restart.
- `TestE2E_DestructiveDelete_RequiresApproval` — DELETE a server with a live session; assert `403 approval_required`; approve via the approval flow; DELETE succeeds.
- `TestE2E_PermissionGate_ReadOnlyJWTRejected` — read-only JWT POSTs server; 403; audit event recorded.
- `TestE2E_TenantIsolation_AdminScope` — admin in tenantA cannot CRUD tenantB's servers without `tenants:admin` cross-tenant scope.
- `TestE2E_ActivityLog_Visible` — every CRUD shows up in the entity_activity for the right entity, redacted.

### Smoke

`scripts/smoke/phase-9.sh`:

- `assert_status` 200 for each list endpoint.
- POST `/api/servers` with a known-good payload; assert 201; GET to confirm.
- PATCH then DELETE.
- POST `/api/admin/tenants` with admin JWT; verify the tenant appears.
- POST `/api/admin/secrets` + reveal flow.
- PUT `/api/policy/rules` then dry-run.
- skip_if_404 for every endpoint not yet wired.

OK ≥ 16 by phase close, FAIL = 0.

### Coverage gates

- `internal/registry`: ≥ 80% (post-extension).
- `internal/policy`: ≥ 80%.
- `internal/secrets/vault` (crud + rotate + reveal): ≥ 80%.
- `internal/server/api`: ≥ 75%.

## Common pitfalls

- **Restart-thundering-herd.** A single edit that touches 50 servers (e.g. updating a shared env var via a tenant-level setting) should batch restarts rather than restart-50-at-once. The supervisor exposes a batch entry point; the registry uses it for bulk updates.
- **Reveal-token timing attacks.** Use constant-time comparison; tokens are 256-bit random and consumed atomically.
- **Concurrent policy edits.** Two operators editing different rules concurrently is fine (rule-level row lock). Two editing the same rule needs optimistic concurrency (`If-Match` with the rule's `updated_at`); a stale write returns 409.
- **YAML/JSON drift in policy editor.** Round-trip is asymmetric until canonicalised. Always store canonical, render YAML on the way out.
- **Approval flow blocking the API request.** Long-running approvals time out the HTTP request. The handler returns `202 Accepted` with an `approval_request_id`; the Console polls or subscribes via the existing approval channel.
- **Cross-tenant admin actions without an audit trail.** When `tenants:admin` is used to act across tenants, the audit row carries `acting_tenant_id` AND `target_tenant_id`. Don't conflate them.
- **Vault root-key rotation partial failure.** If 999 of 1000 entries re-encrypt and the 1000th fails, the operator is in a split state. The rotation runs as a transaction with a temporary "grace" mapping table and only swaps the active key when every entry succeeds. On failure, restore the previous active key and emit `vault.rotate_root.aborted`.
- **Server restart during in-flight tool call.** The supervisor's grace-window must respect already-streaming SSE responses; cutting them off mid-stream produces invalid JSON-RPC. Drain the response, then terminate.
- **Permission scope drift between API and UI.** A scope name typo on either side leads to confusing UX. Source of truth is `internal/auth/scope/scopes.go` (string constants exported); the UI imports those constants by code-gen.
- **Activity log unbounded growth.** `entity_activity` is a denormalised projection; the original audit table is the canonical record. The activity table is purged on a tenant retention schedule (default 30 days), independent of the audit retention.

## Out of scope

- **Bulk import/export of full configurations.** Operators can import once from `portico.yaml`; round-trip + version-controlled config-as-code is post-V1.
- **OPA / external policy decision points.** The policy engine remains in-process. PDPs are post-V1.
- **Quota enforcement.** The fields land in `tenants` but enforcement (rate limiter, concurrent-session limiter) is out of scope; visible-but-not-enforced.
- **Per-user (sub-tenant) scopes.** The JWT carries scopes today; per-user RBAC inside a tenant is a Phase 13+ topic.
- **Self-service tenant onboarding (signup forms).** Tenants are created by an admin operator. Public signup flows are post-V1.
- **Audit log GUI search.** The Console's audit page (Phase 6) is read-only; Phase 9 doesn't add advanced filters; that's part of Phase 11 (telemetry replay).

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-9.sh` shows OK ≥ 16, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. Permission tests cover every write endpoint with both authorized and unauthorized scopes.
5. Hot-reload tests confirm zero binary restarts in the integration suite.
6. PR description references RFC §"Operator surface" + this plan; lists which YAML fields became runtime-managed.
7. README at repo root mentions Console CRUD as a V1 feature (one paragraph).

## Hand-off to Phase 10

Phase 10 (Playground) inherits:

- The CRUD pattern + Form/Modal primitives.
- The audit + entity-activity surface for showing playground sessions.
- The hot-reload guarantees so an operator editing policy or registry mid-playground sees the result on the next call.

Phase 10's first task: build the Playground route at `/playground`, give the operator an interactive way to compose a tool call, see the streamed response, and inspect the audit + trace events that result. Reuses CodeBlock, the policy dry-run pane shape, and the SSE log-tail pattern landed here.

## Carry-overs to follow-up phases

Phase 9 shipped with five documented deviations. They are not lost — each has a designated home in a follow-up phase plan. **Update those plans, not this one, when scoping the carry-over work.**

- **Server log SSE live tail** — Phase 10. Requires a per-process ring buffer in `internal/runtime/process/`. The `/api/servers/{id}/logs` route shipped here as a stub; Phase 10 wires it to real output. See `docs/plans/phase-10-playground.md` §"Phase 9 carry-overs" item 1.
- **Approval-gate middleware for destructive Phase 9 verbs** — Phase 10. The `/v1/approvals` UI is already wired; Phase 10 adds `internal/server/api/middleware/approval_gate.go` with the 202+poll pattern. See `docs/plans/phase-10-playground.md` §"Phase 9 carry-overs" item 2.
- **Phase 9 integration tests** (`TestE2E_PolicyEdit_HotReload`, `TestE2E_DestructiveDelete_RequiresApproval`, `TestE2E_ServerCRUD_NoRestart`) — Phase 10. Land in `test/integration/console_crud/`. See `docs/plans/phase-10-playground.md` §"Phase 9 carry-overs" item 3.
- **`/api/admin/secrets/rotate-root` with grace mapping table** — Phase 11. Currently returns 501; the operator CLI continues to cover root rotation. Phase 11 lands the API + transactional re-encrypt with archived-key grace. See `docs/plans/phase-11-telemetry-replay.md` §"Phase 9 carry-overs" item 1.
- **`entity_activity` retention sweep** — Phase 11. Currently pruned only by FK cascade on entity deletion; Phase 11 adds the per-tenant retention worker aligned with audit-event retention. See `docs/plans/phase-11-telemetry-replay.md` §"Phase 9 carry-overs" item 2.
