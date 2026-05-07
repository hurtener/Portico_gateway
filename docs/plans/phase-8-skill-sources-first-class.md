# Phase 8 — Skill Sources First-Class

> Self-contained implementation plan. Builds on Phase 0–7. Promotes skill provenance from a single LocalDir to a multi-driver, hot-reloadable, in-Console-authorable surface.

## Goal

Make Skill Packs operable end-to-end without commit + rebuild + restart. Two families of sources have to become first-class:

1. **External sources** — Git repositories and HTTP endpoints surfaced through the existing `source.Source` interface (`internal/skills/source/source.go`). The drivers register at boot, the loader watches them, and the runtime picks up additions/updates/removals like it already does for `LocalDir`.
2. **In-Portico authored skills** — operators compose Skill Packs from the Console (manifest, `SKILL.md`, prompts, resource bundles, optional UI) without leaving the binary. The authored output lands in a **built-in store** (`internal/skills/source/authored/`) backed by the existing SQLite database, multi-tenant, hot-reload-driven, audit-logged.

After Phase 8, an operator can: clone a Skill Pack from a Git URL into a tenant; install one from a published HTTP feed; or compose a brand-new pack inside the Console — and the new pack is namespaced into the tenant's catalog without a redeploy. Phase 4's invariants (manifest schema, JSON Schema 2020-12 validation, virtual directory, per-session enablement) are unchanged; new sources slot in behind the seam Phase 4 reserved.

## Why this phase exists

The user feedback after Phase 6: "for every new thing (skills, MCP servers) I need to generate a commit, build and push. Somehow that defeats the great UX we are seeking." The MCP-server side is solved at the registry layer (already runtime-mutable). Skills are not — `LocalDir` is the only driver, and it watches a directory shipped from the build artifact. To meet the V1 promise of an operations console that does its job, skills must be installable, authorable, and editable at runtime.

A second motivator: the Skills spec is open and growing. Treating Git/HTTP as first-class lets an operator subscribe to a vendor's Skill Pack repo and pick up new packs the moment they're tagged. Treating in-Console authoring as first-class lets a security/ops engineer ship a hot-fix workflow (a one-off prompt + tool sequence) without going through engineering. Both exist in commercial gateways already; for Portico, this is the difference between a developer tool and an operator-facing product.

## Prerequisites

Phases 0–7 complete. Specifically:

- `internal/skills/source/source.go` defines `Source` with `Name`, `List`, `Open`, `ReadFile`, `Watch` (Phase 4).
- `internal/skills/loader/` resolves a manifest into the catalog and exposes per-tenant enablement (Phase 4).
- `internal/skills/manifest/` validates Skill Pack manifests against the JSON Schema 2020-12 schema (Phase 4).
- The factory pattern at `internal/skills/source/source.go` + driver self-registration (CLAUDE.md §4.4).
- Console component library (Phase 7) — `Modal`, `Form` primitives, `CodeBlock` with syntax highlighting, `Tabs`, `Toast`.
- Vault (Phase 5) — for the Git/HTTP credential storage.
- Audit store (Phase 5) — for `skill_source.added`, `skill.installed`, `skill.authored`, `skill.published`, `skill.removed` events.
- Catalog snapshot machinery (Phase 6) — every authored or installed skill participates in the next snapshot.

## Deliverables

