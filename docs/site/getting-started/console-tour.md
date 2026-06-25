# Console tour

Portico ships a full-featured operator Console alongside the gateway binary — no separate process, no proxy, no additional deployment step. The same HTTP listener that serves the REST API and the MCP northbound endpoint also serves the Console at the root path (default `http://localhost:8080/`). This page walks through every major section so you can orient quickly and start managing your deployment.

## How the Console is served

The Console is a SvelteKit SPA compiled with `@sveltejs/adapter-static` to `web/console/build/`. During the Go build, the directory is embedded into the binary via a single directive in `web/console/embed.go`:

```go
//go:embed all:build
var Build embed.FS
```

The `internal/server/ui` package mounts this `embed.FS` on the chi router. Requests for `/_app/*` (hashed SvelteKit assets) are served verbatim; requests for any other path fall back to `index.html` so SvelteKit's client-side router can resolve them. No file-system access is required at runtime.

Because the Console shares the HTTP listener it also inherits the tenant context, authentication middleware, and JWT validation that applies to every REST request. A read-only JWT produces a read-only Console: write affordances are disabled and labeled; the API rejects unauthorized writes independently.

::: tip Dev mode
`./bin/portico dev` starts the gateway on `127.0.0.1:8080` with a pre-configured dev tenant and no JWT required. Open `http://localhost:8080/` immediately after boot to reach the Console.
:::

See [dev mode](/getting-started/dev-mode) and [installation](/getting-started/installation) for startup instructions.

---

## Global navigation

### Shell layout

Every page shares a three-part shell:

- **Sidebar** (collapsible, left): primary navigation grouped into sections — MCP, Skills, Governance, LLM, A2A, Admin.
- **Top bar**: tenant indicator, current user identity, and global actions.
- **Toast region**: operation results and background-event notifications.

Keyboard shortcuts are available regardless of which page is focused:

| Shortcut | Action |
|---|---|
| `Cmd/Ctrl + K` | Open the command palette (fuzzy search across all routes and resources) |
| `Cmd/Ctrl + B` | Toggle the sidebar |

### Command palette

The command palette (`Cmd/Ctrl+K`) lets you jump directly to any page or perform common actions without navigating through the sidebar. It is especially useful when managing many servers, skills, or policy rules.

### Page anatomy

Most list pages share the same composition:

1. **Page header** — title, primary action (usually "+ Add"), and a refresh button.
2. **Metric strip** — four KPI tiles giving a live count summary relevant to the section.
3. **Filter chip bar** — quick filters as chips (by status, type, etc.) plus dropdown refiners and a search box.
4. **Table** — sortable list with status badges; clicking a row opens the Inspector.
5. **Inspector rail** — a 304 px sticky right rail with tabs showing details, sub-resources, and write forms for the selected row. The table narrows to accommodate it on screens wider than 1280 px.

---

## MCP surface

### Servers (`/servers`)

The Servers page is the primary operational view for the MCP layer. The metric strip shows totals for registered servers, aggregate capabilities (tools + resources + prompts), approval-gated policy rules, and servers in a drift or review state.

Filter chips let you narrow by status:

| Chip | Meaning |
|---|---|
| `all` | All registered servers |
| `online` | Status is `ready`, `running`, or `healthy` |
| `offline` | Status is `crashed`, `error`, or `unhealthy` |
| `review` | Status is `starting` or `backoff` |
| `skills` | Servers with at least one attached skill |

Additional dropdowns filter by transport (`stdio` / `http`) and runtime mode (`shared_global`, `per_session`, `per_user`, `per_tenant`).

Selecting a row opens the Inspector with four tabs:

- **Overview** — transport, runtime mode, current status, capability counts, policy state, auth strategy, attached skill count.
- **Capabilities** — breakdown of tools, resources, prompts, and MCP Apps exposed by this server.
- **Skills** — count of skills attached to this server (detail drill-through on the full detail page).
- **More** — raw JSON of the server record for debugging.

