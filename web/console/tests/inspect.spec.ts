import { test, expect } from '@playwright/test';

/**
 * Phase 11 — session inspector + bundle endpoints.
 *
 * The dev binary doesn't have a persisted session row by default
 * (sessions only land in the table when StampSession runs through
 * the snapshot binder). The inspector therefore renders an empty /
 * not-found state on a fresh build — we assert the *shell* is wired
 * (page exists, breadcrumbs render, REST calls hit the new
 * endpoints) rather than rich data. That matches the Phase 11 smoke
 * script's "endpoints are mounted" stance.
 */

test.describe('/sessions/[id] (phase 11)', () => {
  test('detail page renders with breadcrumbs', async ({ page }) => {
    await page.goto('/sessions/phase-11-smoke');
    await expect(page.getByRole('heading', { name: 'phase-11-smoke', level: 1 })).toBeVisible();
    await expect(page.getByRole('link', { name: /open inspector/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /export bundle/i })).toBeVisible();
  });

  test('imported sessions show the read-only badge', async ({ page }) => {
    await page.goto('/sessions/imported:phase-11-fixture');
    await expect(page.getByText(/imported.*read-only/i).first()).toBeVisible();
  });
});

test.describe('/sessions/[id]/inspect (phase 11)', () => {
  test('inspector shell loads', async ({ page }) => {
    await page.goto('/sessions/phase-11-smoke/inspect');
    await expect(page.getByRole('heading', { name: 'Session inspector', level: 1 })).toBeVisible();
    await expect(page.getByRole('button', { name: /refresh/i })).toBeVisible();
  });

  test('inspector hits the bundle endpoint', async ({ page }) => {
    let bundleCalls = 0;
    page.on('request', (req) => {
      if (req.url().includes('/api/sessions/phase-11-smoke/bundle')) {
        bundleCalls += 1;
      }
    });
    await page.goto('/sessions/phase-11-smoke/inspect');
    // Wait briefly for the load() to fire.
    await page.waitForTimeout(500);
    expect(bundleCalls).toBeGreaterThanOrEqual(1);
  });
});
