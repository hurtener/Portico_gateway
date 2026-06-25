# Skill sources

A Skill Pack reaches Portico's runtime through a **skill source** — a named, tenant-scoped binding between a driver and the location of one or more packs. Sources are first-class: they are stored in the database, managed through the REST API and the Console, and live-reloaded without a binary restart. Four drivers ship in V1: `local`, `git`, `http`, and `authored`.

This page explains how the driver registry works, what each driver does, how authored packs are versioned and published, how hot reload is wired, and how the validation pipeline surfaces errors.

For an introduction to what Skill Packs contain, see [Skill Packs](/concepts/skill-packs). For how the Console exposes source management, see [Console](/concepts/console).

---

## The driver registry

Every driver self-registers at binary startup through a single call to `source.Register` from its package `init()` function. The registry maps a driver name to a factory function; the factory accepts a JSON config blob and a set of runtime dependencies (vault, tenant ID, data directory, logger) and returns a `source.Source` instance.

The four driver names accepted in the `driver` column of `tenant_skill_sources` are:

| Driver name | Alias | Description |
|---|---|---|
| `local` | `localdir` | Filesystem directory tree, watched with fsnotify |
| `git` | — | Remote Git repository (HTTPS or SSH) |
| `http` | — | JSON feed + per-pack tar+gz bundles |
| `authored` | — | In-Portico authored packs stored in SQLite |

The `authored` driver is synthesised automatically for every tenant even when no `tenant_skill_sources` row exists. Operators do not register it manually; the runtime always presents it.

The factory error message lists registered drivers, so a misconfigured `driver` value produces an actionable error rather than a silent failure.

### Source interface

All drivers implement the same interface, defined in `internal/skills/source/source.go`:

```go
type Source interface {
    Name()     string
    List(ctx context.Context) ([]Ref, error)
    Open(ctx context.Context, ref Ref) (manifest.Manifest, error)
    ReadFile(ctx context.Context, ref Ref, relpath string) (io.ReadCloser, ContentInfo, error)
    Watch(ctx context.Context) (<-chan Event, error)
}
```

`List` enumerates available packs without loading them. `Open` parses the manifest for a specific `Ref` but does not run validation — validation is a separate pipeline step. `ReadFile` returns individual file bytes; implementations reject absolute paths and path traversal attempts. `Watch` returns a channel of change events; drivers that do not support watching return `nil` and the loader falls back to periodic polling at the configured `refresh_interval`.

### The per-tenant Registry

The per-tenant **Registry** (`internal/skills/source/registry.go`) is the component the loader and REST handlers talk to rather than individual driver instances. At request time the Registry:

1. Reads `tenant_skill_sources` rows for the tenant, ordered by `priority` ascending (lower integer wins) with name as the tie-breaker.
2. Always prepends the synthesised `authored` source ahead of all external sources.
3. Materialises a `Source` per enabled row by calling `source.Build(ctx, row.Driver, row.ConfigJSON, deps)`.
4. Caches the materialised bundle in memory; the cache is invalidated immediately on any CRUD write so the next request rebuilds cleanly.

When two sources expose a pack with the same `skill.id`, the source that appears earlier in the priority-ordered list wins. An audit event records both contenders whenever a collision is detected.

---

## Storage: `tenant_skill_sources`

External sources (all drivers except `authored`) are persisted in SQLite as rows in `tenant_skill_sources`:

```sql
CREATE TABLE tenant_skill_sources (
    tenant_id        TEXT NOT NULL,
    name             TEXT NOT NULL,   -- operator-chosen handle, unique per tenant
    driver           TEXT NOT NULL,   -- 'git' | 'http' | 'localdir'
    config_json      TEXT NOT NULL,   -- driver-specific JSON config
    credential_ref   TEXT,            -- vault key; NULL for public sources
    refresh_seconds  INTEGER NOT NULL DEFAULT 300,
    priority         INTEGER NOT NULL DEFAULT 100,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    last_refresh_at  TEXT,
    last_error       TEXT,
    PRIMARY KEY (tenant_id, name)
);
```

Every row is scoped to a `tenant_id`. There are no global source rows. A disabled source (`enabled = 0`) is skipped during materialisation but its configuration is preserved.

`last_refresh_at` and `last_error` are written after every `List` call, whether triggered by the background poll or by a manual `POST /api/skill-sources/{name}/refresh`. The Console displays these fields to give operators visibility into source health without digging through logs.

