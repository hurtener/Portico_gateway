import { test, expect } from '@playwright/test';

/**
 * Phase 10.7b — Observability + admin quartet.
 *
 * /audit, /snapshots, /admin/tenants, /admin/secrets all compose the
 * Phase 10.6 vocabulary. The dev binary may have empty data on every
 * page (no events / no snapshots / one tenant / no vault), so this
 * spec asserts only the shell + region markers + chip behaviour.
 */

const PAGES = [
  { path: '/audit', heading: 'Audit log', expectKpi: true },
  { path: '/snapshots', heading: 'Catalog snapshots', expectKpi: true },
  { path: '/admin/tenants', heading: 'Tenants', expectKpi: true },
  // Secrets falls back to "Vault not configured" when no key — KPI
  // strip only renders when state === 'ready'. We assert the page
  // either shows the unavailable empty state OR the KPI strip.
  { path: '/admin/secrets', heading: 'Vault secrets (admin)', expectKpi: false }
] as const;

for (const { path, heading, expectKpi } of PAGES) {
  test.describe(`${path} (phase 10.7b)`, () => {
    test('renders the redesign shell', async ({ page }) => {
      await page.goto(path);
      await expect(page.getByRole('heading', { name: heading, level: 1 })).toBeVisible();

      if (expectKpi) {
        const kpi = page.locator('[data-region="kpi"]');
        await expect(kpi).toBeVisible();
      } else {
        // Either the KPI strip OR the unavailable empty state must be present.
        const kpi = page.locator('[data-region="kpi"]');
        const unavailable = page.getByText(/vault not configured/i);
        await expect.poll(async () => {
          return (await kpi.isVisible().catch(() => false)) || (await unavailable.isVisible().catch(() => false));
        }, { timeout: 5_000 }).toBeTruthy();
      }
    });
  });
}
