import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright config for the Console.
 *
 * The tests boot the actual Portico Go binary (built by `make build`)
 * because that's the production surface the Console runs against —
 * `embed.FS` serves the SvelteKit build, and the same listener answers
 * REST + MCP. Running against the binary catches integration gaps a
 * standalone vite dev server would hide.
 *
 * Phase 10.5 ships a minimal harness with one happy-path test for the
 * server-create flow (the gap that Phase 9 missed). Subsequent phases
 * are expected to grow this suite — every new operator-facing flow
 * gets a corresponding spec here.
 *
 * The webServer block boots `./bin/portico dev` on a fixed port. CI
 * builds the binary in a prior step; locally, `make build` does the
 * same.
 */
const PORT = Number(process.env.PORTICO_E2E_PORT ?? 28080);
const BINARY = process.env.PORTICO_E2E_BIN ?? '../../bin/portico';

export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',

  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    trace: 'on-first-retry'
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] }
    }
  ],

  webServer: {
    command: `${BINARY} dev -bind 127.0.0.1:${PORT} -data-dir .e2e-data`,
    url: `http://127.0.0.1:${PORT}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    stdout: 'pipe',
    stderr: 'pipe'
  }
});
