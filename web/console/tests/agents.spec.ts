import { test, expect } from '@playwright/test';

/**
 * Phase 14 — Agent Profiles Console (/agents).
 *
 * The headline consumer-binding screen: list + a right-rail Inspector covering
 * the full surface (basics → MCP → skills → models → scopes → bindings). The
 * create flow round-trips through POST /api/agent-profiles.
 */

test.describe('agent profiles', () => {
  test('renders the list with an Add CTA and is reachable from the sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: /agent profiles/i }).click();
    await expect(page).toHaveURL(/\/agents$/);
    await expect(page.getByRole('heading', { name: /^agent profiles$/i, level: 1 })).toBeVisible();
    // Operator-UX gate (§4.5.1): every list page has a visible + Add CTA.
    await expect(page.getByRole('button', { name: /add profile/i }).first()).toBeVisible();
  });

  test('the + Add CTA opens a create form covering the full surface', async ({ page }) => {
    await page.goto('/agents');
    await page
      .getByRole('button', { name: /add profile/i })
      .first()
      .click();

    // The Inspector opens the create form with the full surface (§4.5.1: the
    // create flow is reachable from the list and covers the plan-defined fields).
    await expect(page.getByRole('heading', { name: 'Basics' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'MCP surface' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Skills & models' })).toBeVisible();
    await expect(page.getByLabel('Name')).toBeVisible();
    await expect(page.getByLabel(/allowed mcp servers/i)).toBeVisible();
    await expect(page.getByLabel(/allowed model aliases/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible();
  });
});
