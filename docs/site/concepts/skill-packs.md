# Skill Packs

A Skill Pack is a versioned, self-describing package that turns raw MCP tools into a governed, repeatable workflow. It extends the open Skills spec with a `binding` block that declares which MCP servers and tools the skill depends on, what policy governs them, which plan tiers can access it, and whether it exposes a UI resource. The Portico runtime validates every pack against a JSON Schema 2020-12 definition at load time, surfaces its contents as `skill://` resources and namespaced prompts through standard MCP primitives, and enforces per-tenant and per-session enablement so clients receive only the surface they are entitled to.

Because skills expose themselves through `resources/list`, `resources/read`, and `prompts/list`, any MCP-compliant client discovers and consumes them without Portico-specific knowledge. Portico-aware clients additionally read the `skill://_index` JSON resource to get the full entitlement and status picture in a single request.

## Concept: what a Skill Pack contains

A pack lives in a directory tree with a `manifest.yaml` at its root. The manifest declares:

- **Identity** — a namespaced `id` (e.g. `github.code-review`), a semver `version`, and a human `title`.
- **Instructions** — a relative path to a Markdown file (`SKILL.md`) that the runtime reads and exposes as a resource. This is the behavioral prompt for the agent.
- **Resources** — additional files (guides, example JSON, reference material) the skill makes available for the agent to read.
- **Prompts** — Markdown files with optional YAML frontmatter and Go `text/template` substitution slots. The runtime registers these as named prompts on the MCP `prompts/list` surface.
- **Binding** — the Portico-specific block that ties the skill to the live runtime: server and tool dependencies, policy hints, entitlements, and an optional UI resource URI.

## The `skills/v1` manifest

The manifest is YAML. The schema is published at `https://portico.dev/schema/skills/v1.json` and is embedded into the Portico binary at `internal/skills/manifest/schema.json`. Both JSON Schema 2020-12 and a second semantic validation pass run at load time.

```yaml
id: github.code-review           # namespace.name — lowercase, dots allowed
title: GitHub Code Review
version: 0.1.0                   # semver
spec: skills/v1                  # literal — must be exactly this value
description: Review a GitHub PR following Portico's recommended sequence.
instructions: SKILL.md           # relative path to the instructions file

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

### Required top-level fields

| Field | Constraint |
|---|---|
| `id` | Lowercase, pattern `^[a-z0-9][a-z0-9._-]*[a-z0-9]$`. Conventionally `namespace.name`. |
| `title` | Non-empty string. |
| `version` | Semver: `major.minor.patch[-prerelease][+build]`. |
| `spec` | The literal string `skills/v1`. Any other value fails the schema. |
| `instructions` | Relative path to the instructions file. Absolute paths are rejected by the schema. |
| `binding` | Object; contents may be empty but the key must be present. |

`description` is optional but strongly recommended — it populates the Console skill card and the `skill://_index` JSON entry that agents use for discovery.

### The `binding` block

`server_dependencies` lists the MCP server IDs the skill requires. At load time the semantic validator checks these against the live registry and emits an error when none of the tenant's registered servers satisfy the dependency.

`required_tools` names the tools the skill will call, in `server.tool` form (pattern `^[a-z0-9_-]+\.[a-z0-9_.-]+$`). A tool listed here that cannot be resolved to any registered server blocks the pack from loading.

`optional_tools` follows the same naming convention but missing tools produce a warning rather than a load error. The tool appears in `missing_tools` in the `skill://_index` output so agents can reason about degraded functionality.

### Policy declarations

`policy.requires_approval` lists tools that must pass through the approval flow before they execute. Every tool listed here must also appear in `required_tools` or `optional_tools` — the semantic validator enforces this constraint.

`policy.risk_classes` assigns a risk classification to specific tools:

| Class | Meaning |
|---|---|
| `read` | Safe read of data. |
| `write` | Mutation of data within a system. |
| `destructive` | Irreversible deletion or overwrite. |
| `external_side_effect` | Calls that affect systems outside the immediate data store (e.g. posting a comment). |
| `sensitive_read` | Access to credentials, PII, or secrets. |

Risk classes feed into the policy engine (see [Policy](/concepts/policy)) and determine whether a tool call is subject to the operator-configured risk threshold, independent of what the skill's `requires_approval` list says.

### Entitlements

`entitlements.plans` is a list of plan tier strings. When the list is non-empty, only tenants whose billing plan matches are shown the skill. An empty list means the skill is available on every plan. The catalog's `ForTenant` filter combines plan membership with per-tenant entitlement glob patterns (e.g. `github.*`) configured on the Agent Profile.

### UI binding

`ui.resource_uri` declares a `ui://` URI pointing to an MCP App HTML resource exposed by one of the dependent servers. When the skill is enabled for a session, the runtime surfaces this URI alongside the other skill resources, and the host can render the App inline. See [Agent Profiles](/concepts/agent-profiles) for how MCP App resources flow into the active session.

## On-disk layout

The LocalDir source walks two directory levels below its `root`:

```
{root}/
  {namespace}/          # e.g. github/
    {name}/             # e.g. code-review/
      manifest.yaml
      SKILL.md
      prompts/
        review_pr.md
        summarize_diff.md
      resources/
        guide.md
        examples.json
```

