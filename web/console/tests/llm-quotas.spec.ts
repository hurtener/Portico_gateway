import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM Gateway per-tenant quota, Console surface.
 *
 * Unlike providers/models, a quota is a single per-tenant row, so /llm/quotas
 * is a settings form rather than a list: it loads the tenant's current limits
 * (or the built-in defaults) into four numeric fields and saves them via
 * PUT /api/llm/quota.
 *
 * The save test restores the defaults at the end so the shared dev data dir
 * doesn't leak a low limit into other specs that route LLM requests.
 */

const DEFAULTS = {
  requests_per_minute: 600,
  tokens_per_minute: 200000,
  tokens_per_day: 4000000,
  cost_usd_per_day: 100
};

test.describe('llm quotas', () => {
  test('renders the quota form with the four limit fields and CTAs', async ({ page }) => {
    await page.goto('/llm/quotas');
    await expect(page.getByRole('heading', { name: /^llm quotas$/i, level: 1 })).toBeVisible();

    await expect(page.getByRole('button', { name: /save/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /reset to defaults/i })).toBeVisible();

    // Number inputs render as spinbuttons; the accessible name comes from the
    // Input's <label for> wiring (§4.5.1).
    await expect(page.getByRole('spinbutton', { name: /requests per minute/i })).toBeVisible();
    await expect(page.getByRole('spinbutton', { name: /tokens per minute/i })).toBeVisible();
    await expect(page.getByRole('spinbutton', { name: /tokens per day/i })).toBeVisible();
    await expect(page.getByRole('spinbutton', { name: /cost \(usd\) per day/i })).toBeVisible();
  });

  test('edit + save round-trips through PUT /api/llm/quota', async ({ page, request }) => {
    try {
      await page.goto('/llm/quotas');
      const rpm = page.getByRole('spinbutton', { name: /requests per minute/i });
      await expect(rpm).toBeVisible();
      await rpm.fill('321');
      await page.getByRole('button', { name: /save/i }).click();

      // The PUT is reflected by the API.
      await expect
        .poll(async () => {
          const res = await request.get('/api/llm/quota');
          const body = await res.json();
          return body.requests_per_minute;
        })
        .toBe(321);
    } finally {
      // Restore defaults so we don't leave a tight limit behind.
      await request.put('/api/llm/quota', { data: DEFAULTS }).catch(() => undefined);
    }
  });
});