---

## LocalDir driver

The `local` driver (alias `localdir`) watches a directory tree on the local filesystem. Pack discovery assumes a two-level namespace layout:

```
{root}/
  {namespace}/
    {pack-name}/
      manifest.yaml
      SKILL.md
      prompts/
      resources/
```

The pack ID defaults to `{namespace}.{pack-name}`; when the manifest declares its own `id` field, that value takes precedence.

### fsnotify with 200 ms debounce

The `local` driver calls `fsnotify.NewWatcher` and adds every directory under the root recursively. Because editors and CI pipelines often write multiple files in rapid succession, the driver coalesces filesystem events with a 200 ms debounce timer per pack root. Only a single `EventUpdated` is emitted per pack per burst, regardless of how many underlying `CREATE`/`WRITE`/`REMOVE` operations occurred. New subdirectories created while watching are registered automatically so newly added namespaces are discovered without a restart.

### Security: path traversal

`ReadFile` calls `filepath.Clean` on the requested path and verifies the result starts with the pack root before opening the file. This guards against manifests or remote callers supplying paths like `../../etc/shadow`. The same pattern is implemented across all drivers.

---

## Git driver

The `git` driver (`internal/skills/source/git/git.go`) uses a pure-Go Git implementation (CGo-free) to maintain a shallow clone of a remote repository inside the operator-configured data directory. The clone path is derived from a SHA-256 hash of the URL and branch, namespaced under the tenant ID, so multiple tenants can independently clone the same repository without sharing local state.

### Configuration

```json
{
  "url": "https://github.com/example/skill-packs",
  "branch": "main",
  "subdir_glob": "packs/*",
  "refresh_interval": "5m",
  "credential_ref": "github-pat",
  "allow_submodules": false,
  "basic_username": "x-access-token",
  "ssh_user": "git"
}
```

| Field | Required | Default | Description |
|---|---|---|---|
| `url` | yes | — | HTTPS or SSH remote URL |
| `branch` | no | repository HEAD | Branch to track |
| `subdir_glob` | no | (all dirs) | `filepath.Match` pattern to restrict which subdirectories are scanned for packs |
| `refresh_interval` | no | `5m` | Go duration string; minimum `30s` |
| `credential_ref` | no | — | Vault key name for the credential |
| `allow_submodules` | no | `false` | Opt-in to recursive submodule cloning |
| `basic_username` | no | `x-access-token` | HTTPS Basic-auth username |
| `ssh_user` | no | `git` | SSH username when the credential is a PEM key |

Credentials are resolved from the vault on every fetch, not cached between refresh ticks, so a rotated token takes effect at the next poll. If the resolved credential value begins with `-----BEGIN`, it is treated as a PEM-encoded SSH private key; otherwise it is used as an HTTPS Basic-auth password (PAT convention). Passphrase-protected SSH keys produce a typed error instructing the operator to decrypt the key before storing it in the vault.

Submodules are disabled by default because a maliciously crafted `.gitmodules` can point at attacker-controlled servers. Enable them only when the entire repository tree is operator-controlled.

### Watch and refresh

The driver's `Watch` goroutine fetches the configured branch every `refresh_interval`, computes a diff of pack IDs and versions against the prior snapshot, and emits `EventAdded`, `EventUpdated`, or `EventRemoved` events accordingly. The `Stop()` method joins this goroutine cleanly; calling `DELETE /api/skill-sources/{name}` triggers `Stop` before removing the row so no goroutine leak occurs after deletion.

---

## HTTP driver

The `http` driver (`internal/skills/source/http/http.go`) pulls a structured JSON feed that describes available packs, then fetches per-pack content as a tar+gz bundle on demand.

### Feed document shape

The feed endpoint must return JSON in this shape:

```json
{
  "schema": "skill-feed/v1",
  "updated": "2026-06-01T00:00:00Z",
  "packs": [
    {
      "id": "acme.incident-triage",
      "version": "1.2.0",
      "checksum": "sha256:a3f2...",
      "bundle_url": "https://cdn.example.com/packs/acme.incident-triage-1.2.0.tar.gz"
    }
  ]
}
```

