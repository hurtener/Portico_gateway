# Build a Skill Pack

A Skill Pack is a versioned, self-describing bundle that wires a reusable agentic capability into Portico: it declares which MCP servers and tools it needs, how its tools should be risk-classified and governed by the approval flow, and which prompts and resources to surface to any MCP client. Once loaded, a Skill Pack's content is accessible as `skill://` resources and as namespaced prompts — no client-side coordination required.

This guide walks through authoring a pack from scratch, using the `github.code-review` reference pack as the running example, then shows how to validate, load, and enable it.

## How Skill Packs work

At runtime, Portico's Skill Manager reads packs from one or more configured sources, validates each manifest against a JSON Schema 2020-12 schema, runs semantic checks (does the registry actually expose the declared tools?), and registers the pack in the in-memory catalog. The catalog is per-tenant: an entitlement glob on the tenant controls which packs are visible.

Every enabled pack contributes to two MCP surfaces:

- **Resources** — the manifest, instructions file, declared resource files, and prompt source files are all accessible as `skill://<namespace>/<name>/<relpath>` URIs. A synthetic `skill://_index` resource (always present, never gated by enablement) gives any agent a machine-readable summary of what is loaded and what is missing.
- **Prompts** — each prompt file becomes a named, argument-templated MCP prompt, registered as `<skill-id>.<prompt-name>` (e.g. `github.code-review.review_pr`).

See [Skill Packs](/concepts/skill-packs) for the full architecture and [Skill Sources](/concepts/skill-sources) for the source driver reference.

## Directory layout

A Skill Pack is a directory with a specific two-level path under its source root:

```
{skills-root}/
  {namespace}/               # e.g. "github"
    {name}/                  # e.g. "code-review"
      manifest.yaml          # required
      SKILL.md               # required (referenced from manifest.yaml)
      prompts/
        review_pr.md
        summarize_diff.md
      resources/
        guide.md
        examples.json
```

The `LocalDir` source driver discovers packs by walking exactly two levels: every directory at depth two that contains a `manifest.yaml` becomes a pack. The pack's `id` defaults to `{namespace}.{name}` unless the manifest overrides it (which it must, and the override must match the derived form to prevent copy-paste drift).

## The manifest

`manifest.yaml` is the single source of truth for a pack's identity, content, and bindings. Every manifest must declare `spec: skills/v1`.

```yaml
id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1
description: Review a GitHub PR following Portico's recommended sequence.
instructions: SKILL.md

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

### Manifest fields

| Field | Required | Description |
|---|---|---|
| `id` | yes | Lowercase, dotted namespace. Pattern: `^[a-z0-9][a-z0-9._-]*[a-z0-9]$`. Must equal `{namespace}.{name}`. |
| `title` | yes | Human-readable display name. |
| `version` | yes | Semver (`major.minor.patch[-prerelease][+build]`). |
| `spec` | yes | Must be the literal string `skills/v1`. |
| `description` | no | One-line summary shown in Console and `skill://_index`. |
| `instructions` | yes | Relative path to the pack's primary instructions file (typically `SKILL.md`). No absolute paths. |
| `resources` | no | Additional files served as `skill://` resources. Relative paths only. |
| `prompts` | no | Prompt template files. Each becomes a namespaced MCP prompt. |
| `binding` | yes | Tool, policy, UI, and entitlement declarations (see below). |

### Binding

`binding` is where the pack integrates with the live Portico gateway. All sub-fields are optional within the object, but the object itself must be present.

**`server_dependencies`** — the MCP server IDs the pack depends on. These are logical identifiers (matching `server.id` in the gateway config), not hostnames. A missing server causes a validation warning; the pack still loads but reports missing tools.

**`required_tools`** — tools that must be available. Each entry follows the `{server}.{tool}` pattern (e.g. `github.get_pull_request`). If the tool's server is absent from the tenant's registry at load time, the pack reports an error in the `skill://_index` `missing_tools` field and may not function correctly.

**`optional_tools`** — tools the pack can use if present, but whose absence does not block the skill. Missing optional tools produce a warning, not an error.

**`policy`** — approval and risk-classification hints consumed by the policy engine:

```yaml
policy:
  requires_approval:
    - github.create_review_comment      # always routed through the approval flow
  risk_classes:
    github.create_review_comment: external_side_effect
    postgres.run_sql: read
```

Valid risk class values: `read`, `write`, `destructive`, `external_side_effect`, `sensitive_read`. See [Approvals](/concepts/approvals) for how these interact with the approval flow.

**`ui`** — an optional MCP App resource the pack surfaces inside supporting MCP clients:

```yaml
ui:
  resource_uri: ui://github/code-review-panel.html
```

The `resource_uri` must begin with `ui://`. The server registered for `github` must serve this resource via `resources/read`.

**`entitlements`** — plan-gating for the pack. When `plans` is empty, the pack is available to all tenants regardless of plan. When non-empty, only tenants whose configured plan appears in the list can load the pack.

```yaml
entitlements:
  plans: [pro, enterprise]    # free-plan tenants cannot load this pack
```

## Writing SKILL.md

`SKILL.md` is the instructions file — the text an agent receives when the skill is activated. Write it for the agent, not for a human reader. Be explicit about the tool call sequence, output format expectations, and any tool calls that require human approval.

```markdown
# GitHub Code Review

You are a senior reviewer. Follow this sequence for every PR:

1. Fetch PR metadata with `github.get_pull_request`.
2. Read the diff with `github.get_pull_request_diff`.
3. For each non-trivial file, fetch the latest version with
   `github.get_file_contents` so you can quote a few lines of context.
4. Identify (a) correctness issues, (b) security issues,
   (c) maintainability issues, (d) test gaps.
5. Optionally post a comment with `github.create_review_comment`. This
   tool is destructive and requires approval; do not call it without
   user confirmation.

## Output style

- Group findings by severity: must-fix / should-fix / nit.
- Reference exact line numbers.
- Suggest concrete fixes — do not just point at problems.
```

## Writing prompt files

Prompt files live under `prompts/` and may include a YAML frontmatter block. The frontmatter declares the prompt's name, a description, and the arguments it accepts. The document body is a Go `text/template` rendered at `prompts/get` time.

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

The registered prompt name is `{skill.id}.{frontmatter.name}` — for example `github.code-review.review_pr`. When the frontmatter `name` field is absent, the filename without extension is used instead.

::: warning Template scope
Only leaf variable substitution is supported in prompt templates (<span v-pre>`{{.owner}}`</span>). The <span v-pre>`{{template}}`</span> and <span v-pre>`{{block}}`</span> directives are explicitly rejected at load time to prevent cross-file inclusion. Keep prompt templates self-contained.
:::

## Validating a pack

Before loading a pack into a running gateway, validate it with the CLI:

```bash
# validate a single pack
./bin/portico validate-skills ./examples/skills/github/code-review

# validate every pack under a tree
./bin/portico validate-skills ./examples/skills/...

# validate individual manifest files
./bin/portico validate-skills ./path/to/manifest.yaml
```

The validator runs both the JSON Schema 2020-12 check and the semantic checks (path existence, `requires_approval` subset check, ID format match). Output is one line per pack:

```
OK     github.code-review 0.1.0  (examples/skills/github/code-review)
OK     filesystem.search 0.1.0   (examples/skills/filesystem/search)
OK     linear.triage 0.1.0       (examples/skills/linear/triage)
OK     postgres.sql-analyst 0.1.0 (examples/skills/postgres/sql-analyst)
```

When errors are present:

```
ERROR  my-pack 0.1.0  (./skills/bad/my-pack)
        ERROR: schema: binding.required_tools[0]: does not match pattern "^[a-z0-9_-]+\.[a-z0-9_.-]+$"
WARN   my-pack 0.1.0  (./skills/bad/my-pack)
        WARN:  binding.optional_tools[0] "slack.post_message" server not in registry
```

The process exits 1 when any pack has errors. Warnings do not cause a non-zero exit. This makes the validator safe to use in CI:

```bash
./bin/portico validate-skills ./skills/... && echo "all packs OK"
```

## Loading packs at runtime

### LocalDir source

The simplest source is a local directory tree. Add a `skills` block to `portico.yaml`:

```yaml
skills:
  sources:
    - type: local
      path: ./skills                 # relative to the working directory
  enablement:
    default: opt-in                  # or "auto" to enable all entitled packs by default
  refresh_interval: 30s              # polling fallback; LocalDir uses fsnotify regardless
```

With `type: local`, the Skill Manager mounts a `LocalDir` driver over `path`, watches it with `fsnotify`, and hot-reloads any pack whose files change. A manifest edit that fails validation leaves the previous version active — a typo cannot take down a live skill.

### Git source

To pull packs from a Git repository:

```yaml
skills:
  sources:
    - type: git
      config:
        url: https://github.com/example/skill-packs.git
        branch: main
        subdir_glob: packs/*/*        # optional: restrict which subdirs are discovered
        refresh_interval: 5m
        credential_ref: github-token  # vault secret name (optional)
```

### HTTP feed source

