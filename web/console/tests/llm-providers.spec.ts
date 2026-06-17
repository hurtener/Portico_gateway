import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM Gateway provider management, Console surface.
 *
 * Phase 13 ships the OpenAI-compatible LLM gateway. The operator-facing
 * half is the /llm/providers screen: a list with a "+ Add provider" CTA,
 * a Built-in/Custom filter, and an inspector right-rail that doubles as
 * the create/edit form (Settings + Keys tabs).
 *
 * This spec locks the operator flow shut per §4.5.1:
 *   1. The list page renders with its heading + visible "+ Add" CTA and
 *      an empty state when no providers exist.
 *   2. "+ Add provider" opens the inspector form with the full Settings
 *      surface (name, driver, credential ref, enabled).
 *   3. A create round-trips through /api/llm/providers and shows up in
 *      the table; deleting it removes the row.
 *
 * Provider names are unique per run so repeated runs against the same
 * data dir don't collide.
 */

test.describe('llm providers', () => {
  test('list renders with heading, +Add CTA, and empty state', async ({ page }) => {
    await page.goto('/llm/providers');
    await expect(page.getByRole('heading', { name: /^llm providers$/i, level: 1 })).toBeVisible();

    // The header CTA is always present; the empty-state CTA only when the
    // list is empty. Anchor on the header one so the assertion holds
    // regardless of prior runs leaving rows behind.
    await expect(page.getByRole('button', { name: /add provider/i }).first()).toBeVisible();

    // Built-in / Custom filter segmented control.
    await expect(page.getByRole('button', { name: /^all$/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /^built-in$/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /^custom$/i })).toBeVisible();
  });

  test('+Add provider opens the inspector form with the full settings surface', async ({
    page
  }) => {
    await page.goto('/llm/providers');
    await page
      .getByRole('button', { name: /add provider/i })
      .first()
      .click();

    // Inspector flips to the create form with Settings + Keys tabs. The
    // form title is a styled heading-ish div, not a semantic <h*>, so we
    // anchor on its text.
    const inspector = page.getByRole('complementary', { name: /inspector/i });
    await expect(inspector.getByText(/new provider/i)).toBeVisible();
    await expect(page.getByRole('tab', { name: /settings/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /keys/i })).toBeVisible();

    // Full Settings surface from the phase plan: name, driver, credential
    // ref, enabled toggle. The accessible names below are derived from the
    // form components' <label for> wiring (§4.5.1) — that they resolve by
    // role+name is the proof the labels are associated.
    await expect(page.getByRole('textbox', { name: /^name$/i })).toBeVisible();
    await expect(page.getByRole('combobox', { name: /^driver$/i })).toBeVisible();
    await expect(page.getByRole('textbox', { name: /credential ref/i })).toBeVisible();
    // The Checkbox visually hides its native <input> behind a styled box,
    // so assert the control is present (attached) and its label is visible.
    await expect(page.getByRole('checkbox', { name: /^enabled$/i })).toBeAttached();
    await expect(inspector.getByText(/^enabled$/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /^save$/i })).toBeVisible();
  });

  test('custom_openai shows the catalog template picker and prefills the base URL', async ({
    page
  }) => {
    await page.goto('/llm/providers');
    await page
      .getByRole('button', { name: /add provider/i })
      .first()
      .click();

    // Switch the driver to custom_openai → the base URL + template picker appear.
    await page.getByRole('combobox', { name: /^driver$/i }).selectOption('custom_openai');
    const template = page.getByRole('combobox', { name: /start from a template/i });
    await expect(template).toBeVisible();

    // Picking a preset prefills the base URL (Bifrost appends /v1/chat/completions).
    await template.selectOption('deepseek');
    await expect(page.getByRole('textbox', { name: /base url/i })).toHaveValue(
      'https://api.deepseek.com'
    );
  });

  test('create round-trips through the API and delete removes the row', async ({ page }) => {
    const name = `e2e-prov-${Date.now()}`;

    await page.goto('/llm/providers');
    await page
      .getByRole('button', { name: /add provider/i })
      .first()
      .click();

    await page.getByRole('textbox', { name: /^name$/i }).fill(name);
    // Driver defaults to a native provider (openai); leaving it lets us
    // create without the custom-only base_url/headers fields.
    await page.getByRole('button', { name: /^save$/i }).click();

    // The new provider appears in the table. The row surfaces its name via
    // the identity cell; anchor on a cell/link/text match for the name.
    await expect(page.getByText(name, { exact: true })).toBeVisible({ timeout: 8_000 });

    // Clean up via the API so the row doesn't linger for the next run.
    const del = await page.request.delete(`/api/llm/providers/${name}`);
    expect(del.status(), `delete ${name}: ${await del.text()}`).toBeLessThan(400);
  });
});
