# Phase 10.8 — Console Detail + Form Vocabulary (full sweep)

> Scope-bounded UX phase: roll the Phase 10.6 design vocabulary onto every Console page that Phases 10.6 / 10.7a / 10.7b / 10.7c missed. Two missing list pages get the existing list vocabulary (KPI strip + filter chips + Inspector). Six detail pages and four form pages get a *new sub-vocabulary* derived from the existing primitives. The root landing gets a redesign that retires the bespoke `DashboardTile` in favour of `MetricStrip`. Snapshots compare gets its own compare sub-vocabulary. No new backend work — all substrate already exists.
>
> **Status: planned 2026-05-08, implementation pending approval.** Six steps, each independently shippable.

## Goal

After Phase 10.7c every operator-facing **list page** uses the new vocabulary (12 pages: servers, skills, resources, prompts, apps, audit, snapshots, admin/tenants, admin/secrets, sessions, approvals, policy). The remaining 14 pages are still on the pre-10.6 layout and read as POC against the redesigned ones — clicking from a polished list into a detail page is jarring. Phase 10.8 closes that gap.

The phase has three layered intentions:

1. **Cover the two missing lists.** `/skills/authored` and `/skills/sources` were not part of the 10.6 sweep (which only did the top-level `/skills`). They are list-shaped and adopt the existing list vocabulary mechanically.
2. **Define a detail-page sub-vocabulary.** `PageHeader` + `Breadcrumbs` + meta + `PageActionGroup` → mini-KPI strip (a compact `MetricStrip` variant) → `Tabs` → `.card` sections using the `<h4>` section-label header that Phase 10.7 introduced. Apply to six detail pages.
3. **Define a form-page sub-vocabulary.** `PageHeader` + `Breadcrumbs` + `PageActionGroup` footer, body composed of `.card` sections matching the detail-page sections one-for-one (so create / edit / view share visual rhythm). Multi-step wizards use `SegmentedControl` instead of bespoke step indicators. Apply to four form pages.

The root landing and snapshots-compare get tailored treatments because they don't fit either vocabulary cleanly (landing is dashboard-shaped; compare is delta-shaped).

## Why this phase exists

After Phase 10.7c the user asked "Do we need to do anything else for the remediation ux pass? Any more pages?" The answer was: list pages are done, but 14 routes are still on the old layout. Phase 10.8 finishes the sweep so the entire console reads as a single coherent product.

Per-page evidence from the audit (`docs/plans/phase-10.8-audit.md` — captured below):

| Page | Current symptom | What this phase delivers |
|---|---|---|
| `/` (root landing) | Bespoke `DashboardTile` grid + hero takes 30% viewport | `MetricStrip` reuse, tighter hero, "Recent X" cards adopt the section-label pattern |
| `/skills/authored` | Pre-10.6 list — Table + 2 buttons, no KPI / chips / Inspector | Same redesign as `/skills` got in 10.6 |
| `/skills/sources` | Pre-10.6 list with Modal-based Add (every other page uses a collapsible card) | Mirror `/admin/secrets` pattern |
| `/servers/[id]` | Tabs work but uses bespoke 2-col grid + hand-rolled `.logs-pane` | `PageActionGroup` + mini-KPI + `.card`-wrapped tab content |
| `/skills/[id]` | Body is a JSON dump | Tabbed detail (Overview / Manifest / Bindings) using `KeyValueGrid` |
| `/skills/authored/[id]` | KeyValueGrid above a CodeBlock with no section label | Adopt `.card h4` pattern, mini-KPI, fix per-row buttons |
| `/skills/sources/[name]` | Section headers are bespoke `<h3>`, last_error not visually emphasised | Tabs + `Badge tone="danger"` for errors |
| `/admin/tenants/[id]` | No breadcrumb, activity is bespoke `<ul>` instead of `Table` | Breadcrumbs + Table reuse + meta slot |
| `/snapshots/[id]` | Three Tables stacked; resources/prompts/credentials/policies invisible despite existing in payload | Tabs per category + KPI mini-strip |
| `/snapshots/[a]/diff/[b]` | No orientation strip, no chips, can't swap A/B from page | KPI mini-strip + filter chips + swap action |
| `/servers/new`, `/servers/[id]/edit` | Thin wrappers around `ServerForm` (real work is in the component) | Add `Breadcrumbs`; `ServerForm` audit deferred to a follow-up |
| `/skills/authored/new` | 3-pane editor uses legacy `--color-surface-1` tokens; validation aside isn't an `Inspector` | Token migration + use `Inspector` primitive for validation |
| `/admin/tenants/new` | Custom 3-step indicator; "Cancel" reads as "Back" mid-flow (bug) | `SegmentedControl` step indicator + correct Back/Cancel labelling + `.card` per step |

