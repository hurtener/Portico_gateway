import { test, expect } from '@playwright/test';

/**
 * Phase 10.9 Step 2 — Connect page.
 *
 * Boots /connect against the dev binary, asserts the page renders
 * the KPI strip + the three quick-start snippet cards + the auth
 * card. Dev-mode build means auth.mode is "dev"; the page shows the
 * dev callout instead of JWT fields.
 */

test.describe('/connect (phase 10.9)', () => {
  test('renders gateway connection facts and quick-start snippets', async ({ page }) => {
    await page.goto('/connect');
    await expect(page.getByRole('heading', { name: 'Connect', level: 1 })).toBeVisible();

    // KPI strip with Endpoint / Auth / Tenant / Servers metrics.
    await expect(page.locator('[data-region="kpi"]')).toBeVisible();

    // Three snippet cards.
    await expect(page.getByRole('heading', { name: /claude desktop/i, level: 5 })).toBeVisible();
    await expect(page.getByRole('heading', { name: /mcp inspector/i, level: 5 })).toBeVisible();
    await expect(page.getByRole('heading', { name: /curl/i, level: 5 })).toBeVisible();

    // Authentication card.
    await expect(page.getByRole('heading', { name: /authentication/i })).toBeVisible();

    // Dev binary boots in dev mode → callout (not the JWT KeyValueGrid).
    await expect(page.getByText(/dev mode active/i)).toBeVisible();
  });

  /**
   * Regression: the curl snippet on /connect must (a) be parseable as
   * a `\`-continued shell command — comments inside the continuation
   * break it — and (b) actually succeed against the live gateway.
   *
   * The original Phase 10.9 snippet did `tools/list` which is rejected
   * with "session not found" because MCP requires `initialize` first.
   * This spec ensures the snippet keeps doing the initialize handshake
   * (which alone is enough to prove the gateway reachable + healthy).
   */
  test('curl snippet hits the live gateway and returns a session id', async ({ page, request }) => {
    await page.goto('/connect');
    await page.waitForLoadState('networkidle');

    const curl = (await page.locator('pre.raw').nth(2).innerText()).trim();

    // Must do an initialize call — the only one that works without an
    // existing session.
    expect(curl).toMatch(/"method":"initialize"/);
    // Comments must be at top, never inside a `\`-continuation line.
    // Catches the original bug where `# comment` between `-H ... \`
    // and `-d ...` got interpreted as a separate statement.
    const lines = curl.split('\n');
    for (let i = 0; i < lines.length - 1; i++) {
      if (lines[i].endsWith('\\')) {
        expect(
          lines[i + 1].trimStart().startsWith('#'),
          `line ${i + 1} is a comment after a \\-continuation; bash will misparse`
        ).toBe(false);
      }
    }

    // Pull the JSON body and send it directly so we don't depend on a
    // local shell. Mirrors what the curl would do.
    const m = curl.match(/-d\s+'(\{.*\})'/s);
    expect(m, 'curl missing JSON body').not.toBeNull();
    const body = JSON.parse(m![1]);

    const res = await request.post('/mcp', {
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json, text/event-stream'
      },
      data: body
    });
    expect(res.status()).toBe(200);
    // Initialize must return a session id so subsequent calls can
    // resume — that's the load-bearing connectivity proof.
    expect(res.headers()['mcp-session-id']).toBeTruthy();
  });
});
