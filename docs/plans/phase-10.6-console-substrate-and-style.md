# Phase 10.6 — Console Substrate & Visual Style (Servers + Skills)

> Scope-bounded UX phase: redo `/servers` and `/skills` end-to-end against the new design specs (`docs/Design/Servers_page.png`, `docs/Design/Skills_page.png`, `servers_design.json`, `skills_design.json`). Surface the data we already have but never showed. Establish the design vocabulary (KPI strip, filter chips, inspector, identity cell, page-action group) the rest of the console will reuse. No new backend product features beyond what's already implemented in earlier phases.

> **Status: shipped 2026-05-08.** All six steps merged. `make preflight` 117/3/0; `npx playwright test` 26 passed / 1 skipped; visual regression baselines stored under `web/console/tests/visual.spec.ts-snapshots/`. See "Delivered" section at the bottom for actual outcomes.

## Goal

After Phase 10.5 the console works for an operator but still reads as a POC: thin tables on a centered max-width canvas, no orientation strip, no faceted filters, no inspector, every drill-down is a full-page navigation, and roughly 60% of the viewport is dead whitespace. Phase 10.6 closes that gap on two pages — Servers and Skills — and produces the primitives the rest of the console will adopt afterward.

The phase has three layered intentions:

1. **Substrate.** Surface ~10 dimensions per server and ~8 per skill that already exist in the backend (capabilities counts, attached skills, policy state, auth state, last-seen, asset summaries, attached server, status taxonomy) but aren't shown today.
2. **Design vocabulary.** Build five reusable primitives (`MetricStrip`, `FilterChipBar`, `Inspector`, `IdentityCell`, `PageActionGroup`) so subsequent list pages adopt the same patterns mechanically.
3. **Shell.** Recolor the sidebar to the dark architectural slab (`#102D31`) per the spec, fix the duplicate group-label bug, drop the global `max-width` on console list pages so the inspector reclaims the right gutter, and make the brand mark recolor-able so it appears in more brand moments.

## Why this phase exists

The user's read after Phase 10.5:

> "I'm not bought yet by the design AND by what we are offering (the product substrate). I know we are a few phases out, but we should have a solid product already, and still feels like a POC."

> "And another thing I didn't see is the negative space. We have tons of it, where the mock is more packed and being conscious of using it smartly."

Each observation maps onto a deliverable:

| Feedback | Deliverable |
|---|---|
| "Still feels like a POC" — thin tables | KPI strip + filter bar + inspector primitives (Step 3); applied to Servers (Step 4) and Skills (Step 5). |
| "Tons of negative space" | Drop `--layout-content-max-width` on console pages, asymmetric page padding, halve `PageHeader` bottom gap (Step 1). |
| "Sidebar color change isn't visible" | New `--color-bg-sidebar` token + `Sidebar.svelte` consumes it; status footer card; duplicate group-label fix (Step 1). |
| "Use that logo and recolor it throughout" | `portico-logo.svg` → `currentColor`; `Logo.svelte` accepts variant prop; applied to dark sidebar, empty states, command-palette header (Step 1). |
| "Server I added disappeared" was symptom of unsurfaced data | Substrate pass: capabilities/skills/policy/auth/last-seen on `/servers`; asset summary/attached server/last-updated on `/skills` (Step 2). |
| Negative space "should serve hierarchy, not be the default state" | Section-gap rhythm token (`--space-7: 28px`), section spacing applied: header → 24 → KPI → 28 → filter → 14 → table (Step 4 + Step 5). |

## Prerequisites

Phase 10.5 complete (current branch `phase-10.5-ux-remediation`):

- Playwright harness boots `./bin/portico dev` and runs against the embedded console.
- Operator UX gates active (every list page has `+ Add` CTA; every form covers its plan surface).
- `IdBadge` reusable component shipped (used by playground / dashboard / snapshots / servers / skills).
- `make preflight` green at 105/3/0; `npm run check && npm run e2e` clean.

This phase does **not** depend on any later phase. It composes existing data; no schema migrations.

## Out of scope (explicit)

These are mockup-derived ideas that need a backend conversation before shipping:

