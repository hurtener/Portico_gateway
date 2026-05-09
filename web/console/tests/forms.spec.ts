import { test, expect } from '@playwright/test';

/**
 * Phase 10.8 Step 5 — Form-page sub-vocabulary.
 *
 * Each form page now has a Breadcrumbs slot back to its parent, and
 * uses .card sections for grouping. The tenant wizard replaces the
 * bespoke step indicator with SegmentedControl and adds a Review
 * step.
 *
 * Specs are resilient to varying dev-binary state; we only assert
 * the structural pieces.
 */

const FORM_ROUTES = [
  { path: '/servers/new', parent: 'Servers', parentHref: '/servers' },
  {
    path: '/servers/__phase-10.8-edit-target__/edit',
    parent: 'Servers',
    parentHref: '/servers'
  },
  { path: '/skills/authored/new', parent: 'Authored', parentHref: '/skills/authored' },
  { path: '/admin/tenants/new', parent: 'Tenants', parentHref: '/admin/tenants' }
] as const;

for (const { path, parent, parentHref } of FORM_ROUTES) {
  test.describe(`${path} (phase 10.8 form shell)`, () => {
    test('renders breadcrumbs back to parent', async ({ page }) => {
      await page.goto(path);
      const breadcrumbNav = page.getByLabel('Breadcrumb');
      await expect(breadcrumbNav).toBeVisible();
      const backLink = breadcrumbNav.getByRole('link', { name: new RegExp(parent, 'i') });
      await expect(backLink).toHaveAttribute('href', parentHref);
    });
  });
}

test.describe('/admin/tenants/new wizard (phase 10.8)', () => {
  test('Back button reads "Back" not "Cancel" mid-flow', async ({ page }) => {
    await page.goto('/admin/tenants/new');

    // Step 1: Back button isn't shown; Cancel is.
    await expect(page.getByRole('button', { name: /^Cancel$/i })).toBeVisible();

    // Fill required field and advance.
    await page.getByLabel(/tenant id/i).fill('phase-10.8-test');
    await page.getByRole('button', { name: /^Next$/i }).click();

    // Step 2: Back button now visible (was "Cancel" pre-10.8).
    await expect(page.getByRole('button', { name: /^Back$/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Cancel$/i })).toHaveCount(0);
  });

  test('Review step lists the configured fields', async ({ page }) => {
    await page.goto('/admin/tenants/new');
    await page.getByLabel(/tenant id/i).fill('phase-10.8-review-test');
    await page.getByRole('button', { name: /^Next$/i }).click(); // → runtime
    await page.getByRole('button', { name: /^Next$/i }).click(); // → auth
    await page.getByRole('button', { name: /^Next$/i }).click(); // → review

    // Review step: shows the id we entered (twice — id + display_name
    // default), plus the Create button.
    await expect(page.getByText('phase-10.8-review-test').first()).toBeVisible();
    await expect(page.getByRole('button', { name: /create/i })).toBeVisible();
  });
});
