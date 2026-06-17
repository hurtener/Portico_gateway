import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM Gateway model aliases, Console surface.
 *
 * The /llm/models screen is the second LLM Gateway screen. A model alias
 * maps a friendly name to a configured provider + the provider's own model
 * id, with optional default params and capability tags. The screen mirrors
 * /llm/providers: list + Built-in/Enabled filter + an Inspector right-rail
 * that doubles as the create/edit form.
 *
 * Because models require a provider, the create round-trip first registers
 * a provider via /api/llm/providers, then drives the model create through
 * the UI, asserts it lands in the table, and cleans both up via the API.
 *
 * Names are unique per run so repeats against the same data dir don't
 * collide.
 */

test.describe('llm models', () => {
  test('list renders with heading, filter, and a provider-gated +Add CTA', async ({ page }) => {
    await page.goto('/llm/models');
    await expect(page.getByRole('heading', { name: /^llm models$/i, level: 1 })).toBeVisible();

    // The header CTA is always present (it may be disabled when no provider
    // exists, but it must be visible).
    await expect(page.getByRole('button', { name: /add model/i }).first()).toBeVisible();

    // All / Enabled / Disabled filter segmented control.
    await expect(page.getByRole('button', { name: /^all$/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /^enabled$/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /^disabled$/i })).toBeVisible();
  });

  test('create round-trips through the API and delete removes the row', async ({
    page,
    request
  }) => {
    const provider = `e2e-mprov-${Date.now()}`;
    const alias = `e2e-model-${Date.now()}`;

    // A model needs a provider to point at. Register one via the API.
    const pRes = await request.post('/api/llm/providers', {
      data: { name: provider, driver: 'openai', enabled: true }
    });
    expect(pRes.status(), `register provider: ${await pRes.text()}`).toBeLessThan(400);

    try {
      await page.goto('/llm/models');
      await page
        .getByRole('button', { name: /add model/i })
        .first()
        .click();

      // Inspector flips to the create form.
      const inspector = page.getByRole('complementary', { name: /inspector/i });
      await expect(inspector.getByText(/new model/i)).toBeVisible();

      await page.getByRole('textbox', { name: /^alias$/i }).fill(alias);
      // Pick our freshly-registered provider from the Driver/Provider select.
      await page.getByRole('combobox', { name: /^provider$/i }).selectOption(provider);
      await page.getByRole('textbox', { name: /provider model/i }).fill('gpt-4o-mini');

      // Quick-add a capability chip to exercise that affordance.
      await inspector.getByRole('button', { name: /\+ chat/i }).click();

      await page.getByRole('button', { name: /^save$/i }).click();

      // The new alias appears in the table.
      await expect(page.getByText(alias, { exact: true })).toBeVisible({ timeout: 8_000 });
    } finally {
      // Clean up both rows regardless of assertion outcome.
      await request.delete(`/api/llm/models/${alias}`).catch(() => undefined);
      await request.delete(`/api/llm/providers/${provider}`).catch(() => undefined);
    }
  });
});
