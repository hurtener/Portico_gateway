# Phase 4 — Skills Runtime + Virtual Directory

> Self-contained implementation plan. Builds on Phase 0–3.

## Goal

Introduce the Skill Pack runtime: a virtual-directory abstraction that loads Skill Packs from pluggable sources, validates their manifests, exposes their content as MCP `skill://` resources and prompts (so any MCP client can discover and read them), and enables per-session activation. After Phase 4, a user can drop a Skill Pack folder under `./skills/`, register it via config (or just place it in a watched directory), and have it appear automatically in `resources/list`, `prompts/list`, and the Console catalog — with binding metadata exposed through native `/v1/skills` APIs.

## Why this phase exists

Skills are Portico's differentiator. Phase 4 is where the product takes its real shape. The MCP-first virtual-directory design ensures vanilla clients still benefit (skills are just resources and prompts), while Portico-aware clients get binding metadata for richer behavior. Per-session enablement is the granularity LLMs benefit from — a session opting into `github.code-review` gets the skill's instructions, prompts, and resources surfaced; sessions that don't, don't.

## Prerequisites

Phases 0–3 complete. Specifically:
- Resource and prompt aggregators handle multiple sources and namespacing.
- Apps registry can index `ui://` references.
- Tenant context flows through every read.

## Deliverables

