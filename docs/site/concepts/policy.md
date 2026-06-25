# Policy

Every tool call that enters Portico passes through the policy engine before it reaches the downstream MCP server. The engine runs a single, deterministic evaluation pass over the tenant's registered servers, active Skill Packs, and the SQL-backed ruleset. The result is a `Decision` struct that authorises or denies the call, assigns a risk class, and determines whether the call requires an operator approval before it can proceed.

This page describes the engine, the rule format, risk classes, the approval gate, and the Console editor with its built-in dry-run evaluator.

---

## Evaluation order

`Engine.EvaluateToolCall` runs six checks in sequence. A failing step short-circuits the pass immediately; the engine does not continue to lower checks.

| Step | What is checked | Reason on denial |
|------|----------------|------------------|
| 1 | **Registry membership** — the qualified tool name resolves to a known server for this tenant | `tool_not_found` |
| 2 | **Server enabled** — the server record's `enabled` flag is true | `server_disabled` |
| 3 | **Tenant denylist** — the qualified name does not match any glob in the tenant's `tool_deny_list` | `denied` |
| 4 | **Tenant allowlist** — when a non-empty `tool_allow_list` is configured, the name must match | `not_allowed` |
| 5 | **Risk class resolution** — Skill Pack override, then server default, then engine default (`write`) | — |
| 6 | **Approval gate** — risk class default or explicit Skill Pack flag determines `requires_approval` | — |

Steps 1–4 produce a deny with the corresponding reason string. Steps 5–6 annotate an allowed decision; the approval gate does not deny — it marks the call as requiring an out-of-band confirmation before the downstream tool executes.

A `Decision` carries all annotated fields back to the dispatcher:

```go
type Decision struct {
    Allow            bool
    Reason           string
    Tool             string
    ServerID         string
    SkillID          string
    RiskClass        string
    RequiresApproval bool
    ApprovalTimeout  time.Duration
    Notes            []string
}
```

The dispatcher renders a `policy_denied` JSON-RPC error (`-32003`) when `Allow` is false, carrying the `Reason` string in the structured payload.

---

## Risk classes

Risk classes drive the default approval requirement. The engine ships five canonical values:

| Class | Approval on by default | Description |
|-------|----------------------|-------------|
| `read` | No | Reads tenant-visible data, no side effects |
| `write` | No (policy-dependent) | Mutates tenant data within the integration |
| `sensitive_read` | **Yes** | Returns sensitive payloads — PII, financial, health |
| `external_side_effect` | **Yes** | Emits changes outside Portico (posts a comment, opens a ticket) |
| `destructive` | **Yes** | Removes or irreversibly mutates state |

The engine falls back to `write` when a supplied class is absent or unrecognised, so a typo in configuration cannot silently bypass approval.

Risk class resolution follows a priority chain:

1. Per-tool override from the owning Skill Pack's `binding.policy.risk_classes` map.
2. `auth.default_risk_class` on the server spec.
3. Engine default (`write`).

---

## Rules

Operators extend the default risk-class and allow/deny behaviour by authoring a **ruleset** — an ordered list of rules persisted in the `tenant_policy_rules` table. The engine evaluates rules in ascending priority order; lower numbers win. A rule with no conditions matches every call for the tenant.

### Rule shape

```json
{
  "id": "no-external-calls-after-hours",
  "priority": 10,
  "enabled": true,
  "risk_class": "external_side_effect",
  "conditions": {
    "match": {
      "tools": ["github.*", "jira.*"],
      "time_range": { "from": "18:00", "to": "08:00" }
    }
  },
  "actions": {
    "deny": true,
    "log_level": "warn"
  },
  "notes": "Block external integrations outside business hours."
}
```

Every rule carries:

- **`id`** — a stable, unique slug within the tenant. Required.
- **`priority`** — lower wins. Rules with the same priority are broken by `id` for stability.
- **`enabled`** — disabled rules are skipped by the engine and the dry-run evaluator.
- **`risk_class`** — one of the five canonical values. Validated on write; unknown values are rejected.
- **`conditions.match`** — zero or more matchers. An empty match block matches all calls.
- **`actions`** — the outcome; exactly one of `allow`, `deny`, `require_approval`, or `require_profile_membership` must be true when a verdict is needed. Annotation-only rules (setting only `log_level` or `annotate`) are allowed.
- **`notes`** and **`updated_at`/`updated_by`** — tracked for the Console activity log.

### Conditions

The `match` block supports the following fields. All are optional; omitted fields do not constrain the rule.

| Field | Type | Behaviour |
|-------|------|-----------|
| `tools` | `[]string` | Glob patterns matched against the fully-qualified tool name (e.g. `"github.*"`, `"jira.create_issue"`). Uses `path.Match` semantics. |
| `servers` | `[]string` | Exact server IDs. |
| `tenants` | `[]string` | Exact tenant IDs. Useful in admin-scoped rulesets. |
| `args_expr` | `string` | V1: comma-separated `key=value` pairs that must all hold in the call arguments. |
| `time_range` | `{from, to}` | UTC `HH:MM` window. Midnight-wrapping ranges (e.g. `22:00..06:00`) are supported. |
| `profiles` | `[]string` | Agent Profile IDs or names. Fires when the caller's resolved profile matches. |
| `profile_includes_server` | `string` | Fires when the caller's profile surface includes the named server. |
| `profile_includes_alias` | `string` | Fires when the caller's profile surface includes the named model alias. |
| `vk_ids` | `[]string` | Virtual Key IDs. Only fires when the call is authenticated via a Virtual Key. |
| `vk_scopes` | `[]string` | Fires when the Virtual Key carries any of the listed scopes. |
| `vk_team` / `vk_customer` | `string` | Fires when the Virtual Key's budget parent matches. |
| `cache_would_hit` | `bool` | Fires when the LLM semantic cache would (or would not) serve the request. |
| `budget_headroom_below_pct` | `float64` | Fires when the lowest budget hierarchy headroom falls below this percentage (0–100). |

### Actions

A rule's `actions` block controls both the verdict and any annotations that stack on top.

| Field | Mutually exclusive verdict | Description |
|-------|-----------------------------|-------------|
| `allow` | Yes | Explicitly allow; short-circuits lower-priority rules. |
| `deny` | Yes | Deny the call; engine returns `decision.Reason = "denied"`. |
| `require_approval` | Yes | Mark the call for the approval flow; allow proceeds only after a grant. |
| `require_profile_membership` | Yes | Deny unless the caller's resolved Agent Profile ID or name is in the list. |

Annotation actions compose with the winning verdict and do not conflict with each other:

| Field | Description |
|-------|-------------|
| `annotate` | Override the resolved risk class in the `Decision`. |
| `log_level` | Override the slog severity emitted for this call (`debug`, `info`, `warn`, `error`). |
| `deny_on_cache_miss` | Deny the call when the semantic cache does not serve it (force cache-only operation for a route). |
| `force_cache_bypass` | Skip the semantic cache for this call regardless of similarity score. |
| `clamp_to_customer_budget` | Cap effective budget headroom to the customer level, ignoring looser Virtual Key or team allowances. |

::: warning Approval is sticky
`require_approval` in any matched rule adds approval to the final outcome even if a higher-priority `allow` rule already won. The annotation actions stack; only the primary verdict (allow/deny/require_approval/require_profile_membership) is winner-takes-all.
:::

---

## Allow and deny lists

Beyond structured rules, the engine supports flat glob lists on the tenant's `Policy` object:

```go
type Policy struct {
    ToolAllowlist   []string      // glob patterns; empty = allow all
    ToolDenylist    []string      // glob patterns
    ApprovalTimeout time.Duration // per-tenant override
}
```

These resolve before the structured ruleset (steps 3 and 4 of the evaluation order). The denylist always takes precedence over the allowlist.

Pattern syntax is `path.Match`. Common patterns:

```
github.*            # all tools on the github server
github.delete_*     # destructive github tools
*.export            # any tool named "export" on any server
```