1. **Git source driver** — `internal/skills/source/git/`. Clones, fetches with `git fetch --prune`, refresh interval (default 5 min) with manual-refresh API, supports `https://` (Bearer + Basic) and `ssh://` (deploy key from the vault). Submodules disabled by default.
2. **HTTP source driver** — `internal/skills/source/http/`. Pulls a JSON feed (the "manifest of manifests") + a content endpoint per pack. Authenticated via vault-stored bearer or API-key headers. Designed to be the surface a future hosted Portico registry serves.
3. **Authored source driver** — `internal/skills/source/authored/`. Reads/writes from a new pair of SQLite tables (`tenant_authored_skills`, `tenant_authored_skill_files`). Multi-tenant. Implements `Source.Watch` via an in-process notifier (no fsnotify needed; writes happen through Portico).
4. **Source registry & multi-source loader** — Phase 4's loader rebuilt to consume an ordered list of sources per tenant. Naming collisions across sources resolved by source priority + last-write-wins fingerprint, with an audit event on every collision.
5. **REST API** — `/api/skill-sources` (CRUD external sources), `/api/skills/authored` (CRUD authored skills), `/api/skills/validate` (dry-run a manifest + bundle without persisting), `/api/skills/refresh` (force a List on a named source).
6. **Console screens** — `/skills/sources` (list/add/remove sources), `/skills/sources/[name]` (browse what a source serves, enable/disable into the tenant catalog), `/skills/authored` (list authored packs), `/skills/authored/new` (multi-pane editor: manifest YAML/form, `SKILL.md` body, prompt files, optional `ui://` HTML), `/skills/authored/[id]` (edit + publish + version history).
7. **Validation pipeline** — single canonical pipeline reused by the loader, the REST validator, and the authoring UI. Errors carry pointers (JSON Pointer + line/col) so the UI surfaces them inline.
8. **Versioning + signature surface** — every authored/installed pack carries an immutable version tuple `(major, minor, patch[, qualifier])` and a content checksum (SHA-256 of the canonical manifest + bundle tree). Reserved fields in the schema for future signature support; checksum is enforced in V1.
9. **Hot reload across all sources** — adds, updates, removals propagate to live sessions per Phase 4's `list_changed` discipline. Cooperative session pinning: an in-flight skill keeps its bound version until the session ends; new sessions pick up the latest version.
10. **Audit + observability** — every source event and authoring event lands in the audit store with the operator's `tenant_id` + `user_id` + (where present) `request_id`. OTel spans wrap source pulls and validation runs.

## Acceptance criteria

1. Git driver clones and refreshes a public repo; private repo via vault-stored Bearer or deploy key works end-to-end; checksum mismatch flagged as a refusal-to-load with audit event.
2. HTTP driver pulls a JSON feed + per-pack content; bearer / api-key headers loaded from vault; HTTP 4xx mapped to typed errors that surface in the Console.
3. Authored driver: an operator creates a Skill Pack from the Console, validates it, publishes it, and the next session picks it up via the existing virtual directory. Manifest, `SKILL.md`, and at least one prompt are persisted in SQLite, scoped to `tenant_id`.
4. Authored driver supports edit-then-publish: editing creates a draft revision; publishing rolls the active version. Version history visible in the Console.
5. Naming collisions across sources are deterministic: `priority` field on each source row, lower number wins. Collision audit event records both contenders.
6. Hot reload: a freshly published authored skill becomes available to a new session within ≤ 2 s on a default refresh interval; a Git-source pack added to the upstream repo becomes available within ≤ 1 polling cycle (default 5 min, configurable down to 30 s).
7. Validation pipeline: invalid manifest in any source surfaces JSON-Pointer-tagged errors. The same error shape comes back from `POST /api/skills/validate` and from the loader's startup logs.
8. Cross-tenant isolation: authored skills never leak across tenants. Integration test asserts `tenantA` cannot read or list `tenantB`'s authored packs.
9. Goroutine leak: stopping a source via `DELETE /api/skill-sources/{name}` joins the watcher goroutine; `runtime.NumGoroutine` returns to baseline within the per-phase teardown helper.
10. Smoke: `scripts/smoke/phase-8.sh` exercises every new endpoint (sources CRUD, authored CRUD, validate, refresh) and the dry-run validator. SKIP for any unimplemented surface; all OK by phase close.
11. Coverage: ≥ 75% for `internal/skills/source/git`, `internal/skills/source/http`, `internal/skills/source/authored`, `internal/skills/loader` (rebuilt around the multi-source registry).
12. UI parity: every API surface exposed has a Console flow. `/skills/sources` and `/skills/authored` cover their CRUD without falling back to the CLI.

