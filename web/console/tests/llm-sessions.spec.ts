import { test, expect } from '@playwright/test';

/**
 * Phase 13 — LLM Gateway sessions observability, Console surface.
 *
 * /llm/sessions is a read-only list of conversations the gateway brokered.
 * A fresh e2e dev DB has no recorded chats, so the screen renders its empty
 * state; this spec locks the heading, the Refresh CTA, and the empty state.
 * (A populated transcript is covered once a chat is routed through a
 * configured provider.)
 */

test.describe('llm sessions', () => {
  test('renders heading, Refresh, and the empty state on a fresh DB', async ({ page }) => {
    await page.goto('/llm/sessions');
    await expect(page.getByRole('heading', { name: /^llm sessions$/i, level: 1 })).toBeVisible();
    await expect(page.getByRole('button', { name: /refresh/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /no sessions yet/i })).toBeVisible();
  });
});