1. Skill manifest types in `internal/skills/manifest/` with full Go structs + JSON Schema (`schema.json` embedded).
2. `SkillSource` interface and `LocalDir` implementation in `internal/skills/source/`.
3. Skill loader + validator in `internal/skills/loader/`.
4. Skill runtime that wires skills into the resource and prompt aggregators (`internal/skills/runtime/`).
5. `skill://_index` synthesized resource generator.
6. Per-session skill enablement (uses Phase 0's `skill_enablement` table).
7. Native APIs: `/v1/skills`, `/v1/skills/{id}`, `/v1/skills/{id}/manifest.yaml`, `/v1/skills/{id}/enable`, `/v1/skills/{id}/disable`, `/v1/sessions/{id}/skills/enable`, `/v1/sessions/{id}/skills/disable`.
8. Skill -> tool dependency check at load time + runtime warnings.
9. Console `/skills` and `/skills/{id}` pages.
10. Four reference Skill Packs in `examples/skills/`:
    - `github.code-review`
    - `postgres.sql-analyst`
    - `linear.triage`
    - `filesystem.search`
11. Tests: manifest parse + validate, dependency check, virtual directory exposure, per-session enablement, index resource generation.

## Acceptance criteria

1. With a `LocalDir` source pointing at `./skills/`, skills are loaded at startup and appear in `resources/list` under `skill://...` URIs and in `prompts/list` with `{skill_id}.{prompt_name}` naming.
2. `resources/read` for `skill://_index` returns a JSON listing of every skill available to the requesting tenant (filtered by entitlements).
3. `resources/read` for `skill://{ns}/{id}/manifest.yaml` returns the raw manifest; for `skill://{ns}/{id}/SKILL.md` returns the instructions; for any listed resource returns its bytes.
4. Manifest validation rejects bad inputs with a precise error pointing at the offending field. JSON Schema validation runs in addition to Go-side semantic checks.
5. Skill Pack with a `binding.required_tools` referencing a tool that isn't in any registered server fails to load with a clear error; with `optional_tools` missing, loads with a warning.
6. Per-session enablement: `POST /v1/sessions/{id}/skills/enable {"skill_id": "github.code-review"}` writes to `skill_enablement` (session_id != NULL), and only enabled skills are visible to that session in `resources/list`/`prompts/list`. Tenant-wide enablement (`session_id` NULL) is the fallback.
7. Hot reload: editing a manifest under `./skills/` triggers a re-load (validator runs); on success, the skill catalog updates within 1s; on failure, the previous version stays active and an error is emitted to the audit log + Console banner.
8. Reference packs all pass validation and lint (a `portico validate-skills ./examples/skills/*` command exits 0).
9. The Console `/skills/{id}` page shows the manifest, dependencies, status (per-tenant: enabled/disabled, missing tools), and a markdown render of `SKILL.md`.
10. `entitlements.skills` glob matching works: tenant `beta` with `skills: ["github.*"]` sees `github.code-review` but not `postgres.sql-analyst`.

## Architecture

```
+----------------------------------+
|  internal/skills/source          |
|    SkillSource interface         |
|    LocalDir impl (V1)            |
|    fsnotify watcher              |
+----------------+-----------------+
                 |
                 v
+----------------------------------+
|  internal/skills/loader          |
|    Manifest parse + validate     |
|    Schema-based + semantic check |
|    Dependency resolution         |
+----------------+-----------------+
                 |
                 v
+----------------------------------+
|  internal/skills/runtime         |
|    Catalog (active versions)     |
|    Enablement registry           |
|    Index generator               |
|    Resource/prompt provider      |
+----------------+-----------------+
                 |
                 v
+----------------------------------+
|  internal/server/mcpgw           |
|    ResourceAggregator (Phase 3)  |
|    PromptAggregator (Phase 3)    |
|    Skill provider plugged in     |
+----------------------------------+
```

## Package layout

```
internal/skills/
  manifest/
    types.go
    schema.json          // JSON Schema (embedded via embed.FS)
    schema.go            // schema loading
    types_test.go
  source/
    source.go            // SkillSource interface
    localdir.go          // LocalDir impl
    refresh.go           // periodic + fsnotify-driven refresh
    localdir_test.go
  loader/
    loader.go            // resolves manifest from SkillSource, runs validators
    validator.go         // semantic validation (deps, names, ui refs)
    loader_test.go
  runtime/
    catalog.go           // active skills registry
    enablement.go        // per-tenant + per-session enablement
    provider.go          // ResourceProvider + PromptProvider hooks
    indexgen.go          // skill://_index synthesizer
    catalog_test.go
internal/server/api/
  handlers_skills.go
  handlers_session_skills.go
internal/server/mcpgw/
  resources.go           // extended to merge SkillProvider
  prompts.go             // extended to merge SkillProvider
cmd/portico/
  cmd_validate_skills.go
web/console/templates/
  skills.templ           // filled
  skill_detail.templ
examples/skills/
  github.code-review/
  postgres.sql-analyst/
  linear.triage/
  filesystem.search/
test/integration/
  skills_e2e_test.go
```

## Manifest types

```go
// internal/skills/manifest/types.go
package manifest

type Manifest struct {
    ID          string   `yaml:"id" json:"id"`
    Title       string   `yaml:"title" json:"title"`
    Version     string   `yaml:"version" json:"version"`
    Spec        string   `yaml:"spec" json:"spec"`           // must be "skills/v1"
    Description string   `yaml:"description,omitempty" json:"description,omitempty"`
    Instructions string  `yaml:"instructions" json:"instructions"`  // path to SKILL.md
    Resources   []string `yaml:"resources,omitempty" json:"resources,omitempty"`
    Prompts     []string `yaml:"prompts,omitempty" json:"prompts,omitempty"`
    Binding     Binding  `yaml:"binding" json:"binding"`
}

type Binding struct {
    ServerDependencies []string                       `yaml:"server_dependencies,omitempty" json:"server_dependencies,omitempty"`
    RequiredTools      []string                       `yaml:"required_tools,omitempty" json:"required_tools,omitempty"`
    OptionalTools      []string                       `yaml:"optional_tools,omitempty" json:"optional_tools,omitempty"`
    Policy             Policy                         `yaml:"policy,omitempty" json:"policy,omitempty"`
    UI                 *UIBinding                     `yaml:"ui,omitempty" json:"ui,omitempty"`
    Entitlements       Entitlements                   `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
}

type Policy struct {
    RequiresApproval []string             `yaml:"requires_approval,omitempty" json:"requires_approval,omitempty"`
    RiskClasses      map[string]string    `yaml:"risk_classes,omitempty" json:"risk_classes,omitempty"`
}

type UIBinding struct {
    ResourceURI string `yaml:"resource_uri" json:"resource_uri"`  // ui://...
}