## Architecture

```
internal/skills/
├── manifest/                  # unchanged — schema + canonical model
├── source/
│   ├── source.go              # Source interface (Phase 4) + factory
│   ├── localdir/              # Phase 4 driver, unchanged
│   ├── git/
│   │   ├── git.go             # driver init, refresh loop
│   │   ├── client.go          # go-git wrapper; argv-form; credential pluggability
│   │   ├── creds.go           # vault-backed credential resolution (PAT / SSH key)
│   │   └── git_test.go
│   ├── http/
│   │   ├── http.go            # feed parser + per-pack fetcher
│   │   ├── feed.go            # feed schema + decoder
│   │   ├── creds.go           # vault-backed bearer / api-key
│   │   └── http_test.go
│   ├── authored/
│   │   ├── authored.go        # driver init; reads tenant_authored_skills
│   │   ├── store.go           # SQLite repo (CRUD + versioning + watch fanout)
│   │   ├── notifier.go        # in-process Watch source
│   │   ├── canonical.go       # canonical hashing for content checksum
│   │   └── authored_test.go
│   └── registry.go            # NEW: per-tenant source orchestrator
├── loader/
│   ├── loader.go              # multi-source aware; collision policy
│   ├── validate.go            # canonical validation pipeline (errors with JSONPointer)
│   └── loader_test.go
└── runtime/                   # unchanged — bound to (sourceName, ref) pairs

internal/server/api/
├── skill_sources.go           # /api/skill-sources CRUD + refresh
├── authored_skills.go         # /api/skills/authored CRUD + publish
└── skills_validate.go         # /api/skills/validate

internal/storage/sqlite/migrations/
└── 0008_skill_sources_and_authored.sql

web/console/src/routes/
├── skills/
│   ├── sources/+page.svelte
│   ├── sources/[name]/+page.svelte
│   ├── authored/+page.svelte
│   ├── authored/new/+page.svelte
│   └── authored/[id]/+page.svelte
└── skills/+page.svelte        # extended with source attribution column
```

The factory at `internal/skills/source/source.go` gains driver self-registration for `git`, `http`, and `authored`; `cmd/portico` blank-imports them. The loader stops embedding a single `LocalDir` and instead asks the per-tenant `registry.go` for an ordered list of `Source` instances.

## SQL DDL (migration 0008)