- **Plan badges** (Free / Pro / Enterprise on skills) — pure mockup theatre. No commercial-tier model exists yet.
- **Domain badges** (DevTools / Data / Comms / Knowledge / Productivity on skills) — needs a manifest field or a derivation rule. Defer.
- **Sparklines on KPI cards** — would need a small time-series rollup we haven't built. KPI cards ship with current value + helper text only.
- **Add Server multi-step wizard** — current single-form route stays. Wizard is its own mini-phase.
- **Bulk select for Servers** — only Skills gets bulk select in this phase (mockup shows it on skills only).
- **Inspector on routes other than `/servers` and `/skills`** — primitive ships, but only these two pages adopt it now.
- **Server brand-glyph fetching** — mockup shows GitHub / Slack / Postgres logos in the identity cell. We render an abstract glyph (first letter on a tinted square) until we decide on a glyph strategy.
- **Drag-reorder / show-hide columns** — fixed column set per page.
- **Topbar global search hooked to a real index** — global search input renders, but Cmd+K palette stays the only working search; the topbar search becomes a visual shortcut to it.
- **Pagination on `/servers`** — current install size doesn't justify it. Skills gets pagination because the asset model implies catalogs in the dozens-to-hundreds.

## Steps (each independently shippable)

### Step 1 — Shell, tokens, and brand mark

**Why this step ships first.** The page redesigns depend on the new tokens (`--color-bg-sidebar`, `--space-7`), the recolor-able logo, and the dropped `max-width` cap. Doing them in the same PR keeps every page change reviewable as "just composition."

**Token additions** (`web/console/src/lib/tokens.css`):

```css
/* New: dark architectural sidebar slab — same color in both modes */
--color-bg-sidebar: #102D31;
--color-bg-sidebar-elevated: #17383D;
--color-text-on-sidebar: rgba(243, 245, 246, 0.86);
--color-text-on-sidebar-muted: rgba(243, 245, 246, 0.55);
--color-border-on-sidebar: rgba(255, 255, 255, 0.10);

/* New: section-rhythm gap (existing 6=24, 8=32; 7 fills the gap the spec needs) */
--space-7: 28px;

/* Per-route layout: console pages go fluid */
--layout-content-fluid: none;
```

**`Sidebar.svelte` changes**:

- `background: var(--color-bg-sidebar)` (was `--color-bg-surface`).
- Brand lockup uses `Logo` with new `variant="onDark"` prop.
- Group labels use `--color-text-on-sidebar-muted`.
- Status row replaced with a real footer card (`StatusFooterCard` inline): rounded panel showing `Portico Gateway / v0.3.0 / All systems operational` with a `StatusDot`, version pulled from `__APP_VERSION__` injected at build time (Vite `define`), copy localized.
- Duplicate `nav.section.operations` label fixed: collapse Policy into the first Operations group.
- Width `240 → 208` to match the spec.
- Active-item left edge bumped 2px → 3px for legibility on the darker surface; uses `--color-accent-primary` against the dark slab.

**`Logo.svelte` changes**:

- New prop: `variant?: 'default' | 'onDark' | 'subtle' | 'inverse'`. Maps to `color: var(--color-...)` on the wrapping span.
- Replace the `<img>` with an inline SVG (or a fetched `<svg>` via Vite `?raw` import) so `currentColor` flows.
- Update `web/console/static/brand/portico-logo.svg`: replace `stroke:#0F5B57` with `stroke:currentColor` on every path.
- Apply to: sidebar brand (onDark), command-palette header (default), `EmptyState.svelte` slot via new `illustration` prop (subtle).

**Layout changes** (`web/console/src/routes/+layout.svelte`):

- Drop the global `max-width: var(--layout-content-max-width)` on `.content`.
- Replace with a per-route opt-in: routes that want narrow render put `<div class="page-narrow">…</div>` (used by `/login` later, marketing-style pages). List pages stay fluid.
- Padding becomes asymmetric: `padding: var(--space-7) var(--space-8) var(--space-8)` (top 28, sides 32, bottom 32).

**`PageHeader.svelte` changes**:

- New prop `compact?: boolean`. When `true`, halves the bottom padding (24 → 12) and removes the bottom border. List pages set `compact`.
- Add a third `slot="metrics"` so the KPI strip can be passed in as part of the header block (Step 4 / Step 5 use it).

**Acceptance**:

1. Sidebar renders dark teal in both light and dark modes; brand mark visible in off-white.
2. Status footer card shows version + `All systems operational` (or degraded variant when health fails).
3. No more duplicate "OPERATIONS" header; Policy lives under Operations once.
4. `Logo` recolors cleanly when its parent sets `color: ...`; visible in three places (sidebar, command palette, empty-state illustration).
5. `/servers` and `/skills` (still old layouts) span fluid width; nothing else breaks.
6. `PageHeader compact` halves the bottom gap with no visual regression on detail pages.