To pull packs from a hosted feed:

```yaml
skills:
  sources:
    - type: http
      config:
        feed_url: https://skills.example.com/feed.json
        refresh_interval: 10m
        credential_ref: feed-api-key  # vault secret name (optional)
```

### Authored source

Operators can also compose packs directly in the Console and publish them through the `/api/authored-skills` REST surface. Published authored packs flow through the same loader pipeline as file-based packs. No additional configuration is required; the authored source is always active for each tenant.

See [Skill Sources](/concepts/skill-sources) for the full driver reference.

## Per-tenant entitlements

Tenants restrict which packs are visible using a glob list in `entitlements.skills`. This is evaluated against the pack's `id`:

```yaml
tenants:
  - id: acme
    plan: enterprise
    entitlements:
      skills: ["*"]          # all entitled packs visible
  - id: beta
    plan: pro
    entitlements:
      skills: ["github.*"]   # only packs whose id begins with "github."
```

A tenant on a `free` plan cannot load `github.code-review` because that pack declares `entitlements.plans: [pro, enterprise]`. The entitlement check is additive: both the tenant glob and the pack's plan list must pass.

## Enabling packs

Packs are not automatically visible to sessions. The `enablement.default` config key controls the fallback:

- `opt-in` (default) — packs must be explicitly enabled per-tenant or per-session before they appear in `resources/list` and `prompts/list`.
- `auto` — every entitled pack is enabled by default; operators call disable to exclude specific packs.

**Tenant-wide enable/disable** (affects all sessions that have no per-session override):

```http
POST /v1/skills/github.code-review/enable
POST /v1/skills/github.code-review/disable
```

**Per-session enable/disable** (overrides the tenant default for one session):

```http
POST /v1/sessions/{session_id}/skills/enable
Content-Type: application/json

{"skill_id": "github.code-review"}
```

Resolution order: per-session rule → per-tenant rule → `enablement.default`.

## Consuming packs via MCP

Once a pack is loaded and enabled, an MCP client interacting with Portico sees it through standard MCP primitives.

### Discover loaded packs

Read the index resource to get the full catalog, including missing-tools status and enablement state:

```jsonc
// resources/read  {"uri": "skill://_index"}
{
  "version": 1,
  "tenant_id": "acme",
  "generated_at": "2026-06-25T10:00:00Z",
  "skills": [
    {
      "id": "github.code-review",
      "version": "0.1.0",
      "title": "GitHub Code Review",
      "enabled_for_session": true,
      "enabled_for_tenant": true,
      "missing_tools": [],
      "manifest_uri": "skill://github/code-review/manifest.yaml",
      "instructions_uri": "skill://github/code-review/SKILL.md"
    }
  ]
}
```

### Read the instructions

```jsonc
// resources/read  {"uri": "skill://github/code-review/SKILL.md"}
// returns the SKILL.md content as text/markdown
```

### Call a prompt

```jsonc
// prompts/get  {"name": "github.code-review.review_pr", "arguments": {"owner": "acme", "repo": "api", "pr_number": "42"}}
// returns the rendered prompt body as a user message
```

### List all skill resources

`resources/list` returns every `skill://` URI for packs that are enabled for the current session, interleaved with resources from downstream MCP servers. The `skill://` prefix makes them trivially distinguishable.

## Reference packs

Four reference packs ship in `examples/skills/` and pass `portico validate-skills` out of the box:

| Pack ID | Servers | Notable policy |
|---|---|---|
| `github.code-review` | `github` | `github.create_review_comment` requires approval, `external_side_effect` |
| `postgres.sql-analyst` | `postgres` | `postgres.run_sql` risk class `read`; no approval required |
| `linear.triage` | `linear` | `linear.update_issue` requires approval, `external_side_effect` |
| `filesystem.search` | `fs` | Read-only; no approval; available to all plans |

Use these as templates. The directory structure, frontmatter format, and binding patterns they demonstrate represent the conventions the loader and runtime expect.

## Related

- [Skill Packs](/concepts/skill-packs) — architecture, catalog, hot-reload, and the `skill://_index` spec
- [Skill Sources](/concepts/skill-sources) — full driver reference (LocalDir, Git, HTTP, Authored)
- [Approvals](/concepts/approvals) — how `requires_approval` and `risk_classes` wire into the approval flow
- [Agent Profiles](/concepts/agent-profiles) — restricting which packs an agent consumer may activate via `allowed_skills`
- [Reference: CLI](/reference/cli) — full `validate-skills` flag reference
- [Reference: Configuration](/reference/configuration) — complete `skills` config block schema
