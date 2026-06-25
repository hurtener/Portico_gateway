# Console

The Portico operator Console is a SvelteKit single-page application compiled to static assets and embedded directly into the Go binary at build time. It is served by the same HTTP listener that handles MCP traffic and REST API calls — one process, one port, one artifact. There is no separate Node.js server, no proxy, and no additional infrastructure to run.

## How the Console is embedded

The SvelteKit project lives in `web/console/`. Its build output (`web/console/build/`) is compiled by `npm run build` during CI before `go build` runs. A single Go file in the same directory exposes the output as an `embed.FS`:

```go
// web/console/embed.go
//go:embed all:build
var Build embed.FS
```

The UI handler in `internal/server/ui/handlers.go` reads that `embed.FS` and mounts a file-serving handler at the root of the chi router. Requests for known API prefixes (`/v1/`, `/mcp`, `/healthz`, `/readyz`) pass through to the REST and MCP routes. Every other path falls back to `index.html` so SvelteKit's client-side router resolves it in the browser.

Hashed assets under `/_app/immutable/` are given aggressive cache headers (`max-age=31536000, immutable`). The `index.html` root is served with `Cache-Control: no-cache`. If the build directory is empty — because `npm run build` has not run — the handler renders a minimal placeholder page that tells the operator how to populate it, rather than crashing.

The SvelteKit project uses `@sveltejs/adapter-static` with `fallback: 'index.html'`. SSR is off; the Console is a fully client-side SPA.

```js
// web/console/svelte.config.js (excerpt)
adapter: adapter({
  pages: 'build',
  assets: 'build',
  fallback: 'index.html',
  strict: true
})
```

## Layout and shell

Every page shares the same shell layout defined in `web/console/src/routes/+layout.svelte`: a collapsible `Sidebar`, a `TopBar`, a fluid content area, a `Toaster` for non-blocking feedback, and a `CommandPalette` reachable via `Ctrl+K` (or `Cmd+K` on macOS). Sidebar collapse is toggled with `Ctrl+B` / `Cmd+B`.

The sidebar polls `/healthz` and `/readyz` every 30 seconds and surfaces a compact status indicator at the bottom showing whether the gateway and its readiness checks are healthy. The version number injected at build time appears alongside that indicator.

## Navigation sections

The sidebar organises the Console into seven sections.

### Catalog

The catalog section covers MCP-facing resources.

| Route | Purpose |
|---|---|
| `/servers` | List, create, edit, enable/disable, and restart downstream MCP servers. Detail pages show live health, running process instances, capability counts (tools, resources, prompts, apps), skill attachment, and an activity log of recent changes. |
| `/resources` | Browse the aggregated resource list and templates across all registered servers, sourced from the latest catalog snapshot. |
| `/prompts` | Browse server-contributed prompts and invoke them interactively to preview rendered messages. |
| `/apps` | Browse MCP App entries (`ui://` resources) discovered during catalog enumeration, with their upstream URI and the server that published them. |
| `/skills` | Enable and disable Skill Packs for the current tenant; inspect each skill's required tools, optional tools, and attached assets; access the authored-skill editor. |

The Server detail page (`/servers/[id]`) surfaces the full spec — transport type, command, arguments, environment, health and lifecycle settings, auth strategy, credential reference — with inline forms for every field. Changes take effect via hot reload; no binary restart is required. See [MCP Registry](/concepts/mcp-registry) for the server lifecycle model.

### LLM

The LLM section administers the OpenAI-compatible LLM gateway.

| Route | Purpose |
|---|---|
| `/llm/providers` | Manage LLM provider records and their API key credentials. |
| `/llm/models` | Create model aliases that map an operator-chosen name to a provider and upstream model identifier, with optional default parameters and capability tags. |
| `/llm/quotas` | Set tenant-level rate limits (`requests_per_minute`, `tokens_per_minute`, `tokens_per_day`, `cost_usd_per_day`). |
| `/llm/cost` | Daily cost breakdown by model alias, with a summary row for a selectable date range. Unit prices are editable. |
| `/llm/health` | Live health status for every registered provider: whether it is reachable and returning valid responses. |
| `/llm/sessions` | List LLM chat sessions with per-session transcripts. |