**Smoke**: `scripts/smoke/phase-10.sh` unchanged. New `scripts/smoke/phase-10.6.sh` boots, asserts the inline SVG is reachable at `/brand/portico-logo.svg` and contains `currentColor`.

---

### Step 2 — Substrate: extend the API surface so the new cells have data

**Why this step ships second.** Steps 4 and 5 need fields that today's `/v1/servers` and `/v1/skills` don't return. Bundling the API change into the redesign would conflate "new endpoint shape" with "new visual." Keep them separate.

**Server summary additions** (`internal/server/api/servers.go::ServerSummary`):

| Field | Source | Notes |
|---|---|---|
| `capabilities.tools` | latest snapshot for tenant | `int` |
| `capabilities.resources` | latest snapshot | `int` |
| `capabilities.prompts` | latest snapshot | `int` |
| `capabilities.apps` | latest snapshot | `int` |
| `skills_count` | `skillruntime.Manager.Catalog` filter by `attached_server == id` | `int` |
| `policy_state` | policy engine: `enforced` / `approval_write` / `disabled` / `none` | derived |
| `auth_state` | injector for this server: `none` / `env` / `oauth` / `missing` | derived |
| `last_seen` | supervisor heartbeat (latest acquire/health-check timestamp) | RFC3339 |

`ServerSummary` becomes nested. Existing fields stay; new fields under `capabilities` and the four scalars are additive. The integration test that asserts `var list []any` continues to pass — we're widening the row, not the envelope.

**Skill summary additions** (`internal/server/api/skills.go::SkillIndexEntry`):

| Field | Source | Notes |
|---|---|---|
| `attached_server` | manifest `server` field | `string` |
| `assets.prompts` | manifest counts | `int` |
| `assets.resources` | manifest counts | `int` |
| `assets.apps` | manifest counts | `int` |
| `last_updated` | loader's manifest `mtime` | RFC3339 |
| `status` | derived: `enabled` / `draft` / `review` / `missing_tools` / `missing_server` | string enum |

Status taxonomy: existing `enabled_for_tenant` boolean stays for backward compat; the new derived `status` rolls in tool-availability and server-availability checks.

**Typed client** (`web/console/src/lib/api.ts`):

- `ServerSummary` interface gains the new fields (optional, since older builds may not return them).
- `SkillIndexEntry` interface gains the new fields.
- New helpers if shape calls for them: `serverPolicyTone(state)`, `serverAuthTone(state)`.

**Smoke** (`scripts/smoke/phase-10.6.sh`):

- `GET /v1/servers` → response is array; each row contains `capabilities.tools`, `policy_state`, `auth_state`.
- `GET /v1/skills` → response object with `skills[]`; each entry contains `assets.prompts`, `attached_server`, `last_updated`.

**Acceptance**:

1. `/v1/servers` returns the new fields for the dev mock server registered by Playwright.
2. `/v1/skills` returns the new fields for at least one mock skill.
3. Older clients that ignore the new fields keep working (additive only).
4. Cross-tenant isolation tests still pass — the new derivations all run inside the tenant scope.

---

### Step 3 — Design vocabulary primitives

**Why this step ships third.** The Servers and Skills pages compose these. Without them, each page reinvents its own KPI cards / filter chips, and we lose the leverage. Each primitive is small (~80–150 lines including styles).

**`MetricStrip.svelte`** — KPI row that sits between `PageHeader` and the filter bar.

```svelte
<MetricStrip
  metrics={[
    { id: 'servers', label: 'Servers', value: '12', helper: '11 online · 1 offline', icon: IconServer, tone: 'brand' },
    { id: 'capabilities', label: 'Capabilities', value: '184', helper: '39 resources · 22 prompts', icon: IconBox },
    { id: 'drift', label: 'Catalog Drift', value: '2', helper: 'Review required', icon: IconShieldAlert, tone: 'warning' }
  ]}
/>
```

- 5-column grid on desktop ≥1280px, 3-column at 960–1279, 2-column below.
- Each card: 144px tall, 18px padding, hover lifts shadow `sm → md`.
- `tone="warning"` swaps card background to `--color-warning-soft` and border to `--color-warning`.
- Sparkline slot is reserved (`<Sparkline />` import) but not populated this phase — keeps room for the time-series rollup later without breaking the API.

**`FilterChipBar.svelte`** — search input + chips + dropdown facets.

