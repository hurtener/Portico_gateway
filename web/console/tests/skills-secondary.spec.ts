import { test, expect } from '@playwright/test';

/**
 * Phase 10.8 Step 2 — Skills sub-lists.
 *
 * /skills/authored and /skills/sources adopt the Phase 10.6 list
 * vocabulary (KPI strip + filter chips + Inspector). The dev binary
 * may have empty data on either page, so this spec asserts only the
 * shell + region marker.
 *
 * /skills/sources can return a "vault not configured" empty state
 * when the source store isn't wired (the page falls back to an
 * EmptyState in that case). The KPI strip only renders when state
 * is 'ready', so we accept either.
 */

test.describe('/skills/authored (phase 10.8)', () => {
  test('renders the redesign shell', async ({ page }) => {
    await page.goto('/skills/authored');
    await expect(page.getByRole('heading', { name: 'Authored skills', level: 1 })).toBeVisible();
    await expect(page.locator('[data-region="kpi"]')).toBeVisible();
  });
});

test.describe('/skills/sources (phase 10.8)', () => {
  test('renders the redesign shell', async ({ page }) => {
    await page.goto('/skills/sources');
    await expect(page.getByRole('heading', { name: 'Skill sources', level: 1 })).toBeVisible();

    const kpi = page.locator('[data-region="kpi"]');
    const unavailable = page.getByText(/no sources configured|empty/i);
    await expect
      .poll(
        async () =>
          (await kpi.isVisible().catch(() => false)) ||
          (await unavailable.isVisible().catch(() => false)),
        { timeout: 5_000 }
      )
      .toBeTruthy();
  });
});
