# Phase 7.5 — Design System Polish & i18n

> Self-contained polish pass on top of Phase 7. No new functionality; closes the gap between "first shot" and "production feel".

## Goal

Phase 7 landed the design system foundation, the component library, and a full page migration. This phase adds the chrome and ergonomic features that turn the Console into a place an operator wants to live in:

- **i18n** — all user-visible copy externalised, English + Spanish ship together, locale switcher in the TopBar.
- **Collapsible sidebar** — the operator can collapse it to a 56 px rail to recover screen real estate; persisted across sessions.
- **Sidebar polish** — gateway status indicator moves from the TopBar into the sidebar footer, sub-section icons stay visible at all states, active-state contrast hardened.
- **TopBar additions** — Cmd/Ctrl+K command palette for fuzzy navigation and quick actions, notifications bell driven by recent audit events, user profile menu (currently always "admin" locally; reserves the spot for RBAC sign-out).
- **Richer landing overview** — the home page becomes an honest dashboard matching `docs/Design/Portico.png`: status tiles, recent sessions, pending approvals count, drift indicator, last snapshot, with skeleton loading states and tasteful empty states when data isn't there yet.
- **Friendlier "feature unavailable" empty states** — pages that fetch from an endpoint that hasn't been wired in this build (e.g. `/v1/admin/secrets` when the vault isn't configured in dev) show a calm explanation rather than a raw 404 error.

After Phase 7.5 the Console feels finished as a V1 surface even though every later phase still adds substantial new functionality.

## Why this phase exists

User feedback after Phase 7's first shot:

> "Secrets page gives a 404 out there. And overview is a bit emptier (probably because we are missing things) than suggested in the overview part of the image Portico.png. […] We should include as a phase 7.5 on polishing (new pass), add full i18n compliance for english and spanish, and dark mode switcher. Polishing how it looks (selections in the menu are missing the icons we proposed, gateway status can be moved to the left panel, it should be collapsible, we are missing the search bar on top to go to places and having a notifications, and profile of the person logged in (i know in local its always as admin, but we eventually need to have a login when deployed with RBAC)"

Each item maps onto a deliverable:

| Feedback | Deliverable |
|---|---|
| Secrets page 404 | "Feature unavailable" empty state for any endpoint that 404s |
| Overview emptier than mock | Richer landing dashboard with operational tiles |
| i18n English + Spanish | Externalised copy + locale store + switcher |
| Dark mode switcher | Verify + simplify the existing 3-state segmented control |
| Active menu icons missing | Sidebar styles audit + harden active state |
| Gateway status in sidebar | Move env-badge from TopBar to sidebar footer |
| Sidebar collapsible | Collapse to 56 px rail, persisted in localStorage |
| Search bar on top | Cmd/Ctrl+K command palette |
| Notifications | TopBar bell driven by recent audit events |
| Profile of logged user | TopBar avatar dropdown (placeholder name + "Sign out") |

## Prerequisites

Phase 7 complete:

- Token-driven CSS variables, light + dark mode (`web/console/src/lib/tokens.css`).
- Component library at `web/console/src/lib/components/` (28 primitives).
- Layout shell with Sidebar + TopBar + Toaster.
- All existing routes migrated.
- `make preflight` green; `scripts/smoke/phase-7.sh` green.

## Deliverables