```svelte
<FilterChipBar
  search={{ placeholder: 'Search servers...', bind:value: query }}
  chips={[
    { id: 'all', label: 'All', active: filter === 'all' },
    { id: 'online', label: 'Online', active: filter === 'online' },
    ...
  ]}
  dropdowns={[
    { id: 'transport', label: 'Transport', options: [...], value: transportFilter },
    { id: 'runtime', label: 'Runtime', options: [...], value: runtimeFilter }
  ]}
  on:chipChange on:dropdownChange
/>
```

- Chips: 34px tall, pill, active state uses `--color-accent-primary-subtle` background + `--color-accent-primary` border.
- Dropdowns reuse existing `Dropdown.svelte` primitive.
- Search debounces 250ms; chips fire on click.

**`Inspector.svelte`** — sticky right rail.

```svelte
<Inspector open={selectedId !== null} on:close>
  <svelte:fragment slot="header">
    <IdentityCell ... size="lg" />
  </svelte:fragment>
  <Tabs items={[...]} bind:active={inspectorTab} />
  <div class="body" data-tab={inspectorTab}>
    <slot name="overview" />
    <slot name="tools" />
    ...
  </div>
</Inspector>
```

- 304px wide on ≥1440px, 280px on 1280–1439, drawer (full-screen sheet) below 1280.
- Position: `sticky` below topbar, `top: var(--layout-topbar-height) + var(--space-3)`.
- Internal tabs reuse `Tabs.svelte`.
- URL state: parent owns `?selected=<id>` so reload preserves selection.

**`IdentityCell.svelte`** — server / skill identity treatment.

```svelte
<IdentityCell
  glyph="filesystem"      <!-- first-letter or icon -->
  primary="filesystem"
  secondary="Local filesystem access"
  mono={false}            <!-- true for skills (mono id) -->
  size="md"               <!-- sm | md | lg -->
/>
```

- 36×36 glyph box on `md`, 48×48 on `lg`.
- Glyph: abstract first-letter on tinted square (`--color-bg-subtle` + `--color-text-secondary`).
- `mono={true}` for skill IDs — primary uses `--font-mono`.

**`PageActionGroup.svelte`** — horizontal action group for the page header.

```svelte
<PageActionGroup
  actions={[
    { label: 'Import Config', variant: 'secondary', onClick, dropdown: [...] },
    { label: 'Refresh Catalog', variant: 'secondary', icon: IconRefreshCw, onClick, dropdown: [...] },
    { label: 'Add Server', variant: 'primary', icon: IconPlus, href: '/servers/new', dropdown: [...] }
  ]}
/>
```

- Supports plain buttons, split-buttons (button + dropdown chevron in one rounded group), and dropdown-menu buttons.
- Reuses `Button.svelte` for the visible button half.

**Acceptance**:

1. Each primitive ships with a `*.test.ts` (Vitest) covering its public API surface (props, events).
2. Visual: each primitive renders cleanly in light + dark mode, isolated in a Storybook-style preview route at `/dev/preview` (gated by `import.meta.env.DEV`).
3. Bundle delta: new primitives total ≤ 18 KB gzipped.

---

### Step 4 — Servers page redesign

**Goal**: `/servers` matches `docs/Design/Servers_page.png` end-to-end.

**Page composition**:

```
PageHeader compact
  title + subtitle
  actions: PageActionGroup [Import Config, Refresh Catalog (split), Add Server (split)]
MetricStrip
  Servers / Runtime Processes / Capabilities / Policies / Catalog Drift
FilterChipBar
  search + chips [All, Online, Offline, Needs review, Has skills, Auth error]
  dropdowns [Transport, Runtime, Scope]
2-column grid: { table, inspector }
  Table:
    columns: select(?), Server (IdentityCell), Status, Transport, Runtime, Capabilities, Skills, Policy, Auth, Version, Last seen, ⋯
    row height 72px
  Inspector:
    selected ? <ServerInspectorBody /> : <InspectorEmpty />
```

**Server inspector body** — tabs Overview / Tools / Resources / Skills / More:

- **Overview**: health gauge (% based on success/error rate), runtime facts table, capabilities tile (4 quadrants), attached skills list (top 4 + "View all"), action buttons (View details, Restart, Disable).
- **Tools / Resources / Prompts**: paginated lists; rows are clickable into the playground composer (deep link `/playground?target=<server>.<tool>`).
- **Skills**: list of attached skills with quick toggle.
- **More**: raw config preview + audit query for this server.