type Entitlements struct {
    Plans []string `yaml:"plans,omitempty" json:"plans,omitempty"`
}
```

## JSON Schema

Embed `internal/skills/manifest/schema.json` (Draft 2020-12). Include rules:
- `id` matches `^[a-z0-9][a-z0-9._-]*[a-z0-9]$` (allows dotted namespacing).
- `version` is semver.
- `spec` is the literal `"skills/v1"`.
- `instructions` is required.
- `resources[*]` and `prompts[*]` are non-absolute paths.
- `binding.required_tools[*]` matches `^[a-z0-9_-]+\.[a-z0-9_-]+$` (server.tool form).
- `binding.policy.risk_classes[*]` ∈ {`read`, `write`, `destructive`, `external_side_effect`, `sensitive_read`}.
- `binding.ui.resource_uri` matches `^ui://`.
- `binding.entitlements.plans[*]` ∈ {`free`, `pro`, `enterprise`} (or any operator-defined plan; warn if not in known set).

The schema runs first; semantic validator runs second and produces additional errors.

## SkillSource interface

```go
// internal/skills/source/source.go
package source

type Source interface {
    Name() string
    List(ctx context.Context) ([]Ref, error)
    Open(ctx context.Context, ref Ref) (manifest.Manifest, error)
    ReadFile(ctx context.Context, ref Ref, relpath string) (io.ReadCloser, ContentInfo, error)
    Watch(ctx context.Context) (<-chan Event, error)  // optional; Source returns nil channel if unsupported
}

type Ref struct {
    ID      string
    Version string
    Source  string  // backend name
    Loc     string  // backend-specific locator (e.g. abs path for LocalDir)
}

type ContentInfo struct {
    MIMEType string
    Size     int64
    ModTime  time.Time
}

type Event struct {
    Kind EventKind  // Added | Updated | Removed
    Ref  Ref
}
```

## LocalDir source

```go
// internal/skills/source/localdir.go
package source

type LocalDir struct {
    Root string
    log  *slog.Logger
    // ...
}

func NewLocalDir(root string, log *slog.Logger) (*LocalDir, error)
```

Layout convention:

```
{Root}/
  {namespace}/
    {id}/
      manifest.yaml
      SKILL.md
      prompts/*.md
      resources/*.{md,json,yaml,html}
```

`List` walks two levels under `Root` looking for directories containing `manifest.yaml`. Each becomes a `Ref{ID: namespace + "." + id, Version: <from manifest>, Loc: dir, Source: "local"}`.

`Watch` uses `fsnotify` to monitor the entire `Root`. Event grouping with 200ms debounce; one `manifest.yaml` change emits an `Updated` for the whole pack.

`Open` reads `manifest.yaml`, parses YAML, returns `Manifest`. Does NOT validate — that's the loader's job.

`ReadFile` reads the requested path, scoped to the pack directory (rejects `..`).

## Loader / validator

```go
// internal/skills/loader/loader.go
package loader

type Loader struct {
    sources  []source.Source
    schema   *jsonschema.Schema
    registry *registry.Registry  // for dependency check
    log      *slog.Logger
}

func New(sources []source.Source, registry *registry.Registry, log *slog.Logger) (*Loader, error)

// Load all skills from all sources. Returns a slice of LoadResult with successes and failures.
func (l *Loader) LoadAll(ctx context.Context) []LoadResult

type LoadResult struct {
    Ref       source.Ref
    Manifest  *manifest.Manifest
    Errors    []error           // schema + semantic errors
    Warnings  []string
}

// LoadOne reloads a single pack on demand (used by hot-reload).
func (l *Loader) LoadOne(ctx context.Context, ref source.Ref) LoadResult
```

```go
// internal/skills/loader/validator.go
package loader

func ValidateSemantic(m *manifest.Manifest, registry *registry.Registry) (errs []error, warnings []string)
```

