# Phase 7 — Design System Implementation

> Self-contained implementation plan. Builds on Phase 0–6. First V1-polish phase.

## Goal

Bring the Console up to the standard `docs/Design/design-system.md` describes: **soft architectural minimalism** — mineral neutrals, muted teal accent, restrained shadows, surfaces (not cards everywhere), Inter + JetBrains Mono + Newsreader, table-heavy operational rhythm, premium documentation feel. Replace the ad-hoc CSS variables and one-off styles in `web/console/` with a single token-driven component library that every existing page (servers, resources, prompts, sessions, skills, approvals, audit, snapshots, secrets) consumes uniformly. Land the brand foundation (logo placement, dark mode, empty-state illustrations, accessibility pass) so subsequent phases (CRUD, playground, telemetry replay) build on a coherent surface instead of bolting onto last-week's stylesheets.

After Phase 7 the Console looks like the mockup in `docs/Design/Portico.png` and feels like the design-system doc reads. Functionality is unchanged; visual identity, IA primitives, and component vocabulary land.

## Why this phase exists

Phases 0–6 shipped the engine, but the UX is currently a dozen disconnected templates that share a tiny `tokens.css`. Every later phase (CRUD forms, playground, replay timeline, distribution polish) requires consistent components: tables, tabs, inputs, badges, dialogs. Building those phases on the current foundation would either duplicate primitives or retrofit them later — both wasteful. Locking the design system *before* feature work is the cheaper path.

The mockup at `docs/Design/Portico.png` and the spec at `docs/Design/design-system.md` are the inputs. The arch logo at `docs/Design/portico-logo-assets-v2/` (PNG 256/512/1024 + SVG) is the brand mark.

## Prerequisites

Phases 0–6 complete. Specifically:

- SvelteKit Console builds via `npm run check && npm run lint && npm run build` (Phase 0).
- Typed REST client at `web/console/src/lib/api.ts` covers every shipped endpoint (Phases 0–6).
- Existing pages: `/`, `/servers`, `/servers/[id]`, `/resources`, `/prompts`, `/apps`, `/skills`, `/skills/[id]`, `/sessions`, `/approvals`, `/audit`, `/snapshots`, `/snapshots/[id]`, `/snapshots/[a]/diff/[b]`, `/admin/secrets`.
- Embedded into the Go binary via `//go:embed web/console/build/**` — no separate hosting.

## Deliverables

1. **Token system** — `web/console/src/lib/tokens.css` rewritten as the single source of truth for color, spacing, radius, typography, shadow, motion, surface hierarchy. Light + dark mode. Generated companion JSON file (`web/console/src/lib/tokens.json`) consumed by docs tooling and a future Storybook.
2. **Typography stack** — Inter (sans), JetBrains Mono (mono), Newsreader (optional editorial serif for hero/docs moments only). Self-hosted from `web/console/static/fonts/` so the Console works offline (no Google Fonts CDN). Type scale matches the doc's `display.xl` … `mono.sm` set.
3. **Component library** — primitives owned by us, all in `web/console/src/lib/components/`:
   - `Button.svelte` (primary/secondary/ghost/subtle/destructive × sm/md/lg, with leading/trailing icon slots and loading state)
   - `Input.svelte`, `Select.svelte`, `Textarea.svelte`, `Toggle.svelte`, `Checkbox.svelte`, `RadioGroup.svelte`
   - `Table.svelte` (sticky header, optional zebra, monospace ID column, hover emphasis, sortable columns, empty-state slot)
   - `Badge.svelte` (soft-bg variants per status: success/warning/danger/info/neutral, plus `risk-class` semantic tinting)
   - `Tabs.svelte`, `SegmentedControl.svelte`
   - `Toast.svelte` (drop-oldest queue, success/danger/info)
   - `Modal.svelte`, `Drawer.svelte`, `Popover.svelte`, `Dropdown.svelte`
   - `Sidebar.svelte`, `TopBar.svelte`, `Breadcrumbs.svelte`, `PageHeader.svelte`
   - `EmptyState.svelte` with the architectural-illustration motif
   - `CodeBlock.svelte` (JSON / YAML / shell syntax highlighting via Shiki at build time, no runtime download)
   - `KeyValueGrid.svelte` for the metadata blocks in detail pages
   - `StatusDot.svelte`, `Skeleton.svelte`, `Tooltip.svelte`
