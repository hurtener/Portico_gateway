import { test, expect } from '@playwright/test';

/**
 * Phase 10.6 — design-vocabulary primitives.
 *
 * Covers MetricStrip, FilterChipBar, IdentityCell, Inspector, and
 * PageActionGroup against the /dev/preview route. Each test asserts
 * the primitive renders and exercises one path of its public API
 * (chip change, dropdown change, inspector toggle, split-button).
 *
 * The preview route is intentionally always-rendered — it's how
 * reviewers and CI confirm the primitives are visually intact. If a
 * future revision gates the route to dev-only, this spec needs to
 * switch to running against `npm run dev` or be allowed to fail.
 */

test.describe('phase 10.6 primitives', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/dev/preview');
    await expect(
      page.getByRole('heading', { name: /design vocabulary preview/i })
    ).toBeVisible();
  });

  test('MetricStrip renders five cards with the warning tint on the drift card', async ({
    page
  }) => {
    const strip = page.locator('[data-region="kpi"]');
    await expect(strip).toBeVisible();
    // 5 distinct metric cards by aria-label.
    for (const label of [
      'Servers',
      'Runtime processes',
      'Capabilities',
      'Policies',
      'Catalog drift'
    ]) {
      await expect(strip.getByLabel(label)).toBeVisible();
    }
    // The drift card carries the attention attribute → warning tint.
    await expect(strip.locator('[data-attention="true"]')).toHaveCount(1);
  });

  test('FilterChipBar reports chip and dropdown changes through the controlled state', async ({
    page
  }) => {
    const bar = page.locator('[data-region="filters"]');
    await expect(bar).toBeVisible();

    // The state line under the bar reflects parent-owned filter state.
    // It starts at chip=all / transport=— / runtime=— / search=—.
    const state = page.getByText(/^chip:/);
    await expect(state).toContainText('chip: all');

    // Click "Online" and verify the state line updates.
    await bar.getByRole('button', { name: /^Online/ }).click();
    await expect(state).toContainText('chip: online');

    // Pick stdio from the Transport dropdown.
    await bar
      .locator('select')
      .first()
      .selectOption({ value: 'stdio' });
    await expect(state).toContainText('transport: stdio');
  });

  test('IdentityCell renders mono and sans variants with deterministic glyphs', async ({
    page
  }) => {
    // Sans (server-style) row: "filesystem".
    const fs = page.getByText('filesystem', { exact: true }).first();
    await expect(fs).toBeVisible();

    // Mono (skill-style) row: "github.code-review".
    const skill = page.getByText('github.code-review', { exact: true });
    await expect(skill).toBeVisible();
    // The primary span carries the mono class via a parent wrapper.
    await expect(skill.locator('xpath=ancestor::*[contains(@class, "mono")][1]')).toBeVisible();
  });

  test('PageActionGroup renders the primary split-button with both halves', async ({
    page
  }) => {
    const toolbar = page.getByRole('toolbar', { name: /page actions/i });
    await expect(toolbar).toBeVisible();
    // Exact match avoids the "Add server options" chevron half also matching.
    await expect(toolbar.getByRole('button', { name: 'Add server', exact: true })).toBeVisible();
    // The split half opens a menu.
    await toolbar.getByRole('button', { name: /add server options/i }).click();
    await expect(page.getByRole('menuitem', { name: /register stdio/i })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: /register http/i })).toBeVisible();
  });

  test('Inspector toggles open/closed and switches tabs', async ({ page }) => {
    const inspector = page.locator('[data-region="inspector"]');
    await expect(inspector).toBeVisible();
    // Starts open.
    await expect(inspector.getByRole('tab', { name: 'Overview' })).toBeVisible();
    // Switch to Tools.
    await inspector.getByRole('tab', { name: 'Tools' }).click();
    await expect(inspector.getByText(/12 tools/i)).toBeVisible();
    // Close via the X button — empty state appears.
    await inspector.getByRole('button', { name: /close inspector/i }).click();
    await expect(inspector.getByText(/nothing selected/i)).toBeVisible();
  });
});
