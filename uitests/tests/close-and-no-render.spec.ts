/**
 * close-and-no-render.spec — the rendering pivot's black-box invariant:
 * NO network request the SPA makes has a path matching `/render`. The pivot moved
 * all artifact rendering client-side (typed models → ArtifactRenderer); the server
 * exposes no `/render` endpoint anymore.
 *
 * Pure-UI: needs only the SPA + a Postgres-backed dev server. The /render negative
 * assertion is the UI sibling of a systemtests wire-shape check — it inspects the
 * real network log, not source.
 *
 * ── Stage 5 Task 13 ─────────────────────────────────────────────────────────
 * This file used to hold a SECOND case — "the design experience ✕ returns to the
 * home base" — and to walk catalog → create → home → DESIGN → close. Spec §7.4
 * deleted the design experience, so the ✕ case lost its subject outright: the ✕
 * that survives is the ACTIVITY experience's, and it returns to the PLAN, not to
 * the home base. That one is already pinned deterministically over fixtures by
 * `tests/preview/plan.spec.ts` ("a GRAPH tile opens the activity, and ✕ returns
 * to the GRAPH"), so nothing is owed here. A stubbed FRESH project has no
 * activity to open, so it could not have carried the assertion anyway.
 *
 * The flow below therefore walks catalog → create → home → PLAN, which exercises
 * the same set of reads the design rail used to (project head-state + its slots)
 * on the surface that replaced it.
 *
 * SAFETY (fix-G review ruling): runs under the shared dispatch guard. The create
 * flow runs in the SPA with create-project answered in the browser
 * (stubCreatedProject). Nothing is created.
 */
import type { Request } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer } from './support/gating.js';
import { createProjectFromLanding } from './support/flows.js';
import { stubCreatedProject } from './support/designStubs.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The project faked in the browser for this spec. */
const PROJECT_ID = 'uitest-close-stub';
const PROJECT_NAME = 'Close Stub Project';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
});

test('the SPA issues no /render request across the create → home → plan flow', async ({
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

  // Exercise the full pure-UI flow: catalog → create → home → plan. The create is
  // answered in the browser with PROJECT_ID.
  const created = await stubCreatedProject(page, PROJECT_ID, PROJECT_NAME);
  await createProjectFromLanding(page);
  await expect(page).toHaveURL(new RegExp(`/project/${PROJECT_ID}/home$`));
  const planCard = page.getByTestId(TESTID.homeBaseOpenPlan);
  await expect(planCard).toBeVisible();
  await planCard.getByRole('button', { name: /open plan/i }).click();
  // Let the plan settle (its one project read) before asserting on the log.
  await expect(page.getByTestId(TESTID.planScreen)).toBeVisible();

  expect(
    renderRequests,
    `expected no /render requests after the rendering pivot, saw: ${renderRequests.join(', ')}`,
  ).toEqual([]);
  // The one create was answered in the browser, and no other write left it.
  expect(created.creates).toBe(1);
  expect(dispatchGuard.blocked).toEqual([]);
});