---

## Approval requirement

When a `Decision` has `RequiresApproval: true`, the dispatcher hands control to `approval.Flow.Run` before dispatching the downstream tool call. The approval flow:

1. **Persists a pending row** in the `approvals` table (`tenant_id`, `tool`, `risk_class`, `args_summary`, `args_hash`, `expires_at`).
2. **Emits** an `approval.pending` audit event.
3. **Elicits a decision** from the connected MCP client if it advertised `elicitation` capability, by sending an `elicitation/create` server-initiated request with a structured `{"approve": bool}` schema and a human-readable prompt that includes the tool name, risk class, and an arguments summary.
4. **Falls back** to a structured JSON-RPC error (`-32001 approval_required`) when the client does not support elicitation, carrying the `approval_id` in the error payload. The pending row remains open for manual resolution via the Console or the REST API.

Approval outcomes:

| Status | Meaning |
|--------|---------|
| `approved` | The operator confirmed; the downstream tool call proceeds. |
| `denied` | The operator rejected; the call returns `policy_denied` (`-32003`). |
| `expired` | The approval timed out; the call returns `approval_timeout`. |

The default timeout is five minutes. Operators may override it per-tenant via the `approval_timeout` field on the tenant's `Policy` object.

### Replay window

Once an approval is granted, the Code Mode runtime and other resumable callers may re-dispatch the identical call without prompting again. The replay gate requires a strict three-way match: same `approval_id`, same tool, and a byte-exact SHA-256 hash of the full arguments. Any mismatch fails closed to a fresh pending flow. An approval can never be replayed onto different arguments or a different tool.

### Approval bypass is not a feature

A tool annotated `requires_approval` — whether by its risk class, a Skill Pack binding, or a policy rule — **always** enters the approval flow. There is no configuration flag, no scope, and no code path that skips the gate. An approval cached within the replay window is the only mechanism that shortens the flow, and its identity checks are intentionally strict. PRs that attempt to add a bypass are rejected on sight.

---

## Skill Pack policy hints

Skill Packs declare per-tool policy hints in `manifest.yaml` under `binding.policy`. The engine reads these at evaluation time; they apply only when the Skill Pack is active for the tenant and session.

```yaml
binding:
  required_tools:
    - github.create_pull_request
    - github.delete_branch
  policy:
    risk_classes:
      "github.delete_branch": destructive
      "github.create_pull_request": external_side_effect
    requires_approval:
      - github.delete_branch
```

`requires_approval` is a list of fully-qualified tool names. When any listed tool is called and the skill is active, `RequiresApproval` is set to `true` regardless of the risk-class default. This lets a Skill Pack author enforce approval for tools that would not otherwise require it — for example, a `write`-class tool that has domain-specific sensitivity.

`risk_classes` is a map from qualified tool name to one of the five canonical risk class strings. Skill Pack overrides take precedence over the server's `auth.default_risk_class`.

---

## Policy bundles and Agent Profiles

Each Agent Profile carries an optional `policy_bundle_ref` field. This is a reference identifier that the policy engine can use to load a curated ruleset associated with the profile's operational context.

```json
{
  "id": "prod-readonly-agents",
  "name": "Production Read-Only Agents",
  "allowed_mcp_servers": ["github", "datadog"],
  "policy_bundle_ref": "readonly-bundle",
  "scopes": ["tools:call", "resources:read"]
}
```

When a call resolves a profile with `policy_bundle_ref` set, the engine can apply that bundle's rules on top of the tenant's base ruleset. The `require_profile_membership` action and the `profiles` condition matcher work together with Agent Profile resolution so a rule can simultaneously restrict which tools are callable and which profiles are authorised to call them.

For full coverage of Agent Profiles, including binding principals to profiles and the default-profile fallback, see [/concepts/agent-profiles](/concepts/agent-profiles).

---

## REST API