The `id` in `manifest.yaml` is expected to match `{namespace}.{name}` derived from the directory path. The semantic validator warns when they disagree, preventing silent drift when directories are renamed.

All paths referenced in `resources` and `prompts` are relative to the pack root. The source driver enforces this: any path containing `..` or starting with `/` is rejected with a path-traversal error at read time.

## How the runtime works

### Loading pipeline

At startup (and on file-system change events from the `fsnotify` watcher), the loader runs each pack through two sequential validation stages:

1. **JSON Schema validation** — the embedded `schema.json` (Draft 2020-12) checks types, required fields, and patterns. Violations are reported with JSON Pointer paths and YAML line/column numbers extracted from the node tree.
2. **Semantic validation** — checks that referenced files exist via a `ReadFile` probe, that tool names resolve to registered servers, that `requires_approval` items are a subset of the declared tools, and that the `ui.resource_uri` starts with `ui://`.

If a pack fails either stage, the loader records the errors and the runtime skips registration. Any pack that was previously loaded and healthy keeps its last-known-good version in the catalog until a subsequent successful load replaces it. A failed reload never removes a live pack — a typo in a manifest edit does not silently take down an active skill.

Packs that pass both stages are inserted into an in-memory catalog keyed by skill ID (global packs) or `{tenantID}:{skillID}` (tenant-scoped authored packs from the Authored source driver).

### The `skill://` virtual directory

Once loaded, every pack is materialized as a set of `skill://` resources that appear in `resources/list` responses for entitled sessions. The URI structure mirrors the pack's on-disk tree:

```
skill://{namespace}/{name}/manifest.yaml
skill://{namespace}/{name}/SKILL.md
skill://{namespace}/{name}/resources/guide.md
skill://{namespace}/{name}/resources/examples.json
skill://{namespace}/{name}/prompts/review_pr.md
```

`manifest.yaml` is rendered from the parsed in-memory `Manifest` struct, so it always reflects the canonical view of the loaded pack. All other files are read directly from the source on demand — bytes are not held in memory after the listing is built.

### The `skill://_index` resource

The `skill://_index` URI is always present in `resources/list`, regardless of which individual skills are enabled for the session. Reading it returns a JSON document describing every entitled skill for the current (tenant, session) pair:

```jsonc
{
  "version": 1,
  "tenant_id": "acme",
  "session_id": "sess_01j...",
  "generated_at": "2026-06-24T12:00:00Z",
  "skills": [
    {
      "id": "github.code-review",
      "version": "0.1.0",
      "title": "GitHub Code Review",
      "spec": "skills/v1",
      "required_servers": ["github"],
      "required_tools": ["github.get_pull_request", "github.get_pull_request_diff", "github.get_file_contents"],
      "optional_tools": ["github.create_review_comment"],
      "manifest_uri": "skill://github/code-review/manifest.yaml",
      "instructions_uri": "skill://github/code-review/SKILL.md",
      "ui_resource_uri": "ui://github/code-review-panel.html",
      "enabled_for_tenant": true,
      "enabled_for_session": true,
      "missing_tools": [],
      "warnings": [],
      "status": "enabled",
      "assets": { "prompts": 2, "resources": 2, "apps": 1 }
    }
  ]
}
```

The index is cached per `(tenant, session)` for 60 seconds and invalidated on any catalog change, registry change, or enablement mutation. The `missing_tools` and `status` fields are computed against the live registry at render time, giving agents a one-request summary of exactly which skills are functional without requiring per-pack follow-up reads.

### Prompts

Each file listed under `prompts` in the manifest is registered on the `prompts/list` MCP surface with the name `{skill.id}.{prompt-name}`. The prompt name comes from the file's YAML frontmatter `name` field; when absent, the filename without extension is used.

Prompt files may carry a `---`-delimited YAML frontmatter block:

```markdown
---
name: review_pr
description: Step-by-step PR review prompt template.
arguments:
  - name: owner
    description: Repository owner.
    required: true
  - name: repo
    description: Repository name.
    required: true
  - name: pr_number
    description: Pull request number.
    required: true
---

# Review PR {{.owner}}/{{.repo}}#{{.pr_number}}

You are reviewing a pull request. Follow this sequence:

1. Fetch PR metadata with `github.get_pull_request`.
2. Read the diff with `github.get_pull_request_diff`.
...
```

The `arguments` block populates the `Prompt.arguments` field in the `prompts/list` response, enabling clients to present a structured form before calling `prompts/get`. At `prompts/get` time, the runtime renders the Markdown body through Go's `text/template` engine with the supplied arguments as the template data map. The <span v-pre>`{{template}}`</span> and <span v-pre>`{{block}}`</span> directives are unsupported by design — prompt templates are leaf substitutions only.

## Enablement model

Skills are not automatically visible to a session. The runtime resolves enablement through a three-level precedence chain:

1. **Per-session row** — an explicit enable or disable set for this session ID.
2. **Per-tenant row** — a tenant-wide default set by the operator.
3. **Global default mode** — configured under `skills.enablement.default` in `portico.yaml`. Two modes are supported:
   - `opt-in` (default): all skills are disabled until explicitly enabled.
   - `auto`: all entitled skills are enabled by default; individual packs can be disabled explicitly.

Entitlement filtering runs before the enablement check. A session will never see a skill whose plan tier does not match the tenant's billing plan, regardless of enablement state.

Agent Profiles layer on top: the `skills` field on a Profile is a glob allowlist that restricts which packs a given consumer can enable or read. A session bound to a Profile with `skills: ["github.*"]` cannot access `postgres.sql-analyst` even if the tenant has it enabled. See [Agent Profiles](/concepts/agent-profiles).

### REST API for enablement

```http
POST /v1/skills/{id}/enable
POST /v1/skills/{id}/disable

POST /v1/sessions/{session_id}/skills/enable
Content-Type: application/json

{"skill_id": "github.code-review"}
```

Tenant-wide enable/disable take effect immediately for all new sessions. Per-session overrides shadow the tenant row for the lifetime of that session. The Console `/skills/{id}` detail page exposes toggles wired to both endpoints.

## Four reference packs

The repository ships four reference Skill Packs under `examples/skills/`. Each is a valid, load-testable pack that also serves as a fixture for integration tests.

| Pack ID | Server dependency | Notable policy |
|---|---|---|
| `github.code-review` | `github` | `github.create_review_comment` requires approval; `risk_class: external_side_effect`. Available on `pro` and `enterprise` plans. |
| `postgres.sql-analyst` | `postgres` | `postgres.run_sql` classified as `read`. Available on all plans. |
| `linear.triage` | `linear` | `linear.update_issue` requires approval; `risk_class: external_side_effect`. Available on `pro` and `enterprise` plans. |
| `filesystem.search` | `fs` | `fs.read` classified as `read`. No approval required. Available on all plans. |

Validate all four against a running binary:

```bash
./bin/portico validate-skills ./examples/skills/github/code-review \
                               ./examples/skills/postgres/sql-analyst \
                               ./examples/skills/linear/triage \
                               ./examples/skills/filesystem/search
```

Exit code `0` means every pack cleared both schema and semantic validation.

## Skill sources

The runtime discovers packs through pluggable `Source` drivers. V1 ships four:

| Driver | Key | Description |
|---|---|---|
| `local` / `localdir` | `local` | Watches a filesystem directory with `fsnotify`. Hot-reload on any `manifest.yaml` change (200 ms debounce). |
| `git` | `git` | Clones a Git repository and periodically pulls. No inotify dependency; suitable for network-mounted paths and CI-managed trees. |
| `http` | `http` | Fetches a zip or tarball bundle from an HTTP endpoint. Periodic refresh at the configured interval. |
| `authored` | `authored` | In-Portico tenant-authored packs managed via the Console authoring UI and the REST API. |

Configure sources under `skills.sources` in `portico.yaml`:

```yaml
skills:
  sources:
    - type: local
      path: ./skills
  enablement:
    default: opt-in
  refresh_interval: 30s
```

For details on the driver interface, credentials support, and per-tenant source scoping, see [Skill Sources](/concepts/skill-sources).

## CLI validation

```bash
# Validate one or more packs without a running server
./bin/portico validate-skills ./path/to/pack1 ./path/to/pack2

# Validate from the repo root during development
./bin/portico validate-skills ./examples/skills/...
```

Output is one line per pack:

```
github.code-review v0.1.0   OK
postgres.sql-analyst v0.1.0  OK
linear.triage v0.1.0        OK
filesystem.search v0.1.0    OK
```

On failure, each violation is printed with its JSON Pointer and YAML line/column:

```
my-pack v0.0.1  ERROR
  /binding/required_tools/0  line 14, col 5: pattern: must match "^[a-z0-9_-]+\.[a-z0-9_.-]+$"
```

Exit code `1` when any pack has errors; exit code `0` only when all packs pass.

::: tip Hot reload in development
With a `local` source, saving any file inside a pack directory triggers an automatic reload. The previous version stays active if the new one fails validation — you will never silently break a live skill by making a typo.
:::

::: warning Required tools and missing-tools status
A skill whose `required_tools` list references a tool from a server that is not registered for the tenant will load with errors, and its status in the `skill://_index` will show `missing_tools`. Clients should read the index before invoking a skill to check whether the required infrastructure is present.
:::

## Related

- [Skill Sources](/concepts/skill-sources) — how packs are fetched from local directories, Git repositories, HTTP endpoints, and in-Portico authored storage.
- [Agent Profiles](/concepts/agent-profiles) — per-consumer allowlists that gate which Skill Packs a session may enable.
- [Approvals](/concepts/approvals) — how `policy.requires_approval` declarations trigger the approval flow before a tool executes.
- [MCP Gateway](/concepts/mcp-gateway) — the northbound MCP surface where `skill://` resources and skill-derived prompts appear to clients.
- [Policy](/concepts/policy) — how `risk_classes` feed into the broader policy engine.
- [Guides: Build a Skill Pack](/guides/build-skill-pack) — step-by-step walkthrough from a blank directory to a validated, loaded pack.