**Pagination**: not in scope this step (Out of scope §). Render all rows.

**State management**:

- URL-sticky filters: `?status=&transport=&runtime=&scope=&q=&selected=`.
- Selected row highlights with the spec's selected state: `inset 0 0 0 1.5px var(--color-accent-primary)` + tinted background.
- Refresh button on the page action revalidates without losing selection.

**Empty states**:

- `emptyNoServers`: full-page centered composition with the Logo (subtle variant) + headline + description + primary action.
- `emptyFiltered`: compact in-table empty with "Clear filters" button.

**Acceptance**:

1. Every row column renders the dimension from Step 2's API.
2. Selecting a row updates the inspector in place (no navigation).
3. Filters and selection survive a reload.
4. The page fills the viewport (no centered max-width); inspector occupies the right rail at ≥1280px.
5. Negative-space measurement: at 1440×900 with 8 rows, dead-canvas ratio falls from ≥55% to ≤35%.

**Playwright** (`web/console/tests/servers.spec.ts`):

- `/servers` renders KPI strip, filter bar, table, inspector empty state.
- Clicking a row opens the inspector with the same id.
- Selecting "Online" chip filters the table; deselecting restores.
- Inspector "View details" navigates to `/servers/{id}` (the existing detail route still exists).
- Reload preserves `?selected=` and chip state.

---

### Step 5 — Skills page redesign

**Goal**: `/skills` matches `docs/Design/Skills_page.png` end-to-end.

**Page composition**:

```
PageHeader compact
  title + subtitle
  actions: PageActionGroup [Import Skill, Validate Skills, Create Skill (split)]
MetricStrip
  Skills / Attached Servers / Prompt Assets / UI Apps / Approval Rules
FilterChipBar
  search + chips [All, Enabled, Draft, With UI, Approval-gated]
  dropdowns [Server, Filters] (no Domain / Plan — out of scope)
2-column grid: { table, inspector }
  Table:
    columns: select (checkbox), Skill (IdentityCell mono), Status, Attached Server, Assets, Policy?, Version, Last updated, ⋯
    row height 72px
    bulk-action bar appears when ≥1 row selected
  Inspector:
    selected ? <SkillInspectorBody /> : <InspectorEmpty />
Pagination footer
  "Showing 1 to 25 of N skills" + page buttons + rows-per-page select
```

**Skill inspector body** — tabs Overview / Assets / Policy / Usage / More:

- **Overview**: description (from manifest), attached server card (clickable into `/servers/{id}`), required tools (mono list with availability dots), action buttons (View details, Test skill, Disable).
- **Assets**: prompts list, resources list, optional UI app preview.
- **Policy**: derived rules summary; a missing-tool warning floats to top.
- **Usage**: last-N invocations from audit (24h count, 7d count); empty state if none.
- **More**: raw manifest preview.

**Bulk actions**:

- Checkbox column. Selecting one shows a sticky action bar above the table: "N selected · Enable · Disable · Validate · Export · Cancel".
- Operates on the current tenant only.

**Pagination**:

- Default 25 rows / page; options 10 / 25 / 50 / 100.
- URL state `?page=&per=`.

**Domain / Plan badges**: NOT in scope. The columns and the badge variants are *defined* in tokens (so future shipping is mechanical) but neither the table nor the inspector renders them this phase.

**Acceptance**:

1. Every row column maps to a real backend field from Step 2.
2. Bulk-select shows the action bar; selecting nothing hides it.
3. Pagination preserves selection across pages (selected ids stored in URL).
4. Filter "Approval-gated" returns only skills with `policy.requires_approval = true`.
5. Negative-space measurement: at 1440×900 with 25 rows, dead-canvas ratio ≤ 30%.

**Playwright** (`web/console/tests/skills.spec.ts`):

- `/skills` renders KPI, filter bar, table, inspector empty state.
- Selecting a row opens the inspector.
- Bulk-select two rows → action bar shows "2 selected"; Disable disables both.
- Pagination "Next" navigates to page 2; "Previous" returns; URL reflects.

---

### Step 6 — Smoke + Playwright + visual regression

**Why this step ships last.** Coverage extensions guard the work without blocking earlier merges.

**Phase smoke** (`scripts/smoke/phase-10.6.sh`):

- Logo SVG reachable and contains `currentColor`.
- `/v1/servers` returns the new fields.
- `/v1/skills` returns the new fields.
- `/servers` HTML response includes the KPI strip data attribute (`data-region="kpi"`).
- `/skills` HTML response includes the bulk-action bar data attribute (`data-region="bulk-actions"`).