## Prerequisites

Phase 10.7c complete (current branch `phase-10.5-ux-remediation`, pushed):

- 12 list pages on the new vocabulary.
- Five primitives shipped: `MetricStrip`, `FilterChipBar`, `Inspector`, `IdentityCell`, `PageActionGroup` (plus `IdBadge`, `KeyValueGrid` from earlier).
- `Tabs` and `SegmentedControl` already exist; `Breadcrumbs` already exists.
- `make preflight` 117/3/0; `npx playwright test` 39 passed / 1 skipped.

This phase does **not** depend on any later phase. No schema migrations.

## Out of scope (explicit)

- **`ServerForm.svelte` component overhaul.** Powers `/servers/new` + `/servers/[id]/edit`. The form fields and validation patterns need their own audit pass — bundling that here would balloon Step 5. This phase's deliverable on those two routes is the page-shell pieces (Breadcrumbs, header). The form internals stay as-is until a follow-up phase.
- **`/dev/preview` route.** Internal design-system preview page; not operator-facing.
- **`/playground/**` routes.** These are the Phase 10 playground; they have their own design conversation.
- **A new compare primitive.** `/snapshots/[a]/diff/[b]` gets a tailored layout, not a reusable `DiffStrip` primitive — there's only one diff page and abstracting prematurely violates §4.4 of the contributor norms.
- **A new "DashboardLayout" abstraction.** Root landing reuses `MetricStrip` + section cards directly. If a second dashboard-shaped page appears, lift then.
- **Operator-driven Theme/Brand settings page.** Not implied by any of the audited pages.
- **New endpoints / fields.** All substrate exists in current API responses; this phase composes it.

## Steps (each independently shippable)

### Step 1 — Compact `MetricStrip` variant + token cleanup

**Why this step ships first.** Steps 3, 4, and 6 need a *compact* `MetricStrip` (mini-KPI for above tabs in detail pages and above the diff body). Doing it in Step 1 means every later step is just composition. The token cleanup (`--color-surface-1` → `--color-bg-elevated`) goes in the same step because the affected files (`/skills/authored/[id]`, `/skills/authored/new`, `/skills/sources/[name]`) are already getting touched in Steps 4 and 5 — easier to do the swap in a focused PR than scatter it.

**`MetricStrip.svelte` changes**:

- New prop: `compact?: boolean` (default `false`).
- When `compact`: smaller card padding (`var(--space-3)` → `var(--space-2)`), smaller value type (`var(--font-size-display-sm)` → `var(--font-size-title)`), helper text hidden (or set to a tooltip via the existing `helper` field). Aria-label unchanged.
- Same `data-region="kpi"` marker — Playwright assertions stay generic.

**Token migration**:

- Find every `.svelte` reference to `--color-surface-1`, replace with `--color-bg-elevated`.
- Find every `--color-surface-2` reference, replace with `--color-bg-subtle`.
- Verify visually against `/dev/preview` page.

**Acceptance**:

1. `MetricStrip compact` renders ~50% smaller than the default; KPI region marker still present.
2. `grep -r "color-surface-1\|color-surface-2" web/console/src` returns zero matches.
3. svelte-check 0/0; visual baselines for the 12 redesigned pages still stable (KPI strip there is the *default* variant — unchanged).

**Smoke**: no Go-side changes. Playwright unchanged for this step.

---

### Step 2 — Missing list pages: `/skills/authored` and `/skills/sources`

**Why this step ships second.** These are the simplest wins: they take the *existing* list vocabulary (no new primitives needed). Doing them now keeps the list-page family at 14/14.

**`/skills/authored` plan**:

- KPI strip: Total / Drafts (attention if >0) / Published / Archived.
- Filter chips: All / Drafts / Published / Archived + search.
- Inspector tabs: Overview (id, version, status, checksum, created_at), Versions (lazy-loaded via `api.authoredSkillVersions`).
- PageActionGroup: Refresh + primary "Author skill" → routes to `/skills/authored/new`.

**`/skills/sources` plan**:

- KPI strip: Total / Enabled / Failing (attention if any) / Drivers in use.
- Filter chips: All / Git / HTTP / Failing / Disabled + search.
- Form-as-collapsible-card pattern (mirrors `/admin/secrets`) — primary "Add source" toggles a card above the table; the existing Modal goes away.
- Inspector tabs: Overview (driver, priority, enabled, last_refresh_at, last_error), Config (driver-specific config CodeBlock), Packs (lazy-loaded — small list).
- Per-row buttons (Refresh / Delete) move into the Inspector decisions card so the table row stays clean.

**Acceptance**:

1. Both pages render with KPI strip + filter chip bar + sticky Inspector when a row is selected.
2. `/skills/sources` Add flow uses a collapsible card (no Modal) — verified by Playwright.
3. URL state persists for filter chips, dropdown, search, selected.
4. svelte-check 0/0; visual baselines refreshed.

**Smoke**: no new Go endpoints. Add Playwright spec `tests/skills-secondary.spec.ts` mirroring `governance.spec.ts` (heading + KPI region per page).

---

### Step 3 — Root landing (`/+page.svelte`)

**Why this step ships third.** Highest visibility — the operator's first impression. Independent of detail/form work; can be reviewed separately. Retires `DashboardTile` (currently used only here).

**Plan**:

- Hero compresses: smaller logo (`Logo size="md"`), drop the eyebrow line, tighten vertical padding by 50%.
- Replace the 6-tile `DashboardTile` grid with a `MetricStrip` (default variant, six metrics): Health / Sessions (24h) / Pending approvals (attention if >0) / Last snapshot age / Drift (24h) (attention if >0) / Vault status. Same data sources as today.
- "Recent approvals" + "Recent snapshots" two-column section becomes two `.card` sections with the `<h4>` section-label header pattern.
- "Recent audit" full-width section becomes a third `.card` using `Table` (currently bespoke rows).
- Drop `DashboardTile` from `lib/components/index.ts` once nothing imports it.
- Optional: a sticky right-rail "System status" panel (Inspector-shaped) showing health/ready/version/uptime — gated on whether it adds value vs. the StatusFooterCard already in the sidebar; default off for this phase.

**Acceptance**:

1. Hero block height ≤ 30% of its current height.
2. `MetricStrip` renders six metrics; attention semantic surfaces on approvals + drift when non-zero.
3. `DashboardTile.svelte` is removed (file deleted, exports cleaned).
4. Visual regression baseline refreshed; svelte-check 0/0.

**Smoke**: extend `tests/landing.spec.ts` (new) — boots `/`, asserts hero heading, `[data-region="kpi"]` present, three section-card headings present.

---

### Step 4 — Detail-page sub-vocabulary applied to six detail pages

**Why this step ships fourth.** Defines the pattern; subsequent detail pages adopt it mechanically.

**Sub-vocabulary**:

```
PageHeader (full)
  └─ slot="breadcrumb"  →  Breadcrumbs back to parent list
  └─ slot="meta"        →  Status / identity badges (Badge tone)
  └─ slot="actions"     →  PageActionGroup (refresh + primary + destructive demoted)
[mini-KPI strip — MetricStrip compact, optional, 2–4 high-signal numbers]
Tabs (existing primitive)
  └─ tab body: stack of <section class="card"> with <h4> SECTION LABEL header
                 containing KeyValueGrid / Table / CodeBlock / decisions-row
```

**Per-page application**:

| Page | Tabs | Mini-KPI | Notes |
|---|---|---|---|
| `/servers/[id]` | Overview / Logs / Activity | Instances / Last activity / Log lines streamed | Move 4 header buttons into PageActionGroup; logs pane → `.card`. |
| `/skills/[id]` | Overview / Manifest / Bindings | Version / Tools required / Servers required | Body is currently a JSON dump — extract structured fields into Overview KeyValueGrid; CodeBlock stays under Manifest. |
| `/skills/authored/[id]` | Manifest / Files / Versions | Versions / Status / Last published age | Adopt `.card h4` pattern; per-row publish/archive into Inspector-style decisions card per version. |
| `/skills/sources/[name]` | Overview / Config / Packs | Packs / Last refresh age / Errors (attention if >0) | `.card h4` pattern; `Badge tone="danger"` on `last_error`; tabs replace stacked sections. |
| `/admin/tenants/[id]` | Overview / Quotas / Activity | Sessions / Requests/min / Retention | Add Breadcrumbs slot; activity tab uses the `Table` component (drop bespoke `<ul>`); mirror the `/admin/tenants` Inspector tab structure. |
| `/snapshots/[id]` | Overview / Servers / Tools / Resources / Prompts / Skills / Credentials | Total tools / Servers / Resources / Risky tools (attention if >0) | Resources/prompts/credentials currently invisible — surface them. Add "Compare to…" PageActionGroup action. |

