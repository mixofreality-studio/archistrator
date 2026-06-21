/**
 * homebase.spec — the project's living-document dashboard
 * (route `/project/$projectId/home`).
 *
 * AC flow: the home base shows the phase cards, the artifact TOC, and a "Resume
 * design" CTA that enters the full-screen design experience. Pure-UI: a freshly
 * created project renders all of this from head-state with no draft, so this
 * needs only the SPA + a Postgres-backed dev server — no Temporal/worker. It
 * self-skips when that server is unreachable.
 */
import { test, expect } from '@playwright/test';
import { TESTID, ACTIVE_PHASE_ID, PHASE1_ARTIFACTS } from './support/testids.js';
import { skipUnlessServer } from './support/gating.js';
import { createProjectFromLanding, enterDesignExperience } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('home base shows the system-design phase card and the artifact TOC', async ({ page }) => {
  await createProjectFromLanding(page);
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();

  // The active System Design phase card (typed PhaseId `systemDesign`).
  await expect(page.getByTestId(TESTID.phaseCard(ACTIVE_PHASE_ID))).toBeVisible();

  // The artifact table of contents, with a row per Phase-1 artifact slot.
  const toc = page.getByTestId(TESTID.artifactToc);
  await expect(toc).toBeVisible();
  await expect(toc.getByTestId(TESTID.tocRow(PHASE1_ARTIFACTS[0]))).toBeVisible();
  await expect(toc.getByTestId(TESTID.tocRow('system'))).toBeVisible();
});

test('"Resume design" enters the design experience', async ({ page }) => {
  await createProjectFromLanding(page);
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();

  await expect(page.getByTestId(TESTID.resumeDesign)).toBeVisible();
  await enterDesignExperience(page);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
});
