import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM Gateway provider health, Console surface.
 *
 * /llm/health shows each configured provider cross-referenced with the
 * engine's view of its driver: an enabled provider on a known driver reads
 * Healthy, a disabled one reads Disabled, an enabled one on an unknown
 * driver reads Unhealthy. The test registers all three shapes via the API,
 * asserts the table reflects them, then cleans up.
 */

test.describe('llm health', () => {
  test('renders per-provider status cross-referenced with the engine', async ({
    page,
    request
  }) => {
    const ok = `e2e-h-ok-${Date.now()}`;
    const off = `e2e-h-off-${Date.now()}`;
    const bad = `e2e-h-bad-${Date.now()}`;

    await request.post('/api/llm/providers', {
      data: { name: ok, driver: 'openai', enabled: true }
    });
    await request.post('/api/llm/providers', {
      data: { name: off, driver: 'openai', enabled: false }
    });
    await request.post('/api/llm/providers', {
      data: { name: bad, driver: 'mystery-driver', enabled: true }
    });

    try {
      await page.goto('/llm/health');
      await expect(page.getByRole('heading', { name: /^llm health$/i, level: 1 })).toBeVisible();
      await expect(page.getByRole('button', { name: /refresh/i })).toBeVisible();

      // Each provider lands in the table with its computed status. Anchor each
      // assertion on the provider's row so statuses don't cross-match.
      await expect(page.getByRole('row', { name: new RegExp(`${ok}.*healthy`, 'i') })).toBeVisible({
        timeout: 8_000
      });
      await expect(
        page.getByRole('row', { name: new RegExp(`${off}.*disabled`, 'i') })
      ).toBeVisible();
      await expect(
        page.getByRole('row', { name: new RegExp(`${bad}.*unhealthy`, 'i') })
      ).toBeVisible();
    } finally {
      await request.delete(`/api/llm/providers/${ok}`).catch(() => undefined);
      await request.delete(`/api/llm/providers/${off}`).catch(() => undefined);
      await request.delete(`/api/llm/providers/${bad}`).catch(() => undefined);
    }
  });
});
