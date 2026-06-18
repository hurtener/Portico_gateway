# Phase 14 — Agent Profiles

> Self-contained implementation plan. Builds on Phases 0–13.5. Introduces **Agent Profiles** — a first-class consumer-binding primitive that subsumes the scattered "which agent sees which servers/tools/skills/models" composition spread across Phase 5 scopes, Phase 6 snapshot scoping, Phase 4 Skill enablement, and Phase 15.5 VK MCP allowlists into a single tenant-scoped CRUD surface. This phase replaces the retired Envoy-shaped Bind/Listener/Route/Backend substrate plan (file renamed 2026-05-12) — see [v2-roadmap-agentgateway-parity.md](./v2-roadmap-agentgateway-parity.md) §0 for the pivot rationale.

## Goal

Operators can model **agents** the way they actually think about them: "Agent A is allowed to talk to MCP servers github/jira/slack, can use the `code-review` and `triage-bugs` Skills, can call models `gpt-4o` and `claude-3-5-sonnet`, has scopes `[mcp:call, llm:invoke]`, and authenticates via two Virtual Keys — one for staging, one for prod."

After Phase 14, that sentence is one CRUD object — an **Agent Profile** — with one Console screen (`/agents`), one REST surface (`/api/agent-profiles`), one CLI subcommand (`portico agents`), and one place in the middleware chain where it's resolved. Every downstream consumer (MCP dispatcher, LLM handler, Skills runtime, snapshot generator) reads the Profile from the request context. There is no parallel allowlist on any other surface.

**This is the SOTA primitive Portico has over both `agentgateway` (which models traffic-on-the-wire and has no consumer abstraction) and `bifrost` (which has VK allowlists but no unified Profile object).**

## Why this phase exists (and why now)

Enterprise feedback during V2 planning (2026-05-12):

> "Operators think in terms of agents — 'Agent A connects to MCP A, B, C. Agent B connects to B, C, D, E.' They don't think in listeners and routes. The original Phase 14 substrate was solving a different problem."

This is the right primitive for two reasons:

1. **It matches the mental model.** Operators don't compose entitlement from four surfaces (scopes + snapshots + skills + VK allowlists). They want one object that *is* "Agent A's permission to operate in our environment."
2. **It deduplicates the per-consumer gating logic.** Today, the same intuition ("this caller may see this server, but not that one") is expressed via Phase 5 policy rules AND Phase 6 snapshot inclusion AND Phase 15.5 VK MCP-server allowlists. That's three sources of truth — risk of drift, hard to audit, painful to debug. Phase 14 makes one source authoritative and downgrades the others to read-through layers.
3. **It's the unblocking primitive for Phase 15.5, 16, 17, 18, 19.** Virtual Keys attach to Profiles. A2A consumer-side gating reads Profile. Tool-poisoning policies attach to Profile. GitOps reconciles Profiles as resources. CRDs map to Profile. Building Profile first means every later phase has the right place to attach.

This phase is intentionally scoped to ship in **one PR, one weekend's worth of focused implementer time** for the core resolver + REST + Console. The migrations are surgical (one new table for profiles, one join table per allowlist dimension, one nullable FK on the existing VK table). The middleware change is additive (one new step inserted between JWT/VK resolution and policy).

## Prerequisites

- Phase 5 — scopes + policy engine + audit + redactor (Profile extends policy with a new matcher; audit gains `profile_id` field; scopes become *one* layer of the intersection, not the only one).
- Phase 6 — catalog snapshots (snapshot generation reads Profile to drive per-session projection).
- Phase 4 — Skill Pack runtime (Skill enablement reads `profile.allowed_skills`).
- Phase 13 — LLM gateway (handler reads `profile.allowed_model_aliases`).
- Phase 13.5 — Code Mode (binding-level catalog is still produced from snapshot; snapshot is filtered by Profile; no Code Mode rewiring needed).
- The §4.4 extensibility-seam pattern from `AGENTS.md`. Profile's resolver is interface-driven (in-memory LRU today; Redis-backed in Phase 19).

## Out of scope (explicit)

