import { test, expect } from '@playwright/test';

/**
 * Phase 10.7a — Catalog triple (Resources / Prompts / Apps).
 *
 * Each page composes the Phase 10.6 vocabulary (KPI strip + filter
 * chip bar + table + sticky inspector). The dev binary may have an
 * empty catalog (no servers registered, no snapshot taken), so the
 * specs accept either populated or empty paths and assert only the
 * shell + region markers always render.
 */

const PAGES = [
  { path: '/resources', heading: 'Resources' },
  { path: '/prompts', heading: 'Prompts' },
  { path: '/apps', heading: 'MCP Apps' }
] as const;

for (const { path, heading } of PAGES) {
  test.describe(`${path} (phase 10.7a)`, () => {
    test('renders the redesign shell — header, KPI strip, filter bar', async ({ page }) => {
      await page.goto(path);
      await expect(page.getByRole('heading', { name: heading, level: 1 })).toBeVisible();

      const kpi = page.locator('[data-region="kpi"]');
      await expect(kpi).toBeVisible();

      const filters = page.locator('[data-region="filters"]');
      await expect(filters).toBeVisible();
      await expect(filters.getByRole('button', { name: /^All\b/ })).toBeVisible();
    });

    test('chip click pushes ?…= to the URL via replaceState', async ({ page }) => {
      await page.goto(path);
      const filters = page.locator('[data-region="filters"]');
      // Click any non-"All" chip; the URL gains a query param keyed
      // per the page (`category` / `args` / `binding`).
      const chips = filters.getByRole('button', { name: /\d+$/ });
      // Skip the "All" chip — its handler clears the param instead of
      // setting it. Find a non-All chip that isn't disabled.
      const count = await chips.count();
      let targetIdx = -1;
      for (let i = 0; i < count; i++) {
        const label = await chips.nth(i).innerText();
        if (!/^All\b/i.test(label)) {
          targetIdx = i;
          break;
        }
      }
      // Empty catalog → only "All" exists. Skip in that case.
      if (targetIdx === -1) test.skip(true, 'only the All chip rendered');
      await chips.nth(targetIdx).click();
      await expect(page).toHaveURL(/\?[^#]/);
    });
  });
}