The driver fetches the feed on `List`, then lazily fetches bundles on `Open`/`ReadFile`. Bundles are cached on disk keyed by their checksum, so a pack that has not changed between feed polls is served from disk without a network round-trip. When the feed advertises a new checksum for an existing pack ID, the stale cached bundle is discarded and the new bundle is downloaded and verified before serving.

### Configuration

```json
{
  "feed_url": "https://registry.example.com/feeds/tenant-a.json",
  "refresh_interval": "10m",
  "credential_ref": "registry-token",
  "header_name": "Authorization",
  "header_prefix": "Bearer "
}
```

| Field | Required | Default | Description |
|---|---|---|---|
| `feed_url` | yes | — | URL of the feed document |
| `refresh_interval` | no | `5m` | Minimum `30s` |
| `credential_ref` | no | — | Vault key for the authentication credential |
| `header_name` | no | `Authorization` | HTTP header to carry the credential |
| `header_prefix` | no | `Bearer ` | Prefix prepended to the credential value |

The client retries 5xx responses and connection errors up to three times with exponential back-off starting at 200 ms. 4xx responses are not retried and map to a typed error that populates `last_error` and surfaces in the Console.

---

## Authored driver: in-Portico pack authoring

The `authored` driver (`internal/skills/source/authored/authored.go`) stores Skill Packs directly in Portico's SQLite database. Operators compose packs through the Console without touching the filesystem, a repository, or a separate registry. Because writes happen in-process, the driver does not need fsnotify; it uses an in-process pub/sub notifier instead.

### SQLite schema

Three tables back authored packs:

```sql
-- One row per (tenant, skill_id, version).
CREATE TABLE tenant_authored_skills (
    tenant_id       TEXT NOT NULL,
    skill_id        TEXT NOT NULL,
    version         TEXT NOT NULL,
    status          TEXT NOT NULL,  -- 'draft' | 'published' | 'archived'
    manifest_json   TEXT NOT NULL,
    checksum        TEXT NOT NULL,
    author_user_id  TEXT,
    created_at      TEXT NOT NULL,
    published_at    TEXT,
    archived_at     TEXT,
    PRIMARY KEY (tenant_id, skill_id, version)
);

-- Associated files: SKILL.md, prompts/*, resources/*, optional UI assets.
CREATE TABLE tenant_authored_skill_files (
    tenant_id  TEXT NOT NULL,
    skill_id   TEXT NOT NULL,
    version    TEXT NOT NULL,
    relpath    TEXT NOT NULL,  -- e.g. 'SKILL.md', 'prompts/triage.md'
    mime_type  TEXT NOT NULL,
    contents   BLOB NOT NULL,
    PRIMARY KEY (tenant_id, skill_id, version, relpath)
);

-- Active-version pointer updated atomically on publish.
CREATE TABLE tenant_authored_active_skill (
    tenant_id      TEXT NOT NULL,
    skill_id       TEXT NOT NULL,
    active_version TEXT NOT NULL,
    PRIMARY KEY (tenant_id, skill_id)
);
```

All tables carry `tenant_id NOT NULL`. Queries always include `WHERE tenant_id = ?`; no authored skill is ever visible to a different tenant.

### Draft / publish / archive lifecycle

An authored Skill Pack moves through three statuses:

1. **`draft`** — created by `POST /api/skills/authored` or by editing an existing version. A draft is never visible to sessions or included in catalog snapshots. Multiple drafts for the same `skill_id` can coexist at different version strings.
2. **`published`** — promoted from draft by `POST /api/skills/authored/{id}/versions/{v}/publish`. On publish, the `tenant_authored_active_skill` pointer is updated atomically and a `source.EventAdded` is emitted to all subscribers. The next new session picks up the pack within the hot-reload window (typically under 2 seconds on the default configuration).
3. **`archived`** — set by `POST /api/skills/authored/{id}/versions/{v}/archive`. Archived versions remain in the database for history and audit purposes but are excluded from `List`. An `EventRemoved` is emitted so open sessions can react cooperatively.

Only one version can be active (published) per `skill_id` at any time; publishing a new version atomically replaces the pointer.

### Content checksum

The `checksum` column stores a SHA-256 digest over the canonical manifest JSON plus all associated files, sorted by `relpath`:

```
sha256:<hex-of-SHA256(
  "manifest:" + canonical_manifest_json + "\n" +
  "file:" + relpath + ":" + contents + "\n" +   // repeated per file, sorted by relpath
)>
```