- **No Envoy-shaped substrate.** No Bind/Listener/Route/Backend. The original Phase 14 plan was retired; this is its replacement.
- **No HTTP/gRPC reverse proxy.** Phase 15 is deferred.
- **No Bifrost-shaped flat VK store.** Profiles sit *above* VKs; VKs attach to Profiles. (Phase 15.5 ships the VK CRUD; Phase 14 only ships the Profile schema and the FK on the VK table.)
- **No A2A integration in this phase.** Profile applies to A2A automatically in Phase 16 because the A2A handler will read Profile from context using the same helper as MCP; Phase 14 just provides the helper.
- **No multi-region Profile replication.** Profiles are tenant-scoped, stored in the tenant's instance's DB. Phase 19's federation considers Profiles tenant-scoped → never replicated across federation boundaries.
- **No Profile-level OAuth scopes / IDP integration.** Profile carries a *scope set* (the strings the JWT or VK would carry); the IDP that *issues* those scopes is unchanged.
- **No Profile inheritance / templating yet.** A future enhancement may add `parent_profile_id` for "Base Customer Support" → "Customer Support EU." Out of scope this phase; the schema reserves the column for forward compat.
- **No automatic Profile discovery.** Profiles are operator-authored. No "we noticed Agent X uses these servers; auto-create a Profile for it."
- **No Profile-level rate limits separate from VK budgets.** Budgets (Phase 15.5) attach to VK / Team / Customer. Adding Profile-level budgets is unnecessary nesting; the relationship is Profile → N VKs, and budgets sit on the VK side.

## Deliverables