**Acceptance**:

1. Every detail page has a Breadcrumbs slot pointing back to its parent list.
2. Every detail page uses `PageActionGroup` for header actions (no raw `<div slot="actions">` with multiple buttons).
3. Every detail page tab body is composed of `.card` sections with the `<h4>` section-label header.
4. `/snapshots/[id]` surfaces resources, prompts, credentials, and policies (currently absent despite existing in the payload).
5. svelte-check 0/0; visual baselines for each page captured.

**Smoke**: extend `tests/detail.spec.ts` (new) — for each detail page, navigate from the list page (clicking a row), assert breadcrumb + first tab heading + first KPI metric. This catches both routing and substrate-derivation regressions.

---

### Step 5 — Form-page sub-vocabulary applied to four form pages

**Why this step ships fifth.** Form internals are the highest-touch — small mistakes here look broken to the operator. Doing form pages last lets the detail-page work in Step 4 establish the section-card rhythm that forms mirror.

**Sub-vocabulary**:

```
PageHeader (compact for single-form, full for multi-step)
  └─ slot="breadcrumb"  →  Breadcrumbs back to parent
  └─ slot="actions"     →  PageActionGroup (Cancel + Submit; multi-step: Back / Next / Submit)
[SegmentedControl — multi-step only]
<section class="card"> per form section, with <h4> SECTION LABEL header
  └─ Inputs / Selects / Textareas, grouped logically
[Optional sticky right-rail Inspector for live validation feedback]
```

**Per-page application**:

| Page | Sections | Notes |
|---|---|---|
| `/servers/new` | (delegates to ServerForm — out of scope) | Add `Breadcrumbs`; PageHeader compact. ServerForm internals stay. |
| `/servers/[id]/edit` | (delegates to ServerForm — out of scope) | Same as above; load skeleton wraps in a `.card`. |
| `/skills/authored/new` | Manifest / SKILL.md / Prompts (tabs as today) + sticky Inspector for validation | Migrate `--color-surface-1` → `--color-bg-elevated`; validation aside becomes the actual `Inspector` primitive (consistent with list pages). Tabs stay as the in-editor surface. |
| `/admin/tenants/new` | Step 1 Identity / Step 2 Runtime / Step 3 Auth + summary | Replace bespoke `.steps` indicator with `SegmentedControl`. Fix the Back/Cancel labelling bug (currently "Cancel" calls `prev()` mid-flow). Add a fourth review step using `KeyValueGrid` so the operator can confirm before create. |

**Acceptance**:

1. All four form pages have a Breadcrumbs slot.
2. `/skills/authored/new` no longer references `--color-surface-1` and uses `Inspector` for the validation pane.
3. `/admin/tenants/new` uses `SegmentedControl`; the Back button reads "Back" (not "Cancel") in steps 2+; a review step lists the configured fields before submit.
4. svelte-check 0/0; visual baselines refreshed.

**Smoke**: extend `tests/forms.spec.ts` (new) — for `/admin/tenants/new`, walk through the wizard (Identity → Runtime → Auth → Review), asserting the SegmentedControl active state advances and the Back button label flips correctly. For `/skills/authored/new`, assert the `Inspector` validation panel renders.

---

### Step 6 — `/snapshots/[a]/diff/[b]` compare page

**Why this step ships last.** Compare is the only page that doesn't fit either the list or detail vocabulary. Doing it after the patterns are established prevents over-abstraction.

**Plan**:

- `PageHeader` with two `IdBadge` IDs in the meta slot (snapshot A and B), a swap action that flips the route, and a "Pick another snapshot…" PageActionGroup primary that opens a snapshot picker (Inspector-shaped or Modal — Modal is fine here since this is a one-shot interaction).
- Mini-KPI strip (compact `MetricStrip`): Added (across categories) / Removed / Modified / Unchanged. Attention surfaces on Removed when >0 (catalog regression).
- Filter chips above the body: All / Tools / Resources / Prompts / Skills / Only changes (default) — collapses categories with no diffs.
- Body: one `.card` per visible category, each with sub-sections for added / removed / modified using `Badge tone="success" / "danger" / "warning"` rows.