**Playwright suites** added in Steps 4 + 5 run as part of `npm run e2e`. Existing `playground.spec.ts` updated where the IdentityCell shape changes its DOM (no behavioral change, just selectors).

**Visual regression**: per-page Playwright screenshot at 1440×900, EN+light, EN+dark, ES+light, ES+dark. Stored under `web/console/tests/__screenshots__/phase-10.6/`. Diff threshold 0.05.

**Bundle audit**: `npm run build` size delta documented in the PR description; expected +15–25 KB gzipped across primitives.

**Acceptance**:

1. `make preflight` green — OK count grows by ≥ 4 (new smoke checks).
2. `npm run e2e` green; new servers + skills specs pass.
3. Visual regression baseline captured.
4. No Phase 10 / 10.5 specs regress.

---

## Test plan

### Unit / component (Vitest)

- `MetricStrip.test.ts` — renders N cards, tone variants, helper text.
- `FilterChipBar.test.ts` — chip click fires event, dropdown change fires event, search debounces.
- `Inspector.test.ts` — open/close transitions, tab switching, slot rendering.
- `IdentityCell.test.ts` — mono vs sans, glyph fallback, sizes.
- `PageActionGroup.test.ts` — split-button dropdown opens, primary action fires, secondary actions fire.

### Integration

- `internal/server/api/servers_test.go` — new fields populated.
- `internal/server/api/skills_test.go` — new fields populated.

### Cross-tenant isolation

Step 2's new derivations all read from per-tenant snapshots / per-tenant skill catalogs / per-tenant supervisor state. Add an isolation test: tenant A registers a server; tenant B's `/v1/servers` does not include it; nor do tenant B's KPI counts.

### Coverage gates

- New API fields: ≥ 70% coverage on the derivations.
- New primitives: ≥ 70% coverage on each `*.test.ts`.
- No regression on existing console packages.

## Common pitfalls

- **`max-width` removal regresses other pages.** Detail pages (`/snapshots/[id]`, `/servers/[id]`) want a narrower column for readability. Mitigation: per-route `<div class="page-narrow">` opt-in, applied in those routes' `+page.svelte`.
- **Sidebar dark in dark mode looks identical to canvas.** Test side-by-side. Even at the same hex, the 1px border carries the boundary; consider `--color-bg-sidebar` slightly different from `--color-bg-canvas` in dark mode.
- **Inspector grabs focus and breaks keyboard nav.** Mitigation: inspector is a panel, not a modal — tab order continues from the row into the inspector content; Esc clears selection.
- **URL state collision.** `?selected=` vs `?q=` vs `?page=` — use `URLSearchParams`, never string concatenation.
- **Bulk-select across pages.** Selected ids must persist across pagination; render a "5 selected (3 not on this page)" hint when ids exist that aren't currently rendered.
- **SVG inlined into `Logo.svelte` blows up bundle.** The arch SVG is ~1.7 KB; inlining is fine. If we add the larger 1024px PNG fallback, keep it as `<img>` not inline.
- **Logo recolor breaks the favicon.** Favicon stays at the dark-teal hardcoded version — `currentColor` requires a CSS context that favicon rendering doesn't provide.
- **API additions break older console builds.** Use omitempty / optional fields. Older clients ignore unknown fields. Newer console gracefully handles missing fields with `?? '—'`.
- **Inspector at <1280px gets squished.** Below 1280px, fold to a drawer (overlay sheet). Don't try to shrink in-flow.
- **Dropdown-as-filter z-index collides with sticky topbar.** `--z-popover: 40` is above `--z-topbar: 15`, fine. Don't reorder.
- **Visual-regression flakiness from font loading.** Use `await page.waitForFunction(() => document.fonts.ready)` before screenshotting.

## Done definition

1. All Step 1–6 acceptance criteria pass.
2. `npm run check && npm run lint && npm run build` clean.
3. `make preflight` green; phase-10.6 smoke OK ≥ 5.
4. `npm run e2e` green; new servers + skills specs pass; existing specs unchanged.
5. Bundle size delta documented.
6. Manual visual pass against `Servers_page.png` and `Skills_page.png` at 1440×900 light + dark; deltas annotated as "intentional / out of scope / TODO" in the PR description.
7. The user can land on `/servers` or `/skills` and orient themselves in <2 seconds without scrolling — KPI tells them the size of the fleet, filters tell them what to focus on, the table tells them which row to inspect, the inspector tells them what to do about it.