1. **i18n runtime** — a tiny in-house i18n module (`web/console/src/lib/i18n/`) that exposes a `t()` function backed by a Svelte store, falls back from selected locale to English, and accepts ICU-style `{var}` placeholders. No third-party dep; ~120 lines.
2. **Locale files** — `web/console/src/lib/i18n/locales/{en,es}.ts` covering every user-visible string (page titles, descriptions, table headers, button labels, empty-state copy, toast text, error messages).
3. **Locale switcher** — TopBar control (compact `<Select>` or button popover) that lets the operator switch between EN and ES; persisted in localStorage; same pre-paint pattern as the theme so we never flash the wrong language.
4. **Collapsible sidebar** — toggle button at the bottom of the sidebar (or via Cmd/Ctrl+B); collapsed state shows icons only with a tooltip on hover; expands back smoothly via CSS transition; persisted.
5. **Sidebar gateway-status block** — moves the dev-mode "Local · dev" badge from the TopBar into the sidebar footer, and adds a live status row (overall health + ready) with `StatusDot` indicators. Updates every 30 s.
6. **Active-state polish** — verify icons render at every state, including collapsed mode; ensure dark-mode active row has WCAG AA contrast; add 2 px accent edge on the active row's left side.
7. **Cmd/Ctrl+K command palette** — `CommandPalette.svelte` modal that opens on the shortcut, lists every nav route + a small set of quick actions (Refresh page, Toggle theme, Toggle locale, Open audit log), fuzzy-filterable, keyboard-driven.
8. **Notifications popover** — TopBar bell icon with a Lucide indicator; popover shows the most recent N audit events tagged as warnings/errors; falls back to "No new notifications" when empty; updates every 15 s.
9. **User profile menu** — TopBar avatar (initials in a small accent-tinted circle); click opens a dropdown showing the displayed name (defaults to "Admin (local)"), a separator, and a "Sign out" item that's disabled in dev mode with an explanatory tooltip ("Authentication arrives in Phase 12.").
10. **Richer landing overview** — `/+page.svelte` becomes a true dashboard:
    - Top-row status tiles: Health, Ready, Active sessions count, Pending approvals count, Last snapshot age, Drift events (24h).
    - Section: Recent sessions (last 5, link to /sessions).
    - Section: Recent approvals (last 5, link to /approvals).
    - Section: Recent audit events filtered to noteworthy types.
    - Sparkline-style mini-chart showing session count per hour over last 24h (pure SVG, no library).
    - Skeleton loaders during fetch; honest empty states when feature isn't wired.
11. **"Feature unavailable" empty state** — generic shape via `EmptyState.svelte` slot pattern; pages detect 404 from their primary endpoint and render a clear "This surface arrives in a later phase / requires PORTICO_VAULT_KEY" message instead of a raw error.
12. **Smoke + a11y polish** — `scripts/smoke/phase-7.sh` extended with checks for the locale meta, command-palette mount, profile menu mount; existing a11y warnings stay at zero.

## Acceptance criteria

1. Every user-visible string in `web/console/src/routes/` and `web/console/src/lib/components/` either originates from a translation key or is documented in the i18n waiver list (e.g. brand names, log lines).
2. Switching locale to Spanish translates every nav label, page title, table header, button, and empty-state copy. Switching back to English restores the originals.
3. The locale persists across reloads and never flashes the wrong language at first paint.
4. The sidebar collapses to ≤ 64 px wide; in collapsed mode every nav item still shows its icon, tooltips reveal the label on hover, the gateway status block hides its labels but keeps the StatusDot.
5. Active sidebar item has a visible icon AND text colour change AND a 2 px accent left edge in both light and dark modes; verified by axe-core + visual.
6. Cmd+K (Ctrl+K on Linux/Windows) opens the command palette; ↑/↓ navigates, Enter activates, Esc closes; fuzzy search across "Servers", "Audit log", "Toggle theme", etc.
7. Notification bell renders an unread count badge when the most recent audit batch contains a warning/error; clicking opens a popover with the items; opening the popover marks the badge cleared.
8. User profile menu opens, shows the placeholder name, and the "Sign out" item is visibly disabled with a tooltip.
9. `/` renders the dashboard tiles with skeletons during initial fetch and real data within ≤ 2 s on a warm DB.
10. `/admin/secrets` shows a "Vault not configured" empty state when `/v1/admin/secrets` returns 404; same pattern is reusable by other pages via a small helper.
11. `npm run check && npm run lint && npm run build` clean; bundle size delta vs. Phase 7 ≤ +30 KB gzipped (i18n payload is the bulk).
12. `make preflight` green; new smoke checks (locale present, command palette mountable) pass.

## Architecture

