import { test, expect } from '@playwright/test';

/**
 * Phase 13.5 — Code Mode observability dashboard, Console surface.
 *
 * /observability/code-mode renders the token-savings ROI rollup
 * (GET /api/code-mode/savings) plus the recent execution history
 * (GET /api/code-mode/executions). With no executions in a fresh dev DB the
 * page shows the metric strip (zeros) and the empty state for the table.
 */

test.describe('code mode observability', () => {
  test('renders the savings summary, range filter, and executions section', async ({ page }) => {
    await page.goto('/observability/code-mode');

    await expect(page.getByRole('heading', { name: /^code mode$/i, level: 1 })).toBeVisible();

    // Date-range segmented control.
    await expect(page.getByRole('button', { name: /last 7 days/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /last 30 days/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /all time/i })).toBeVisible();

    // Savings metric strip.
    await expect(page.getByText(/tokens saved \(est\)/i)).toBeVisible();
    await expect(page.getByText(/^executions$/i)).toBeVisible();

    // Recent executions section (empty state on a fresh dev DB).
    await expect(page.getByRole('heading', { name: /recent executions/i })).toBeVisible();
  });

  test('is reachable from the sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: /code mode/i }).click();
    await expect(page).toHaveURL(/\/observability\/code-mode$/);
  });
});