### Governance

The governance section covers consumer identity and spend control introduced with Virtual Keys and hierarchical budgets.

| Route | Purpose |
|---|---|
| `/governance/customers` | Manage customer records (top of the hierarchy). |
| `/governance/teams` | Manage teams nested under customers. |
| `/governance/virtual-keys` | Create, rotate, and revoke Virtual Keys; view per-key budget headroom. The one-time secret token is displayed only at creation or rotation. |
| `/governance/budgets` | Define budget rules scoped to a Virtual Key, team, customer, or tenant. Metrics: requests, tokens, or cost (USD). Periods: per-minute through per-year, rolling or calendar-aligned. |
| `/governance/cache` | Inspect the semantic cache configuration and live statistics (entries, hit rate); manually invalidate by alias or scope. |

See [Virtual Keys](/concepts/virtual-keys) and [Hierarchical Budgets](/concepts/hierarchical-budgets) for the concepts behind these surfaces.

### Operations

The operations section covers runtime observability and control.

| Route | Purpose |
|---|---|
| `/agents` | Agent Profile list; links to create and manage profiles. |
| `/sessions` | MCP session list with per-session inspector: span waterfall, audit events, policy decisions, drift events, and snapshot state at that point in time. Bundles can be exported as a gzipped archive and re-imported for offline replay. |
| `/approvals` | Pending and decided tool-call approval requests. Operators approve or deny from this screen; the underlying flow uses MCP `elicitation/create`. |
| `/policy` | Structured policy rule editor with risk-class assignment, a dry-run evaluator that shows which rules matched a synthetic tool call and the final action, and a change-history log. A raw YAML toggle round-trips canonically with the form. |
| `/audit` | Full-text searchable audit event log with filtering by type, session, and time range. |
| `/snapshots` | Catalog snapshot list; snapshot detail with per-server capability counts; side-by-side diff between any two snapshots showing added, removed, and modified tools. |
| `/playground` | Interactive tool-call and prompt workbench. See [Playground](/concepts/playground). |
| `/observability/code-mode` | Code Mode execution log with per-execution status, tool call counts, and estimated token savings. See [Code Mode](/concepts/code-mode). |

### Admin

| Route | Purpose |
|---|---|
| `/admin/secrets` | Vault entry CRUD. Metadata (name, version, timestamps) is listed; plaintext values are never shown on the list. A reveal-on-demand flow issues a one-shot token gated by a confirmation step, and the reveal is audit-logged with the operator's identity. Secret rotation re-encrypts in place. |
| `/admin/tenants` | Tenant CRUD (admin scope only). Fields include display name, plan tier, runtime mode, concurrent session limit, request-rate limit, audit retention window, and the JWT issuer and JWKS URL the gateway uses to validate that tenant's tokens. |

### A2A

| Route | Purpose |
|---|---|
| `/a2a/peers` | Register and manage A2A peer agents. Each peer record holds an endpoint URL, an optional egress credential reference, and the agent card fetched automatically via the A2A handshake. See [A2A](/concepts/a2a). |

## Design token system

Every visual property in the Console derives from CSS custom properties declared in a single file: `web/console/src/lib/tokens.css`. Raw color literals, spacing values, font sizes, radii, or shadows in `.svelte` files are forbidden by project policy; all values must come from token references.

The token file defines two modes: light (`:root`) and dark (`[data-theme="dark"]`). The `[data-theme]` attribute is set synchronously on `<html>` before the first paint — via a small inline script in `app.html` — to eliminate theme-flash on load. The user's preference is persisted in `localStorage` and overrides the system default when set. Switching modes is a single CSS-variable swap with no per-component theme conditionals.