The policy rules surface is served at `/api/policy/` and requires a `policy:write` scope for mutations. Read operations require only a valid tenant JWT.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/policy/rules` | Return the tenant's ruleset in priority order. |
| `PUT` | `/api/policy/rules` | Atomically replace the entire ruleset. All rules are validated before any write. |
| `POST` | `/api/policy/rules` | Append or upsert a single rule. |
| `PUT` | `/api/policy/rules/{id}` | Update a rule by ID. Path ID and body ID must match. |
| `DELETE` | `/api/policy/rules/{id}` | Delete a rule by ID. |
| `POST` | `/api/policy/dry-run` | Evaluate a synthetic call against the live (or a supplied) ruleset without side effects. |

Every mutation emits a `policy.rule_changed` or `policy.rule_deleted` audit event with `tenant_id`, `user_id`, `rule_id`, `priority`, and `risk_class`.

### Dry-run request shape

```jsonc
// POST /api/policy/dry-run
{
  "call": {
    "tenant_id": "acme",
    "server": "github",
    "tool": "github.delete_branch",
    "args": { "branch": "feature/old" }
  },
  // "rules" is optional — omit to evaluate against the live tenant ruleset
  "rules": {
    "rules": []
  }
}
```

Response:

```jsonc
{
  "matched_rules": [
    { "rule_id": "no-external-calls-after-hours", "priority": 10, "reason": "tool=github.delete_branch" }
  ],
  "losing_rules": [],
  "final_action": {
    "deny": true,
    "log_level": "warn"
  },
  "final_risk": "destructive"
}
```

`matched_rules` lists every rule that both fired and influenced the outcome (winning verdict plus sticky annotations). `losing_rules` lists rules that matched conditions but were overridden by a higher-priority rule. `final_action` collapses all matched rules into the effective verdict. `final_risk` is the resolved risk class after any `annotate` overrides.

---

## Console editor

The Console surfaces the policy engine at `/policy`. The page has two panes:

**Left pane — Ruleset editor.** Rules are presented as a sortable list with inline form fields for conditions and actions. A "Raw YAML" toggle switches to a text editor that accepts the same structure and round-trips byte-for-byte after canonicalisation. Changes are written immediately via `PUT /api/policy/rules/{id}` (single rule) or `PUT /api/policy/rules` (bulk replace). An Activity tab shows the most recent mutations sourced from the audit store, including who made each change and when.

**Right pane — Dry-run evaluator.** The operator fills in a tool call shape (tenant, server, tool name, optional args expression) and submits it. The pane calls `POST /api/policy/dry-run` against the current saved ruleset and renders the evaluation tree: which rules matched, which lost on priority, the final action, and the resolved risk class. The evaluator runs against the live ruleset, not the unsaved editor state — save first, then dry-run.

::: tip Hot reload
Every rule mutation takes effect on the next tool call dispatched by that tenant. The `RuleStore` publishes a `RuleChange` event on an internal channel; the engine re-reads the ruleset before the next evaluation. Running sessions do not need to reconnect, and a binary restart is not required.
:::

The policy editor requires the `policy:write` scope. Operators with read-only tokens see the ruleset and the dry-run pane but cannot save changes; write affordances are disabled with a tooltip explaining the required scope.

---

## Related

- [/concepts/approvals](/concepts/approvals) — approval flow mechanics: elicitation, fallback errors, manual resolution, the replay window.
- [/concepts/agent-profiles](/concepts/agent-profiles) — Agent Profiles and `policy_bundle_ref`; how the `require_profile_membership` action integrates with profile resolution.
- [/concepts/audit](/concepts/audit) — policy decision events, approval events, and the audit query API.
- [/concepts/skill-packs](/concepts/skill-packs) — authoring `binding.policy` blocks in Skill Pack manifests.
- [/concepts/virtual-keys](/concepts/virtual-keys) — Virtual Key scope and budget matchers used in policy conditions.
- [/reference/rest-api](/reference/rest-api) — full REST API reference including policy endpoints.
- [/guides/create-agent-profile](/guides/create-agent-profile) — step-by-step guide to creating Agent Profiles and attaching policy bundles.