```sql
-- External skill sources, scoped per tenant.
CREATE TABLE IF NOT EXISTS tenant_skill_sources (
    tenant_id        TEXT NOT NULL,
    name             TEXT NOT NULL,            -- operator-chosen handle, unique per tenant
    driver           TEXT NOT NULL,            -- 'git' | 'http' | 'localdir' | 'authored'
    config_json      TEXT NOT NULL,            -- driver-specific config (URL, branch, feed URL, …)
    credential_ref   TEXT,                     -- vault key for creds, NULL for public sources
    refresh_seconds  INTEGER NOT NULL DEFAULT 300,
    priority         INTEGER NOT NULL DEFAULT 100,  -- lower wins on collision
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    last_refresh_at  TEXT,
    last_error       TEXT,
    PRIMARY KEY (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_tenant_skill_sources_driver ON tenant_skill_sources(tenant_id, driver);

-- Authored skills are stored as a (manifest, files) tuple per tenant.
CREATE TABLE IF NOT EXISTS tenant_authored_skills (
    tenant_id        TEXT NOT NULL,
    skill_id         TEXT NOT NULL,            -- pack id from the manifest
    version          TEXT NOT NULL,            -- "1.2.0" or "1.2.0-rc1"
    status           TEXT NOT NULL,            -- 'draft' | 'published' | 'archived'
    manifest_json    TEXT NOT NULL,            -- canonical manifest
    checksum         TEXT NOT NULL,            -- SHA-256 of canonical(manifest + files)
    author_user_id   TEXT,
    created_at       TEXT NOT NULL,
    published_at     TEXT,
    archived_at      TEXT,
    PRIMARY KEY (tenant_id, skill_id, version)
);

CREATE INDEX IF NOT EXISTS idx_tenant_authored_skills_status
    ON tenant_authored_skills(tenant_id, status, published_at DESC);

-- Files belonging to an authored skill (SKILL.md, prompts, resources, optional ui://).
CREATE TABLE IF NOT EXISTS tenant_authored_skill_files (
    tenant_id        TEXT NOT NULL,
    skill_id         TEXT NOT NULL,
    version          TEXT NOT NULL,
    relpath          TEXT NOT NULL,            -- 'SKILL.md', 'prompts/triage.md', 'apps/console.html'
    mime_type        TEXT NOT NULL,
    contents         BLOB NOT NULL,
    PRIMARY KEY (tenant_id, skill_id, version, relpath),
    FOREIGN KEY (tenant_id, skill_id, version)
        REFERENCES tenant_authored_skills(tenant_id, skill_id, version)
        ON DELETE CASCADE
);

-- Pointer to the active version per (tenant, skill_id). Updated on publish.
CREATE TABLE IF NOT EXISTS tenant_authored_active_skill (
    tenant_id        TEXT NOT NULL,
    skill_id         TEXT NOT NULL,
    active_version   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, skill_id),
    FOREIGN KEY (tenant_id, skill_id, active_version)
        REFERENCES tenant_authored_skills(tenant_id, skill_id, version)
);
```

Every query in the new repos filters by `tenant_id`. No global rows.

## Public types

```go
// internal/skills/source/registry.go

// Registry resolves the per-tenant ordered Source list. The loader
// asks the Registry instead of holding a single concrete Source.
type Registry interface {
    // Sources returns the active sources for tenantID, ordered by
    // priority ascending (lower wins on collision). Includes both
    // external (git/http/localdir) and authored drivers.
    Sources(ctx context.Context, tenantID string) ([]Source, error)

    // Refresh forces a List on every source for the tenant and
    // updates last_refresh_at / last_error. Used by the manual
    // refresh API and the post-publish path.
    Refresh(ctx context.Context, tenantID string) error

    // Subscribe returns a channel that fans in Watch events from
    // every active source for the tenant. Closes when ctx ends.
    Subscribe(ctx context.Context, tenantID string) (<-chan Event, error)
}
```

```go
// internal/skills/source/git/git.go

type Config struct {
    URL              string        // https://… or ssh://…
    Branch           string        // default: HEAD
    SubdirGlob       string        // optional, e.g. "packs/*" — restricts which pack roots count
    RefreshInterval  time.Duration // default 5m
    CredentialRef    string        // vault key, "" for public
}

func NewSource(cfg Config, vault secrets.Vault, log *slog.Logger) (source.Source, error)
```

```go
// internal/skills/source/http/http.go

type Config struct {
    FeedURL         string        // GET returns FeedDocument
    RefreshInterval time.Duration
    CredentialRef   string        // vault key for Authorization
    HeaderName      string        // default "Authorization"
    HeaderPrefix    string        // default "Bearer "
}

// FeedDocument is the JSON body the feed endpoint returns.
type FeedDocument struct {
    Schema   string         `json:"schema"`   // "skill-feed/v1"
    Updated  time.Time      `json:"updated"`
    Packs    []FeedPackEntry `json:"packs"`
}

type FeedPackEntry struct {
    ID       string   `json:"id"`
    Version  string   `json:"version"`
    Checksum string   `json:"checksum"` // sha256:<hex>
    BundleURL string  `json:"bundle_url"` // tar+gz of the pack tree
}

func NewSource(cfg Config, vault secrets.Vault, log *slog.Logger) (source.Source, error)
```