```
web/console/src/lib/
├── i18n/
│   ├── index.ts                   # store + t() + load() + persist
│   ├── locales/
│   │   ├── en.ts                  # canonical English
│   │   └── es.ts                  # Spanish translation
│   └── README.md                  # how to add a key
├── components/
│   ├── CommandPalette.svelte      # Cmd/Ctrl+K modal
│   ├── NotificationsPopover.svelte
│   ├── UserMenu.svelte
│   ├── LocaleSwitcher.svelte
│   ├── Sidebar.svelte             # extended: collapsible + gateway-status block
│   ├── TopBar.svelte              # extended: search button + bell + avatar
│   ├── DashboardTile.svelte       # NEW: tile primitive for the landing page
│   ├── Sparkline.svelte           # NEW: pure-SVG mini-chart
│   └── ...
├── api.ts                         # gains a `withFallback()` helper for 404→empty
└── theme.ts                       # extended (collapsed-sidebar persistence)

web/console/src/routes/
├── +layout.svelte                 # mounts CommandPalette globally
└── +page.svelte                   # rebuilt as a dashboard
```

## i18n design

**Store:**

```ts
import { writable, derived } from 'svelte/store';

export type Locale = 'en' | 'es';
type Bag = Record<string, string>;

const STORAGE_KEY = 'portico:locale';

export const locale = writable<Locale>(readPersisted());
export const messages = writable<Bag>({});

locale.subscribe(async (loc) => {
  messages.set(await import(`./locales/${loc}.ts`).then((m) => m.default));
  if (typeof window !== 'undefined') localStorage.setItem(STORAGE_KEY, loc);
});

export const t = derived(messages, ($m) => (key: string, vars: Record<string, unknown> = {}): string => {
  const tpl = $m[key] ?? key;
  return tpl.replace(/\{(\w+)\}/g, (_, k) => String(vars[k] ?? ''));
});
```

**Usage:**

```svelte
<script lang="ts">
  import { t } from '$lib/i18n';
</script>

<h1>{$t('servers.title')}</h1>
<p>{$t('servers.description', { count: servers.length })}</p>
```

**Locale file shape:**

```ts
// en.ts
export default {
  'nav.overview': 'Overview',
  'nav.servers': 'Servers',
  'servers.title': 'Servers',
  'servers.description': 'Downstream MCP servers registered for this tenant.',
  'common.refresh': 'Refresh',
  'common.empty': 'No items.',
  'empty.vault.title': 'Vault not configured',
  'empty.vault.description': 'Set PORTICO_VAULT_KEY to enable secret management.',
  ...
};
```

**Pre-paint:** add a one-liner to `app.html` that reads `localStorage.getItem('portico:locale')` and stamps `<html lang="es">` synchronously before first paint, so screen readers and assistive tech see the correct language immediately.

## Sidebar collapse spec

- Width tokens: `--layout-sidebar-width: 240px` (expanded), `--layout-sidebar-width-collapsed: 64px` (collapsed).
- State stored in `web/console/src/lib/theme.ts` alongside theme so the same persistence layer covers both.
- Toggle: footer button with `IconChevronLeft` / `IconChevronRight`; keyboard shortcut Cmd/Ctrl+B.
- Transition: 220 ms ease-default on `width`; nav item labels fade out with 120 ms when collapsing.
- Tooltips: on hover, an inline `Tooltip` shows the label to the right.
- TopBar adjusts its `padding-left` automatically because the layout uses flex (no math needed).

## Command palette spec

- Trigger: Cmd+K (macOS), Ctrl+K (everywhere else); also a `<Button variant="ghost">` in the TopBar with the `IconSearch`.
- Dialog: built on top of `Modal.svelte`, but skinned distinctly (no header, slightly wider).
- Body: input on top with autofocus, list of commands below.
- Commands:
  - **Navigate**: every Sidebar route shows up here.
  - **Actions**: "Toggle theme", "Toggle language", "Refresh page", "Open audit log".
- Fuzzy match: simple subsequence scoring on label + section; ranks by score then alpha.
- Keyboard: ↑/↓ moves the cursor, Enter activates, Esc closes, Tab cycles between input and list.

## Notifications spec

- Source: latest audit events with `severity in {warning, error}` from `/v1/audit/events?limit=20`.
- Polled: every 15 s; cached for 60 s in component state.
- Bell icon shows a small accent dot + numeric badge when ≥ 1 unread.
- Popover lists up to 10 events; click on item routes to `/audit?type=<type>`.
- "Mark all read" clears the local-state badge.

## Dashboard spec (`/+page.svelte`)