The canonical manifest is produced by JSON-marshalling the manifest struct, then re-encoding with sorted keys and null fields dropped — the same encoder used by the catalog snapshot machinery. This means a manifest edited to produce equivalent semantics but different whitespace will produce the same checksum. The checksum is shown in the Console publish confirmation dialog before the operator commits.

---

## Hot reload

All four drivers participate in the same hot-reload loop:

1. A source emits a `source.Event` (kind `added`, `updated`, or `removed`) on its `Watch` channel.
2. The Registry fans events from all active sources into a single per-tenant channel (`Subscribe`). The fan-in uses a bounded buffer (capacity 32) with drop-oldest semantics; an audit event is emitted on the first drop in any window so operators can detect a backlogged source.
3. The loader consumes the fanned-in channel and re-evaluates affected packs. Valid packs are merged into the catalog; removed packs are withdrawn.
4. MCP northbound sessions receive a `notifications/tools/list_changed` notification (and `notifications/prompts/list_changed` where applicable) so connected agents can refresh their view.

Sessions currently executing a tool call against a skill cooperatively pin the in-flight pack version until the call completes. New sessions after a publish always receive the latest version.

For the `local` driver, the hot-reload round-trip from a filesystem write to a session notification is bounded by the 200 ms debounce plus the catalog update latency. For `git` and `http` drivers the first notification arrives no sooner than one `refresh_interval` tick, with a default of five minutes and a minimum of 30 seconds. For `authored`, notification is synchronous with the publish transaction — the loader is notified within the same process before the HTTP response to the operator is returned.

---

## Validation pipeline

A single validation pipeline runs in three contexts:

- The loader's load path, on every `List` result.
- `POST /api/skills/validate` — a dry-run endpoint that validates a manifest + file bundle without persisting anything.
- The Console authoring editor, which debounces calls to the validate endpoint 500 ms after each edit.

The pipeline produces a `ValidationResult`:

```go
type Violation struct {
    Pointer string `json:"pointer"` // JSON Pointer, e.g. "/policy/risk_class"
    Line    int    `json:"line,omitempty"`
    Col     int    `json:"col,omitempty"`
    Reason  string `json:"reason"`
    Kind    string `json:"kind,omitempty"` // "schema" | "semantic"
}

type ValidationResult struct {
    Violations []Violation        `json:"violations"`
    Warnings   []string           `json:"warnings,omitempty"`
    Manifest   *manifest.Manifest
}
```

When the manifest is written in YAML (the most common case), the pipeline walks the YAML node tree to extract the `Line` and `Column` for each violation so the Console editor can highlight the offending line inline. The JSON Pointer (`/policy/risk_class`, `/tools/0/name`, and so on) is always present regardless of format; the Console renders it as a breadcrumb even when line/column is unavailable.

### Error shape on the wire

The REST API returns validation failures in a uniform envelope:

```json
{
  "error": "manifest_invalid",
  "message": "manifest failed schema validation",
  "details": {
    "violations": [
      {
        "pointer": "/policy/risk_class",
        "line": 12,
        "col": 14,
        "reason": "required",
        "kind": "schema"
      }
    ]
  }
}
```

The `pointer` value is a standard RFC 6901 JSON Pointer. Client tooling, CI pipelines, and the Console all consume the same shape; there is no separate error format for interactive versus programmatic callers.

---

## REST API

All endpoints are tenant-scoped via the JWT claim and require `skills:read` (read operations) or `skills:write` (mutations).

### Skill sources (external drivers)

```http
GET    /api/skill-sources
POST   /api/skill-sources
GET    /api/skill-sources/{name}
PUT    /api/skill-sources/{name}
DELETE /api/skill-sources/{name}
POST   /api/skill-sources/{name}/refresh
GET    /api/skill-sources/{name}/packs
```

`POST /api/skill-sources` body must include `driver`, `name`, `config` (driver-specific JSON object), and optionally `credential_ref`, `priority`, and `enabled`.

`POST /api/skill-sources/{name}/refresh` forces an immediate `List` on the source, updates `last_refresh_at` and `last_error`, and emits a `skill_source.refreshed` audit event.

`DELETE /api/skill-sources/{name}` joins the watcher goroutine before removing the row so no background goroutine outlives the deleted source.