```go
// internal/skills/source/authored/authored.go

// Source is a SQLite-backed Source. Watch events fire when a publish
// commits in the same process; cross-process notifications would need
// a polling loop on top — out of scope for V1.
type Source struct { /* … */ }

// Repo is the SQLite-backed CRUD surface used by the REST handlers.
type Repo interface {
    ListAuthored(ctx context.Context, tenantID string) ([]Authored, error)
    GetAuthored(ctx context.Context, tenantID, skillID, version string) (*Authored, error)
    CreateDraft(ctx context.Context, tenantID, userID string, m manifest.Manifest, files []File) (*Authored, error)
    UpdateDraft(ctx context.Context, tenantID, skillID, version string, m manifest.Manifest, files []File) (*Authored, error)
    Publish(ctx context.Context, tenantID, skillID, version string) (*Authored, error)
    Archive(ctx context.Context, tenantID, skillID, version string) error
    History(ctx context.Context, tenantID, skillID string) ([]Authored, error)
}

type Authored struct {
    SkillID      string
    Version      string
    Status       string // "draft" | "published" | "archived"
    Manifest     manifest.Manifest
    Files        []File
    Checksum     string
    AuthorUserID string
    CreatedAt    time.Time
    PublishedAt  *time.Time
}

type File struct {
    RelPath  string
    MIMEType string
    Body     []byte
}
```

## REST API

All endpoints require a JWT with `skills:write` (create/update/delete) or `skills:read` (list/get/validate). Tenant-scoped via the JWT claim.

```
GET    /api/skill-sources                 → list sources for tenant
POST   /api/skill-sources                 → add a source (driver, config, credential_ref)
GET    /api/skill-sources/{name}          → fetch a source (incl. last_refresh_at, last_error)
PUT    /api/skill-sources/{name}          → update config/credential_ref/priority/enabled
DELETE /api/skill-sources/{name}          → remove (joins watcher; cascades nothing)
POST   /api/skill-sources/{name}/refresh  → force List + audit event
GET    /api/skill-sources/{name}/packs    → list what this source currently serves

GET    /api/skills/authored               → list authored packs (active version + status)
POST   /api/skills/authored               → create draft from manifest + files
GET    /api/skills/authored/{id}          → get active version
GET    /api/skills/authored/{id}/versions → version history (drafts + published)
GET    /api/skills/authored/{id}/versions/{v} → fetch a specific version (manifest + files)
PUT    /api/skills/authored/{id}/versions/{v} → update a draft (rejected if published)
POST   /api/skills/authored/{id}/versions/{v}/publish → publish a draft
POST   /api/skills/authored/{id}/versions/{v}/archive → archive an old version
DELETE /api/skills/authored/{id}/versions/{v} → delete a draft (rejects if published)

POST   /api/skills/validate               → dry-run validator; body = manifest + files;
                                            response = canonical errors (JSON Pointer + line/col)
                                            and a content checksum the UI can show.
```

Error shape uniform with `docs/plans/README.md` §"Errors on the wire":

```json
{ "error":"manifest_invalid",
  "message":"manifest failed schema validation",
  "details":{"violations":[{"pointer":"/policy/risk_class","line":12,"col":14,"reason":"required"}]} }
```

## MCP surface (no new methods)

Phase 4's `list_changed` notification carries the new authored/git/http packs once they hit the loader. No MCP method introduced. The catalog snapshot machinery (Phase 6) records each pack with its `(source_name, ref)` provenance; existing `tools/list` and `prompts/list` responses gain a `_meta.source` field for traceability (optional, additive).

## Implementation walkthrough

### Step 1 — Migration + repos

Land migration `0008_skill_sources_and_authored.sql`. Implement `internal/storage/sqlite/repo_skill_sources.go` and `repo_authored_skills.go` against the existing `Backend` interface. Round-trip tests cover insert/list/update/delete + the active-version pointer.

### Step 2 — Source registry (`registry.go`)

