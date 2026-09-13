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
 *
 * SAFETY (fix-G review ruling): runs under the shared dispatch guard, over a
 * project faked in the browser (openStubbedProject). It creates nothing. It used
 * to open the run's shared project, and in CI, as the first spec to ask on an
 * empty catalog, it was the one that created it.
 */
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, gotoApp } from './support/gating.js';
import { openStubbedProject } from './support/flows.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The project faked in the browser for this spec. */
const PROJECT_ID = 'uitest-billing-stub';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('Billing renders the honest "backend not yet provisioned" pending state', async ({
  page,
  dispatchGuard,
}) => {
  tagUseCase('bill-the-user-for-usage');

  await openStubbedProject(page, PROJECT_ID, 'Billing Stub Project');

  await gotoApp(page, `/project/${PROJECT_ID}/billing`);
  await expect(page.getByTestId(TESTID.billingRoot)).toBeVisible();
  await expect(page.getByTestId(TESTID.billingPendingState)).toBeVisible();
  await expect(page.getByTestId(TESTID.billingPendingState)).toContainText(
    'Billing backend not yet provisioned',
  );

  await page.getByTestId(TESTID.billingHomeLink).click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});