4. **Iconography** — Lucide via `lucide-svelte`, tree-shaken at build. One agreed set per surface (no mixed icon families).
5. **Surface hierarchy** — `surface.canvas` / `surface.base` / `surface.raised` / `surface.overlay` applied consistently. Page → panel → row → cell layering replaces the current "card-on-card" patterns.
6. **Dark mode** — system-respecting by default, manual toggle in the top bar, persisted in localStorage. CSS variables flip via a single `[data-theme="dark"]` selector on `<html>`.
7. **Migrated pages** — every existing page rebuilt to use the component library and tokens. Functional behavior identical; visual identity matches `docs/Design/Portico.png`.
8. **Brand placement** — the arch logo from `portico-logo-assets-v2/` in the top-bar, on `/healthz` HTML fallback (Phase 0 scaffolding), and as a hero element on the (still bare) landing route `/`.
9. **Accessibility** — focus rings on every interactive (3px ring using `accent.primary` at 18% alpha per spec), `Tab` / `Shift+Tab` navigation works through tables and forms, ARIA labels on icon-only buttons, color contrast ≥ AA on every surface.
10. **Embedded Storybook (optional V1.5)** — defer; see "Out of scope".

## Acceptance criteria

1. `web/console/src/lib/tokens.css` defines every token from `docs/Design/design-system.md` §5–§9. The file's structure matches the doc's section ordering so a reader can grep for "Brand accent" or "Spacing system" and land on the right block.
2. Light + dark modes both render every page legibly; switching themes is a single CSS-variable swap (no per-component theme conditionals).
3. No `.svelte` file outside `web/console/src/lib/components/` declares hard-coded colors, spacing, font sizes, radii, or shadows — all values come from CSS custom properties. Verified by a CI grep that fails on `#[0-9a-fA-F]{3,8}` / `\d+px` literals in `.svelte` files (excludes `tokens.css`, `static/`).
4. Every existing page renders without functional regression — manual smoke against the dev mode covers servers/resources/prompts/skills/approvals/audit/snapshots/secrets, plus a Playwright headless run that asserts page titles and basic navigation.
5. Typography is loaded from `web/console/static/fonts/` (no external CDN). Page-load network tab in DevTools shows no third-party font requests.
6. Lucide icons appear consistently (one icon family) across the Console; an audit script flags any other icon import.
7. Logo appears in `<aside class="sidebar">` brand block and on the landing page hero. SVG variant is the canonical source; PNG 512 is the favicon fallback.
8. Empty states across the Console use `EmptyState.svelte` with the architectural illustration. The skills/audit/approvals empty states all read consistently.
9. Keyboard navigation: every interactive can be reached via `Tab`; focus rings are visible on all states; Esc closes Modal/Drawer/Popover.
10. Color contrast: every text-on-surface pair passes WCAG AA (4.5:1 body, 3:1 large). Verified by a CI script that walks the token pairs.
11. `npm run check && npm run lint && npm run build` continues to pass with zero new errors or warnings.
12. Bundle size: the production `web/console/build/` stays under +50 KB gzipped vs. Phase 6 baseline (Lucide tree-shaking + font subsetting must keep this in check).

## Architecture

```
web/console/src/
├── lib/
│   ├── tokens.css            # the single token file (light + dark CSS vars)
│   ├── tokens.json           # generated; consumed by tooling
│   ├── api.ts                # typed REST client (unchanged)
│   ├── theme.ts              # localStorage-backed theme store
│   ├── components/           # component library (one file per primitive)
│   │   ├── Button.svelte
│   │   ├── Table.svelte
│   │   ├── ...
│   │   └── illustrations/    # SVG architectural motifs for EmptyState
│   └── icons.ts              # Lucide re-exports (single import surface)
├── routes/
│   ├── +layout.svelte        # imports tokens, picks theme, mounts Sidebar+TopBar
│   ├── +page.svelte          # landing (logo hero + status tiles)
│   ├── servers/+page.svelte  # rebuilt against new components
│   └── ...                   # every page rebuilt
└── static/
    └── fonts/                # Inter, JetBrains Mono, Newsreader (subset)
```