`registry.go` reads `tenant_skill_sources` rows, materialises a `Source` via the per-driver factory, and orders by priority. Caches per-tenant Source instances; invalidates on CRUD writes. `Subscribe` fans in `Watch` channels with a single bounded buffer (drop-oldest on backpressure, audit event on first drop).

### Step 3 — Authored driver

Implement the SQLite-backed `Source` and the `Repo` CRUD. Canonical hashing reuses Phase 6's `internal/catalog/snapshots/canonical.go` (re-export the canonicalEncode helper for the file/manifest tree). Publish flips the `tenant_authored_active_skill` row in a single transaction so concurrent List calls never see a torn state.

### Step 4 — Git driver

Use `github.com/go-git/go-git/v5` (CGo-free, MIT). Clone into `${PORTICO_DATA_DIR}/sources/git/<tenant>/<sha256(url+branch)>`. `Watch` is a goroutine that fetches every `RefreshInterval`, diffs the `tools/list` of the prior pull, and emits Events. Credentials resolved via the vault: PAT (Bearer-style HTTP), or SSH deploy key blob. Argv-form for any subprocess (per CLAUDE.md §13). Submodules disabled. Shallow clone (`--depth=1`) by default.

### Step 5 — HTTP driver

Pure stdlib `net/http` client with retry (3× exponential 200ms-1.6s, only on 5xx + connection errors). Feed pull every `RefreshInterval`; per-pack content fetched lazily on `Open`/`ReadFile` and cached in `${PORTICO_DATA_DIR}/sources/http/<tenant>/<sha256(feed)>/`. Cache invalidation by checksum mismatch — when the feed says a pack moved to a new checksum, prune the cached tree and refetch.

### Step 6 — Loader rebuild

`internal/skills/loader/loader.go` no longer holds a single Source. It calls `Registry.Sources(ctx, tenantID)`, iterates in priority order, and for each Ref builds the catalog entries. On collision (same `skill.id` from two sources), the lower-priority source wins; an audit event records the runner-up. The validator pipeline runs once per Ref and surfaces JSON-Pointer errors that the UI consumes verbatim.

### Step 7 — REST endpoints + auth

Each handler reads the tenant from `tenant.MustFrom(ctx)`, scopes every query, and emits an audit event for every write (`skill_source.added`, `skill_source.removed`, `skill.authored.draft_created`, `skill.authored.published`, `skill.authored.archived`). Validate-only endpoint reuses the loader's pipeline against an in-memory virtual Source so authoring drafts never hit the disk before publishing.

### Step 8 — Console screens

Source list & detail pages at `/skills/sources` use the Phase 7 `Table` + `Modal` for the add-source form. The driver picker reveals driver-specific fields (Git URL/branch, HTTP feed URL). Credential reference dropdown lists the operator's Phase 5 vault entries for the tenant. The detail view shows `last_refresh_at`, `last_error`, and a "Refresh now" button.

Authoring page at `/skills/authored/new` uses a three-pane layout:

- Left: form for top-level manifest fields (id, version, summary, policy, capabilities) — drives a YAML preview.
- Centre: tabbed editor for `SKILL.md`, prompts, optional `apps/*.html`. Save buttons stage to draft.
- Right: live validation panel — pings `POST /api/skills/validate` on every save (debounced 500ms). Errors render with JSON-Pointer breadcrumbs that scroll the relevant editor pane.

Publish flow: a confirmation `Modal` shows the diff against the active version, the new checksum, and a publish button that POSTs `/publish`. A `Toast` confirms success and the route navigates back to the version list.

### Step 9 — Hot reload wiring

The Phase 4 `list_changed` mux subscribes to the per-tenant fanned-in `Subscribe` channel. On Event, sessions running on that tenant either (a) emit `notifications/tools/list_changed` (and friends) on their northbound transport or (b) pin the in-flight version if the session has a running tool call (cooperative pinning). Existing snapshot machinery picks up the new pack on the next snapshot.

