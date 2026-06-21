/**
 * design-experience.spec — the full-screen System Design co-author experience
 * (route `/project/$projectId/design/system`).
 *
 * AC flow:
 *   • the SlimSpine renders a step per Phase-1 artifact;
 *   • "Request draft" → the generating scene → a rendered artifact;
 *   • the gate panel Approve advances the spine;
 *   • Send-back enables after entering feedback (free-form note or anchored comment);
 *   • Send back with feedback regenerates the artifact (give-feedback → redraft loop).
 *
 * The structural pieces (spine, steps, close) are PURE-UI and run whenever the
 * server is reachable. The live drafting half (generating → render → gate) needs
 * the full Postgres+Temporal+worker stack and is GATED behind
 * UITESTS_LIVE_DRAFTING=1 — the UI analogue of systemtests' requireStack.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID, PHASE1_ARTIFACTS } from './support/testids.js';
import { skipUnlessServer, skipUnlessLiveDrafting } from './support/gating.js';
import { createProjectFromLanding, enterDesignExperience } from './support/flows.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

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

test.describe('structure (pure UI — server reachable)', () => {
  test.beforeEach(async ({ request }) => {
    await skipUnlessServer(request, BASE);
  });

  test('the spine renders a step per Phase-1 artifact', async ({ page }) => {
    await createProjectFromLanding(page);
    await enterDesignExperience(page);

    await expect(page.getByTestId(TESTID.slimSpine)).toBeVisible();
    for (const kind of PHASE1_ARTIFACTS) {
      await expect(page.getByTestId(TESTID.spineStep(kind))).toBeVisible();
    }
  });

  test('the first step offers a "Request draft" affordance', async ({ page }) => {
    await createProjectFromLanding(page);
    await enterDesignExperience(page);
    // The active first step is `mission`; with no session yet it shows the CTA.
    await expect(page.getByTestId(TESTID.spineStep(PHASE1_ARTIFACTS[0]))).toBeVisible();
    await expect(page.getByTestId(TESTID.requestDraft)).toBeVisible();
  });
});

test.describe('co-author drafting (live backend — UITESTS_LIVE_DRAFTING=1)', () => {
  // Real drafting on a live worker can take a few minutes; the per-test timeout
  // must exceed the 180s artifact-render waits below (the default 60s would kill
  // the test mid-draft before the model ever reaches the gate).
  test.describe.configure({ timeout: 300_000 });

  test.beforeEach(async ({ request }) => {
    skipUnlessLiveDrafting();
    await skipUnlessServer(request, BASE);
  });

  test('Request draft shows the generating scene then a rendered artifact', async ({ page }) => {
    await createProjectFromLanding(page);
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
    await createProjectFromLanding(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });

    // The human gate is offered on the awaitingReview stage.
    const gate = page.getByTestId(TESTID.gatePanel);
    await expect(gate).toBeVisible({ timeout: 180_000 });

    // Send-back is disabled until feedback is entered; Approve is always live.
    await expect(page.getByTestId(TESTID.gateSendback)).toBeDisabled();
    await expect(page.getByTestId(TESTID.gateApprove)).toBeEnabled();

    await page.getByTestId(TESTID.gateApprove).click();

    // Approve seals the artifact and auto-advances: the first step becomes
    // committed and the active step moves on. We assert the spine survived the
    // transition and the gate cleared.
    await expect(page.getByTestId(TESTID.slimSpine)).toBeVisible();
    await expect(gate).toHaveCount(0, { timeout: 30_000 });
  });

  test('Send-back enables after entering free-form feedback', async ({ page }) => {
    await createProjectFromLanding(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 180_000 });

    // No feedback yet → Send-back disabled.
    await expect(page.getByTestId(TESTID.gateSendback)).toBeDisabled();

    // Free-form feedback needs NO anchor: open the rail, type a note, post it.
    // The composer is enabled without arming a selection (an anchored comment is
    // an optional way to pin a note to a spot, not a precondition for sending back).
    const toggle = page.getByTestId(TESTID.chatToggle);
    if ((await toggle.count()) > 0) {
      await toggle.click();
    }
    const input = page.getByTestId(TESTID.chatInput);
    await expect(input).toBeVisible();
    await expect(input).toBeEnabled();

    await input.fill('Please tighten the definitions — several read as circular.');
    await page.getByTestId(TESTID.chatSend).click();

    // The note accumulates and now arms Send-back.
    await expect(page.getByTestId(TESTID.chatRail)).toContainText('tighten the definitions');
    await expect(page.getByTestId(TESTID.gateSendback)).toBeEnabled();
  });

  test('Send back with feedback regenerates the artifact', async ({ page }) => {
    await createProjectFromLanding(page);
    await enterDesignExperience(page);

    await requestFirstDraft(page);
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 180_000 });

    // Enter feedback and send it back.
    const toggle = page.getByTestId(TESTID.chatToggle);
    if ((await toggle.count()) > 0) {
      await toggle.click();
    }
    const input = page.getByTestId(TESTID.chatInput);
    await expect(input).toBeEnabled();
    await input.fill('Redraft: make each definition stand alone; drop the circular references.');
    await page.getByTestId(TESTID.chatSend).click();

    await expect(page.getByTestId(TESTID.gateSendback)).toBeEnabled();
    await page.getByTestId(TESTID.gateSendback).click();

    // The reject loops the workflow back to drafting: the generating scene returns,
    // then a fresh artifact + gate — the give-feedback → regenerate loop, end to end.
    await expect(page.getByTestId(TESTID.generatingScene)).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible({ timeout: 180_000 });
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 180_000 });
  });
});
