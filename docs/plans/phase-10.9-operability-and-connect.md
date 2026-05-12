# Phase 10.9 — Operability & Connect

> Close the "I added a server but I can't use it" gap. Adds the smallest backend endpoint needed to surface gateway-connection facts (bind, MCP path, auth mode, JWT requirements), a new `/connect` Console page that turns those facts into copy-paste client configs, a reshaped root landing that reads as setup+status (not just runtime telemetry), and a "Connect" tab on `/servers/[id]` that shows how to call a registered server's tools through the gateway.
>
> **Status: planned 2026-05-09, implementing now.** Four steps, end-to-end in one branch.

## Why this phase exists

After Phases 10.5 → 10.8 the Console *looks* like a control plane but doesn't *function* as one. Operator feedback: "we can declare servers, skills, etc. but it's not usable. how do i consume one server present in the gateway? cannot. except for the playground, i cannot connect external agents."

The gap is structural. The gateway listens on `/mcp` (HTTP+SSE per the MCP spec) — that's the endpoint external agents connect to — but the URL is invisible in the UI. Auth requirements (dev mode? JWT issuer? audience?) live only in `portico.yaml`. There's no path from "I registered a server" to "an agent can call its tools."

Comparison anchor: `agentgateway` exposes its dataflow chain (Port Binds → Listeners → Routes → Backends) directly on the overview, with Quick Actions for the chronological setup flow and a Configuration Status panel that validates "all listeners have routes and all routes have backends." Portico has the equivalent dataflow (Bind → /mcp listener → Policy routing → Servers) but only the Servers end is visible.

This phase makes the missing layers visible.

## Goal

A first-day operator can:
1. Open Portico, see exactly **where the gateway listens** and **what auth it requires**.
2. Copy a Claude Desktop / `npx @modelcontextprotocol/inspector` / curl snippet that connects against the running instance.
3. See the registered server's namespaced tool names (`{server_id}.{tool}`) and a sample JSON-RPC `tools/call` payload — the seam between "registered" and "callable".
4. See on the landing whether the configuration is wired (servers + auth + at least one tenant + at least one snapshot) and what the next setup step is.

No new product surface — every fact already exists in `portico.yaml` / running config / API responses. This is a presentation layer.

## Prerequisites

Phase 10.8 complete (current branch `phase-10.5-ux-remediation`, 8 commits unpushed):
- 28 Console pages on the new vocabulary; subtitles stripped.
- `MetricStrip` + `compact` variant; `PageActionGroup`; `Inspector`; `Tabs`; `Breadcrumbs`; `IdBadge`; `KeyValueGrid`; `CodeBlock`.
- `make preflight` 117/3/0; Playwright 55/1.

This phase does **not** depend on later phases. No schema migrations.

## Out of scope (explicit)

- **Per-tenant JWT issuance UI.** Issuing a JWT is a key-management decision; the operator already has whatever process they use (Auth0 / Okta / static keys / `portico jwt-issue`). The `/connect` page documents which fields the JWT must carry and links to `/admin/tenants` for the issuer/JWKS configuration — it doesn't sign tokens itself.
- **A new MCP transport.** The northbound `/mcp` endpoint stays as-is; this phase only documents it.
- **Custom listener configuration.** Bind / port / TLS stay in `portico.yaml`. The page surfaces them read-only.
- **Quick-action wizards.** "Add server" links to the existing `/servers/new`. "Connect agent" links to `/connect`. No new flows.

## Steps

### Step 1 — Backend: `GET /api/gateway/info`

A small unauthenticated read-only endpoint returning the facts the Console needs to populate `/connect` and the new landing. Already-public information; no secrets.

**Response shape**:

```json
{
  "bind": "127.0.0.1:8080",
  "mcp_path": "/mcp",
  "version": "v0.3.0",
  "build_commit": "abcdef",
  "dev_mode": true,
  "dev_tenant": "default",
  "allowed_origins": [],
  "auth": {
    "mode": "dev",
    "issuer": "",
    "audiences": [],
    "jwks_url": "",
    "tenant_claim": "tenant",
    "scope_claim": "scope"
  }
}
```

In `auth.mode = "jwt"`, the `issuer` / `audiences` / `jwks_url` carry config-loaded values; `tenant_claim` and `scope_claim` reflect their applied defaults.

**Where it lives**: `internal/server/api/handlers_gateway.go` (new file). Mounted on the public router (no auth — the bind/auth-mode is observable from any client that can reach the port).

**Acceptance**:
1. `curl http://127.0.0.1:8080/api/gateway/info` returns 200 with the shape above.
2. Smoke check exercises the endpoint and asserts `bind`, `mcp_path`, `auth.mode` are populated.
3. Unit test verifies the JWT-mode response correctly omits empty fields.