1. **`internal/profiles/`** — the Profile module: types, repo, resolver, middleware, policy extension.
2. **Profile schema** (Migration 0020) — `agent_profiles`, `agent_profile_mcp_servers`, `agent_profile_tools`, `agent_profile_skills`, `agent_profile_models`, plus a nullable `profile_id` FK on the existing virtual_keys table (when Phase 15.5 lands; Phase 14 reserves the column even though Phase 15.5 will materialise the VK table).
3. **Profile resolver** — `internal/profiles/resolver.go` exposes `Resolve(ctx, principal) (*Profile, error)`. Principal is the post-auth identity (JWT subject or VK id). The resolver returns the bound Profile, or a synthesised "default profile" (the tenant's full surface) for back-compat with V1/V1.5 callers that have no Profile bound yet.
4. **Middleware step** — `internal/dataplane/middleware/profile.go` (the only file added under `internal/dataplane/middleware/`; the rest of the dataplane substrate is not built). Runs after `tenant + jwt|vk` and before `policy`. Writes the Profile into `ctx`.
5. **Dispatcher / handler integration** — three small reads:
   - **MCP dispatcher** reads `profile.AllowedMCPServers` + `profile.AllowedTools` to filter `tools/list` and reject out-of-surface `tools/call` with `agent_profile_violation`.
   - **LLM gateway handler** reads `profile.AllowedModelAliases` to reject out-of-surface alias requests with `agent_profile_violation`.
   - **Skills runtime** reads `profile.AllowedSkills` to skip pack enablement for non-allowed packs.
6. **Snapshot integration** — `internal/catalog/snapshots` derives per-session projection by intersecting `profile.AllowedMCPServers ∩ profile.AllowedTools` with the live catalog. Drift detection runs against the projected slice, not the raw catalog.
7. **Policy extension** — new matchers (`profile.id`, `profile.name`, `profile.includes_server`, `profile.includes_alias`) and new action (`require_profile_membership: [list-of-profile-ids]`). Profiles become first-class in the policy DSL.
8. **REST API** — CRUD over Profiles + the four allowlist dimensions. `GET /api/agent-profiles/{id}/surface` returns the live materialised surface (which servers/tools/skills/models the Profile currently sees), useful for operator debugging when the live catalog has changed since Profile creation.
9. **CLI** — `portico agents list|get|create|update|delete|surface`. Plus `portico agents from-yaml <file>` for GitOps. Plus `portico agents test --as <profile> --tool <namespaced-tool>` for "would this Profile be allowed to call this tool right now?" — analogous to `kubectl auth can-i`.
10. **Console** — `/agents` is the new headline route. List view: name, attached VK count, allowed-server count, allowed-model count, allowed-skill count, last used. Detail view: four expandable inventories (servers + tools, skills, models, scopes) with deep links into each. Create flow: 5-step wizard (basics → servers → skills → models → review). VK attachment surfaces in detail page once Phase 15.5 ships.
11. **Smoke** — `scripts/smoke/phase-14.sh` covers Profile CRUD + the four allowlist enforcement points + the "default profile" back-compat path + a cross-tenant isolation check.
12. **Backward compatibility** — every Phase 0–13.5 acceptance criterion still passes against the Phase 14 build with no Profiles configured. The synthesised "default profile" is the back-compat seam.

## Acceptance criteria

1. **Schema + repo.** Migration 0020 applies cleanly; round-trip tests for every column; tenant isolation enforced at the repo layer.
2. **Profile resolution — happy path.** A request authenticated by a JWT whose `sub` matches a Profile-bound user resolves the right Profile. A request authenticated by a VK (Phase 15.5; tested with a stub VK row for this phase) resolves the Profile via the VK's `profile_id`.
3. **Profile resolution — no Profile bound.** A request whose principal has no Profile attached receives the synthesised default Profile (full tenant surface). This is the V1/V1.5 back-compat path — every prior phase's smoke still passes.
4. **MCP `tools/list` filtering.** With a Profile allowing only `[github, jira]`, `tools/list` returns tools only from those two namespaces. Tools from other registered servers are absent (not just hidden — absent from the JSON).
5. **MCP `tools/call` rejection.** A `tools/call` for a tool outside the Profile's surface returns a typed `agent_profile_violation` error with `details.profile_id`, `details.tool`, `details.reason`. An audit event records the violation.
6. **Tool-level allowlist.** A Profile with `allowed_mcp_servers: [github]` and `allowed_tools: [github.list_issues, github.comment]` exposes only those two tools in `tools/list`; other GitHub tools are absent.
7. **LLM alias filtering.** `/v1/chat/completions` with `model: gpt-4o` when the Profile's `allowed_model_aliases` doesn't include `gpt-4o` returns `403 agent_profile_violation`. `/v1/models` returns only allowed aliases.
8. **Skill filtering.** Skills runtime enables only the packs in `profile.allowed_skills`. Surface assertion: a Skill not in the allowlist does not appear in `prompts/list` or as a `skill://` resource.
9. **Snapshot integration.** A session's catalog snapshot includes only the Profile-allowed servers/tools. Drift detection runs against that projection; tools added to a server *not* in the Profile's allowlist do not raise drift events for that Profile's sessions.
10. **Intersection with VK MCP allowlist.** When a VK with its own MCP allowlist is attached to a Profile with its own MCP allowlist, the effective surface is the intersection (most-restrictive wins). Test asserts.
11. **Intersection with Phase 5 scopes.** A Profile carrying `scopes: [mcp:call]` for a principal whose JWT carries `scopes: [mcp:call, llm:invoke]` results in an effective scope set of `[mcp:call]` (intersection). The principal can call `/mcp` but not `/v1`.
12. **`agent_profiles.surface` endpoint accuracy.** `GET /api/agent-profiles/{id}/surface` returns the live materialised surface; a server added to the registry after the Profile was created but matching the Profile's allowlist appears immediately.
13. **`portico agents test` parity.** The CLI subcommand returns the same allow/deny decision (with the same reason payload) the live dispatcher would, for any (profile, tool) pair.
14. **Resolver performance.** P95 resolver overhead ≤ 1 ms with an in-memory LRU of 1024 entries. Microbenchmark gate fails the build on regression.
15. **Cross-tenant isolation.** Tenant A's Profile is not visible to tenant B. A Profile with `id=X` under tenant A and `id=X` under tenant B coexist without leakage. Cross-tenant integration test asserts every read path.
16. **Console UX gates.** Per §4.5.1: `/agents` list page has `+ Add` CTA; the 5-step wizard covers the full spec; edit and delete actions visible; Playwright covers create, edit, surface-view, delete.
17. **Backward compatibility.** Boot a Phase 14 build against an existing V1/V1.5 portico.db; run every Phase 0–13.5 smoke. OK counts identical. Zero FAIL.
18. **Smoke gate.** `scripts/smoke/phase-14.sh` shows OK ≥ 16, FAIL = 0; prior phases' smokes still pass.
19. **Coverage.** `internal/profiles/...` ≥ 85% overall, ≥ 90% in `resolver.go` (the hot path).

## Architecture

### Package layout

```
internal/profiles/
├── profile.go                 # Profile type (DTO; read-only after Resolve)
├── repo.go                    # ProfileRepo interface
├── sqlite_repo.go             # SQLite-backed implementation
├── resolver.go                # Resolve(ctx, principal) → *Profile; in-memory LRU
├── default.go                 # synthesised default profile (full tenant surface)
├── policy.go                  # policy-engine matchers + actions
├── surface.go                 # materialised-surface computation (intersection with live catalog)
└── profiles_test.go

internal/dataplane/middleware/
└── profile.go                 # the ONE new middleware step (sits between auth and policy)

internal/storage/sqlite/migrations/
└── 0020_agent_profiles.sql

internal/server/api/
├── handlers_agent_profiles.go
└── handlers_agent_profiles_test.go

cmd/portico/
└── cmd_agents.go              # `portico agents list|get|create|update|delete|surface|test|from-yaml`

web/console/src/routes/agents/
├── +page.svelte               # list
├── new/+page.svelte           # 5-step wizard
├── [id]/+page.svelte          # detail (4 expandable inventories)
├── [id]/edit/+page.svelte
└── agents.spec.ts             # Playwright

web/console/src/lib/api/agent-profiles.ts   # typed API client
```

### Profile type

```go
// internal/profiles/profile.go
package profiles

type Profile struct {
    TenantID            string
    ID                  string                // ULID
    Name                string                // operator-visible
    Description         string
    AllowedMCPServers   []string              // server names from the tenant's registry
    AllowedTools        []string              // optional finer-grain; namespaced (server.tool); empty = all in allowed servers
    AllowedSkills       []string              // Skill Pack IDs
    AllowedModelAliases []string              // LLM aliases
    Scopes              []string              // scopes this profile grants when used as the effective scope set
    PolicyBundleRef     string                // optional reference to a policy bundle
    ParentProfileID     string                // reserved for future inheritance; nullable
    Enabled             bool
    CreatedAt           time.Time
    UpdatedAt           time.Time
}

type Repo interface {
    List(ctx context.Context, tenantID string) ([]Profile, error)
    Get(ctx context.Context, tenantID, id string) (*Profile, error)
    Put(ctx context.Context, tenantID string, p Profile) error
    Delete(ctx context.Context, tenantID, id string) error
    // ResolveByPrincipal returns the Profile bound to the given principal (JWT subject or VK id).
    // Returns ErrNoBinding when there's no explicit binding — caller should synthesise the default.
    ResolveByPrincipal(ctx context.Context, tenantID, principalKind, principalID string) (*Profile, error)
}
```

### Resolver

```go
// internal/profiles/resolver.go
package profiles

type Resolver interface {
    Resolve(ctx context.Context, principal Principal) (*Profile, error)
    Invalidate(ctx context.Context, tenantID, profileID string)  // on Put/Delete
}

type Principal struct {
    TenantID string
    Kind     string  // "jwt_sub" or "vk_id"
    ID       string
}

// In-memory LRU keyed by (tenant_id, principal_kind, principal_id) → *Profile.
// TTL: 60s. Invalidation broadcast over the internal pub/sub on Profile or
// binding changes; effective within ~1s across instances when Phase 19 Redis
// coordinator lands.
```

### Middleware

```go
// internal/dataplane/middleware/profile.go
package middleware

// ProfileMiddleware sits after tenant + jwt|vk resolution, before policy.
// It writes a *profiles.Profile into the request context. Every downstream
// handler reads it via profiles.FromContext(ctx).
func ProfileMiddleware(resolver profiles.Resolver, defaultBuilder profiles.DefaultBuilder) func(http.Handler) http.Handler
```

The "default profile" builder synthesises a Profile that allows the tenant's full surface — every registered server, every registered Skill, every registered LLM alias. Existing principals that don't have a Profile bound get this default. Operators opt into restriction by creating a Profile and binding their principal to it.

### Intersection semantics (the rule)

When multiple allowlist layers apply, **most-restrictive wins** (intersection):

```
effective_servers = profile.allowed_mcp_servers
                  ∩ (vk.allowed_mcp_servers if VK has its own allowlist)
                  ∩ (scope-implied surface from Phase 5 policy)

effective_tools   = (profile.allowed_tools if non-empty, else all tools in effective_servers)
                  ∩ (vk.allowed_tools if VK has its own)
                  ∩ (policy-allowed tools)
```

This is documented in `docs/concepts/agent-profiles.md` and the policy engine asserts the property in `internal/profiles/policy_test.go::TestIntersection_MostRestrictiveWins`.

The Profile is the headline; VK / Scope / Snapshot may further restrict but never relax. This is the V2 cross-cutting rule (`v2-roadmap-agentgateway-parity.md` §5.1).

### SQL DDL — Migration 0020

```sql
CREATE TABLE IF NOT EXISTS agent_profiles (
    tenant_id          TEXT NOT NULL,
    id                 TEXT NOT NULL,                -- ULID
    name               TEXT NOT NULL,
    description        TEXT,
    scopes             TEXT NOT NULL DEFAULT '[]',   -- JSON array
    policy_bundle_ref  TEXT,
    parent_profile_id  TEXT,                         -- reserved; nullable; no FK in V2 (inheritance is post-V2)
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS agent_profile_mcp_servers (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    server_name TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, server_name),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_tools (
    tenant_id     TEXT NOT NULL,
    profile_id    TEXT NOT NULL,
    namespaced_id TEXT NOT NULL,                     -- "github.list_issues" etc.
    PRIMARY KEY (tenant_id, profile_id, namespaced_id),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_skills (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    skill_id    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, skill_id),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_models (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    alias       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, alias),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

-- Profile binding for JWT subjects (the typical operator-issued JWT case).
-- VK binding is added by Phase 15.5 via a nullable column on the virtual_keys table.
CREATE TABLE IF NOT EXISTS agent_profile_jwt_bindings (
    tenant_id   TEXT NOT NULL,
    jwt_sub     TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, jwt_sub),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);
```

The `agent_profile_jwt_bindings` table is what lets operators say "this JWT subject belongs to Agent X." It's a thin map; the JWT validation itself remains Phase 0's. For Phase 15.5 VKs, the binding is the `profile_id` column on `virtual_keys` — no separate join table needed (each VK belongs to at most one Profile).

## Configuration

`portico.yaml` gains an optional top-level `agent_profiles:` block for static (cold-start) Profile declaration. Hot reload supported.

```yaml
agent_profiles:
  - name: "customer-support-eu"
    description: "EU customer support agent — read-only data access, GDPR-compliant model set"
    allowed_mcp_servers: [zendesk, intercom-eu]
    allowed_tools:
      - zendesk.search_tickets
      - zendesk.get_ticket
      - intercom-eu.list_conversations
    allowed_skills:
      - "customer-support-triage"
    allowed_model_aliases:
      - "fast-summary"           # alias resolving to a low-cost EU-hosted model
    scopes: [mcp:call, llm:invoke]
  - name: "engineering-debug"
    description: "Engineer debugging tool — full surface, scoped via JWT"
    allowed_mcp_servers: [github, jira, sentry, datadog]
    allowed_skills:
      - "code-review"
      - "incident-triage"
    allowed_model_aliases: [gpt-4o, claude-3-5-sonnet]
    scopes: [mcp:call, llm:invoke]
```

A `portico.yaml` without an `agent_profiles:` block means "no Profiles configured" → every authenticated request gets the default profile → V1/V1.5 behaviour unchanged.

## REST API

```
GET    /api/agent-profiles
POST   /api/agent-profiles
GET    /api/agent-profiles/{id}
PUT    /api/agent-profiles/{id}
DELETE /api/agent-profiles/{id}

GET    /api/agent-profiles/{id}/surface          # live materialised inventory
GET    /api/agent-profiles/{id}/bindings         # which JWT subjects (and later VKs) point to this Profile
POST   /api/agent-profiles/{id}/bindings         # bind a JWT subject to this Profile
DELETE /api/agent-profiles/{id}/bindings/{sub}

POST   /api/agent-profiles/test                  # body: {profile_id, kind: 'tool'|'alias'|'skill', target}; returns {allowed: bool, reason: ...}
```

New typed error slugs:
- `agent_profile_violation` — 403; consumer attempted access outside profile surface.
- `agent_profile_unknown` — 404; named profile doesn't exist.
- `agent_profile_binding_conflict` — 409; JWT subject already bound to another Profile (operator must unbind first).

## CLI

```bash
portico agents list
portico agents get <name>
portico agents create --from-yaml profile.yaml
portico agents update <name> --from-yaml profile.yaml
portico agents delete <name>
portico agents surface <name>                    # what does this profile see right now
portico agents bind <name> --jwt-sub <subject>
portico agents unbind <name> --jwt-sub <subject>
portico agents test --as <name> --tool github.list_issues
portico agents test --as <name> --alias gpt-4o
```

## Console screens

### `/agents` (the new headline route)

`Table`: name, description (truncated), bindings count, allowed-server count, allowed-skill count, allowed-model count, enabled. `+ Add` CTA opens the 5-step wizard.

### `/agents/new` (5-step wizard)

1. **Basics** — name, description, scopes.
2. **MCP surface** — multi-select over the tenant's registered servers; optional finer-grain per-tool drill-down.
3. **Skills** — multi-select over the tenant's available Skill Packs.
4. **Models** — multi-select over the tenant's LLM aliases.
5. **Review + create** — full preview, "test against current catalog" button.

### `/agents/[id]` (detail)

Four expandable inventories: MCP surface (servers → tools), Skills, Models, Scopes. Each row deep-links to the underlying resource (server detail, skill detail, model alias detail). The detail page also surfaces:

- **Bindings** — JWT subjects bound to this Profile (and, once Phase 15.5 ships, attached VKs).
- **Last used** — recent audit events from any principal bound to this Profile.
- **Drift indicators** — if a server in the Profile's allowlist has been deregistered or has incoming drift events, surface a warning chip.

Edit and Delete actions are obvious. Delete is gated by approval flow when any binding exists (operators must explicitly confirm they want to revoke).

### `/agents/[id]/edit`

The same 5-step wizard pre-filled. Save produces a diffable audit event with the before/after surfaces.

### Playwright

`web/console/tests/agents.spec.ts` covers: create profile (full wizard), edit, surface view, bind + unbind, delete with approval-confirmation; cross-resource navigation (click a server in the inventory → land on the server detail page).

## Implementation walkthrough

Order matters. Each step lands as one commit.

### Step 1 — Migration + repo

Migration 0020. SQLite repo with round-trip tests. Tenant isolation enforced at the repo layer (every query takes `tenantID`).

### Step 2 — Profile resolver (no integration yet)

`internal/profiles/resolver.go` with the in-memory LRU + 60 s TTL. `Default` builder synthesises the full-tenant-surface fallback. Unit tests cover: cache hit, cache miss, invalidation on Put/Delete, default fallback.

### Step 3 — Middleware step

`internal/dataplane/middleware/profile.go`. Inserts between `jwt | vk` and `policy`. Writes the resolved Profile (or default) into `ctx`. Unit tests cover both bound and unbound principals.

This is the **single dataplane file added in this phase.** No other middleware files, no Listener/Route/Backend code.

### Step 4 — Wire integration: MCP dispatcher

The MCP dispatcher's `tools/list` and `tools/call` paths read Profile via `profiles.FromContext(ctx)`. `tools/list` filters; `tools/call` rejects with typed error. Existing handler shapes unchanged for callers — the filtering happens inside.

Tests: every acceptance criterion 4–6 has a unit test in `internal/mcp/dispatcher/...`.

### Step 5 — Wire integration: LLM gateway handler

`/v1/chat/completions`, `/v1/completions`, `/v1/embeddings`, `/v1/models` all read Profile. Aliases outside the surface are rejected with `agent_profile_violation`; `/v1/models` returns only allowed aliases.

### Step 6 — Wire integration: Skills runtime

Skills runtime's pack-enablement step intersects `tenant.skills ∩ profile.allowed_skills`. Skills outside the Profile's allowlist are not enabled for the session.

### Step 7 — Wire integration: snapshots

`internal/catalog/snapshots` accepts a Profile filter when generating a snapshot. Drift detection runs against the filtered slice. Existing snapshot machinery learns a new pure-function helper; the heart of the implementation doesn't move.

### Step 8 — Policy extension

`internal/profiles/policy.go` defines new matchers (`profile.id`, `profile.name`, `profile.includes_server`, `profile.includes_alias`) and a new action (`require_profile_membership`). Registered with the Phase 5 policy engine.

### Step 9 — REST API + CLI

`internal/server/api/handlers_agent_profiles.go` ships CRUD + `/surface` + `/bindings` + `/test`. CLI subcommand wraps the REST surface.

### Step 10 — Console (the new headline)

Routes, components, typed API client, Playwright. The Console navigation surface gets a new top-level entry: **Agents** (alongside Servers, Skills, Models, etc.). The visual hierarchy reflects what operators reason about first.

### Step 11 — Smoke + microbenchmark

`scripts/smoke/phase-14.sh`:
- Create a profile via REST → 201.
- `GET /api/agent-profiles/{id}/surface` → 200 + materialised inventory.
- Bind a JWT subject → 201.
- Issue a request with that JWT → `tools/list` filtered; `tools/call` outside surface → 403 with typed error.
- `/v1/chat/completions` with a disallowed alias → 403 with typed error.
- Boot with an existing V1.5 DB; run prior phases' smokes → OK counts identical.
- `portico agents test --as <name> --tool foo` returns the same allow/deny as the live dispatcher.
- Cross-tenant: create same-name Profiles in two tenants; assert no leakage.

Microbenchmark: `internal/profiles/bench_test.go::BenchmarkResolver_HotPath` asserts p95 ≤ 1 ms with LRU populated.

## Test plan

### Unit (next to source)

- `internal/profiles/repo_test.go` — CRUD round-trip + tenant isolation + bindings round-trip.
- `internal/profiles/resolver_test.go`
  - `TestResolver_CacheHit`
  - `TestResolver_CacheMiss_FetchesAndCaches`
  - `TestResolver_InvalidateOnPut`
  - `TestResolver_InvalidateOnDelete`
  - `TestResolver_DefaultProfile_WhenNoBinding`
  - `TestResolver_TenantIsolated`
- `internal/profiles/policy_test.go`
  - `TestMatcher_ProfileIncludesServer`
  - `TestMatcher_ProfileIncludesAlias`
  - `TestAction_RequireProfileMembership_Allow`
  - `TestAction_RequireProfileMembership_Deny`
  - `TestIntersection_MostRestrictiveWins`     # the canonical rule
- `internal/profiles/surface_test.go`
  - `TestSurface_LiveMaterialisation`
  - `TestSurface_HonoursToolLevelAllowlist`
  - `TestSurface_OmitsDeregisteredServers`
- `internal/dataplane/middleware/profile_test.go`
  - `TestMiddleware_BoundPrincipal_WritesProfile`
  - `TestMiddleware_UnboundPrincipal_WritesDefault`
  - `TestMiddleware_TenantIsolated`

### Integration (`test/integration/agent_profiles/`)

- `TestE2E_Profile_MCP_ToolsListFiltered`
- `TestE2E_Profile_MCP_ToolsCall_DeniedOutsideSurface`
- `TestE2E_Profile_MCP_ToolLevelAllowlist`
- `TestE2E_Profile_LLM_AliasFilter_OnModelsList`
- `TestE2E_Profile_LLM_AliasFilter_OnChatCompletions`
- `TestE2E_Profile_Skills_EnabledOnlyInAllowlist`
- `TestE2E_Profile_Snapshot_FilteredCorrectly`
- `TestE2E_Profile_Intersection_WithJWTScopes`
- `TestE2E_Profile_Intersection_WithVKAllowlist`  # uses a stub VK row
- `TestE2E_Profile_CrossTenantIsolation_Comprehensive`
- `TestE2E_Profile_BackCompat_NoProfilesConfigured` — every V1/V1.5 smoke passes
- `TestE2E_Profile_HotReload_AppliesImmediately`
- `TestE2E_Profile_PolicyAction_RequireMembership`

### Frontend tests

- Playwright: full 5-step wizard happy path; edit; surface view; bind a JWT subject; unbind with approval-confirmation; delete with approval-confirmation; cross-resource deep links (Profile → server detail).

### Smoke

`scripts/smoke/phase-14.sh` — listed in the implementation walkthrough. OK ≥ 16.

### Microbenchmark

- `BenchmarkResolver_HotPath` — p95 ≤ 1 ms.
- `BenchmarkSurfaceMaterialisation_100ServersTenant` — p95 ≤ 10 ms.

### Coverage gates

- `internal/profiles/...` ≥ 85% overall.
- `internal/profiles/resolver.go` ≥ 90% (security-adjacent hot path).
- `internal/dataplane/middleware/profile.go` ≥ 90%.

## Common pitfalls

- **Treating Profile as a synonym for "user."** Profile is the *binding* — a single human or service may use multiple Profiles depending on context (debug vs. prod), and a single Profile may serve multiple principals (every customer-support agent on the EU team shares one Profile). Bindings (table `agent_profile_jwt_bindings`) are many-to-one (subject → profile).
- **Leaking the default Profile.** The synthesised default Profile is a code construct, not a DB row. It should never be returned from `GET /api/agent-profiles`. Tests assert.
- **Forgetting the tenant scope on bindings.** A JWT subject `alice@acme` in tenant A is unrelated to `alice@acme` in tenant B. The bindings table primary key is `(tenant_id, jwt_sub)` for that reason.
- **Profile resolution cache stalenes after Profile edits.** The resolver invalidates on Put/Delete. But edits to the *allowlist join tables* without touching the parent row must also invalidate; the repo wraps both kinds of edit in a transaction that bumps `updated_at` on the parent and emits an invalidation event. Tests assert.
- **Tool-level allowlist surprise.** A Profile with `allowed_mcp_servers: [github]` and a non-empty `allowed_tools: [github.list_issues]` exposes *only* `list_issues` — not every github tool. Operators sometimes assume empty `allowed_tools` means "no tools"; documentation states the inverse: empty = all tools in allowed servers. The Console wizard makes this explicit ("Limit to specific tools? [+] Yes / [-] No").
- **Profile change with in-flight sessions.** A session with a stale snapshot continues to see the old surface until its next snapshot refresh. Operators expecting "kick everyone out" must use the explicit approval-flow-gated session-terminate action (which already exists from Phase 6).
- **Profile vs. Skill Pack-implied surface.** A Skill Pack declares `required_tools`. If the Profile's `allowed_tools` doesn't include those, the Skill is enabled but its calls fail at dispatch. The Console surface view flags this with a "skill X requires tool Y which isn't in this profile's surface" warning. Document; test.
- **Intersection gotchas with Phase 5 scopes.** A Profile carrying `scopes: [mcp:call]` for a principal whose JWT carries `scopes: [mcp:call, llm:invoke]` gives the principal `[mcp:call]` effective. Operators sometimes expect Profile scopes to *grant* what the JWT didn't carry — they cannot. Profile narrows; never broadens. Document.
- **A2A in advance.** Phase 16 will read the same Profile from context for A2A dispatch. The Profile schema already includes a placeholder `allowed_a2a_peers` column? **No** — Phase 14 does *not* preemptively add A2A columns. Phase 16 will add `allowed_a2a_peers` and `allowed_a2a_tasks` as additive columns then. The schema additive-evolution rule from §10 of `AGENTS.md` covers this.
- **GitOps `from-yaml` mismatched IDs.** `portico agents create --from-yaml` mints a new ID and ignores any `id:` in the YAML. `portico agents update --from-yaml` looks up by name; if the name changed, the update fails — operators rename in two steps. Document.
- **Profile deletion with active bindings.** Hard-delete is allowed only when no binding exists. Soft-delete (set `enabled: false`) is the path for "keep audit trail but stop accepting bindings."

## Out of scope (recap)

- Envoy-shaped substrate (the original Phase 14).
- HTTP/gRPC reverse proxy (Phase 15 deferred).
- Profile inheritance / templating (column reserved; behaviour post-V2).
- Profile-level rate limits (those live on the VK in Phase 15.5).
- Automatic Profile discovery from traffic patterns.
- A2A integration (Phase 16 plugs in; Profile-side machinery is reused).
- Federation-wide Profile sharing (Profiles are tenant-scoped, never cross trust boundaries).

## Status (2026-06-18) — DONE

Phase 14 is complete and merged. Acceptance #1–#8, #10–#19 are implemented and
covered by unit + integration + smoke tests; `make preflight` is green
(211 OK / 0 FAIL). The Console `/agents` screen ships as a list + sectioned
Inspector (the "5-step wizard" surface rendered as the Console idiom — a §4.3
deviation noted in #16's implementation).

### Acceptance #9 (snapshot-drift scoping) — §4.3 deferral

**What #9 asks:** tools added to a server *not* in a Profile's allowlist must
not raise drift events for that Profile's sessions.

**Why it is deferred (not faked):** the snapshot **drift detector**
(`internal/catalog/snapshots/drift.go`) is a background sweep with no request
context, so it has no resolved Profile. Its only per-session handle,
`snapshots.ActiveSession`, carries `{SessionID, TenantID, SnapshotID, StartedAt}`
— **no principal subject** — so it cannot resolve the session's Profile without:
(a) extending the Phase 6 active-sessions storage projection to carry the
principal subject, AND (b) injecting a `profiles.Resolver` into a background
component. Filtering the snapshot *contents* at creation instead would corrupt
the per-server schema fingerprint the detector compares against the live tool
list (stored-filtered ≠ live-full ⇒ false drift), so that path is wrong.

Drift is **operator-facing observability**, not an agent-facing surface: the
agent-facing projection (`tools/list`, `tools/call`, prompts, resources,
`skill://`, `/v1` aliases) is already fully Profile-enforced (#4–#8). A drift
event for an out-of-profile *server* is therefore at most a minor observability
imprecision, never a security or correctness gap.

**Clean future fix (when warranted):** add the principal subject to
`ActiveSession` (and the `ActiveSessions` query), inject the `profiles.Resolver`
into the `Detector`, resolve each active session's Profile in the sweep, and
skip any `snap.Servers` entry the Profile disallows via `AllowsServer`. This
scopes drift at emit time at the **server** granularity (the granularity #9
specifies) without changing the snapshot contract or the fingerprint baseline.
Tool-level drift scoping stays out of scope — it would corrupt the per-server
fingerprint and #9 is specifically about servers not in the allowlist.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-14.sh` shows OK ≥ 16, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. Docs site gains `/docs/concepts/agent-profiles` (the headline concept document), `/docs/how-to/create-an-agent-profile`, `/docs/how-to/bind-a-jwt-subject`, `/docs/how-to/agent-profile-yaml`.
5. `AGENTS.md` §13 forbidden practices updated:
   - "Adding a per-consumer allowlist on any surface (server, tool, skill, model, A2A task) outside the Agent Profile schema. Profile is the single source of truth for consumer entitlement."
   - "Returning the synthesised default Profile from `GET /api/agent-profiles` or any other read path that lists profiles. The default is a code construct."
6. RFC-001 §6 updated with an Agent Profile section documenting the primitive, the intersection semantics, and the back-compat path.
7. `docs/plans/README.md` index updated.

## Hand-off to Phase 15.5

Phase 15.5 inherits:

- The Profile schema and resolver. VK CRUD in Phase 15.5 adds a `profile_id` nullable FK to the `virtual_keys` table (Phase 14 reserves the conceptual binding; Phase 15.5 materialises the table).
- The middleware ordering. VK auth strategy slots in *before* Profile resolution; Profile resolution writes the bound Profile into context regardless of whether auth came from JWT or VK.
- The intersection rule. VK's own MCP / model allowlists, if present, intersect with the bound Profile's allowlists — most-restrictive wins.
- The Console navigation. `/governance` (Phase 15.5) sits *next to* `/agents`; the two surfaces are sibling, not nested.
- Budgets. VK / Team / Customer budgets are independent of Profile; a single Profile may have many VKs across many environments, each with its own budget. The Profile detail page in Phase 15.5 surfaces the aggregate spend across attached VKs.

The stable invariants Phase 14 leaves in place: multi-tenant from V1, headless approval, credentials behind the gateway, Profile is the source of truth for consumer entitlement. Phase 15.5 onward must respect all four.
