import { test, expect } from '@playwright/test';

/**
 * Phase 10.6 — `/skills` redesign locks.
 *
 * Mirrors the /servers spec plus skills-specific affordances:
 *   1. KPI strip + filter chip bar render and reflect aggregate substrate.
 *   2. Row click opens the Inspector and pushes ?selected= to URL.
 *   3. Bulk-select shows the action bar and reports the count.
 *   4. Pagination footer is present (and non-functional in single-page).
 *
 * The dev binary loads bundled skills (filesystem.search, github.code-
 * review, linear.triage, postgres.sql-analyst) so we don't have to
 * register fixtures via API.
 */

test.describe('skills page (phase 10.6)', () => {
  test('renders KPI strip, filter bar, table, and the inspector empty state', async ({ page }) => {
    await page.goto('/skills');
    await expect(page.getByRole('heading', { name: /^skills$/i, level: 1 })).toBeVisible();

    // KPI strip — five cards.
    const kpi = page.locator('[data-region="kpi"]');
    await expect(kpi).toBeVisible();
    await expect(kpi.getByLabel('Skills')).toBeVisible();
    await expect(kpi.getByLabel('Attached servers')).toBeVisible();
    await expect(kpi.getByLabel('Missing tools')).toBeVisible();

    // Filter bar.
    const filters = page.locator('[data-region="filters"]');
    await expect(filters).toBeVisible();
    await expect(filters.getByRole('button', { name: /^All\s+\d+/ })).toBeVisible();

    // At least one bundled skill row visible.
    await expect(page.getByText('github.code-review', { exact: true })).toBeVisible();

    // Inspector starts empty.
    const inspector = page.locator('[data-region="inspector"]');
    await expect(inspector.getByText(/no skill selected/i)).toBeVisible();
  });

  test('row click opens the inspector with mono identity + tabs', async ({ page }) => {
    await page.goto('/skills');

    const row = page.locator('tbody tr', { hasText: 'github.code-review' });
    await row.click();

    await expect(page).toHaveURL(/\/skills\?[^#]*selected=github\.code-review/);
    const inspector = page.locator('[data-region="inspector"]');
    await expect(inspector).toBeVisible();
    // Inspector header carries the mono id.
    await expect(inspector.getByText('github.code-review', { exact: true })).toBeVisible();
    // Default tab is Overview; switch to Assets.
    await inspector.getByRole('tab', { name: /assets/i }).click();
    await expect(inspector.getByRole('tab', { name: /assets/i })).toHaveAttribute(
      'aria-selected',
      'true'
    );
  });

  test('bulk-select reveals the action bar with selected count', async ({ page }) => {
    await page.goto('/skills');

    // No bulk bar before selection.
    await expect(page.locator('[data-region="bulk-actions"]')).toHaveCount(0);

    // The Checkbox component hides the underlying <input> with
    // pointer-events: none and renders a styled <span class="box"> as
    // the visible surface. Click the box (it lives inside the same
    // <label> the input is nested in, so the label's implicit
    // association toggles `checked`).
    const boxes = page.locator('tbody .box');
    await boxes.nth(0).click();
    await boxes.nth(1).click();

    const bar = page.locator('[data-region="bulk-actions"]');
    await expect(bar).toBeVisible();
    await expect(bar.getByText(/2 selected/i)).toBeVisible();
    await expect(bar.getByRole('button', { name: /enable/i })).toBeVisible();
    await expect(bar.getByRole('button', { name: /disable/i })).toBeVisible();

    // Cancel clears selection.
    await bar.getByRole('button', { name: /cancel/i }).click();
    await expect(page.locator('[data-region="bulk-actions"]')).toHaveCount(0);
  });

  test('pagination footer renders the showing-X-to-Y line', async ({ page }) => {
    await page.goto('/skills');
    const footer = page.locator('[data-region="pagination"]');
    await expect(footer).toBeVisible();
    await expect(footer.getByText(/showing 1 to \d+ of \d+ skills/i)).toBeVisible();
  });
});
