import { test, expect } from '@playwright/test';

/**
 * Phase 10.7c — Governance trio.
 *
 * /sessions, /approvals, and /policy all compose the Phase 10.6
 * vocabulary. The dev binary may have empty data on every page (no
 * pending approvals, no active sessions, no rules), so this spec
 * asserts only the shell + region markers and that the empty state
 * is reachable.
 *
 * /sessions has no API; it derives from snapshots + audit. The KPI
 * strip renders even when the totals are zero, so the assertion is
 * the same as for /approvals.
 */

const PAGES = [
  { path: '/sessions', heading: 'Sessions' },
  { path: '/approvals', heading: 'Pending approvals' },
  { path: '/policy', heading: 'Policy rules' }
] as const;

for (const { path, heading } of PAGES) {
  test.describe(`${path} (phase 10.7c)`, () => {
    test('renders the redesign shell', async ({ page }) => {
      await page.goto(path);
      await expect(page.getByRole('heading', { name: heading, level: 1 })).toBeVisible();
      await expect(page.locator('[data-region="kpi"]')).toBeVisible();
    });
  });
}