### Authored skills

```http
GET    /api/skills/authored
POST   /api/skills/authored
GET    /api/skills/authored/{id}
GET    /api/skills/authored/{id}/versions
GET    /api/skills/authored/{id}/versions/{v}
PUT    /api/skills/authored/{id}/versions/{v}
POST   /api/skills/authored/{id}/versions/{v}/publish
POST   /api/skills/authored/{id}/versions/{v}/archive
DELETE /api/skills/authored/{id}/versions/{v}
```

`GET /api/skills/authored` lists every authored pack for the tenant, returning the active version and its status. Draft-only packs appear in the list but with `status: "draft"`.

`PUT /api/skills/authored/{id}/versions/{v}` is rejected with a typed error when the target version is already published. Published versions are immutable; edits require creating a new version string.

`DELETE /api/skills/authored/{id}/versions/{v}` is rejected when the version is published. Archive first, then delete.

### Validate endpoint

```http
POST /api/skills/validate
```

Body: multipart or JSON bundle containing a `manifest` field (YAML or JSON text) and optional files (`SKILL.md`, `prompts/*.md`, and so on). Returns a `ValidationResult` including the computed checksum if validation passes — the Console shows this checksum in the publish confirmation dialog.

---

## Console screens

The Console provides two top-level screens under the Skills section:

**`/skills/sources`** — lists every external skill source for the tenant (name, driver, priority, enabled state, last refresh time, last error). A **+ Add source** button opens a form that reveals driver-specific fields when the driver is selected. Sources can be enabled/disabled inline. Clicking a source name opens the detail view (`/skills/sources/{name}`), which shows the packs the source currently serves, a refresh button, and the full `last_error` text.

**`/skills/authored`** — lists every authored Skill Pack. The **+ Author pack** button navigates to `/skills/authored/new`, which presents a three-pane editor:

- Left pane: structured form for top-level manifest fields (ID, version, summary, policy, capabilities) with a live YAML preview.
- Centre pane: tabbed editor for `SKILL.md`, prompt files, and optional UI assets.
- Right pane: live validation panel, debounced 500 ms, displaying JSON-Pointer-tagged violations with inline line highlights.

Clicking an existing pack in the list opens the version history and edit/publish/archive controls at `/skills/authored/{id}`.

---

## Priority and collision resolution

When two sources expose packs with the same `skill.id`, the Registry selects the pack from the source with the lower `priority` integer. The default priority for new sources is `100`. The synthesised `authored` driver is prepended before all external sources and always wins a collision unless an external source is explicitly given a lower priority value.

Collision handling is deterministic — the sort is `priority ASC, name ASC` for tie-breaking — so restarting the gateway produces the same resolution order given the same database rows. An audit event of type `skill_source.collision` is emitted for each displaced pack so operators can investigate conflicts without trawling logs.

---

## Security considerations

- **Credential isolation.** Git and HTTP credentials are stored in the tenant-scoped vault and resolved per-fetch. A credential stored under tenant A is inaccessible to tenant B's source drivers.
- **Path traversal.** Every driver's `ReadFile` implementation validates the resolved absolute path starts within the pack root before opening any file.
- **HTTP checksum enforcement.** The HTTP driver verifies the SHA-256 checksum advertised in the feed before serving a bundle to the loader. A bundle that fails checksum is rejected and an error is recorded; the pack is not loaded.
- **Git submodules.** Disabled by default. A `.gitmodules` file pointing at an attacker-controlled server is an exfiltration path for vault credentials. Enable only when the full repository tree is operator-controlled.
- **Authored draft isolation.** Only packs with `status = 'published'` are visible to sessions, snapshots, or `tools/list` responses. Draft packs are never leaked to downstream agents.

---

## Related

- [Skill Packs](/concepts/skill-packs) — manifest schema, virtual directory, and per-session enablement
- [Credentials vault](/concepts/credentials-vault) — storing Git PATs and HTTP tokens referenced by `credential_ref`
- [Catalog and sessions](/concepts/catalog-and-sessions) — how published packs enter the live catalog
- [Console](/concepts/console) — operator UI including `/skills/sources` and `/skills/authored`
- [Audit](/concepts/audit) — `skill_source.added`, `skill.authored.published`, and related event types
- [Observability](/concepts/observability) — OTel spans wrapping source pulls and validation runs
