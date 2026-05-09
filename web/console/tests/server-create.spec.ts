import { test, expect } from '@playwright/test';

/**
 * Phase 10.5 — the regression test that locks the Phase 9 gap shut.
 *
 * Phase 9 promised the operator could register a new MCP server from
 * the Console. The smoke covered the API endpoint but the UX flow
 * (`+ Add server` → fill form → submit → land on detail) had a broken
 * link from the list page and a shallow form. This test asserts the
 * Console-side flow end-to-end.
 *
 * The test uses a unique server id per run so re-runs against the same
 * data dir don't collide.
 */

test.describe('servers — create flow', () => {
  test('+ Add server reaches the form, submit registers via /api/servers', async ({ page }) => {
    const id = `e2e-srv-${Date.now()}`;

    // 1. List page renders with a visible "+ Add" CTA. Phase 9 shipped
    //    only Refresh — failing the click below would catch a regression.
    await page.goto('/servers');
    await expect(page.getByRole('heading', { name: /^servers$/i, level: 1 })).toBeVisible();

    const addLink = page.getByRole('link', { name: /add server/i }).first();
    await expect(addLink).toBeVisible();
    await addLink.click();

    // 2. New-server route renders with the full Phase 9 form surface.
    await expect(page).toHaveURL(/\/servers\/new$/);
    // Asterisks on required labels make exact regex anchors brittle —
    // anchor on the input attributes instead. Tabindex 0 inputs in
    // document order: id, display_name, transport, runtime_mode, command, args.
    const inputs = page.locator('input[type="text"]:visible');
    await expect(inputs.first()).toBeVisible();

    // 3. Fill stdio fields. /bin/true is a guaranteed-present binary on
    //    every CI runner; the mock servers are in-process so we don't
    //    need an actual MCP server to test the registration UX.
    const idInput = inputs.nth(0);
    const displayInput = inputs.nth(1);
    const commandInput = inputs.nth(2);
    await idInput.fill(id);
    await displayInput.fill(`E2E test ${id}`);
    await commandInput.fill('/bin/true');

    // 4. Submit. The form polls health for up to 3 s and then navigates
    //    to the detail page. We only assert the navigation; whether the
    //    binary started successfully is a supervisor concern.
    await page.getByRole('button', { name: /save & start/i }).click();
    await page.waitForURL(new RegExp(`/servers/${id}$`), { timeout: 8_000 });

    // 5. Detail page surfaces the server we just created. The Restart /
    //    Edit / Delete CTAs Phase 10.5 added must be present.
    await expect(page.getByRole('button', { name: /restart/i })).toBeVisible();
    await expect(page.getByRole('link', { name: /edit/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /delete/i })).toBeVisible();
  });
});
