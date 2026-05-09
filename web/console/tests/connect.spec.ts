import { test, expect } from '@playwright/test';

/**
 * Phase 10.9 Step 2 — Connect page.
 *
 * Boots /connect against the dev binary, asserts the page renders
 * the KPI strip + the three quick-start snippet cards + the auth
 * card. Dev-mode build means auth.mode is "dev"; the page shows the
 * dev callout instead of JWT fields.
 */

test.describe('/connect (phase 10.9)', () => {
  test('renders gateway connection facts and quick-start snippets', async ({ page }) => {
    await page.goto('/connect');
    await expect(page.getByRole('heading', { name: 'Connect', level: 1 })).toBeVisible();

    // KPI strip with Endpoint / Auth / Tenant / Servers metrics.
    await expect(page.locator('[data-region="kpi"]')).toBeVisible();

    // Three snippet cards.
    await expect(
      page.getByRole('heading', { name: /claude desktop/i, level: 5 })
    ).toBeVisible();
    await expect(
      page.getByRole('heading', { name: /mcp inspector/i, level: 5 })
    ).toBeVisible();
    await expect(
      page.getByRole('heading', { name: /curl \(tools\/list\)/i, level: 5 })
    ).toBeVisible();

    // Authentication card.
    await expect(page.getByRole('heading', { name: /authentication/i })).toBeVisible();

    // Dev binary boots in dev mode → callout (not the JWT KeyValueGrid).
    await expect(page.getByText(/dev mode active/i)).toBeVisible();
  });
});
