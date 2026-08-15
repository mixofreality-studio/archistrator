/**
 * billing.spec — the Billing screen (route `/project/$projectId/billing`),
 * the UI surface for the Method core use case "Bill the User for Usage"
 * (see .coreUseCases in project.json).
 *
 * The billing backend (billingManager + Stripe via billingGatewayAccess) is
 * human-gated on Stripe provisioning and has no read endpoint yet — see
 * Billing.tsx's own doc comment. So this screen renders NO fake data and NO
 * fetch to a non-existent endpoint; it is a static, explicitly-pending
 * placeholder wired into nav. That pending state IS the accurate, honest
 * exercise of this use case against this repo's real data today: no
 * settlement cycle has run and no invoice exists.
 *
 * Pure-UI: static content, no backend call at all — needs only the SPA + a
 * Postgres-backed dev server (for the session gate).
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';
import { openSharedProject } from './support/flows.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('Billing renders the honest "backend not yet provisioned" pending state', async ({ page }) => {
  tagUseCase('bill-the-user-for-usage');

  await openSharedProject(page);
  const projectIdMatch = /\/project\/([^/]+)\/home$/.exec(page.url());
  const projectId = projectIdMatch?.[1];
  expect(projectId, 'expected openSharedProject to land on /project/$id/home').toBeDefined();

  await gotoApp(page, `/project/${String(projectId)}/billing`);
  await expect(page.getByTestId(TESTID.billingRoot)).toBeVisible();
  await expect(page.getByTestId(TESTID.billingPendingState)).toBeVisible();
  await expect(page.getByTestId(TESTID.billingPendingState)).toContainText(
    'Billing backend not yet provisioned',
  );

  await page.getByTestId(TESTID.billingHomeLink).click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
});
