import { test, expect } from '@playwright/test';

/**
 * Phase 15.5 — Governance Console (/governance/*).
 *
 * Five operator screens: Customers, Teams, Virtual Keys, Budgets, Semantic
 * Cache. Per the §4.5.1 operator-UX gates, every list page has a visible + Add
 * CTA and is reachable from the sidebar; the VK create flow surfaces the secret
 * exactly once in a modal. The dev binary may have empty data, so list
 * assertions check the shell + CTA, not specific rows.
 */

const LIST_PAGES = [
  { path: '/governance/customers', heading: /^customers$/i, cta: /add customer/i },
  { path: '/governance/teams', heading: /^teams$/i, cta: /add team/i },
  { path: '/governance/virtual-keys', heading: /^virtual keys$/i, cta: /add virtual key/i },
  { path: '/governance/budgets', heading: /^budgets$/i, cta: /add budget/i }
] as const;

for (const { path, heading, cta } of LIST_PAGES) {
  test.describe(path, () => {
    test('renders with a + Add CTA', async ({ page }) => {
      await page.goto(path);
      await expect(page.getByRole('heading', { name: heading, level: 1 })).toBeVisible();
      await expect(page.getByRole('button', { name: cta }).first()).toBeVisible();
    });
  });
}

test.describe('/governance/cache', () => {
  test('renders config + stats + invalidate', async ({ page }) => {
    await page.goto('/governance/cache');
    await expect(page.getByRole('heading', { name: /^semantic cache$/i, level: 1 })).toBeVisible();
    await expect(page.getByRole('heading', { name: /^invalidate$/i })).toBeVisible();
  });
});

test.describe('virtual key create flow', () => {
  test('the + Add CTA opens a create form covering the full surface', async ({ page }) => {
    await page.goto('/governance/virtual-keys');
    await page
      .getByRole('button', { name: /add virtual key/i })
      .first()
      .click();
    await expect(page.getByLabel('Name')).toBeVisible();
    await expect(page.getByLabel(/scopes/i)).toBeVisible();
    await expect(page.getByLabel(/provider allowlist/i)).toBeVisible();
    await expect(page.getByLabel(/mcp server allowlist/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible();
  });

  test('creating a VK surfaces the secret exactly once', async ({ page }) => {
    await page.goto('/governance/virtual-keys');
    await page
      .getByRole('button', { name: /add virtual key/i })
      .first()
      .click();
    await page.getByLabel('Name').fill('e2e-vk-' + Date.now());
    await page.getByRole('button', { name: /^create$/i }).click();
    // The secret modal appears with a pk-portico-* token + a save acknowledgement.
    const secret = page.getByTestId('vk-secret');
    await expect(secret).toBeVisible();
    await expect(secret).toContainText('pk-portico-');
    await expect(page.getByRole('button', { name: /i've saved it/i })).toBeVisible();
  });
});
