import { test, expect } from '@playwright/test';

/**
 * Phase 16 — A2A Peers Console (/a2a/peers).
 *
 * Verifies the list page shell, the "+ Add peer" CTA, and the create/edit/
 * delete flow. The dev binary may have empty data, so list assertions check
 * the shell + CTA, not specific rows.
 */

test.describe('/a2a/peers', () => {
  test('renders with a heading and + Add peer CTA', async ({ page }) => {
    await page.goto('/a2a/peers');
    await expect(page.getByRole('heading', { name: /^a2a peers$/i, level: 1 })).toBeVisible();
    await expect(page.getByRole('button', { name: /add peer/i }).first()).toBeVisible();
  });

  test('sidebar has A2A nav entry', async ({ page }) => {
    await page.goto('/a2a/peers');
    await expect(page.getByRole('link', { name: /^peers$/i })).toBeVisible();
  });
});

test.describe('a2a peers create flow', () => {
  test('+ Add peer opens a create form covering the full surface', async ({ page }) => {
    await page.goto('/a2a/peers');
    await page
      .getByRole('button', { name: /add peer/i })
      .first()
      .click();
    await expect(page.getByLabel('Name')).toBeVisible();
    await expect(page.getByLabel('Endpoint URL')).toBeVisible();
    await expect(page.getByLabel(/egress auth ref/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible();
  });

  test('creating a peer adds it to the list', async ({ page }) => {
    await page.goto('/a2a/peers');
    await page
      .getByRole('button', { name: /add peer/i })
      .first()
      .click();
    const ts = Date.now();
    const peerName = 'e2e-peer-' + ts;
    await page.getByLabel('Name').fill(peerName);
    await page.getByLabel('Endpoint URL').fill('https://agent.example.com/a2a');
    await page.getByRole('button', { name: /^create$/i }).click();
    // Inspector closes; peer appears in list.
    await expect(page.getByText(peerName)).toBeVisible();
  });

  test('clicking a row opens the edit inspector', async ({ page }) => {
    await page.goto('/a2a/peers');
    // Create a peer first so we have something to click.
    await page
      .getByRole('button', { name: /add peer/i })
      .first()
      .click();
    const ts = Date.now();
    const peerName = 'e2e-edit-peer-' + ts;
    await page.getByLabel('Name').fill(peerName);
    await page.getByLabel('Endpoint URL').fill('https://agent2.example.com/a2a');
    await page.getByRole('button', { name: /^create$/i }).click();
    await expect(page.getByText(peerName)).toBeVisible();

    // Click the row to open edit inspector.
    await page.getByText(peerName).click();
    await expect(page.getByRole('button', { name: /^save$/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /^delete$/i })).toBeVisible();
  });

  test('deleting a peer removes it from the list', async ({ page }) => {
    await page.goto('/a2a/peers');
    // Create a peer to delete.
    await page
      .getByRole('button', { name: /add peer/i })
      .first()
      .click();
    const ts = Date.now();
    const peerName = 'e2e-delete-peer-' + ts;
    await page.getByLabel('Name').fill(peerName);
    await page.getByLabel('Endpoint URL').fill('https://agent3.example.com/a2a');
    await page.getByRole('button', { name: /^create$/i }).click();
    await expect(page.getByText(peerName)).toBeVisible();

    // Open it and delete.
    await page.getByText(peerName).click();
    await page.getByRole('button', { name: /^delete$/i }).click();
    await expect(page.getByText(peerName)).not.toBeVisible();
  });
});