The Inspector surfaces two inline actions: "View details" (navigates to `/servers/{id}`) and "Restart". The "+ Add" button in the page header navigates to the server registration form at `/servers/new`.

The `/servers/{id}` detail page provides five tabs — Overview, Logs (live SSE tail), Tools, Activity (audit-driven before/after diff history), and Configuration — along with Restart, Disable/Enable, and Delete actions in the header. Delete for a server with active sessions triggers the approval flow.

See [MCP northbound](/concepts/mcp-northbound), [MCP southbound](/concepts/mcp-southbound), and [register an MCP server](/guides/register-mcp-server) for the underlying mechanics.

### Sessions (`/sessions`)

The Sessions page aggregates session data from two substrate sources — catalog snapshots and audit events — both of which carry a `session_id`. For each unique session ID the page derives first-seen, last-seen, snapshot count, event count, tenant, and whether the session originated from the Playground (these carry a `psn_` prefix).

Selecting a row opens an Inspector showing the timeline summary and links to the related snapshots and audit log slice. A "Replay in Playground" CTA on session rows navigates to `/playground/sessions/{id}` where the session can be re-executed against its original snapshot.

### Resources (`/resources`) and Prompts (`/prompts`)

These pages browse the resources and prompt templates that the registered MCP servers expose. They reflect the live catalog snapshot; they are read-only surfaces — resources and prompts come from downstream servers and are not operator-created.

### MCP Apps (`/apps`)

MCP Apps are `ui://` resources embedded in downstream skill packs. The Apps page lists every indexed MCP App and cross-references it with the skills catalog to show which skill (if any) declares each app as its `binding.ui.resource_uri`. Apps not referenced by an installed skill appear as "unbound" — still reachable over MCP, but without a curated entry point.

The metric strip shows totals for all apps, bound apps, unbound apps, and the number of distinct servers that publish them.

See [skill packs](/concepts/skill-packs) for how MCP Apps are declared and served.

---

## Skills (`/skills`)

The Skills page lists every installed skill the gateway has resolved from its configured skill sources. The metric strip surfaces totals by status bucket. Filter chips cover `all`, `enabled`, `disabled`, `missing` (manifest resolved but source unreachable), and `withUI` (skill has a bound MCP App).

Bulk selection is available: check one or more rows to reveal a sticky action bar offering Enable, Disable, and Cancel operations. A pagination footer handles catalogs that grow into the hundreds.

Sub-routes:

| Route | Content |
|---|---|
| `/skills/sources` | Skill sources registered by the operator (local directory, Git, HTTP, OCI) |
| `/skills/authored` | Inline-authored skills created directly in the Console |
| `/skills/authored/new` | Skill authoring form |

See [skill packs](/concepts/skill-packs) and [skill sources](/concepts/skill-sources) for full detail, or [build a skill pack](/guides/build-skill-pack) for a hands-on guide.

---

## Playground (`/playground`)

The Playground is the interactive execution surface for the gateway. It lets an operator pick any tool, resource, or prompt from the live catalog, compose a call, execute it, and watch the streamed response alongside correlated trace, audit, policy, and drift data — all in one screen.

### Layout

The Playground uses a three-column layout:

**Left rail** (catalog browser, ~320 px): servers expand to show their tools (namespaced `<server>.<tool>`), followed by Resources, Prompts, and Skills (grouped by source: Local / Git / HTTP / Authored). A search box at the top filters all entries. Drift badges appear per item when the catalog snapshot has diverged.

**Centre** (composer and output): the upper portion renders a schema-driven form generated from the tool's JSON Schema (`tools/list` `inputSchema`). Required fields are marked; `oneOf`/`anyOf` surfaces as a tab strip; a "Raw JSON" tab is available for power users. Hitting "Run" streams the response in real time via SSE. The output panel shows the structured result with pretty-print and raw toggles.

**Right rail** (correlation, ~380 px): four tabs update live as the call executes:

| Tab | Content |
|---|---|
| Trace | OTel span tree for the call — root → southbound → tool-internal, with timings and attributes |
| Audit | Events emitted during the call, redacted per the audit policy |
| Policy | Evaluation tree showing which rules matched, which lost on priority, and the final risk class |
| Drift | `schema.drift` events that fired during the call (signals catalog has changed under the session) |

### Saved cases and replay (`/playground/cases`)

Operators can name and save any composed call as a test case. Cases are persisted per tenant and can be tagged and grouped. The Cases list at `/playground/cases` shows name, kind (tool call / resource read / prompt get), last run status, and tags.

Opening a case navigates to `/playground/cases/{id}`, which shows the payload and a run history. The "Replay" button re-executes the call. If the case was pinned to a specific catalog snapshot and that snapshot has since drifted, the replay surfaces a diff banner showing what changed; the call still executes against the live snapshot.

An arbitrary past session from the Sessions page can also be replayed at `/playground/sessions/{id}`.

### Code Mode playground (`/playground/code-mode`)

A dedicated surface for running and inspecting Code Mode scripts. See [code mode](/concepts/code-mode) for the runtime model.

See [use code mode](/guides/use-code-mode) for a practical walkthrough.

---

## Policy editor (`/policy`)

The Policy page is the primary surface for managing the gateway's tool-call evaluation rules.

The metric strip shows a rule mix breakdown (how many rules are set to allow, deny, or require approval). Filter chips narrow by action type. Selecting a rule opens the Inspector, which has two tabs:

**Editor tab**: structured form with fields for ID, priority, enabled flag, risk class (`low` / `medium` / `high` / `sensitive`), conditions (tool names, server names, tenant scope, argument expression, optional time range), and actions (allow / deny / require approval / log level override). A "Save" button in the Inspector header stages the change; a "Discard" button reverts. The engine picks up saved rules on the next tool call — no binary restart required.

**Dry-run tab**: accepts a synthetic tool-call shape (server, tool, args, tenant) and evaluates the current ruleset against it, rendering the same evaluation tree that appears in the Playground's Policy tab. This lets operators verify rule behavior before committing.

The "+ Add rule" button in the page header opens the Inspector with an empty draft rule on the editor tab.

See [policy](/concepts/policy) and [approvals](/concepts/approvals) for the enforcement model.

---

## Admin

### Tenants (`/admin/tenants`)

Visible only to operators whose JWT carries `tenants:admin` scope. Displays all tenants in a table with columns for ID, name, runtime mode, status, and per-minute request quota.

The detail page for each tenant (`/admin/tenants/{id}`) shows:

- JWT issuer and JWKS URL (with a copy button).
- Runtime mode and quota settings: `max_concurrent_sessions`, `max_requests_per_minute`, `audit_retention_days`.
- An Activity tab showing the last N changes with before/after diffs sourced from the audit store.

The create wizard at `/admin/tenants/new` walks through three steps: identity (ID, name), runtime (mode and quotas), and auth (JWT issuer and JWKS URL). A newly created tenant accepts JWTs immediately at the gateway.

See [multi-tenancy](/concepts/multi-tenancy) for the full isolation model.

### Secrets (`/admin/secrets`)

The Secrets page is the operator surface for the credentials vault. The list shows secret names, version numbers, and timestamps — never plaintext values. CRUD operations work through modal forms.

Revealing a value is a two-click flow: "Reveal" opens a re-confirm modal; confirming returns a one-shot reveal token; the value is displayed once and never again. Every reveal emits an audit event carrying the operator's identity.

The root-key rotation action (page header, behind an approval gate) re-encrypts every vault entry under the new key. The rotation runs transactionally; if any entry fails, the previous key remains active and a `vault.rotate_root.aborted` audit event is emitted.

See [credentials vault](/concepts/credentials-vault) and [OAuth token exchange](/concepts/oauth-token-exchange).

---