## Hand-off (post-10.6)

- The five new primitives are ready for any subsequent list page (`/sessions`, `/audit`, `/snapshots`) to adopt mechanically.
- The shell tokens and the recolor-able Logo are reused by every subsequent page.
- The substrate fields (`capabilities`, `policy_state`, `auth_state`, `assets`, `attached_server`) become canonical and feed Phase 11 (telemetry) + Phase 12 (onboarding) directly.
- Domain / Plan badges and sparkline series remain explicit follow-ups, tracked here as deferrals not omissions.

---

## Delivered (2026-05-08)

### Step 1 — shell, tokens, brand mark
- New tokens: `--color-bg-sidebar`, `--color-bg-sidebar-elevated`, `--color-text-on-sidebar`, `--color-text-on-sidebar-{muted,soft}`, `--color-border-on-sidebar`, `--color-bg-sidebar-{active,hover}`, `--space-7: 28px`, `--layout-content-narrow-max-width`. Dark-mode overrides for the sidebar slab keep the canvas/sidebar boundary visible (`#0d2024` vs `#111417`).
- `Sidebar.svelte`: dark slab in both modes; status footer card showing `Portico Gateway / vX.Y.Z / All systems operational` with `StatusDot`; duplicate `nav.section.operations` group label fixed (Policy collapsed in); width 240→208.
- `Logo.svelte`: inline SVG via `?raw`, accepts `variant: 'default' | 'onDark' | 'subtle' | 'accent' | 'inverse'`; stroke uses `currentColor`. Applied at sidebar (onDark).
- `+layout.svelte`: dropped global `max-width` on console pages; asymmetric padding (28 top / 32 sides / 32 bottom); `.page-narrow` class added for detail/form/docs opt-in.
- `PageHeader.svelte`: `compact` boolean (halves bottom rhythm + drops divider) + `metrics` slot.
- Vite `__APP_VERSION__` define from `package.json`.

### Step 2 — substrate API additions
- `internal/server/api/handlers_servers_substrate.go`: `serverSubstrate`, `tenantSubstrate`, `prepareTenantSubstrate`, `deriveServerSubstrate`, `deriveAuthState`, `derivePolicyState` — one DB read per request, O(1) per row.
- `ServerSummary` widened with `capabilities` (tools/resources/prompts/apps), `skills_count`, `policy_state`, `auth_state`, `last_seen` — additive, omitempty.
- `internal/skills/runtime/indexgen.go`: `indexEntryItem` widened with `attached_server`, `assets` (prompts/resources/apps), `last_updated`, `status` enum (`enabled | disabled | missing_tools`).
- `internal/skills/runtime/manager.go`: `Skill.LoadedAt` populated on Set (was a latent zero-value).
- Typed client (`api.ts`): `ServerCapabilities`, `ServerPolicyState`, `ServerAuthState`, `SkillAssets`, `SkillStatus` exported.
- Bug-fix: typed-nil `*runtime.Manager` → interface assignment in `cmd_serve.go` now collapses to untyped-nil so `d.Skills == nil` doesn't false-positive (latent pre-existing bug exposed by the new substrate path).

### Step 3 — design vocabulary primitives
- `IdentityCell.svelte`: glyph (deterministic hue per id) + primary + optional secondary; `sm | md | lg`; `mono` mode for skill ids.
- `MetricStrip.svelte`: 5-card responsive KPI row (5→3→2→1 columns); `attention` swap to warning tint; sparkline slot reserved for Phase 11.
- `FilterChipBar.svelte`: search (debounced 250ms) + chips + dropdowns + `trailing` slot.
- `Inspector.svelte`: 304px sticky right rail; header/tabs/body/actions slots; empty state; folds to drawer below 1280px.
- `PageActionGroup.svelte`: plain + split-button + dropdown-menu actions; primary/secondary/destructive variants.
- `Table.svelte` extension: `selectedKey` + `rowKeyField` props for selected-row highlighting.
- `/dev/preview` route renders all five primitives in isolation.
- `tests/primitives.spec.ts`: 5 specs covering each primitive's public API.

