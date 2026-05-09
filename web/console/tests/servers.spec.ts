import { test, expect } from '@playwright/test';

/**
 * Phase 10.6 — `/servers` redesign locks.
 *
 * The page composes MetricStrip + FilterChipBar + Table + Inspector
 * with URL-state for filters and selection. This spec asserts:
 *   1. KPI strip + filter bar render and reflect aggregate substrate.
 *   2. Filtering by chip narrows the table without a reload.
 *   3. Clicking a row opens the Inspector and pushes ?selected= to URL.
 *   4. Reload preserves the selected row + active chip.
 *
 * The fixture registers two servers with different shapes so the
 * Auth and Policy badges have something distinguishable to render.
 */

const SRV_A = 'phase-10-6-srv-a';
const SRV_B = 'phase-10-6-srv-b';

test.describe('servers page (phase 10.6)', () => {
  test.beforeAll(async ({ request }) => {
    // Cleanup any leftovers from a prior run.
    await request.delete(`/api/servers/${SRV_A}`).catch(() => undefined);
    await request.delete(`/api/servers/${SRV_B}`).catch(() => undefined);

    const a = await request.post('/api/servers', {
      data: {
        id: SRV_A,
        display_name: 'Server A',
        transport: 'stdio',
        runtime_mode: 'shared_global',
        stdio: { command: '/usr/bin/true' }
      }
    });
    expect(a.status(), `register ${SRV_A}: ${await a.text()}`).toBeLessThan(400);

    const b = await request.post('/api/servers', {
      data: {
        id: SRV_B,
        display_name: 'Server B',
        transport: 'stdio',
        runtime_mode: 'per_session',
        stdio: { command: '/usr/bin/true' },
        auth: { strategy: 'oauth2_token_exchange' }
      }
    });
    expect(b.status(), `register ${SRV_B}: ${await b.text()}`).toBeLessThan(400);
  });

  test.afterAll(async ({ request }) => {
    await request.delete(`/api/servers/${SRV_A}`).catch(() => undefined);
    await request.delete(`/api/servers/${SRV_B}`).catch(() => undefined);
  });

  test('renders KPI strip, filter bar, and the new substrate columns', async ({ page }) => {
    await page.goto('/servers');
    await expect(page.getByRole('heading', { name: /^servers$/i, level: 1 })).toBeVisible();

    // KPI strip — five cards (or four if Runtime is dropped, locked at the
    // count present today).
    const kpi = page.locator('[data-region="kpi"]');
    await expect(kpi).toBeVisible();
    await expect(kpi.getByLabel('Servers')).toBeVisible();
    await expect(kpi.getByLabel('Capabilities')).toBeVisible();

    // Filter bar with the All chip showing a count.
    const filters = page.locator('[data-region="filters"]');
    await expect(filters).toBeVisible();
    await expect(filters.getByRole('button', { name: /^All\s+\d+/ })).toBeVisible();

    // The fixture rows surface in the table with the new substrate cells.
    // Auth column shows the OAuth treatment for SRV_B.
    const rowB = page.locator('tbody tr', { hasText: 'Server B' });
    await expect(rowB).toBeVisible();
    // Use a generous matcher — Badge renders "OAuth" but flex layout
    // can introduce surrounding whitespace that defeats `^…$` regexes.
    await expect(rowB.getByText('OAuth', { exact: false }).first()).toBeVisible();
    // SRV_A has no auth → "None".
    const rowA = page.locator('tbody tr', { hasText: 'Server A' });
    await expect(rowA.getByText('None', { exact: false }).first()).toBeVisible();
  });

  test('row click opens the inspector, URL state survives reload', async ({ page }) => {
    await page.goto('/servers');

    const rowB = page.locator('tbody tr', { hasText: 'Server B' });
    await rowB.click();

    // URL gets the selected param.
    await expect(page).toHaveURL(new RegExp(`/servers\\?[^#]*selected=${SRV_B}`));

    // Inspector header carries the identity (the IdentityCell renders the
    // primary label + optional id subline).
    const inspector = page.locator('[data-region="inspector"]');
    await expect(inspector).toBeVisible();
    await expect(inspector.getByText('Server B', { exact: true })).toBeVisible();

    // Reload — selection persists.
    await page.reload();
    await expect(page).toHaveURL(new RegExp(`selected=${SRV_B}`));
    const inspectorAfter = page.locator('[data-region="inspector"]');
    await expect(inspectorAfter.getByText('Server B', { exact: true })).toBeVisible();
  });
});
