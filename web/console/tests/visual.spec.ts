import { test, expect } from '@playwright/test';

/**
 * Phase 10.6 — visual regression baselines.
 *
 * Captures `/servers`, `/skills`, and `/dev/preview` in the four
 * combinations the design ships in: EN+light, EN+dark, ES+light,
 * ES+dark. First run generates baseline images under
 * `tests/__screenshots__/visual.spec.ts/`; subsequent runs diff
 * against them with `maxDiffPixelRatio: 0.05` (5%).
 *
 * The baseline directory is checked in so reviewers can see what's
 * "current" without booting the binary. To regenerate after a
 * deliberate visual change, run:
 *
 *     npx playwright test visual --update-snapshots
 *
 * Visual regression is famously flaky on font + animation + scrollbar
 * differences. Mitigations applied:
 *   - waitForFunction(document.fonts.ready) before each shot.
 *   - Reduced motion via prefers-reduced-motion override.
 *   - Fixed viewport at 1440x900.
 *   - maxDiffPixelRatio is generous (0.05) — catches structural
 *     regressions without screaming about a 1-pixel anti-alias drift.
 *
 * Committed baselines are darwin-only today. Linux CI looks for
 * `*-chromium-linux.png` baselines we don't ship, which makes the
 * suite fail with "snapshot doesn't exist." Skip on linux until the
 * linux baselines are regenerated and committed; tracked as Phase 12
 * polish.
 */
test.skip(
  process.platform === 'linux',
  'visual baselines are darwin-only; regenerate for linux before re-enabling on CI'
);

const SHOTS = [
  { theme: 'light', locale: 'en' },
  { theme: 'dark', locale: 'en' },
  { theme: 'light', locale: 'es' },
  { theme: 'dark', locale: 'es' }
] as const;

async function setup(page: import('@playwright/test').Page, theme: string, locale: string) {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  // Force theme + locale via localStorage before navigation so the
  // pre-paint script in app.html sees them.
  await page.addInitScript(
    ({ t, l }) => {
      try {
        // Storage keys are colon-namespaced — see theme.ts and i18n/index.ts.
        localStorage.setItem('portico:theme', t);
        localStorage.setItem('portico:locale', l);
      } catch {
        // ignore — Playwright on some hosts denies localStorage
      }
    },
    { t: theme, l: locale }
  );
}

async function settle(page: import('@playwright/test').Page) {
  await page.waitForFunction(() => document.fonts && document.fonts.ready);
  // Hide nav status dot pulse + topbar notification "unread" counter
  // animation by overriding their styles. Anything non-deterministic
  // that influences a screenshot has to either be removed or pinned.
  await page.addStyleTag({
    content: `
      *, *::before, *::after { animation: none !important; transition: none !important; }
      [data-region="kpi"] { will-change: auto !important; }
    `
  });
}

for (const { theme, locale } of SHOTS) {
  test.describe(`visual ${locale}+${theme}`, () => {
    test.beforeEach(async ({ page }) => {
      await setup(page, theme, locale);
    });

    test('servers landing', async ({ page }) => {
      await page.goto('/servers');
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await settle(page);
      await expect(page).toHaveScreenshot(`servers-${locale}-${theme}.png`, {
        maxDiffPixelRatio: 0.05,
        fullPage: false
      });
    });

    test('skills landing', async ({ page }) => {
      await page.goto('/skills');
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await settle(page);
      await expect(page).toHaveScreenshot(`skills-${locale}-${theme}.png`, {
        maxDiffPixelRatio: 0.05,
        fullPage: false
      });
    });

    test('design vocabulary preview', async ({ page }) => {
      await page.goto('/dev/preview');
      await expect(page.getByRole('heading', { name: /design vocabulary/i })).toBeVisible();
      await settle(page);
      await expect(page).toHaveScreenshot(`dev-preview-${locale}-${theme}.png`, {
        maxDiffPixelRatio: 0.05,
        fullPage: false
      });
    });
  });
}