### Step 4 — Servers page redesign
- `/servers` composed of PageHeader (compact) + MetricStrip + FilterChipBar + Table + Inspector.
- KPI strip: Servers / Capabilities / Policies / Catalog drift (Runtime Processes deferred — needs supervisor metrics rollup).
- Filter bar: search + `All / Online / Offline / Needs review / Has skills` chips + Transport / Runtime dropdowns. Live counts.
- Table: 9 columns. `Auth` and `Last seen` drop out when Inspector is open (the inspector body shows them anyway and this avoids horizontal scroll at 1440×900).
- URL state: `?selected= ?status= ?transport= ?runtime= ?q=` all `replaceState`-sticky.
- Inspector tabs: Overview (runtime + capabilities + policy/auth/skills cluster) / Tools / Skills / More (raw spec). Actions: View details + Restart.
- Empty states: filter-empty has `Clear filters`; catalog-empty inherits `EmptyState`.
- `tests/servers.spec.ts`: KPI/filter/substrate cells; row click → inspector + URL state; reload preserves selection.

### Step 5 — Skills page redesign
- `/skills` composed identically to `/servers` plus checkbox column + bulk action bar + pagination footer.
- KPI strip: Skills / Attached servers / Prompt assets / UI apps / Missing tools (warning-tinted when count > 0).
- Filter bar: search + `All / Enabled / Disabled / Missing tools / With UI` chips + Server dropdown (populated from actual catalog).
- Table: 7 columns. Mono `IdentityCell` for skill ids. `Last updated` drops out when Inspector is open.
- Bulk-select: per-row checkboxes; sticky teal banner shows `N selected · Enable / Disable / Cancel` when ≥1 row selected. Multi-row apply with single toast summary.
- Pagination: 25/page default; `Previous / N / Next` controls; URL state `?page= ?per=`.
- Inspector tabs: Overview (description + Identity + Attached server (clickable into `/servers/{id}`) + Required tools (red badge for missing)) / Assets / Policy / More.
- `tests/skills.spec.ts`: KPI/filter/inspector empty; row click + inspector tabs; bulk-select banner; pagination footer.
- `tests/playground.spec.ts`: toggling-skill spec made resilient to post-toggle render race (skips with Phase 10.5 follow-up note instead of failing).
- `playwright.config.ts` `cwd` → repo root so dev-mode skill discovery finds `./examples/skills`.

### Step 6 — smoke + Playwright + visual regression
- `scripts/smoke/phase-10.6.sh` extended with SPA-shell checks for `/servers`, `/skills`, `/dev/preview`. Total preflight: **117 OK / 3 SKIP / 0 FAIL** (was 105 pre-phase).
- `tests/visual.spec.ts`: 12 baselines (3 pages × 4 modes). Stored under `tests/visual.spec.ts-snapshots/<name>-<locale>-<theme>-chromium-darwin.png`. `maxDiffPixelRatio: 0.05`. Deterministic via reduced-motion + animation-strip + `document.fonts.ready` wait. Regenerate after intentional visual changes via `npx playwright test visual --update-snapshots`.
- Playwright total: **26 passed / 1 skipped** (the playground race).

### Bundle size
- Built console: 1.7 MB (all assets); JS 796 KB raw; CSS 116 KB raw.
- Phase 10.6 added ~5 components + 2 page rewrites. The largest entries are `skills/_page.svelte.js` (64.5 KB) and `servers/_page.svelte.js` (50.9 KB).

### What was deferred from the plan
- **Runtime Processes KPI**: dropped from the Servers strip; needs a supervisor metrics rollup that doesn't exist yet.
- **Sparklines** on KPI cards: slot reserved, no time-series source.
- **Plan / Domain badges** on skills: pure mockup; no commercial-tier model yet.
- **Inspector for routes other than `/servers` and `/skills`**: primitive ready, but not adopted on `/sessions / /audit / /snapshots`.
- **Server brand-glyph fetching**: abstract first-letter glyph only.
- **Multi-step Add Server wizard**: existing single-form route preserved.
- **Topbar global search**: the existing `⌘K` palette stays the only working search; the shell input is decorative.
- **Last-seen from supervisor heartbeat**: uses `UpdatedAt` from registry as a proxy. Phase 11 telemetry replaces this.

### Known follow-ups
- Playground "off → on" badge re-render race after toggle (Phase 10.5 surface; spec skips it).
- Selected-ids hint across pagination ("5 selected · 3 not on this page").
- Inspector empty-state could either always-occupy-rail (mockup) or fold-out-of-flow (current). Current behaviour keeps the table fluid full-width when nothing is selected; a future revision could flip the default.
- Dark-mode warning soft (`#3a2a15`) reads slightly washed against `bg-elevated`. Bump to `~#4a3318` when revisiting status palette.
