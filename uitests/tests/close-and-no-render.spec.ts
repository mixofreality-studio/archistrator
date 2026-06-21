/**
 * close-and-no-render.spec — two black-box invariants of the rendering pivot.
 *
 *   1. The design experience `✕` (design-close) returns to the home base.
 *   2. NO network request the SPA makes has a path matching `/render` — the
 *      rendering pivot moved all artifact rendering client-side (typed models →
 *      ArtifactRenderer); the server exposes no `/render` endpoint anymore.
 *
 * Pure-UI: needs only the SPA + a Postgres-backed dev server. The /render
 * negative assertion is the UI sibling of a systemtests wire-shape check — it
 * inspects the real network log, not source.
 */
import { test, expect, type Request } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer } from './support/gating.js';
import { createProjectFromLanding, enterDesignExperience } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('the design-experience ✕ returns to the home base', async ({ page }) => {
  await createProjectFromLanding(page);
  await enterDesignExperience(page);

  await page.getByTestId(TESTID.designClose).click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  await expect(page.getByTestId(TESTID.designExperience)).toHaveCount(0);
  await expect(page).toHaveURL(/\/project\/[^/]+\/home$/);
});

test('the SPA issues no /render request across the create → design → close flow', async ({
  page,
}) => {
  const renderRequests: string[] = [];
  page.on('request', (req: Request) => {
    const path = new URL(req.url()).pathname;
    if (/\/render/i.test(path)) {
      renderRequests.push(`${req.method()} ${path}`);
    }
  });

  // Exercise the full pure-UI flow: catalog → create → home → design → close.
  await createProjectFromLanding(page);
  await enterDesignExperience(page);
  // Let the design experience settle (session probe + slots) before closing.
  await expect(page.getByTestId(TESTID.slimSpine)).toBeVisible();
  await page.getByTestId(TESTID.designClose).click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();

  expect(
    renderRequests,
    `expected no /render requests after the rendering pivot, saw: ${renderRequests.join(', ')}`,
  ).toEqual([]);
});