The taxonomy covers:

```css
/* web/console/src/lib/tokens.css — surface colors (light mode excerpt) */
--color-bg-canvas:    #f6f4ef;   /* outermost page background */
--color-bg-surface:   #fbfaf7;   /* primary content surface */
--color-bg-elevated:  #ffffff;   /* modal, popover, raised panel */
--color-bg-subtle:    #f1eee8;   /* row hover, inset areas */

/* Brand accent — muted teal */
--color-accent-primary: #2d6f73;

/* Sidebar — dark architectural slab */
--color-bg-sidebar: #102d31;

/* Layout constants */
--layout-sidebar-width:           208px;
--layout-sidebar-width-collapsed:  64px;
--layout-topbar-height:            56px;
```

The full set covers borders, text hierarchy (primary / secondary / tertiary / muted), icons, semantic status colors (success / warning / danger / info), spacing on a 4 px base with an 8 px rhythm, radii from `--radius-xs` (6 px) through `--radius-pill` (999 px), shadows, motion durations, and z-index scale.

Typography is self-hosted from `web/console/static/fonts/` using variable-font packages (`@fontsource-variable/inter`, `@fontsource-variable/jetbrains-mono`, `@fontsource-variable/newsreader`). No requests are made to external CDNs at runtime.

## Component library

The Console builds its UI from a set of primitives in `web/console/src/lib/components/`. These are authored components, not wrappers over an external framework, chosen because they integrate directly with the token system.

Key primitives:

- **Table** — sticky header, hover emphasis, monospace ID column, sortable columns, empty-state slot.
- **Button** — primary / secondary / ghost / subtle / destructive variants at small / medium / large sizes, with leading/trailing icon slots and loading state.
- **Input, Select, Textarea, Toggle, Checkbox, RadioGroup** — form controls that auto-generate `id` attributes when a `label` prop is set, ensuring `getByLabel` accessibility queries and screen-reader labels work without extra boilerplate.
- **Modal, Drawer, Popover, Dropdown** — overlay primitives. `Esc` closes all of them. Focus is trapped inside modals.
- **CodeBlock** — syntax-highlighted code display for JSON, YAML, and shell output.
- **EmptyState** — consistent empty-list and error placeholder following the architectural illustration motif.
- **Badge** — semantic tinting for status values (success / warning / danger / info / neutral) and risk classes.
- **Skeleton** — loading placeholder that matches the shape of the content it will replace.
- **CommandPalette** — keyboard-driven navigation across all Console routes, triggered by `Ctrl+K` / `Cmd+K`.

Icons are sourced exclusively from `lucide-svelte`, tree-shaken at build time. The import surface is consolidated through `web/console/src/lib/icons.ts`; mixing icon families in `.svelte` files is not permitted.

## Typed API client

All data fetching in the Console flows through `web/console/src/lib/api.ts`. Hand-rolled `fetch` calls in `.svelte` components are not allowed. The file exports a single `api` object with one typed function per REST endpoint:

```typescript
// web/console/src/lib/api.ts (excerpt)
api.listServers()                         // GET /v1/servers
api.upsertServer(spec)                    // POST /v1/servers
api.putServer(id, spec)                   // PUT /v1/servers/{id}
api.restartServer(id, reason)             // POST /api/servers/{id}/restart
api.listAgentProfiles()                   // GET /api/agent-profiles
api.createAgentProfile(p)                // POST /api/agent-profiles
api.listA2APeers()                        // GET /api/a2a/peers
api.createVirtualKey(vk)                  // POST /api/governance/virtual-keys
api.createBudget(b)                       // POST /api/governance/budgets
api.listLLMProviders()                    // GET /api/llm/providers
api.dryRunPolicy(call, rules)             // POST /api/policy/dry-run
api.getSessionBundle(sid)                 // GET /api/sessions/{sid}/bundle
api.exportSessionBundle(sid)              // POST /api/sessions/{sid}/export → Blob
```

