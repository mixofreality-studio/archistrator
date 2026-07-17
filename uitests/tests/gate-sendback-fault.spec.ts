/**
 * gate-sendback-fault.spec — F-QA2-47 regression: a Send back whose
 * submit-review-decision POST dies with a 503 (Infrastructure kind — in the live
 * incident the Temporal signal WAS delivered and only the response was lost) must
 * NOT silently discard the staged review feedback.
 *
 * Verified invariants at the review gate after the stubbed 503:
 *   1. The staged "PENDING · NOT SENT" note is RETAINED (client state not cleared);
 *   2. Send back re-enables for a retry;
 *   3. A cause-neutral error banner renders near the gate actions — it does NOT
 *      claim the decision failed outright (the session may have received it) and
 *      does NOT parrot the raw transport detail.
 *
 * Route-intercepted (support/designStubs.stubAwaitingReviewGlossary): the spec
 * stubs the wire and drives the REAL SPA — GatePanel, ChatRail, CommentContext —
 * hermetically, no live drafting stack. Poll-cadence behavior (F-QA2-48) is
 * deliberately NOT asserted here: interval timing is flaky under Playwright and is
 * pinned by the sessionPolling unit tests instead.
 */
import { test, expect } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { stubAwaitingReviewGlossary } from './support/designStubs.js';

/** Wire ModelGlossaryItem literals for the draft under review. */
const ITEMS: { term: string; definition: string; category: string }[] = [
  { term: 'Architect', definition: 'The single design authority for the system.', category: 'Who' },
  {
    term: 'Draft Task',
    definition: 'One artifact-producing unit of work dispatched to a worker.',
    category: 'How',
  },
];

const NOTE = 'Tighten the Draft Task definition — name the dispatch venue.';

test.describe('Review-gate send back over a lost-response 503 (F-QA2-47)', () => {
  test('retains the staged note, re-enables Send back, and shows a cause-neutral banner', async ({
    page,
  }) => {
    const projectId = await stubAwaitingReviewGlossary(page, ITEMS);
    // The decision submit dies at the transport with the Infrastructure-kind 503
    // envelope the generated handler writes for a lost Temporal response.
    await page.route('**/api/v1/system-design/submit-review-decision/**', (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'design session unavailable: signal response lost',
          code: 'unavailable',
        }),
      }),
    );

    await page.goto(`/project/${projectId}/design/system`);
    await expect(page.getByTestId(TESTID.gatePanel)).toBeVisible();

    // Stage one free-form send-back note through the chat rail composer.
    const sendBack = page.getByTestId(TESTID.gateSendback);
    await expect(sendBack).toBeDisabled(); // no feedback staged yet
    await page.getByTestId(TESTID.chatInput).getByRole('textbox').fill(NOTE);
    await page.getByTestId(TESTID.chatSend).click();
    await expect(page.getByText('PENDING · NOT SENT · 1')).toBeVisible();
    await expect(page.getByText(NOTE)).toBeVisible();
    await expect(sendBack).toBeEnabled();

    await sendBack.click();

    // 3. Cause-neutral banner near the gate actions: no false "it failed" claim,
    // no raw transport detail.
    const banner = page.getByTestId(TESTID.gateError);
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('could not be confirmed');
    await expect(banner).toContainText('Try again if the gate remains');
    await expect(banner).not.toContainText('signal response lost');

    // 1. The staged note survives the fault — nothing was discarded.
    await expect(page.getByText('PENDING · NOT SENT · 1')).toBeVisible();
    await expect(page.getByText(NOTE)).toBeVisible();

    // 2. The decision buttons settle back to actionable for a retry.
    await expect(sendBack).toBeEnabled();
    await expect(page.getByTestId(TESTID.gateApprove)).toBeEnabled();
  });
});