**Acceptance**:

1. KPI mini-strip shows Added / Removed (attention if >0) / Modified / Unchanged.
2. "Only changes" chip collapses empty categories.
3. Swap action flips A/B without a page reload.
4. svelte-check 0/0; visual baseline captured.

**Smoke**: extend the smoke from Step 4 — when navigating to `/snapshots/[id]` and clicking "Compare to…", a picker appears.

---

## Forbidden practices added by this phase

- ❌ Importing `DashboardTile` after Step 3 — the file should be removed entirely.
- ❌ Adding new `--color-surface-1` / `--color-surface-2` references — both tokens are migrated to `--color-bg-elevated` / `--color-bg-subtle` in Step 1.
- ❌ Stacking three or more `Table`s on one detail page without `Tabs` — `/snapshots/[id]` was the canonical example; Step 4 fixes it. Reviewers reject new offenders on sight.
- ❌ Hand-rolled step indicators in form pages — use `SegmentedControl` or `Tabs` (Step 5).

## Done definition

Phase 10.8 is done when:

1. All six steps' acceptance criteria pass.
2. svelte-check 0/0 across the console.
3. `npx playwright test` shows the new specs (`landing.spec.ts`, `skills-secondary.spec.ts`, `detail.spec.ts`, `forms.spec.ts`) all green; existing 39 pass count goes up by the new spec count.
4. `make preflight` 117 OK / 3 SKIP / 0 FAIL — unchanged (no Go-side changes).
5. Visual regression baselines refreshed for every touched page (12 existing + 14 redesigned = 26 total).
6. `grep -rn "DashboardTile\|--color-surface-1\|--color-surface-2" web/console/src` returns zero matches.
7. Every detail page has Breadcrumbs back to its parent list (verified by grep + the `detail.spec.ts` suite).

## Order of operations / commits

Each step is one PR (or one commit on the feature branch). Suggested commit titles:

1. `feat(phase-10.8): MetricStrip compact variant + surface-token migration`
2. `feat(phase-10.8): redesign /skills/authored and /skills/sources lists`
3. `feat(phase-10.8): retire DashboardTile, redesign root landing`
4. `feat(phase-10.8): detail-page sub-vocabulary across 6 pages`
5. `feat(phase-10.8): form-page sub-vocabulary + tenant wizard fix`
6. `feat(phase-10.8): snapshots compare redesign`

Total estimate: ~14–18 hours of focused implementation, matching the 10.6 phase shape (which was 6 steps × ~2 hours each with substrate work). The lighter parts (Steps 1, 2, 6) take ~2h each; Step 4 is the heaviest at ~5h because it touches six pages.

## Audit findings (captured 2026-05-08)

The per-page findings that grounded this plan are inlined here so the plan stands alone:

### Detail-page sub-vocabulary
**Composition**: `PageHeader` (full, not compact) + `Breadcrumbs` slot + `meta` slot for status/identity badges + `PageActionGroup` for actions → optional **mini-KPI strip** (compact MetricStrip variant; 2–4 high-signal numbers) → `Tabs` → tab body composed of `.card` sections using the `<h4 class="section-label">` pattern wrapping `KeyValueGrid`, `Table`, `CodeBlock`, or a `decisions-row` of buttons.

### Form-page sub-vocabulary
**Composition**: `PageHeader` (compact when single-form, full when multi-step) + `Breadcrumbs` + `PageActionGroup` (primary submit + secondary cancel; multi-step: Back/Next/Submit). Body is a stack of `.card` sections — each form section gets its own card with the section-label header. Multi-step wizards use `SegmentedControl` for step navigation. Validation aside (when present) becomes a sticky `Inspector`-shaped panel using the same `.layout.has-selection` two-column grid as the list pages.

### Notable findings flagged for follow-up
- `ServerForm.svelte` component powers `/servers/new` + `/servers/[id]/edit` and needs its own audit pass — deferred from Phase 10.8.
- `/admin/tenants/new` 3-step wizard has a labelling bug (Cancel reads as "Back" mid-flow). Fixed in Step 5 of this phase.
- Legacy `--color-surface-1` token references exist in `/skills/authored/[id]`, `/skills/authored/new`, `/skills/sources/[name]`. Migrated in Step 1.
