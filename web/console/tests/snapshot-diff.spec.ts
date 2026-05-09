import { test, expect } from '@playwright/test';

/**
 * Phase 10.8 Step 6 — Snapshots compare redesign.
 *
 * The diff endpoint returns 4xx on synthetic ids, so the page lands
 * on the error EmptyState. The PageHeader + Breadcrumbs still
 * render. We only assert the structural pieces; the live data path
 * is exercised manually since the dev binary's snapshot pair is
 * non-deterministic.
 */

const A = '__phase-10.8-diff-a__';
const B = '__phase-10.8-diff-b__';

test.describe('/snapshots/[a]/diff/[b] (phase 10.8)', () => {
  test('renders breadcrumbs back to /snapshots', async ({ page }) => {
    await page.goto(`/snapshots/${A}/diff/${B}`);
    const breadcrumbNav = page.getByLabel('Breadcrumb');
    await expect(breadcrumbNav).toBeVisible();
    const backLink = breadcrumbNav.getByRole('link', { name: /snapshots/i });
    await expect(backLink).toHaveAttribute('href', '/snapshots');
  });
});