The token file is structured so a single CSS rule (`[data-theme="dark"]`) overrides every surface/text/border variable. Components reference variables only — no `@media (prefers-color-scheme)` inside components.

## Token file shape

`web/console/src/lib/tokens.css` (excerpt — full file owns the entire spec):

```css
:root {
  /* === Color: light mode (default) === */
  --color-bg-canvas:        #F6F4EF;
  --color-bg-surface:       #FBFAF7;
  --color-bg-elevated:      #FFFFFF;
  --color-bg-subtle:        #F1EEE8;

  --color-border-soft:      #E7E1D8;
  --color-border-default:   #D9D1C5;
  --color-border-strong:    #BDAF9E;

  --color-text-primary:     #1F252B;
  --color-text-secondary:   #58616B;
  --color-text-tertiary:    #7A848E;
  --color-text-muted:       #98A1A8;

  --color-icon-default:     #5E6872;
  --color-icon-subtle:      #8C959D;

  --color-accent-primary:        #2D6F73;
  --color-accent-primary-hover:  #245D61;
  --color-accent-primary-active: #1D4D50;
  --color-accent-primary-soft:   #D8EAE8;
  --color-accent-primary-subtle: #EAF4F3;
  --color-accent-on-primary:     #FFFFFF;

  --color-accent-warm:           #9A7653;
  --color-accent-warm-soft:      #EEE3D8;
  --color-accent-warm-subtle:    #F7F1EB;

  --color-success:           #2F7A55;
  --color-success-soft:      #DDEFE4;
  --color-warning:           #A36A1B;
  --color-warning-soft:      #F5E7CD;
  --color-danger:            #B24A3B;
  --color-danger-soft:       #F6DFDB;
  --color-info:              #356E9A;
  --color-info-soft:         #DCEAF4;

  /* === Typography === */
  --font-sans:  "Inter", ui-sans-serif, system-ui, sans-serif;
  --font-mono:  "JetBrains Mono", ui-monospace, monospace;
  --font-serif: "Newsreader", ui-serif, Georgia, serif;

  --font-size-display-xl: 3.5rem;   --font-line-display-xl: 1.14;
  --font-size-display-lg: 2.75rem;  --font-line-display-lg: 1.18;
  --font-size-heading-1:  2.25rem;  --font-line-heading-1:  1.22;
  --font-size-heading-2:  1.875rem; --font-line-heading-2:  1.26;
  --font-size-heading-3:  1.5rem;   --font-line-heading-3:  1.33;
  --font-size-heading-4:  1.25rem;  --font-line-heading-4:  1.4;
  --font-size-title:      1.125rem; --font-line-title:      1.44;
  --font-size-body-lg:    1.0625rem;--font-line-body-lg:    1.65;
  --font-size-body-md:    0.9375rem;--font-line-body-md:    1.6;
  --font-size-body-sm:    0.875rem; --font-line-body-sm:    1.57;
  --font-size-label:      0.8125rem;--font-line-label:      1.38;
  --font-size-mono-sm:    0.8125rem;--font-line-mono-sm:    1.54;

  /* === Spacing === */
  --space-1:  4px;   --space-2:  8px;   --space-3:  12px;  --space-4:  16px;
  --space-5:  20px;  --space-6:  24px;  --space-8:  32px;  --space-10: 40px;
  --space-12: 48px;  --space-16: 64px;  --space-20: 80px;  --space-24: 96px;

  /* === Radius === */
  --radius-xs:   6px;  --radius-sm:   8px;  --radius-md:  12px;
  --radius-lg:  16px;  --radius-xl:  20px;  --radius-pill: 999px;

  /* === Shadows === */
  --shadow-sm: 0 1px 2px rgba(16,24,40,.04);
  --shadow-md: 0 4px 12px rgba(16,24,40,.06);
  --shadow-lg: 0 12px 32px rgba(16,24,40,.08);
  --ring-focus: 0 0 0 3px rgba(45,111,115,.18);

  /* === Motion === */
  --motion-fast:    120ms;
  --motion-default: 180ms;
  --motion-panel:   220ms;
  --ease-default:   cubic-bezier(0.2, 0, 0, 1);
}

[data-theme="dark"] {
  --color-bg-canvas:        #111417;
  --color-bg-surface:       #171C20;
  --color-bg-elevated:      #1D2328;
  --color-bg-subtle:        #222A31;

  --color-border-soft:      #2A333B;
  --color-border-default:   #36414A;
  --color-border-strong:    #4A5864;

  --color-text-primary:     #F3F5F6;
  --color-text-secondary:   #C7CED3;
  --color-text-tertiary:    #97A3AD;
  --color-text-muted:       #73808B;

  --color-icon-default:     #A7B1B8;
  --color-icon-subtle:      #7E8A94;

  --color-success-soft:     #183726;
  --color-warning-soft:     #3A2A15;
  --color-danger-soft:      #44211D;
  --color-info-soft:        #1B3446;
}
```

