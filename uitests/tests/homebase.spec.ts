/**
 * homebase.spec — the project's living-document dashboard
 * (route `/project/$projectId/home`).
 *
 * AC flow: the home base shows ONE card for the project's plan, the artifact TOC,
 * and that card's "Open plan →" button lands on the plan. Pure-UI: a fresh project
 * renders all of this from head-state with no draft, so this needs only the SPA +
 * a Postgres-backed dev server — no Temporal/worker. It self-skips when that
 * server is unreachable.
 *
 * ── Stage 5 Task 13 ─────────────────────────────────────────────────────────
 * Both cases are RETARGETED, not rewritten. The three phase cards and the header
 * "Resume design" button described a project that moved through three separate
 * places; spec §7.4 replaced them with the ONE plan card (Task 11) and deleted
 * the design rail the button opened. So "the active phase card is visible"
 * becomes "the plan card is visible, and it names the project's phase", and
 * "Resume design enters the design experience" becomes "Open plan enters the
 * plan". The artifact TOC half is untouched — it reads committed head-state and
 * survives.
 *
 * SAFETY (fix-G review ruling): runs under the shared dispatch guard, over a fresh
 * project faked in the browser (openStubbedProject). It creates nothing.
 */
import { test, expect } from './support/dispatchGuard.js';
import { TESTID, PHASE1_ARTIFACTS } from './support/testids.js';
import { requireServer } from './support/gating.js';
import { openStubbedProject } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** The fresh project faked in the browser for this spec. */
const PROJECT_ID = 'uitest-homebase-stub';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
});

test('home base shows the plan card and the artifact TOC', async ({ page, dispatchGuard }) => {
  await openStubbedProject(page, PROJECT_ID, 'Home Base Stub Project');

  // The ONE card that replaced the three phase cards. A fresh project is in
  // System Design, and the card says so in its eyebrow.
  const planCard = page.getByTestId(TESTID.homeBaseOpenPlan);
  await expect(planCard).toBeVisible();
  await expect(planCard).toContainText('PHASE 1 · SYSTEM DESIGN');
  // No phase cards survive: the plan is the one door.
  await expect(page.getByTestId(/^phase-card-/)).toHaveCount(0);

  // The artifact table of contents, with a row per Phase-1 artifact slot.
  const toc = page.getByTestId(TESTID.artifactToc);
  await expect(toc).toBeVisible();
  await expect(toc.getByTestId(TESTID.tocRow(PHASE1_ARTIFACTS[0]))).toBeVisible();
  await expect(toc.getByTestId(TESTID.tocRow('system'))).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('"Open plan" enters the plan', async ({ page, dispatchGuard }) => {
  await openStubbedProject(page, PROJECT_ID, 'Home Base Stub Project');

  const planCard = page.getByTestId(TESTID.homeBaseOpenPlan);
  await expect(planCard).toBeVisible();
  await planCard.getByRole('button', { name: /open plan/i }).click();

  await expect(page.getByTestId(TESTID.planScreen)).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/project/${PROJECT_ID}/plan\\?lens=list$`));
  expect(dispatchGuard.blocked).toEqual([]);
});
