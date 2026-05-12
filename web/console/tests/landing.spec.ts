import { test, expect } from '@playwright/test';

/**
 * Phase 10.9 — Root landing reshape.
 *
 * Asserts the setup-and-status layout: KPI strip with Endpoint as
 * the leftmost card, Configuration Status panel, Quick Actions
 * panel. The recent-activity section stays but is demoted below.
 *
 * Dev binary may have empty data; we only assert structure.
 */

test.describe('/ (phase 10.9 landing)', () => {
  test('renders setup KPIs + status + quick actions', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Portico Console', level: 1 })).toBeVisible();

    // KPI strip with the new setup metrics.
    await expect(page.locator('[data-region="kpi"]')).toBeVisible();
    // The Endpoint metric card carries the bind URL — using getByLabel
    // rather than getByText because the value is a host:port string and
    // matches across multiple elements.
    await expect(page.getByLabel('Endpoint')).toBeVisible();

    // Configuration Status + Quick Actions cards.
    await expect(page.getByRole('heading', { name: /configuration status/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /quick actions/i })).toBeVisible();

    // Quick Actions row labels.
    await expect(page.getByText(/connect an agent/i)).toBeVisible();
    await expect(page.getByText(/add a server/i).first()).toBeVisible();

    // Recent activity is demoted but still renders.
    await expect(page.getByRole('heading', { name: /recent activity/i })).toBeVisible();
  });
});
