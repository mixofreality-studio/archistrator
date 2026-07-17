/**
 * design-experience-regressions.spec — black-box coverage for two founder-reported
 * regressions in the System Design experience:
 *
 *   1. Core Use Cases WALKTHROUGH — clicking "Next" must move the you-are-here map's
 *      camera to the new current step (the founder-loved per-step zoom), not leave the
 *      viewport static. We assert the `.react-flow__viewport` transform CHANGES after
 *      Next and that a single ringed current-step node is present and advances.
 *
 *   2. Architecture "Design job failed" panel — the terminal DraftFailedPanel must be
 *      horizontally CENTERED in the content area at wide viewports (the founder saw it
 *      shoved to the right with a huge empty gutter on the left).
 *
 * Both are route-intercepted (see support/designStubs.ts): the specs stub the wire and
 * drive the REAL SPA, so they run hermetically WITHOUT a live drafting stack — the same
 * "stub the wire" tactic the design experience already relies on for gate-error paths.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import {
  stubCommittedCoreUseCases,
  stubDraftFailedArchitecture,
  stubRetryableDraftFailedGlossary,
} from './support/designStubs.js';

/** The inline transform React-Flow writes on its pan/zoom viewport element. */
async function viewportTransform(page: Page): Promise<string> {
  // eslint-disable-next-line no-restricted-syntax -- the pan/zoom transform lives on React-Flow's generated .react-flow__viewport element; there is no testid/role on it, and its transform IS the assertion (proof the camera moved).
  const viewport = page.locator('.react-flow__viewport').first();
  return viewport.evaluate((el) => (el as HTMLElement).style.transform);
}

/**
 * settledTransform waits for the viewport transform to stop changing (an fitView /
 * setCenter glide animates over a few hundred ms) and returns the settled value.
 */
async function settledTransform(page: Page): Promise<string> {
  let last = await viewportTransform(page);
  await expect
    .poll(
      async () => {
        const now = await viewportTransform(page);
        const stable = now === last;
        last = now;
        return stable;
      },
      { timeout: 5_000, intervals: [120, 120, 120] },
    )
    .toBe(true);
  return last;
}

test.describe('Core Use Cases walkthrough — per-step camera focus (regression 1)', () => {
  test('clicking Next moves the you-are-here camera to the new current step', async ({ page }) => {
    const projectId = await stubCommittedCoreUseCases(page);
    await page.goto(`/project/${projectId}/design/system`);
    await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();

    // The committed coreUseCases slot is not the first-open step — select it.
    await page.getByTestId(TESTID.spineStep('coreUseCases')).click();
    await expect(page.getByTestId(TESTID.artifactRender)).toBeVisible();
    await expect(page.getByTestId(TESTID.useCasePicker)).toBeVisible();

    // Ensure walkthrough mode (it is the default, but assert the toggle explicitly).
    await page.getByTestId(TESTID.useCaseViewWalkthrough).click();

    // The you-are-here map renders exactly one ringed current-step node.
    const currentNode = page.getByTestId(TESTID.walkthroughCurrentNode);
    await expect(currentNode).toHaveCount(1);
    await expect(currentNode).toBeVisible();

    // Let the initial whole-graph fit settle, then snapshot the camera + current step.
    const beforeTransform = await settledTransform(page);
    const beforeCurrentText = await currentNode.innerText();

    // Advance one step.
    await page.getByTestId(TESTID.walkthroughNext).click();

    // The camera must actually move — the viewport transform changes (regression: it
    // stayed put because AutoFit only ever re-fit the whole graph).
    await expect
      .poll(async () => viewportTransform(page), { timeout: 5_000 })
      .not.toBe(beforeTransform);

    // Still exactly one ringed current-step node, and it has advanced to a new step.
    await expect(currentNode).toHaveCount(1);
    await expect
      .poll(async () => currentNode.innerText(), { timeout: 5_000 })
      .not.toBe(beforeCurrentText);
  });
});

test.describe('Architecture draft-failed panel — centered at wide viewports (regression 2)', () => {
  // The founder saw the failed-state icon/title/text/buttons off to one side with a
  // huge empty gutter — it looked broken at wide viewport. Two invariants guard the
  // fix at 1300 / 1600 / 2000: (a) the failure content is centered WITHIN the panel;
  // (b) the panel is centered within the CONTENT AREA (the region left of the chat
  // rail). A shift to either side by hundreds of px — the regression — breaks these.
  const centerX = (b: { x: number; width: number }): number => b.x + b.width / 2;

  for (const width of [1300, 1600, 2000]) {
    test(`the DraftFailedPanel is centered in the content area at ${String(width)}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const projectId = await stubDraftFailedArchitecture(page);
      await page.goto(`/project/${projectId}/design/system`);
      await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();

      const panel = page.getByTestId(TESTID.draftFailed);
      await expect(panel).toBeVisible();
      const reason = page.getByTestId(TESTID.draftFailureReason);
      await expect(reason).toBeVisible();

      const panelBox = await panel.boundingBox();
      const reasonBox = await reason.boundingBox();
      // eslint-disable-next-line no-restricted-syntax -- the content area is the <main> landmark ExperienceChrome renders; it carries no testid and <main> is the correct structural handle for "the content region" (chat rail excluded below).
      const mainBox = await page.locator('main').first().boundingBox();
      const chatBox = await page.getByTestId(TESTID.chatRail).first().boundingBox();
      expect(panelBox).not.toBeNull();
      expect(reasonBox).not.toBeNull();
      expect(mainBox).not.toBeNull();
      if (panelBox === null || reasonBox === null || mainBox === null) return;

      // (a) failure reason centered within the panel card.
      expect(Math.abs(centerX(reasonBox) - centerX(panelBox))).toBeLessThan(24);

      // (b) panel centered within the content area (main, minus the chat rail on the
      // right when it is open). A regression that pushes the panel to one side moves
      // its center far off the content-area center.
      const contentLeft = mainBox.x;
      const contentRight = chatBox !== null ? chatBox.x : mainBox.x + mainBox.width;
      const contentCenter = (contentLeft + contentRight) / 2;
      const tolerance = (contentRight - contentLeft) * 0.06;
      expect(Math.abs(centerX(panelBox) - contentCenter)).toBeLessThan(tolerance);
    });
  }
});

test.describe('Retry from the failed card reaches the review gate (F-QA2-50)', () => {
  test('failed → Retry → the gate renders WITHOUT a reload', async ({ page }) => {
    // The live defect: after Retry, the server resumed the SAME session
    // (failed → awaitingReview within seconds, no new CI job) but the SPA froze
    // on a stale generating view for 4+ minutes — the failed stage stopped the
    // poll permanently and the retry mutation's single invalidation refetch was
    // the only (racy) escape. The failure gates now keep an 8s safety-net poll,
    // so the polled server stage must win within one interval, reload never.
    const projectId = await stubRetryableDraftFailedGlossary(page, [
      { term: 'Engagement', definition: 'A billable client project.', category: 'How' },
    ]);
    await page.goto(`/project/${projectId}/design/system`);

    const retry = page.getByTestId(TESTID.retryDraft);
    await expect(retry).toBeVisible();
    await retry.click();

    // No reload: the SAME document must flip the failed card to the review gate.
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId(TESTID.draftFailed)).toHaveCount(0);
  });
});