The client resolves the base URL from `window.location.origin` in the browser (same-origin, no hardcoded host), and falls back to `http://127.0.0.1:8080` for server-side contexts. Every request sends `credentials: same-origin` and an `Accept: application/json` header.

Error responses are parsed from the server's flat error envelope (`{ error, message, details }`) and surfaced as `HTTPError` instances with `status`, `code`, and `detail` fields. The helper `isFeatureUnavailable(e)` returns true for 404, 405, and 501 responses so pages can render a calm "not configured" empty state rather than a raw error when an endpoint is not wired in the current build.

The TypeScript interfaces in `api.ts` are the Console's type contract for every resource it manages. `svelte-check` catches mismatches between the interface definitions and their usage across components at compile time, before the binary is built.

## Development workflow

Frontend developers run the Vite dev server (`npm run dev` in `web/console/`) while the Go binary handles API requests. The Vite config proxies `/v1/`, `/mcp`, `/healthz`, and `/readyz` to `http://127.0.0.1:8080`:

```typescript
// web/console/vite.config.ts (excerpt)
server: {
  port: 5173,
  proxy: {
    '/v1':     'http://127.0.0.1:8080',
    '/healthz': 'http://127.0.0.1:8080',
    '/mcp':    'http://127.0.0.1:8080'
  }
}
```

This means the Go binary must be running (`./bin/portico dev`) for the Console to have live data during development. No separate API stub or mock server is needed; the proxy keeps the development surface identical to production.

For production and CI, `npm ci && npm run build` in `web/console/` populates the `build/` directory, which `go build` then embeds. The `postbuild` script writes a `.gitkeep` so the `//go:embed` directive succeeds even in repositories where the build has never run. The `build/` directory itself is gitignored; CI is the canonical place where the full artifact is assembled.

## CI integration

The frontend CI job runs four commands in sequence before the Go build:

```bash
npm ci
npm run check     # svelte-check: type errors block the build
npm run lint      # prettier + eslint
npm run build     # produces web/console/build/
```

End-to-end tests run via Playwright (`npm run e2e`), which boots a Chromium browser against the real `./bin/portico dev` server — the same binary surface that the Go smoke tests exercise. Every operator-facing flow is covered by a `.spec.ts` in `web/console/tests/`. These tests use `getByLabel` queries to assert that form fields are accessible, and navigate the actual SPA routing, catching embed and SPA-fallback drift alongside functional regressions.

::: info Permission model
Write operations throughout the Console — server creation, secret rotation, tenant configuration, policy rule changes — require a JWT with the corresponding write scope (`servers:write`, `secrets:write`, `policy:write`, `tenants:admin`). Read-only JWTs see write affordances as disabled. The API enforces the same scope checks server-side regardless of what the UI presents.
:::

## Related

- [Console Tour](/getting-started/console-tour) — step-by-step walkthrough of the Console for new operators
- [Playground](/concepts/playground) — interactive MCP workbench embedded in the Console
- [Agent Profiles](/concepts/agent-profiles) — the agent identity and entitlement model managed from `/agents`
- [MCP Registry](/concepts/mcp-registry) — server registration and lifecycle backing the Servers section
- [Virtual Keys](/concepts/virtual-keys) — consumer authentication tokens managed from the Governance section
- [Hierarchical Budgets](/concepts/hierarchical-budgets) — spend control across tenant, customer, team, and key scopes
- [Policy](/concepts/policy) — the rule engine behind the policy editor
- [Credentials Vault](/concepts/credentials-vault) — the secret storage layer exposed through the Secrets section
- [A2A](/concepts/a2a) — A2A peer protocol backing the Peers section
- [REST API Reference](/reference/rest-api) — the full endpoint surface the Console client calls
