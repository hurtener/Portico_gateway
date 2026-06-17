import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM Gateway cost dashboard, Console surface.
 *
 * /llm/cost has two halves: usage telemetry (per-tenant daily rollups
 * summarised over a date range, GET /api/llm/costs) and the global price
 * book (unit costs per provider driver+model, GET/PUT /api/llm/costs/prices).
 *
 * Without real upstream traffic there are no daily rollups, so the usage
 * half asserts the summary + empty state. The price book half is fully
 * exercisable: add a price through the form and confirm it round-trips.
 */

test.describe('llm cost', () => {
  test('renders the summary, range filter, and price book', async ({ page }) => {
    await page.goto('/llm/cost');
    await expect(page.getByRole('heading', { name: /^llm cost$/i, level: 1 })).toBeVisible();

    // Date-range segmented control.
    await expect(page.getByRole('button', { name: /last 7 days/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /last 30 days/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /all time/i })).toBeVisible();

    // Summary metric strip.
    await expect(page.getByText(/total cost/i)).toBeVisible();
    await expect(page.getByRole('heading', { name: /daily usage/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /price book/i })).toBeVisible();
  });

  test('adding a price round-trips through PUT /api/llm/costs/prices', async ({
    page,
    request
  }) => {
    const model = `e2e-priced-${Date.now()}`;

    await page.goto('/llm/cost');

    // The inline price form: driver, model, input/1k, output/1k.
    await page.getByPlaceholder(/driver/i).fill('openai');
    await page.getByPlaceholder(/provider model/i).fill(model);
    await page.getByPlaceholder(/input \/ 1k/i).fill('1.5');
    await page.getByPlaceholder(/output \/ 1k/i).fill('6');
    await page.getByRole('button', { name: /save price/i }).click();

    // The new price appears in the price-book table.
    await expect(page.getByText(model, { exact: true })).toBeVisible({ timeout: 8_000 });

    // And it is persisted server-side.
    const res = await request.get('/api/llm/costs/prices');
    const body = await res.json();
    const found = (body.prices ?? []).some(
      (p: { provider_model: string }) => p.provider_model === model
    );
    expect(found, `price ${model} should be persisted`).toBe(true);
  });
});