### Step 10 — Smoke + audit

`scripts/smoke/phase-8.sh`:

- POST `/api/skill-sources` with a Git driver pointing at `examples/skills/external-git-fixture` (a small fixture repo); poll until `last_refresh_at` updates; GET `/packs` returns the expected slugs.
- POST `/api/skills/authored` with a minimal manifest + SKILL.md; expect status `draft`. POST `/publish`; expect status `published`. GET `/api/skills` (Phase 4 endpoint) lists the authored pack.
- POST `/api/skills/validate` with an intentionally-broken manifest; expect `manifest_invalid` + at least one `pointer`-tagged violation.
- DELETE the source; goroutine count returns to baseline.

### Step 11 — Snapshot integration

`internal/catalog/snapshots/builder.go` (Phase 6) gains a `source_name` + `version` + `checksum` per skill entry so the snapshot fingerprint diffs cleanly across publishes. The diff page (Phase 6 UI) gets a "Skill provenance changed" row when only `source_name` differs.

## Test plan

### Unit

- `internal/skills/source/git/git_test.go`
  - `TestGit_Clone_PublicRepo` — fixture repo on `httptest`-backed git server.
  - `TestGit_Refresh_DiffEmitsEvents` — second commit; Watch yields `EventUpdated`.
  - `TestGit_PrivateRepo_PATFromVault` — vault returns a token; Authorization header asserted.
  - `TestGit_RejectsTraversal` — pack with `../../../etc/passwd` refused.
  - `TestGit_ChecksumMismatch_RefusesLoad`.
- `internal/skills/source/http/http_test.go`
  - `TestHTTP_FeedDecode_Valid`.
  - `TestHTTP_FeedDecode_BadSchema_TypedError`.
  - `TestHTTP_BundleFetch_CachesByChecksum`.
  - `TestHTTP_BundleFetch_5xxRetries`.
- `internal/skills/source/authored/authored_test.go`
  - `TestAuthored_DraftRoundTrip`.
  - `TestAuthored_Publish_FlipsActivePointer`.
  - `TestAuthored_Watch_FiresOnPublish`.
  - `TestAuthored_TenantIsolation`.
  - `TestAuthored_Canonical_HashStableAcrossOSEncodings`.
- `internal/skills/source/registry_test.go`
  - `TestRegistry_OrdersByPriority`.
  - `TestRegistry_CollisionAuditEvent`.
  - `TestRegistry_Subscribe_DropsOldestOnBackpressure`.
- `internal/skills/loader/loader_test.go`
  - `TestLoader_MultiSource_HappyPath`.
  - `TestLoader_ValidationErrors_HaveJSONPointers`.

### Integration (`test/integration/skill_sources/`)

- `TestE2E_AuthoredPublish_VisibleToSession` — boot Portico, create authored draft via REST, publish, open a session, assert the pack appears in `tools/list`.
- `TestE2E_GitSource_RefreshThenSession` — boot with a Git source pointing at a temp repo on disk; commit a new pack; force refresh; new session sees it.
- `TestE2E_HTTPSource_BadFeed_SurfacesError` — feed returns malformed JSON; `last_error` populated; Console fetch returns the error; sessions unaffected.
- `TestE2E_TenantIsolation_Authored` — `tenantA` publishes; `tenantB`'s `/api/skills/authored` is empty.
- `TestE2E_GoroutineLeak_AfterDelete` — add and delete sources in a loop; `runtime.NumGoroutine` returns to baseline.

### Smoke

`scripts/smoke/phase-8.sh` covers the REST surface end-to-end. SKIP for endpoints that aren't wired yet; OK ≥ 8 by phase close.

### Coverage gates

- `internal/skills/source/git`: ≥ 75%
- `internal/skills/source/http`: ≥ 75%
- `internal/skills/source/authored`: ≥ 80%
- `internal/skills/source/registry.go`: ≥ 80%
- `internal/skills/loader`: ≥ 75%