`tokens.json` is generated from this file by a small Vite plugin so future tooling (Figma sync, Storybook, docs generators) reads structured data instead of regex-parsing CSS.

## Component library shape

Every primitive lives in `web/console/src/lib/components/`. The conventions:

- One default export per file. Named slots for content composition.
- Props typed in `<script lang="ts">`. Variant unions exhaustively typed (`type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'subtle' | 'destructive'`).
- Styles via `<style>` block referencing CSS variables only. No Tailwind.
- All interactive components surface `aria-*` props passthrough. Keyboard handlers explicit.
- Storybook-friendly story files (`.stories.svelte`) co-located but excluded from the production build.

`Button.svelte` shape (illustrative):

```svelte
<script lang="ts">
  export let variant: 'primary' | 'secondary' | 'ghost' | 'subtle' | 'destructive' = 'primary';
  export let size: 'sm' | 'md' | 'lg' = 'md';
  export let loading = false;
  export let disabled = false;
  export let type: 'button' | 'submit' | 'reset' = 'button';
  export let href: string | null = null;
</script>

{#if href}
  <a {href} class="btn {variant} {size}" data-loading={loading || null}>
    <slot name="leading" />
    <span class="label"><slot /></span>
    <slot name="trailing" />
  </a>
{:else}
  <button {type} class="btn {variant} {size}" {disabled} data-loading={loading || null}>
    <slot name="leading" />
    <span class="label"><slot /></span>
    <slot name="trailing" />
  </button>
{/if}

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border-radius: var(--radius-md);
    padding: 0 var(--space-4);
    font-family: var(--font-sans);
    font-weight: 500;
    transition: background var(--motion-fast) var(--ease-default),
                box-shadow var(--motion-fast) var(--ease-default);
  }
  .sm { height: 32px; font-size: var(--font-size-body-sm); }
  .md { height: 40px; font-size: var(--font-size-body-md); }
  .lg { height: 48px; font-size: var(--font-size-body-lg); }
  .primary {
    background: var(--color-accent-primary);
    color: var(--color-accent-on-primary);
  }
  .primary:hover { background: var(--color-accent-primary-hover); }
  .primary:active { background: var(--color-accent-primary-active); }
  .secondary {
    background: var(--color-bg-elevated);
    border: 1px solid var(--color-border-default);
    color: var(--color-text-primary);
  }
  .ghost {
    background: transparent;
    color: var(--color-text-secondary);
  }
  .destructive {
    background: var(--color-danger);
    color: white;
  }
  .btn:focus-visible { box-shadow: var(--ring-focus); outline: none; }
  .btn[data-loading] { cursor: progress; opacity: .85; }
</style>
```

`Table.svelte` shape (illustrative — sticky header + monospace ID + hover row):

