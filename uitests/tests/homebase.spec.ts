/**
 * homebase.spec — the project's living-document dashboard
 * (route `/project/$projectId/home`).
 *
 * AC flow: the home base shows the phase cards, the artifact TOC, and a "Resume
 * design" CTA that enters the full-screen design experience. Pure-UI: a fresh
 * project renders all of this from head-state with no draft, so this needs only
 * the SPA + a Postgres-backed dev server — no Temporal/worker. It self-skips when
 * that server is unreachable.
 *
 * SAFETY (fix-G review ruling): runs under the shared dispatch guard, over a fresh
 * project faked in the browser (openStubbedProject). It creates nothing.
 */
import { test, expect } from './support/dispatchGuard.js';
import { TESTID, ACTIVE_PHASE_ID, PHASE1_ARTIFACTS } from './support/testids.js';
import { requireServer } from './support/gating.js';
import { openStubbedProject, enterDesignExperience } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The fresh project faked in the browser for this spec. */
const PROJECT_ID = 'uitest-homebase-stub';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
});

test('home base shows the system-design phase card and the artifact TOC', async ({
  page,
  dispatchGuard,
}) => {
  await openStubbedProject(page, PROJECT_ID, 'Home Base Stub Project');

  // The active System Design phase card (typed PhaseId `systemDesign`).
  await expect(page.getByTestId(TESTID.phaseCard(ACTIVE_PHASE_ID))).toBeVisible();

  // The artifact table of contents, with a row per Phase-1 artifact slot.
  const toc = page.getByTestId(TESTID.artifactToc);
  await expect(toc).toBeVisible();
  await expect(toc.getByTestId(TESTID.tocRow(PHASE1_ARTIFACTS[0]))).toBeVisible();
  await expect(toc.getByTestId(TESTID.tocRow('system'))).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('"Resume design" enters the design experience', async ({ page, dispatchGuard }) => {
  await openStubbedProject(page, PROJECT_ID, 'Home Base Stub Project');

  await expect(page.getByTestId(TESTID.resumeDesign)).toBeVisible();
  await enterDesignExperience(page);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});