## Agent Profiles (`/agents`)

Agent Profiles are the gateway's consumer-binding primitive: one profile per logical agent that declares which MCP servers, tools, skills, and LLM model aliases it may use, along with the JWT subjects bound to it and any scope restrictions.

The list table shows profile name and counts for allowed servers, tools, skills, and models. Selecting a row opens the Inspector with a multi-section editor covering:

- **Basics** — name, description, enabled flag.
- **MCP allowlists** — explicit lists of allowed servers and tools (empty list = all).
- **Skills** — allowed skill IDs.
- **Models** — allowed LLM model aliases.
- **Scopes** — additional scope restrictions beyond the JWT's own claims.
- **Bindings** — JWT `sub` values or API key identifiers bound to this profile.

The "+" button creates a new profile inline in the Inspector.

::: info Default profile
There is no stored "default" profile row. The gateway synthesizes a permissive default in code when no profile matches an incoming request. The `/agents` list will never show a default profile entry.
:::

See [agent profiles](/concepts/agent-profiles) for the full data model and enforcement semantics.

---

## A2A Peers (`/a2a/peers`)

The A2A Peers page manages Portico's registry of Agent-to-Agent peer gateways. Each peer entry records:

- **Name** — a human-readable label.
- **Endpoint** — the peer's A2A listener URL.
- **Auth ref** — a vault secret reference used for egress authentication when forwarding tasks to this peer.
- **Enabled** — whether the gateway will route tasks to this peer.
- **Agent card** — the peer's `/.well-known/agent.json` card, fetched automatically on registration and periodically refreshed. The JSON is rendered in the Inspector as read-only; operators cannot edit the discovered card.

See [A2A](/concepts/a2a) and [A2A bridges](/concepts/a2a-bridges), and [set up an A2A peer](/guides/setup-a2a-peer).

---

## LLM screens

Portico exposes an OpenAI-compatible LLM gateway. The LLM section of the sidebar provides four operational screens:

### Providers (`/llm/providers`)

CRUD over the per-tenant LLM provider registry. Each provider entry specifies a driver name and its credentials (referenced from the vault). Multiple API keys per provider are supported with a weight field for load-balanced routing.

A segmented filter separates built-in drivers from the `custom_openai` slot, which accepts any OpenAI-compatible `base_url`. Provider preset templates pre-fill `base_url` for common OpenAI-compatible endpoints.

### Models (`/llm/models`)

CRUD over model aliases — the friendly names clients route to (for example `gpt-4o`). Each alias maps to a configured provider and the provider's own model ID, with optional default parameters (`temperature`, `max_tokens`, `top_p`) and capability tags (`chat`, `completion`, `embedding`, `vision`, `tools`, `streaming`).

### Health (`/llm/health`)

Live provider health dashboard — latency, error rate, and circuit-breaker state per configured provider.

### Cost and quotas (`/llm/cost`, `/llm/quotas`)

Cost monitoring aggregates token consumption and estimated spend per provider and model alias. The quotas page surfaces the per-tenant and per-virtual-key spend and request limits.

See [LLM gateway](/concepts/llm-gateway), [LLM providers](/concepts/llm-providers), and [LLM routing](/concepts/llm-routing).

---

## Governance

### Virtual Keys (`/governance/virtual-keys`)

Virtual Keys are `pk-portico-*`-prefixed API credentials that agents and applications present instead of broad provider tokens. They carry their own scope, provider/model/MCP server allowlists, an optional budget parent, and an optional Agent Profile binding.

**The full secret is shown exactly once** — in a non-dismissible modal immediately after creation or rotation. After dismissal the Console (and the API) never returns the secret again; only the `salt` + HMAC are stored.

Operators cannot edit a Virtual Key in place. The operations available are: create, view metadata, rotate (generates a new secret, returns it once), and revoke.

See [virtual keys](/concepts/virtual-keys).

### Budgets (`/governance/budgets`)