Semantic checks:
- All `instructions`/`resources`/`prompts` paths are readable from the source (loader does a probe via `ReadFile`).
- Every `binding.required_tools[*]` is exposed by some registered server (errors if not in any tenant; warns if only in some tenants — Skill is loadable but won't function for tenants without it).
- Every `binding.optional_tools[*]` similar but only warns.
- Every `binding.server_dependencies[*]` exists in registry.
- `binding.ui.resource_uri` is a `ui://` URI; if the apps.Registry already knows it, fine; if not, warning (server may register on first list).
- All `binding.policy.requires_approval` items are subset of `binding.required_tools ∪ binding.optional_tools`.
- `id` matches the expected `<namespace>.<name>` form derived from on-disk path (avoid copy-paste errors).

Errors include line/column from the YAML parser when possible (use `yaml.v3`'s node positions).

## Runtime

```go
// internal/skills/runtime/catalog.go
package runtime

type Catalog struct {
    mu     sync.RWMutex
    skills map[string]*Skill   // key = id
    log    *slog.Logger
    audit  audit.Emitter
    onChange chan ChangeEvent
}

type Skill struct {
    Manifest *manifest.Manifest
    Source   source.Source
    Ref      source.Ref
    LoadedAt time.Time
}

type ChangeEvent struct {
    Kind   ChangeKind   // Added | Updated | Removed
    Skill  *Skill
    Errors []error
}

func (c *Catalog) Set(s *Skill)
func (c *Catalog) Remove(id string)
func (c *Catalog) Get(id string) (*Skill, bool)
func (c *Catalog) List() []*Skill
func (c *Catalog) ForTenant(tenantID string, ents config.Entitlements, plan string) []*Skill
func (c *Catalog) Subscribe() <-chan ChangeEvent
```

`ForTenant` filters by:
- `entitlements.skills` glob match (`github.*`, `*`).
- `binding.entitlements.plans` includes tenant's plan, OR plans is empty.

```go
// internal/skills/runtime/enablement.go
package runtime

type Enablement struct {
    db *sqlite.DB
}

func (e *Enablement) IsEnabled(ctx context.Context, tenantID, sessionID, skillID string) (bool, error)
func (e *Enablement) Enable(ctx context.Context, tenantID, sessionID, skillID string) error
func (e *Enablement) Disable(ctx context.Context, tenantID, sessionID, skillID string) error
func (e *Enablement) ListEnabledForSession(ctx context.Context, tenantID, sessionID string) ([]string, error)
func (e *Enablement) ListEnabledForTenant(ctx context.Context, tenantID string) ([]string, error)
```

`IsEnabled` resolution:
1. Per-session row exists (session_id != NULL): use `enabled` flag.
2. Else per-tenant row (session_id NULL): use `enabled` flag.
3. Else fall back to manifest default (Phase 4 default: enabled if entitlement passes).

```go
// internal/skills/runtime/provider.go
package runtime

// SkillProvider is plugged into the Phase 3 ResourceAggregator and PromptAggregator.
// The aggregator queries SkillProvider in addition to the per-server fan-out.

type SkillProvider struct {
    catalog    *Catalog
    enablement *Enablement
    sources    map[string]source.Source
    indexGen   *IndexGenerator
}

func (p *SkillProvider) ListResources(ctx context.Context, tenantID, sessionID string) ([]protocol.Resource, error)
func (p *SkillProvider) ReadResource(ctx context.Context, tenantID, sessionID, uri string) (*protocol.ReadResourceResult, error)
func (p *SkillProvider) ListPrompts(ctx context.Context, tenantID, sessionID string) ([]protocol.Prompt, error)
func (p *SkillProvider) GetPrompt(ctx context.Context, tenantID, sessionID, name string, args map[string]string) (*protocol.GetPromptResult, error)
```

Resource production: for each enabled skill,
- Synthesize `skill://{namespace}/{id}/manifest.yaml` resource (MIME `application/yaml`, size of manifest).
- Synthesize `skill://{namespace}/{id}/SKILL.md` resource (MIME `text/markdown`).
- For each `manifest.resources[*]` (e.g. `resources/guide.md`), synthesize `skill://{namespace}/{id}/{path}`.
- For each `manifest.prompts[*]`, synthesize `skill://{namespace}/{id}/{path}` resources too (so the bytes are accessible as resources).

Plus the synthetic index `skill://_index` (always present, regardless of enablement).

Prompt production: for each enabled skill, register prompts named `{skill.id}.{prompt_filename_without_ext}`. The `Prompt.Description` is taken from the prompt file's frontmatter or first paragraph.

### Index generator

```go
// internal/skills/runtime/indexgen.go
package runtime

type IndexGenerator struct {
    catalog    *Catalog
    enablement *Enablement
}

func (g *IndexGenerator) Render(ctx context.Context, tenantID, sessionID string) ([]byte, error)
```

Output:

```json
{
  "version": 1,
  "tenant_id": "acme",
  "generated_at": "2026-05-05T16:30:00Z",
  "skills": [
    {
      "id": "github.code-review",
      "version": "0.1.0",
      "title": "GitHub Code Review",
      "description": "Review a GitHub PR following best practices.",
      "spec": "skills/v1",
      "required_servers": ["github"],
      "required_tools": ["github.get_pull_request", "github.get_pull_request_diff", "github.get_file_contents"],
      "optional_tools": ["github.create_review_comment"],
      "manifest_uri": "skill://github/code-review/manifest.yaml",
      "instructions_uri": "skill://github/code-review/SKILL.md",
      "ui_resource_uri": "ui://github/code-review-panel.html",
      "enabled_for_session": true,
      "enabled_for_tenant": true,
      "missing_tools": [],
      "warnings": []
    }
  ]
}
```

The index includes `enabled_for_*`, `missing_tools` (computed against the live registry), and `warnings`. This gives the agent enough info to decide which skill to opt into without making more requests.

## Aggregator integration (Phase 3 changes)

`internal/server/mcpgw/resources.go`:

```go
type ResourceAggregator struct {
    // ... Phase 3 fields ...
    skill *runtime.SkillProvider
}

func (a *ResourceAggregator) ListAll(ctx, sess) (...) {
    // 1. Fan out to downstream servers (Phase 3 logic)
    // 2. ALSO call a.skill.ListResources(ctx, sess.TenantID, sess.ID)
    // 3. Concat lists; sort
}

func (a *ResourceAggregator) Read(ctx, sess, uri) (...) {
    if strings.HasPrefix(uri, "skill://") {
        return a.skill.ReadResource(ctx, sess.TenantID, sess.ID, uri)
    }
    // Phase 3 path
}
```

`prompts.go` similarly delegates the `skill://`-derived prompt names to the SkillProvider.

## Configuration additions

```yaml
skills:
  sources:
    - type: local
      path: ./skills
  enablement:
    default: opt-in   # opt-in (must call enable to use) | auto (all entitled skills enabled by default)
  refresh_interval: 30s   # for sources without watch support; LocalDir uses fsnotify regardless
```

Per-tenant overrides:

```yaml
tenants:
  - id: acme
    skills:
      enablement:
        default: auto
      additional_sources:
        - type: local
          path: /etc/portico/acme-skills
```

## External APIs

```
GET    /v1/skills
       → list of {id, version, title, description, required_servers, missing_tools, enabled_for_tenant}

GET    /v1/skills/{id}
       → full manifest + binding metadata + status

GET    /v1/skills/{id}/manifest.yaml
       → raw YAML (Content-Type: application/yaml)

POST   /v1/skills/{id}/enable
       → 200 (tenant-wide enable)

POST   /v1/skills/{id}/disable
       → 200 (tenant-wide disable)

POST   /v1/sessions/{session_id}/skills/enable
       Body: {"skill_id": "..."}
       → 200

POST   /v1/sessions/{session_id}/skills/disable
       → 200

GET    /v1/sessions/{session_id}/skills
       → [{skill_id, enabled, source}]
```

## Implementation walkthrough

### Step 1: Manifest types + schema

Author `schema.json` and embed via `embed.FS`. Compile schema once at startup; cache on `Loader`.

### Step 2: Source interface + LocalDir

LocalDir.List walks two levels. Watch debounces fsnotify events. Open parses YAML to `Manifest` without validation.

### Step 3: Loader

Compose schema validator + semantic validator. `LoadAll` aggregates results; the runtime decides which to register based on `len(errors) == 0`.

`LoadOne` is the hot-reload path: re-reads, re-validates, returns. Caller (runtime watcher) registers or rejects.

### Step 4: Catalog

A simple in-memory map with RWMutex. `Set` stores; `Subscribe` fans out change events. The catalog persists nothing — runtime state is rebuilt from sources on startup.

### Step 5: Enablement

Use the `skill_enablement` table. Methods are thin wrappers around SQL.

### Step 6: SkillProvider

Implements the resource + prompt provider interfaces. The synthetic resources are produced from the manifest + on-disk file metadata (size and modtime queried via SkillSource). The actual content read goes back to `SkillSource.ReadFile`.

The synthetic `skill://_index` is generated on every read (cheap; cached for 60s). It must be regenerated on:
- A skill catalog change.
- A registry change (missing-tools status may flip).
- An enablement change.

### Step 7: Aggregator wiring

Phase 3 aggregator now also calls SkillProvider. Tests verify both per-server and skill resources appear in one list with correct sort order (skills sorted last alphabetically; deterministic order for snapshot hashing in Phase 6).

### Step 8: APIs

`/v1/skills*` and `/v1/sessions/*/skills*` handlers map to runtime methods. JSON shapes per RFC §13.

### Step 9: CLI

`portico validate-skills <path>...` walks the given paths, runs `Loader.LoadOne` on each manifest, prints results in a structured format. Exits 1 if any errors.

```
github.code-review v0.1.0  OK
postgres.sql-analyst v0.1.0  ERROR: binding.required_tools[1] postgres.unknown_tool not in registry
linear.triage v0.1.0  WARNING: binding.optional_tools[0] linear.archive missing
```

### Step 10: Console

`web/console/templates/skills.templ`: list with status pills (Enabled/Disabled/Missing tools).
`web/console/templates/skill_detail.templ`: manifest pretty-print, dependency tree, markdown render of SKILL.md, enablement toggles.

### Step 11: Reference packs

Each pack is a real, runnable example. They serve as both demos and integration test fixtures.

#### `examples/skills/github.code-review/`

```
manifest.yaml
SKILL.md
prompts/
  review_pr.md
  summarize_diff.md
resources/
  guide.md
  examples.json
```

`manifest.yaml`:

```yaml
id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1
description: Review a GitHub PR following best practices.
instructions: ./SKILL.md
resources:
  - resources/guide.md
  - resources/examples.json
prompts:
  - prompts/review_pr.md
  - prompts/summarize_diff.md
binding:
  server_dependencies: [github]
  required_tools:
    - github.get_pull_request
    - github.get_pull_request_diff
    - github.get_file_contents
  optional_tools:
    - github.create_review_comment
  policy:
    requires_approval:
      - github.create_review_comment
    risk_classes:
      github.create_review_comment: external_side_effect
  ui:
    resource_uri: ui://github/code-review-panel.html
  entitlements:
    plans: [pro, enterprise]
```

Each prompt file uses YAML frontmatter:

```markdown
---
name: review_pr
description: Step-by-step PR review prompt template.
arguments:
  - name: owner
    description: Repo owner.
    required: true
  - name: repo
    description: Repo name.
    required: true
  - name: pr_number
    description: PR number.
    required: true
---

# Review PR {{owner}}/{{repo}}#{{pr_number}}

You are reviewing a pull request. Follow this sequence:
1. Fetch PR metadata with `github.get_pull_request`.
2. Read the diff with `github.get_pull_request_diff`.
3. ...
```

Templates use Go's `text/template` for `{{var}}` substitution at `prompts/get` time.

#### Other reference packs

- `postgres.sql-analyst/`: requires `postgres.run_sql`, `postgres.list_schemas`, `postgres.describe_table`. Risk class `read` for queries; `destructive` for ad-hoc DDL.
- `linear.triage/`: requires `linear.list_issues`, `linear.update_issue`. Approval on `linear.update_issue`.
- `filesystem.search/`: requires `fs.list`, `fs.read`. Read-only, no approval needed; risk class `read`.

## Test plan

### Unit

- `internal/skills/manifest/types_test.go`
  - `TestParseManifest_Valid`
  - `TestParseManifest_BadYAML`
  - `TestSchemaCompiles` — schema.json loads cleanly.
  - `TestSchemaValidate_Valid`/`_BadID`/`_BadSpec`/`_BadVersion`/`_BadRiskClass`/`_BadUIURI`.

- `internal/skills/source/localdir_test.go`
  - `TestList_DiscoversTwoPacks`
  - `TestOpen_ReturnsManifest`
  - `TestReadFile_TraversalRejected` — path with `..` returns error.
  - `TestWatch_FiresOnManifestChange`

- `internal/skills/loader/loader_test.go`
  - `TestLoadAll_ValidPack`
  - `TestLoadAll_MissingRequiredTool` — error.
  - `TestLoadAll_MissingOptionalTool` — warning.
  - `TestLoadAll_BadIDFormat` — schema error.
  - `TestLoadAll_RequiresApprovalOutsideRequiredTools` — error.

- `internal/skills/runtime/catalog_test.go`
  - `TestSetGetRemove`
  - `TestForTenant_GlobMatch_Plan`
  - `TestForTenant_PlanMismatch_Excluded`

- `internal/skills/runtime/enablement_test.go`
  - `TestPerSessionOverride`
  - `TestTenantWideFallback`
  - `TestDefaultOptIn` vs `TestDefaultAuto`

- `internal/skills/runtime/provider_test.go`
  - `TestListResources_IncludesSkillFiles`
  - `TestReadResource_Manifest`
  - `TestReadResource_PromptFileBytes`
  - `TestReadResource_Index_RendersJSON`
  - `TestListPrompts_NamesPrefixed`
  - `TestGetPrompt_RendersWithArgs`

### Integration

- `test/integration/skills_e2e_test.go`
  - `TestE2E_SkillVisibleInResources` — load a pack; init session; resources/list contains skill files.
  - `TestE2E_PromptListIncludesSkillPrompts`
  - `TestE2E_IndexGeneration` — read `skill://_index`; assert structure.
  - `TestE2E_PerSessionEnableDisable`
  - `TestE2E_HotReload_ManifestEdit` — modify version, observe catalog update.
  - `TestE2E_HotReload_BadEdit_KeepsOldVersion`
  - `TestE2E_EntitlementsFiltering` — tenant beta only sees github.* skills.

## Common pitfalls

- **Circular file references**: a manifest could reference a prompt that includes itself via template includes. Phase 4 prompts use plain `text/template` with no `{{include}}`; document and reject any future `{{template}}` directives at load time.
- **Path traversal in `ReadFile`**: enforce `filepath.Clean` and `strings.HasPrefix(absPath, packRoot)`. Test explicitly with `..` and absolute paths.
- **Hot reload partial state**: a manifest edit that breaks validation must NOT remove the previously-loaded version. The runtime keeps the old `Skill` until a successful new version arrives. This is critical for ops — a typo shouldn't take down a tenant's skills.
- **Prompt frontmatter parsing**: ad-hoc YAML frontmatter parsing is error-prone. Use a small parser (or `frontmatter` library) and treat parse failures as warnings, not errors — fall back to filename for `name` and the first paragraph for `description`.
- **Binding `required_tools` semantics**: a "required" tool missing across ALL servers means the skill is broken globally → error. A tool present in some tenants but not others means the skill is conditionally usable → warning, but loadable. The skill provider returns `missing_tools` per tenant in the index.
- **`skill://_index` cache invalidation**: must be invalidated on (a) catalog changes, (b) registry changes (missing tools may flip), (c) enablement changes for the session/tenant. Easy to miss (b).
- **YAML manifest IDs vs filesystem paths**: enforce `id == namespace + "." + dirname` to prevent confusion when packs are renamed on disk but not in manifest.
- **Glob entitlements case-sensitivity**: enforce lowercase IDs; glob match is case-sensitive.

## Out of scope

- `Git`, `OCI`, `HTTP` skill sources (post-V1; the `Source` interface admits them additively).
- Per-skill telemetry / usage counts (Phase 6 audit covers this).
- Skill versions A/B tested per session (post-V1).
- Skill marketplace / sharing (post-V1).
- Cross-tenant skill sharing (post-V1; entitlements are tenant-local).

## Done definition

1. All acceptance criteria pass.
2. Coverage ≥ 80% for `internal/skills/*`.
3. Four reference packs validated by `portico validate-skills` and exposed live in dev mode.
4. Console `/skills` and `/skills/{id}` populate live, including missing-tools status.
5. Demo flow (after Phase 4 complete):
   ```bash
   ./bin/portico dev --config examples/dev-with-skills.yaml &
   curl -s -X POST http://localhost:8080/mcp -d '{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"skill://_index"}}' | jq
   curl -X POST http://localhost:8080/v1/sessions/<id>/skills/enable -d '{"skill_id":"github.code-review"}'
   ```

## Hand-off to Phase 5

Phase 5 inherits a working Skills runtime. Its job: wire policy + credentials + the approval flow on top.

- The `binding.policy` block becomes operational: tools listed in `requires_approval` trigger the approval flow (elicitation or fallback error). Risk-class assignments override per-tool defaults.
- The credential vault grows from stub to full: OAuth token exchange, secret references, per-strategy credential injection.
- Phase 5 also adds the audit store proper (Phase 4 emits to slog only).
