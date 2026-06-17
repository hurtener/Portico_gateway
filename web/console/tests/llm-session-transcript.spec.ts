import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM session transcript detail.
 *
 * /llm/sessions/{chat_id} renders one brokered conversation. A fresh e2e DB
 * has no recorded chats, so navigating to an arbitrary chat id must render
 * the "Session not found" state (a 404 from the API, distinct from the
 * gateway-not-configured case). This locks the route + 404 handling; a
 * populated transcript is exercised once a chat is routed through a provider.
 */

test.describe('llm session transcript', () => {
  test('unknown chat id renders the not-found state', async ({ page }) => {
    await page.goto('/llm/sessions/01HNOPE000000000000000000');
    await expect(
      page.getByRole('heading', { name: /session transcript/i, level: 1 })
    ).toBeVisible();
    await expect(page.getByRole('heading', { name: /session not found/i })).toBeVisible();
    await expect(page.getByRole('link', { name: /back to sessions/i }).first()).toBeVisible();
  });
});