Budgets define spend and usage caps on any `(scope_kind, scope_id, metric, period)` tuple. The hierarchical enforcer evaluates budgets at four levels in order: Virtual Key → team → customer → tenant. All levels are checked in a pre-flight pass before the request is dispatched; a single atomic transaction debits all levels on completion.

`scope_kind` values: `vk`, `team`, `customer`, `tenant`.
`metric` values: `cost_usd`, `tokens`, `requests`.
`period` values: `1m`, `1h`, `1d`, `1w`, `1M`, `1Y`.

See [hierarchical budgets](/concepts/hierarchical-budgets).

### Semantic Cache (`/governance/cache`)

Shows the active cache configuration: driver, scope, TTL, and semantic similarity threshold. Live per-tenant statistics show hit rate and entry count. An invalidation form lets operators clear entries by scope: all entries for the tenant, entries for a specific model alias, or entries for a specific Virtual Key.

See [semantic cache](/concepts/semantic-cache).

### Teams and Customers (`/governance/teams`, `/governance/customers`)

Teams and customers are the organizational groupings that budgets attach to. Teams are internal groupings; customers represent external consumers with their own billing identity. Both map to the same hierarchical budget enforcer.

---

## Observability

### Audit log (`/audit`)

Read-only view into the structured audit event stream. Every write operation, policy decision, credential access, and approval action produces an audit event carrying `tenant_id`, `user_id`, a redacted before/after diff, and a trace ID for correlation. The table supports filtering by event type and time range.

See [audit](/concepts/audit).

### Snapshots (`/snapshots`)

Catalog snapshots are point-in-time captures of the tool, resource, and prompt surface the gateway exposes. The Snapshots page lists them with fingerprints and creation timestamps. Selecting two snapshots and choosing "Diff" navigates to `/snapshots/{a}/diff/{b}`, which renders a structured diff of the schema changes between the two points — the same mechanism the Playground uses to surface drift on replay.

See [catalog and sessions](/concepts/catalog-and-sessions) and [drift detection](/concepts/drift-detection).

### Approvals (`/approvals`)

The Approvals queue surfaces pending `elicitation/create` requests that destructive or policy-gated operations have emitted. Each entry shows the operation, the requesting actor, the tenant, and the approval timeout. Operators with the appropriate scope approve or deny directly from this page.

---

## Permission model

The Console reads the JWT scopes on boot and passes them to every page via a Svelte store. Write affordances (buttons, form submit actions) are disabled with an explanatory tooltip when the current JWT lacks the required scope. The API enforces the same scope check independently, so a manually crafted request from a read-only token still returns `403 permission_denied`.

Key write scopes:

| Scope | Governs |
|---|---|
| `servers:write` | Register, edit, restart, delete MCP servers |
| `policy:write` | Create, edit, delete policy rules |
| `secrets:write` | Create, update, rotate, delete vault secrets |
| `tenants:admin` | Create, edit, archive tenants |
| `playground:execute` | Execute tool calls from the Playground |
| `playground:save` | Save and delete Playground test cases |

---

## Related

- [Console concept](/concepts/console) — architecture and embedding model
- [Playground](/concepts/playground) — deeper coverage of the Playground's session lifecycle, snapshot binding, and drift detection
- [Agent Profiles](/concepts/agent-profiles) — full data model and enforcement semantics
- [Policy](/concepts/policy) — rule evaluation model and risk classes
- [Credentials vault](/concepts/credentials-vault) — vault architecture and reveal-token protocol
- [Virtual keys](/concepts/virtual-keys) — credential lifecycle, HMAC storage, and rotation
- [Hierarchical budgets](/concepts/hierarchical-budgets) — pre-check and atomic debit model
- [Multi-tenancy](/concepts/multi-tenancy) — tenant isolation guarantees
- [Register an MCP server](/guides/register-mcp-server) — step-by-step server registration
- [Set up an A2A peer](/guides/setup-a2a-peer) — peer registration and egress auth