```svelte
<script lang="ts">
  export let columns: Array<{ key: string; label: string; mono?: boolean; sortable?: boolean }>;
  export let rows: Array<Record<string, unknown>>;
  export let onRowClick: ((row: Record<string, unknown>) => void) | null = null;
  export let empty: string = 'No items.';
</script>

<div class="table-wrap">
  <table>
    <thead>
      <tr>{#each columns as c}<th class:sortable={c.sortable}>{c.label}</th>{/each}</tr>
    </thead>
    <tbody>
      {#each rows as row}
        <tr on:click={() => onRowClick?.(row)} class:clickable={!!onRowClick}>
          {#each columns as c}
            <td class:mono={c.mono}>{row[c.key] ?? '—'}</td>
          {/each}
        </tr>
      {/each}
      {#if rows.length === 0}
        <tr><td colspan={columns.length} class="empty"><slot name="empty">{empty}</slot></td></tr>
      {/if}
    </tbody>
  </table>
</div>

<style>
  table { width: 100%; border-collapse: separate; border-spacing: 0; }
  th, td {
    padding: var(--space-3) var(--space-4);
    text-align: left;
    border-bottom: 1px solid var(--color-border-soft);
  }
  thead th {
    position: sticky; top: 0;
    background: var(--color-bg-subtle);
    color: var(--color-text-tertiary);
    font-size: var(--font-size-label);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  tr.clickable:hover { background: var(--color-bg-subtle); cursor: pointer; }
  td.mono { font-family: var(--font-mono); font-size: var(--font-size-mono-sm); }
  td.empty { color: var(--color-text-muted); text-align: center; padding: var(--space-12); }
</style>
```

## Page migration plan

Each existing page rebuilt with the new components. Order minimises rework:

1. `+layout.svelte` — Sidebar + TopBar + theme toggle + tokens import. Establishes the surface hierarchy.
2. `/` — landing page with logo hero + status tiles (mock-data-driven for dev mode).
3. `/servers`, `/servers/[id]` — Table + Badge + KeyValueGrid + EmptyState.
4. `/resources`, `/prompts`, `/apps`, `/skills`, `/skills/[id]` — same primitives, prompt detail uses CodeBlock.
5. `/sessions` — Table + StatusDot.
6. `/approvals` — Table + Modal (for resolve), Toast for confirmation.
7. `/audit` — Table with filter chips + CodeBlock for payload preview.
8. `/snapshots`, `/snapshots/[id]`, `/snapshots/[a]/diff/[b]` — Table + Tabs + KeyValueGrid + diff layout.
9. `/admin/secrets` — Table + Modal-driven create form.

Each page PR is independent and reverts cleanly if the component shape needs revision.

## Implementation walkthrough

### Step 1 — Tokens + theme bootstrap

Write `tokens.css` end-to-end. Wire `+layout.svelte` to import it once and to read/write `data-theme` on `<html>` from a Svelte store. Theme toggle in TopBar.

### Step 2 — Self-host fonts

Subset Inter (Latin + Latin-Extended), JetBrains Mono (Latin), Newsreader (Latin) via `fonttools` or the `subfont` CLI. Drop into `web/console/static/fonts/`. `@font-face` declarations in `tokens.css` (or a sibling `fonts.css`) reference them. Verify in DevTools that no Google Fonts requests fire.

### Step 3 — Lucide consolidation

`web/console/src/lib/icons.ts` re-exports the icons the Console actually uses (~40 symbols). Every page imports from `$lib/icons`, not from `lucide-svelte` directly — keeps the tree-shake set obvious and bounded.

### Step 4 — Primitive components, one at a time

Order: Button → Input/Select → Table → Badge → Tabs → Modal → Toast → EmptyState → CodeBlock → Sidebar/TopBar/PageHeader → Skeleton → Tooltip. Each ships with a `.stories.svelte` driving usage examples (Storybook integration is V1.5 but the story files are valuable as living docs).

### Step 5 — Page migration

One page per commit. Run `npm run check` + visual diff after each. The layout migration first, then leaf pages.

### Step 6 — Accessibility audit

Run `axe-core` via the Playwright harness against every page. Fix any contrast or ARIA findings before closing the phase.

### Step 7 — Bundle audit

`npm run build` produces `web/console/build/`. Compare gzipped size against the Phase 6 baseline; if up by more than 50 KB, find the culprit (usually a non-tree-shaken dep) before merging.

## Test plan

### Unit / component

