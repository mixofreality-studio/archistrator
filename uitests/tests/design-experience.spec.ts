/**
 * design-experience.spec — the full-screen System Design co-author experience
 * (route `/project/$projectId/design/system`).
 *
 * AC flow:
 *   • the SlimSpine renders a step per Phase-1 artifact;
 *   • "Request draft" → the generating scene → a rendered artifact;
 *   • the submit bar's Approve advances the spine;
 *   • the bar's verb flips to Send back once feedback is staged (a free-form
 *     margin note or an anchored comment);
 *   • Send back with feedback regenerates the artifact (give-feedback → redraft loop).
 *
 * The structural pieces (spine, steps, close) are PURE-UI and run whenever the
 * server is reachable. The live drafting half (generating → render → gate) needs
 * the full Postgres+Temporal+worker stack and is GATED behind
 * UITESTS_LIVE_DRAFTING=1 — the UI analogue of systemtests' requireStack.
 *
 * SAFETY (fix-F review): the structure cases run under the shared dispatch guard
 * (support/dispatchGuard), which aborts every non-GET, over a FRESH project stubbed
 * in the browser (stubCreatedProject). They create nothing. The live-drafting
 * block drives real drafting writes against a real project, so the guard lets
 * through the co-author loop's writes there, and ONLY those (LIVE_DRAFTING_WRITES,
 * fix-G review ruling): it cannot create a project or dispatch construction. The
 * real project comes from the seed step (tests/seed/shared-project.setup.ts). It
 * is opt-in (UITESTS_LIVE_DRAFTING=1) and self-skips otherwise, CI included
 * (uitests.yml runs with UITESTS_LIVE_DRAFTING=off).
 */
import type { Page } from '@playwright/test';
import { test, expect, LIVE_DRAFTING_WRITES } from './support/dispatchGuard.js';
import { TESTID, PHASE1_ARTIFACTS } from './support/testids.js';
import { requireServer, skipUnlessLiveDrafting, gotoApp } from './support/gating.js';
import { openSharedProject, enterDesignExperience } from './support/flows.js';
import { stubCreatedProject } from './support/designStubs.js';
import { tagUseCase } from './support/useCases.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/** A fresh project, stubbed in the browser: no slots, no design session yet. */
const FRESH_ID = 'uitest-design-fresh';

// The first step (mission) may require research input before drafting can start.
const RESEARCH_NOTE =
  'A small community-events coordination app. Organizers post events; members RSVP, ' +
  'comment, and get reminders. Volatilities: notification channels, identity provider, ' +
  'venue data source. The architect should draft a mission from this.';

/**
 * requestFirstDraft clicks "Request draft" and answers the research-input
 * precondition if the server reports one (first step / 409). Leaves the page in
 * the generating-or-rendered state.
 */
async function requestFirstDraft(page: Page): Promise<void> {
  await page.getByTestId(TESTID.requestDraft).click();

  // The first step may surface the research-input panel (409 failed_precondition).
  const research = page.getByTestId(TESTID.researchInput);
  await Promise.race([
    research.waitFor({ state: 'visible' }).catch(() => undefined),
    page.getByTestId(TESTID.generatingScene).waitFor({ state: 'visible' }).catch(() => undefined),
  ]);
  if ((await research.count()) > 0 && (await research.isVisible())) {
    // The panel requires BOTH a source title and content before submit enables.
    await page.getByTestId(TESTID.researchInputTitle).fill('Founder brief');
    await page.getByTestId(TESTID.researchInputText).fill(RESEARCH_NOTE);
    await page.getByTestId(TESTID.researchInputSubmit).click();
  }
}

/**
 * Open a FREE-FORM (unanchored) draft card in the comment margin — the margin's
 * own ＋ affordance, and the only composer it owns itself now that arming a row
 * composes in place. Re-opens the margin first if the architect collapsed it
 * (the chrome's toggle renders only while it is closed).
 */
async function openMarginComposer(page: Page): Promise<void> {
  const toggle = page.getByTestId(TESTID.marginToggle);
  if ((await toggle.count()) > 0) {
    await toggle.click();
  }
  await expect(page.getByTestId(TESTID.marginRoot)).toBeVisible();
  await page.getByTestId(TESTID.marginAddNote).click();
  await expect(page.getByTestId(TESTID.marginComposer)).toBeVisible();
}

/** Open the stubbed fresh project's home base, then enter System Design from it. */
async function openFreshDesign(page: Page): Promise<void> {
  await stubCreatedProject(page, FRESH_ID, 'Design Fresh Fixture');
  await gotoApp(page, `/project/${FRESH_ID}/home`);
  await expect(page.getByTestId(TESTID.homeBaseScreen)).toBeVisible();
  await enterDesignExperience(page);
}

test.describe('structure (pure UI — server reachable)', () => {
  test.beforeEach(async ({ request }) => {
    await requireServer(request, BASE);
  });

  test('the spine renders a step per Phase-1 artifact', async ({ page, dispatchGuard }) => {
    await openFreshDesign(page);

    await expect(page.getByTestId(TESTID.slimSpine)).toBeVisible();
    for (const kind of PHASE1_ARTIFACTS) {
      await expect(page.getByTestId(TESTID.spineStep(kind))).toBeVisible();
    }
    expect(dispatchGuard.blocked).toEqual([]);
  });

  test('the first step offers a "Request draft" affordance', async ({ page, dispatchGuard }) => {
    await openFreshDesign(page);
    // The active first step is `mission`; with no session yet it shows the CTA.
    await expect(page.getByTestId(TESTID.spineStep(PHASE1_ARTIFACTS[0]))).toBeVisible();
    await expect(page.getByTestId(TESTID.requestDraft)).toBeVisible();
    expect(dispatchGuard.blocked).toEqual([]);
  });
});