## Common pitfalls

- **Git authentication mid-flight.** A token rotated in the vault won't be picked up unless the Git driver re-resolves credentials on every fetch. Cache the resolved auth only for the duration of a single `git fetch` call; never across refresh ticks.
- **HTTP feed cache poisoning.** A feed that lies about checksums must not bind a future fetch. Always recompute the checksum on download and refuse to serve the pack to the loader if it disagrees with the feed entry.
- **Authored draft leaks into snapshots.** Drafts must never be visible to sessions or snapshots; only `status='published'` rows participate. Tested by `TestE2E_AuthoredPublish_VisibleToSession` (the inverse — a draft alone is invisible).
- **Concurrent publish races.** Two operators publishing the same `skill_id` simultaneously must serialize through a row-level lock (SQLite `BEGIN IMMEDIATE`). Otherwise the active-version pointer can land on a torn state.
- **Watch fanout backpressure.** A noisy upstream (e.g. a Git repo with frequent commits) can flood the loader. Bounded channel + drop-oldest + an audit event on first drop in a window — same pattern as Phase 5's audit batcher.
- **Per-tenant source caches sticky after CRUD.** When a source is updated or deleted, the registry's per-tenant cache must invalidate immediately. Invalidate inside the same transaction that writes the row.
- **JSON-Pointer line/col tracking through YAML.** Operators write manifests in YAML, the schema is JSON. The validator must run on the parsed JSON tree but trace pointers back to YAML line/col so the UI can highlight inline. Use `gopkg.in/yaml.v3` `Node.Line` + `Node.Column` and propagate through to the violation struct.
- **Submodules + recursive clones.** Disabled by default; an op who needs them must opt in via a config flag. A misconfigured submodule URL is a credential disclosure path.
- **Local clone disk growth.** Source caches are unbounded by default; cap with a `max_disk_mb` per source (default 256 MB) and prune oldest on overflow.

## Out of scope

- **OCI registry source.** Reserved for a follow-up; the seam is in place (driver self-registration), the V1 surface is Git + HTTP + authored.
- **Cross-tenant skill sharing.** No "publish to all tenants" path. An operator who wants to share clones the manifest into each target tenant explicitly.
- **Manifest signing / verifying signatures.** The schema reserves a `signature` field; V1 enforces the SHA-256 checksum but does not verify cryptographic signatures.
- **Authored skills with binary assets > 1 MB.** Large blobs (videos, screenshots) live in external object storage; the manifest references URLs. SQLite is not the asset CDN.
- **Federated registry** (multiple Portico instances cooperating on a shared feed). Post-V1; today's HTTP source talks to a static or single-vendor feed only.
- **Visual manifest builder** (drag-drop tools into a sequence). Phase 8 ships a structured form + raw editor; visual sequencing is post-V1.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-8.sh` shows OK ≥ 8 and FAIL = 0; prior smoke scripts unaffected.
3. Coverage gates met.
4. CI green: vet, lint, race tests, frontend `npm run check && npm run lint && npm run build`.
5. PR description references RFC §"Skill Pack runtime" and links to this plan.
6. README at repo root mentions skill sources and authoring (one paragraph).
7. Audit dashboard in the Console (Phase 6) shows the new event types.

## Hand-off to Phase 9

Phase 9 (Console CRUD: servers / tenants / secrets / policy editor) inherits:

- The CRUD pattern landed here (REST + audit + hot-reload + Console form).
- The Phase 7 component library + the new `Modal`-driven form composition used by `/skills/authored`.
- The Phase 5 vault as the storage seam for credentials referenced from Console-managed entities.

Phase 9's first task: rebuild the existing read-only `/servers` page into a CRUD surface, then extend to `/tenants`, `/admin/secrets`, and `/policy`. The same hot-reload story (registry watcher, vault watcher, policy file watcher) lands so changes from the Console take effect without a binary restart.