- `web/console/src/lib/components/__tests__/Button.test.ts` — variant rendering, loading state, click handler, disabled state. Use `@testing-library/svelte`.
- Same for Table (sort, row click, empty slot), Modal (Esc close, focus trap), Toast (queue), Tabs (keyboard nav), Tooltip (hover/focus).
- `tokens.test.ts` — assert every token used in components is defined in `tokens.css`. Walks the AST of every `.svelte` file, regexes `var(--...)`, and asserts the variable exists in the token file.

### Visual / a11y

- `web/console/tests/visual.spec.ts` — Playwright snapshot tests for every page in light + dark modes. Stored in `web/console/tests/__screenshots__/`. CI runs on Linux only (snapshots are platform-sensitive).
- `web/console/tests/a11y.spec.ts` — `axe-core` scan of every page. Zero violations.
- `web/console/tests/keyboard.spec.ts` — table tab order, modal focus trap, escape close.

### Integration

- The existing `make preflight` smoke covers HTTP surface. Add a Playwright phase-7 smoke (`scripts/smoke/phase-7.sh`) that:
  - Boots Portico dev mode + the Console build.
  - Loads `/` and asserts the logo `<img>` and tagline render.
  - Switches theme and asserts `data-theme="dark"` on `<html>`.
  - Walks the nav, asserts every page returns 200 + has its expected `<h1>`.

## Common pitfalls

- **Fonts in CDN by default.** SvelteKit + `@fontsource` packages default to bundling, but a misconfigured `app.html` can pull from `fonts.googleapis.com`. The CI smoke must assert no third-party font requests.
- **Lucide bundle bloat.** `import { Foo } from 'lucide-svelte'` works, but importing the barrel can trip Vite's tree-shaking on certain plugin combinations. Use the per-icon path: `import Foo from 'lucide-svelte/icons/foo'`.
- **Hard-coded colors creeping back.** Easy to do in Svelte where a quick `style="color: #2D6F73"` looks clean. CI grep enforces tokens.
- **Dark-mode contrast holes.** The doc's dark palette is fine for surfaces but watch the `.muted` text on `.subtle` background — verify the AA pair manually after migrating each page.
- **Theme flash on first paint.** SvelteKit SSG renders without knowing the theme. Use a small inline `<script>` in `app.html` that reads localStorage and sets `data-theme` synchronously *before* the first paint.
- **Sticky-header z-index wars.** Sidebar + sticky table headers + modals can fight. Define a z-index scale in tokens (`--z-sidebar: 10; --z-sticky: 20; --z-modal: 50; --z-toast: 70`) and reference it.
- **Visual snapshot brittleness.** Lock a single browser version (Chromium pinned in `playwright.config.ts`) and a deterministic font-render path; regenerate snapshots on intentional changes only.

## Out of scope

- **Storybook deployment.** Stories are co-located but the Storybook hosting target ships post-V1.
- **Component library externalisation.** Components stay inside `web/console`; extracting to a published npm package is a post-V1 nicety.
- **Animation library.** Motion stays at the spec's 120/180/220ms transitions via CSS. No GSAP / Motion One.
- **i18n.** All copy is English. The `next` boundary is a translation pass; Phase 7 lays the structure (no hard-coded strings inside components — strings via slots/props) so that work is mechanical.
- **Marketing site.** The brand-site mock in `Portico.png` is for reference only; the public website is a separate project that will reuse `tokens.json`.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `npm run check && npm run lint && npm run build` clean.
3. Visual diff against `docs/Design/Portico.png` reviewed by the user; deviations documented and approved.
4. Light and dark mode both pass the `axe-core` a11y scan with zero violations.
5. Bundle size delta vs. Phase 6 documented in the PR description.
6. Token coverage: 100% — no raw color/spacing literals in `.svelte` files.

## Hand-off to Phase 8

Phase 8 (Skill sources first-class) inherits the component library, the typed REST client, and the `EmptyState` / `Modal` / form primitives. Its first task is the in-Console "Add skill source" flow: a Modal that lets an operator paste a URL or browse to a local manifest, validates it, and persists. The skill source UX is built on top of the design system, not parallel to it.
