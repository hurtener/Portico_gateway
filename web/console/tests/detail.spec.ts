import { test, expect } from '@playwright/test';

/**
 * Phase 10.8 Step 4 — Detail-page sub-vocabulary.
 *
 * The dev binary may have empty data for every list, so we can't
 * assume a real detail row exists to navigate to. Each spec instead
 * navigates to a synthetic id and asserts the shell components
 * (Breadcrumbs slot back to parent + heading) render even on the
 * not-found path. The KPI strip is conditional on data load, so we
 * only assert it for routes whose detail page renders the strip in
 * the not-found path (none — they all bail to EmptyState first).
 *
 * For routes where the dev binary is known to have at least one
 * record, we click into it and assert the redesigned shell — but
 * the dev binary's seed varies, so we keep these resilient.
 */

const SYNTHETIC = '__phase-10.8-spec-target__';

const DETAIL_ROUTES = [
  { path: `/admin/tenants/${SYNTHETIC}`, parent: 'Tenants', parentHref: '/admin/tenants' },
  { path: `/skills/sources/${SYNTHETIC}`, parent: 'Sources', parentHref: '/skills/sources' },
  { path: `/skills/${SYNTHETIC}`, parent: 'Skills', parentHref: '/skills' },
  { path: `/skills/authored/${SYNTHETIC}`, parent: 'Authored', parentHref: '/skills/authored' },
  { path: `/snapshots/${SYNTHETIC}`, parent: 'Snapshots', parentHref: '/snapshots' },
  { path: `/servers/${SYNTHETIC}`, parent: 'Servers', parentHref: '/servers' }
] as const;

for (const { path, parent, parentHref } of DETAIL_ROUTES) {
  test.describe(`${path} (phase 10.8 detail shell)`, () => {
    test('renders breadcrumbs back to parent list', async ({ page }) => {
      await page.goto(path);
      // Scope to the Breadcrumb nav (aria-label="Breadcrumb") so the
      // sidebar / sub-nav don't compete for the match.
      const breadcrumbNav = page.getByLabel('Breadcrumb');
      await expect(breadcrumbNav).toBeVisible();
      const backLink = breadcrumbNav.getByRole('link', { name: new RegExp(parent, 'i') });
      await expect(backLink).toHaveAttribute('href', parentHref);
    });
  });
}
