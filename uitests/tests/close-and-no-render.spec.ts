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
 *
 * SAFETY (fix-G review ruling): runs under the shared dispatch guard. The first
 * case opens a project faked in the browser (openStubbedProject); the second runs
 * the create flow in the SPA with create-project answered in the browser
 * (stubCreatedProject). Nothing is created.
 */
import type { Request } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer } from './support/gating.js';
import {
  createProjectFromLanding,
  enterDesignExperience,
  openStubbedProject,
} from './support/flows.js';
import { stubCreatedProject } from './support/designStubs.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The project faked in the browser for this spec. */
const PROJECT_ID = 'uitest-close-stub';
const PROJECT_NAME = 'Close Stub Project';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
});

test('the design-experience ✕ returns to the home base', async ({ page, dispatchGuard }) => {
  await openStubbedProject(page, PROJECT_ID, PROJECT_NAME);
  await enterDesignExperience(page);

  await page.getByTestId(TESTID.designClose).click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  await expect(page.getByTestId(TESTID.designExperience)).toHaveCount(0);
  await expect(page).toHaveURL(/\/project\/[^/]+\/home$/);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the SPA issues no /render request across the create → design → close flow', async ({
  page,
  dispatchGuard,
}) => {
  const renderRequests: string[] = [];
  page.on('request', (req: Request) => {
    const path = new URL(req.url()).pathname;
    // Match a `render` PATH SEGMENT (an API render endpoint, e.g. `/api/.../render`)
    // — not any substring. Vite dev-server module fetches for
    // `src/components/construction/renderers/*.tsx` also contain "render" as a
    // substring of "renderers", which is not the invariant under test.
    const segments = path.split('/').filter((s) => s.length > 0);
    if (segments.includes('render')) {
      renderRequests.push(`${req.method()} ${path}`);
    }
  });

  // Exercise the full pure-UI flow: catalog → create → home → design → close. The
  // create is answered in the browser with PROJECT_ID.
  const created = await stubCreatedProject(page, PROJECT_ID, PROJECT_NAME);
  await createProjectFromLanding(page);
  await expect(page).toHaveURL(new RegExp(`/project/${PROJECT_ID}/home$`));
  await enterDesignExperience(page);
  // Let the design experience settle (session probe + slots) before closing.
  await expect(page.getByTestId(TESTID.slimSpine)).toBeVisible();
  await page.getByTestId(TESTID.designClose).click();
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();

  expect(
    renderRequests,
    `expected no /render requests after the rendering pivot, saw: ${renderRequests.join(', ')}`,
  ).toEqual([]);
  // The one create was answered in the browser, and no other write left it.
  expect(created.creates).toBe(1);
  expect(dispatchGuard.blocked).toEqual([]);
});