### Step 2 — Console: `/connect` page

A new operator-facing page in the primary nav, placed near the top (above /servers — it's the answer to the first question a new operator asks).

**Composition**:

- `PageHeader` (compact)
- `MetricStrip` compact: Endpoint URL (clickable copy) / Auth mode / Dev tenant / Servers (count)
- A "Quick start" section with three `.card` panels stacked vertically:
  - **Claude Desktop / generic MCP client** — JSON snippet for the `mcpServers` config block, using the live bind URL.
  - **Inspector** — `npx @modelcontextprotocol/inspector --transport http $URL` ready to paste.
  - **curl** — `tools/list` request via JSON-RPC, with a Bearer placeholder when auth is JWT or `# no auth required (dev mode)` comment when in dev.
- An "Authentication" `.card` showing:
  - Current mode (dev / JWT) as a Badge
  - When JWT: issuer, audiences, JWKS URL, tenant claim — read-only `KeyValueGrid`, with a link to `/admin/tenants` for per-tenant configuration
  - When dev: a callout that tells the operator the tenant is hardcoded to `dev_tenant` and links to the dev-mode docs section in the RFC
- A "Headers reference" `.card` with the standard MCP request headers (`Origin`, `Authorization`, `Mcp-Session-Id`)

Each code snippet has a Copy button (clipboard).

**i18n**: ~25 new keys; both locales.

**Test**: `tests/connect.spec.ts` — boots /connect, asserts the heading, the KPI strip, all three snippet cards present, the auth card present.

### Step 3 — Console: reshape the root landing

Replace today's runtime-telemetry KPI strip with a setup-status one. Move recent activity below the fold.

**New shape**:

- Hero (unchanged from 10.8)
- `MetricStrip` (default, 5 cards, all clickable):
  - **Endpoint** → `/connect`, value = `host:port`
  - **Servers** → `/servers`, value = N (attention if N=0)
  - **Skills** → `/skills`, value = N
  - **Tenants** → `/admin/tenants`, value = N
  - **Auth** → `/connect#auth`, value = `dev` / `jwt`
- "Configuration Status" `.card`:
  - Green if servers > 0 AND (dev_mode OR auth.issuer != "") AND tenants > 0
  - Yellow with a per-issue list otherwise
  - Mirrors agentgateway's "Configuration looks good!" semantic
- "Quick Actions" `.card`:
  - Connect an agent → `/connect`
  - Add a server → `/servers/new`
  - Author a skill → `/skills/authored/new`
  - Test in playground → `/playground`
- A collapsed "Recent activity" section below containing the previous "Recent approvals" / "Recent snapshots" / "Recent audit" cards (the runtime-telemetry view stays available, just demoted)

**Test**: extend `tests/landing.spec.ts` to assert the new "Endpoint" KPI and the "Configuration Status" + "Quick Actions" headings.

### Step 4 — `/servers/[id]` Connect tab

A 4th tab on the server detail page, alongside Overview / Logs / Activity:

**Tab body**:

- `KeyValueGrid` of routing facts:
  - Server id
  - Tool prefix (`{server_id}.*`)
  - Gateway endpoint (`http://host:port/mcp`)
  - Auth mode (links back to /connect for the full story)
- `CodeBlock` with a JSON-RPC `tools/call` example:
  ```json
  {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "{server_id}.{tool}",
      "arguments": { "...": "..." }
    }
  }
  ```
- A note that `tools/list` returns the full namespaced catalog and links to the Playground for an interactive smoke test.

**Test**: extend `tests/detail.spec.ts` to assert the Connect tab is visible on /servers/[id].

## Done definition

- All four steps' acceptance criteria pass.
- svelte-check 0/0; build clean.
- Playwright count goes up by ≥4 (connect + landing + detail extensions).
- `make preflight` shows OK count goes up by 1 (the new `/api/gateway/info` smoke check).
- `make preflight` 0 FAIL.
- A first-day operator can read the landing → /connect → /servers/[new]/Connect-tab in one session and successfully call a tool from `npx @modelcontextprotocol/inspector` against the running instance. (Verified by the user during hands-on testing — not by automation.)

## Order of operations

One commit per step:
1. `feat(phase-10.9): gateway info endpoint` (Step 1 — Go + smoke)
2. `feat(phase-10.9): /connect page with copy-paste client snippets` (Step 2)
3. `feat(phase-10.9): reshape landing to setup+status` (Step 3)
4. `feat(phase-10.9): connect tab on server detail` (Step 4)

Total estimate: ~6 hours focused implementation.
