import { test, expect } from '@playwright/test';

/**
 * Phase 10.8 Step 3 — Root landing redesign.
 *
 * Asserts the redesigned shell: compressed hero, MetricStrip in
 * place of the legacy DashboardTile grid, and the three section
 * cards (Recent approvals / Recent snapshots / Recent audit). The
 * dev binary may have empty data on every section, so the section
 * cards may be empty — the spec only checks that they render.
 */

test.describe('/ (phase 10.8 landing)', () => {
  test('renders the redesigned shell', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Portico Console', level: 1 })).toBeVisible();
    await expect(page.locator('[data-region="kpi"]')).toBeVisible();

    // Three section card headings — case-insensitive because the
    // <h4> uses uppercase letter-spacing CSS, not actual uppercase.
    await expect(page.getByRole('heading', { name: /recent approvals/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /recent snapshots/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /recent audit/i })).toBeVisible();
  });
});
