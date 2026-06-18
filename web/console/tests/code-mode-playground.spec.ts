import { test, expect } from '@playwright/test';

/**
 * Phase 13.5 — Code Mode playground, Console surface.
 *
 * /playground/code-mode browses the virtual stub files (GET /api/code-mode/files),
 * lets the operator write a snippet, and runs it (POST /api/code-mode/run)
 * against a synthetic Console code-mode session. The happy path runs a
 * pure-compute snippet and shows the result; a sandbox rejection shows the
 * typed error.
 */

test.describe('code mode playground', () => {
  test('renders the file tree and editor, and is linked from /playground', async ({ page }) => {
    await page.goto('/playground');
    await page.getByRole('link', { name: 'Code Mode →' }).click();
    await expect(page).toHaveURL(/\/playground\/code-mode$/);
    await expect(
      page.getByRole('heading', { name: /code mode playground/i, level: 1 })
    ).toBeVisible();
    await expect(page.getByRole('heading', { name: /tool files/i })).toBeVisible();
    await expect(page.getByLabel(/starlark snippet/i)).toBeVisible();
  });

  test('runs a pure-compute snippet and shows the result', async ({ page }) => {
    await page.goto('/playground/code-mode');
    const editor = page.getByLabel(/starlark snippet/i);
    await editor.fill('result = 6 * 7');
    await page.getByRole('button', { name: /^run$/i }).click();
    await expect(page.getByRole('heading', { name: /^result$/i })).toBeVisible();
    await expect(page.getByText('42', { exact: false })).toBeVisible();
  });

  test('shows a typed error for an unsafe snippet', async ({ page }) => {
    await page.goto('/playground/code-mode');
    const editor = page.getByLabel(/starlark snippet/i);
    await editor.fill('load("os", "system")\nresult = 1');
    await page.getByRole('button', { name: /^run$/i }).click();
    await expect(page.getByText(/unsafe/i)).toBeVisible();
  });
});