test.describe('co-author drafting (live backend — UITESTS_LIVE_DRAFTING=1)', () => {
  // Real drafting on a live worker can take a few minutes; the per-test timeout
  // must exceed the 180s artifact-render waits below (the default 60s would kill
  // the test mid-draft before the model ever reaches the gate).
  test.describe.configure({ timeout: 300_000 });
  test.use({ dispatchGuardAllows: LIVE_DRAFTING_WRITES });

  test.beforeEach(async ({ request }) => {
    skipUnlessLiveDrafting();
    await requireServer(request, BASE);
    // This whole block drives the real dispatch → observe → gate → approve/
    // redraft loop — the Method core use case "Drive System Design" (see
    // .coreUseCases in project.json).
    tagUseCase('drive-system-design');
  });

  test('Request draft shows the generating scene then a rendered artifact', async ({ page }) => {
    await openSharedProject(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);

    // The generating loader appears while drafting/redrafting…
    await expect(page.getByTestId(TESTID.generatingScene)).toBeVisible({ timeout: 30_000 });
    // …and carries the async-CI affordance (the draft runs as a GitHub Action in
    // the user's CI — minutes — so the loader explains the wait, not a hung spinner).
    await expect(page.getByTestId(TESTID.ciJobNotice)).toBeVisible();
    // …then the typed artifact renders (drafting can take a while on a real model).
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });
    await expect(page.getByTestId(TESTID.generatingScene)).toHaveCount(0);
  });

  test('the gate panel appears and Approve advances the spine', async ({ page }) => {
    await openSharedProject(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });

    // The human gate is offered on the awaitingReview stage.
    const gate = page.getByTestId(TESTID.gatePanel);
    await expect(gate).toBeVisible({ timeout: 180_000 });

    // The commit-authority verbs moved OFF this panel onto the one submit bar
    // (GatePanel omits `actions` in Phase 1 now). With nothing staged, the single
    // verb it offers is Approve — which is also how "no empty send back" is
    // guaranteed: Send back is not a verb until a change request is staged.
    const submit = page.getByTestId(TESTID.submitBarPrimary);
    await expect(submit).toHaveText(/Approve/);
    await expect(submit).toBeEnabled();

    await submit.click();

    // Approve seals the artifact and auto-advances: the first step becomes
    // committed and the active step moves on. We assert the spine survived the
    // transition and the gate cleared.
    await expect(page.getByTestId(TESTID.slimSpine)).toBeVisible();
    await expect(gate).toHaveCount(0, { timeout: 30_000 });
  });

  test('Send-back enables after entering free-form feedback', async ({ page }) => {
    await openSharedProject(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 180_000 });

    // No feedback yet → the bar's one verb is Approve, not Send back.
    await expect(page.getByTestId(TESTID.submitBarPrimary)).toHaveText(/Approve/);

    // Free-form feedback needs NO anchor. In the comment margin that is the ＋
    // affordance: it opens an UNANCHORED draft card, proving an anchored comment
    // is an optional way to pin a note to a spot, not a precondition for sending
    // back. (The old rail typed into a composer at its foot; composing is in
    // place now, so the card is the composer.)
    await openMarginComposer(page);
    const input = page.getByTestId(TESTID.marginComposerInput).getByRole('textbox');
    await expect(input).toBeVisible();
    await expect(input).toBeEnabled();

    await input.fill('Please tighten the definitions — several read as circular.');
    await page.getByTestId(TESTID.marginComposerSubmit).click();

    // The note accumulates in the margin and the bar's verb flips to Send back,
    // naming what is staged and what pressing it will do.
    await expect(page.getByTestId(TESTID.marginRoot)).toContainText('tighten the definitions');
    const armed = page.getByTestId(TESTID.submitBarPrimary);
    await expect(armed).toHaveText(/Send back \(1\)/);
    await expect(armed).toBeEnabled();
    await expect(page.getByTestId(TESTID.submitBarConsequence)).toContainText(
      '1 change request → redraft',
    );
  });

  test('Send back with feedback regenerates the artifact', async ({ page }) => {
    await openSharedProject(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 180_000 });

    // Enter feedback and send it back.
    await openMarginComposer(page);
    const input = page.getByTestId(TESTID.marginComposerInput).getByRole('textbox');
    await expect(input).toBeEnabled();
    await input.fill('Redraft: make each definition stand alone; drop the circular references.');
    await page.getByTestId(TESTID.marginComposerSubmit).click();

    const sendBack = page.getByTestId(TESTID.submitBarPrimary);
    await expect(sendBack).toHaveText(/Send back \(1\)/);
    await sendBack.click();

    // The reject loops the workflow back to drafting: the generating scene returns,
    // then a fresh artifact + gate — the give-feedback → regenerate loop, end to end.
    await expect(page.getByTestId(TESTID.generatingScene)).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 180_000 });
  });
});