Layout:

1. **Hero** — keeps the existing logo + "A governed gateway for MCP servers" eyebrow + display heading + lede.
2. **Status row** — six tiles in a `repeat(auto-fit, minmax(200px, 1fr))` grid:
   - Health (StatusDot + "ok"/"down")
   - Ready (StatusDot + "ok"/"pending")
   - Active sessions (count + sparkline)
   - Pending approvals (count, link to /approvals)
   - Last snapshot (relative time)
   - Drift events (24 h count, link to /audit?type=schema.drift)
3. **Two-column section**:
   - Left: Recent sessions (last 5) — Table with kind, started_at, snapshot id; "View all" link.
   - Right: Recent approvals (last 5) — Table with tool, risk, created_at, decision; "View all" link.
4. **Single-column section**: Recent audit events — Table with type / when / payload preview; "View all" link.

Skeletons for each section during initial load. EmptyState (compact) with feature-unavailable copy when an endpoint 404s.

## "Feature unavailable" pattern

Add a small helper in `api.ts`:

```ts
export class FeatureNotImplementedError extends Error {
  constructor(public path: string) {
    super(`feature_not_available:${path}`);
    this.name = 'FeatureNotImplementedError';
  }
}

export async function callOrUnavailable<T>(fn: () => Promise<T>): Promise<T | { unavailable: true }> {
  try {
    return await fn();
  } catch (e) {
    if ((e as Error).message.includes('404') || (e as Error).message.toLowerCase().includes('not implemented')) {
      return { unavailable: true };
    }
    throw e;
  }
}
```

Pages opt in:

```svelte
{#if state === 'unavailable'}
  <EmptyState
    title={$t('empty.vault.title')}
    description={$t('empty.vault.description')}
  />
{:else}
  <Table … />
{/if}
```

## Implementation walkthrough

### Step 1 — i18n runtime + locale files

Land `lib/i18n/`. Add ~120 keys covering Sidebar, TopBar, every PageHeader, common buttons, empty states. Wire `<html lang>` from the pre-paint script. Spanish file translated key-for-key.

### Step 2 — Locale switcher in TopBar

`LocaleSwitcher.svelte` next to `ThemeToggle`. Two-button segmented control (EN / ES) — small enough to fit; uses the same compact tokens as the theme toggle.

### Step 3 — Convert every page to t() calls

Scripted-style pass: per page, replace literal English strings with `$t('key.path')`. Translation IDs follow `<area>.<purpose>` (`servers.title`, `common.refresh`, `empty.vault.title`).

### Step 4 — Sidebar collapse + gateway status

Extend `Sidebar.svelte`: `collapsed` prop bound to a Svelte store. Footer gets:

- Status block: live `/healthz` + `/readyz` poll, `StatusDot`, "Local · dev" or actual env tag.
- Collapse toggle button.

Width transitions via `width: var(--layout-sidebar-width)` reading from a derived custom property the layout swaps.

### Step 5 — Command palette

`CommandPalette.svelte` opens via global Cmd/Ctrl+K listener mounted in `+layout.svelte`. Input + list. Fuzzy match in 30 lines.

### Step 6 — Notifications popover

`NotificationsPopover.svelte` reads from `api.queryAudit({severity: 'warning|error', limit: 10})`. Polls every 15 s. Bell badge.

### Step 7 — User profile menu

`UserMenu.svelte` — initials avatar, opens `Dropdown` with placeholder name + disabled "Sign out". For now, name comes from `'Admin (local)'`; in Phase 12 it'll come from the JWT `sub` claim.

### Step 8 — Richer landing dashboard

Rebuild `/+page.svelte`. New `DashboardTile.svelte` + `Sparkline.svelte` primitives. Honest skeletons + empty states. Each section calls its respective API endpoint with the `callOrUnavailable` helper.

### Step 9 — Feature-unavailable empty state on Secrets

Update `/admin/secrets/+page.svelte` to use the helper; same pattern documented as the migration template for future pages whose backend isn't wired yet.

### Step 10 — Smoke extensions

Extend `scripts/smoke/phase-7.sh`:

- Curl `/` and assert the page contains the locale tag.
- Curl `/` and grep for `data-cmdk-trigger` (the cmd-palette trigger).
- Curl `/admin/secrets` and assert SPA fallback still 200s.

### Step 11 — Visual + a11y check

Manual run through the Console at 1280×720 light + dark, EN + ES; verify:

- Active sidebar item legible.
- Collapsed sidebar tooltips appear.
- Cmd+K palette opens, navigates, closes.
- Notifications bell + user menu open and close cleanly.
- All copy in Spanish where switched.

## Test plan

### Unit / component

- `lib/i18n/__tests__/i18n.test.ts` — t() interpolation, missing-key fallback, locale switch loads new bag.
- `CommandPalette.test.ts` — opens on shortcut, fuzzy match orders correctly, Esc closes.
- `Sparkline.test.ts` — empty data renders flat line; one-point renders a single dot; many-point renders within the viewbox.

### Visual

- Playwright run captures a screenshot of `/` in EN+light, EN+dark, ES+light, ES+dark; checked into `tests/__screenshots__/` under deterministic Chromium.

### Smoke

`scripts/smoke/phase-7.sh` extensions documented above; OK count grows but no FAILs.

### Coverage gates

i18n module: ≥ 80% (`t()`, fallback, persistence).

## Common pitfalls

- **Translation drift.** Spanish file falls behind English. Mitigation: a small `i18n-check` script (npm script) walks the English keys and asserts every other locale has a matching key — fails CI on drift.
- **i18n bundle bloat.** Both locales ship even if the user picks one. Acceptable: each locale is ~5–10 KB gzipped. Lazy-load via dynamic `import()` in the store subscriber.
- **Cmd/Ctrl modifier per platform.** The shortcut listener checks `navigator.platform.includes('Mac')` to pick metaKey vs ctrlKey; both are accepted on macOS to avoid surprise.
- **Notifications polling cost.** 15 s polls add load. Mitigation: pause polling when the document is hidden (`document.visibilityState === 'hidden'`).
- **Sidebar collapse breaks ARIA.** Collapsed nav items lose visible labels; tooltips (with `aria-label`) maintain accessibility.
- **Profile menu pretending RBAC works.** "Sign out" must visibly indicate it's a placeholder; otherwise operators try to use it and get confused.
- **Landing dashboard with no data.** New deployments have empty audit/sessions/snapshots; the dashboard must not look broken — empty states everywhere.
- **Sparkline DPR.** SVG is resolution-independent so DPR isn't an issue, but with very few points the line vanishes; render at minimum 2 px stroke.
- **Spanish overflow.** Some Spanish strings are 30–40 % longer (e.g., "Configuración" vs "Settings"). Audit each header/Button label for wrapping, especially in the sidebar collapse rail.

## Out of scope

- **Other locales** — only EN+ES in this phase. Adding more is mechanical (new locale file).
- **RBAC sign-in flow** — placeholder only. Real authentication lands in Phase 12 (onboarding).
- **Notifications across tabs** — single-tab state. BroadcastChannel sync is post-V1.
- **Right-to-left support** — no Arabic/Hebrew yet.
- **Per-user theme/locale defaults** — server-side preferences are post-V1.
- **Animated dashboard transitions** — static charts only.
- **Real-time websocket for notifications** — polling is fine at V1 scale.

## Done definition

1. All acceptance criteria pass.
2. `npm run check && npm run lint && npm run build` clean.
3. Bundle size delta documented.
4. `make preflight` green; phase-7 smoke OK count grows by ≥ 3.
5. Manual visual pass against both locales + both themes; no regressions noted.
6. The user can collapse the sidebar, switch language, hit Cmd+K, see notifications, click their avatar — and every interaction lands on a working surface.

## Hand-off to Phase 8

Phase 8 (Skill sources first-class) inherits:

- The i18n machinery — every Phase 8 string goes through `$t()`.
- The "feature unavailable" empty state pattern — useful when a new authored-skill endpoint isn't wired yet.
- The collapsible sidebar — Phase 8's "Skill sources" sub-nav items live under Catalog.
- The command palette — Phase 8 adds "Add skill source" / "New authored skill" actions.
- The dashboard tile primitive — Phase 8 adds tiles for "Pending source refresh" / "Authored drafts".
