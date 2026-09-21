/**
 * comment-margin.spec — acceptance for the Google-Docs comment margin that
 * replaced the chat rail (2026-09-19 stage 1).
 *
 * The rail was a conversation pinned to the side of the screen; the margin is a
 * set of notes pinned to the CONTENT. Four things follow from that, and they are
 * exactly what this spec holds:
 *
 *   1. a thread card sits LEVEL with the row it anchors to;
 *   2. a card with no row to sit beside leads the margin in its UNPLACED group;
 *   3. pressing a card's anchor reference brings that row back toward the middle
 *      of the reading surface — the reference is a link, not a label;
 *   4. arming a row composes IN PLACE (the draft card opens level with the row it
 *      will be filed against), and what is staged names its own consequence in the
 *      one submit bar.
 *
 * ── RULING P16: why (3) is not "scroll the row off-screen, then click" ────────
 * The brief asked for: scroll `design-scroll` to the bottom, assert the row is out
 * of the viewport, click the card, assert the row is back. That is unsatisfiable
 * against a FAITHFUL margin. There is ONE scroller now, spanning the content
 * column and the margin together, and a card sits level with its row — so
 * scrolling the row out of view scrolls its card out of view with it, and there is
 * nothing left to click. The only way to make that assertion pass is to clamp
 * cards to the margin viewport, which would break the level-with-its-row
 * alignment test (1) asserts. So (3) is restaged: put the row OFF-CENTRE with both
 * row and card still on screen, press the anchor reference, and assert the row's
 * centre moved TOWARD the scroll container's centre. That is the behaviour the
 * affordance actually promises.
 *
 * SAFETY: runs under the shared dispatch guard (support/dispatchGuard) over a
 * project stubbed in the browser (stubCommittedMissionWithThread). It creates
 * nothing and needs no drafting stack — it is in the ungated, stubbed tier, not
 * behind UITESTS_LIVE_DRAFTING.
 */
import type { Locator, Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, gotoApp } from './support/gating.js';
import {
  stubCommittedMissionWithThread,
  MARGIN_CHANGE_REQUEST_ID,
  MARGIN_QUESTION_ID,
  MARGIN_ANCHOR_TEXT,
} from './support/designStubs.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

/**
 * The CommentableList row key MissionView mints for the third objective:
 * `obj-${number}-${index}` — number 3 at index 2, the same index the thread's
 * `$.objectives[2]` anchor names.
 */
const ANCHORED_ROW = 'obj-3-2';

/** How far off the anchored row's centre may sit from the scroller's, in px. */
function centreOffset(
  row: { y: number; height: number },
  scroller: { y: number; height: number },
): number {
  return row.y + row.height / 2 - (scroller.y + scroller.height / 2);
}

/** A locator's bounding box, failing loudly rather than returning null. */
async function box(locator: Locator): Promise<{ x: number; y: number; width: number; height: number }> {
  const b = await locator.boundingBox();
  expect(b, 'expected the element to be laid out and visible').not.toBeNull();
  return b ?? { x: 0, y: 0, width: 0, height: 0 };
}

async function openMissionMargin(page: Page): Promise<void> {
  const projectId = await stubCommittedMissionWithThread(page);
  await gotoApp(page, `/project/${projectId}/design/system/mission`);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
  await expect(page.getByTestId(TESTID.marginRoot)).toBeVisible();
}

test.describe('the comment margin (stubbed committed mission — hermetic)', () => {
  test.beforeEach(async ({ request, page }) => {
    await requireServer(request, BASE);
    await openMissionMargin(page);
  });

  test('a thread card sits level with the objective it anchors to, and an unanchored one does not', async ({
    page,
    dispatchGuard,
  }) => {
    const card = await box(page.getByTestId(TESTID.marginCard(MARGIN_CHANGE_REQUEST_ID)));
    const row = await box(page.getByTestId(TESTID.commentListItem(ANCHORED_ROW)));

    // Level with its anchor, within one card's stacking slack.
    expect(Math.abs(card.y - row.y)).toBeLessThan(24);

    // The answered question carries no anchor, so it has no row to sit beside: it
    // leads the margin under the UNPLACED heading instead of being mispositioned.
    const unplaced = page.getByTestId(TESTID.marginUnplaced);
    await expect(unplaced).toBeVisible();
    await expect(unplaced.getByTestId(TESTID.marginCard(MARGIN_QUESTION_ID))).toBeVisible();
    // Its PM answer rides inside the same card — the thread, not a separate bubble.
    await expect(unplaced.getByTestId(TESTID.marginCard(MARGIN_QUESTION_ID))).toContainText(
      'An aspiration for now',
    );

    expect(dispatchGuard.blocked).toEqual([]);
  });

  test("pressing a card's anchor reference brings its objective back toward the middle", async ({
    page,
    dispatchGuard,
  }) => {
    // A short viewport so the mission page genuinely overflows its scroller —
    // without scroll room there is no "toward the centre" to observe.
    await page.setViewportSize({ width: 1400, height: 560 });

    const scroll = page.getByTestId(TESTID.designScroll);
    const row = page.getByTestId(TESTID.commentListItem(ANCHORED_ROW));

    // Start from the row centred, then nudge the page so it sits clearly OFF-centre
    // while staying on screen. The nudge picks whichever direction has scroll room,
    // so this does not depend on how much prose the fixture happens to carry.
    await row.evaluate((el) => {
      el.scrollIntoView({ block: 'center' });
    });
    const nudge = await scroll.evaluate((el) => {
      const want = Math.round(el.clientHeight * 0.3);
      const down = Math.min(want, el.scrollHeight - el.clientHeight - el.scrollTop);
      const up = Math.min(want, el.scrollTop);
      const delta = down >= up ? down : -up;
      el.scrollTop += delta;
      return delta;
    });
    expect(
      Math.abs(nudge),
      'the stubbed mission must leave the scroller enough room to push the row off centre',
    ).toBeGreaterThan(60);

    // Precondition: off centre, but BOTH the row and its card are still on screen —
    // which is precisely why "scroll it away, then click its card" cannot be the
    // test (RULING P16): the card rides with the row.
    await expect(row).toBeInViewport();
    const anchorJump = page
      .getByTestId(TESTID.marginCard(MARGIN_CHANGE_REQUEST_ID))
      .getByRole('button', { name: MARGIN_ANCHOR_TEXT });
    await expect(anchorJump).toBeVisible();

    const before = Math.abs(centreOffset(await box(row), await box(scroll)));
    expect(before, 'the row should start well off the scroller centre').toBeGreaterThan(60);

    await anchorJump.click();

    // scrollIntoView({ block: 'center' }) is SMOOTH, so poll until it settles.
    await expect
      .poll(async () => Math.abs(centreOffset(await box(row), await box(scroll))), {
        timeout: 5_000,
      })
      .toBeLessThan(before / 2);

    expect(dispatchGuard.blocked).toEqual([]);
  });

  test('the submit bar names the consequence of what is staged', async ({
    page,
    dispatchGuard,
  }) => {
    // Nothing staged on a clean committed slot: no verb, so no bar at all.
    await expect(page.getByTestId(TESTID.submitBar)).toHaveCount(0);

    // Activate the card (its reply box only opens on the margin's one active card),
    // stage a reply, and read what the bar says pressing its verb will do.
    const card = page.getByTestId(TESTID.marginCard(MARGIN_CHANGE_REQUEST_ID));
    await card.click();
    await card
      .getByTestId(TESTID.marginReply(MARGIN_CHANGE_REQUEST_ID))
      .getByRole('textbox')
      .fill('Still too vague');
    await card.getByRole('button', { name: 'post comment' }).click();

    // The reply is STAGED, not sent — it shows in the thread it answers…
    await expect(card).toContainText('STAGED · NOT SENT');
    await expect(card).toContainText('Still too vague');
    // …and the slot is COMMITTED, so the one verb is Amend, not Send back.
    const bar = page.getByTestId(TESTID.submitBar);
    await expect(bar).toContainText('Amend (1)');
    await expect(bar.getByTestId(TESTID.submitBarConsequence)).toContainText(
      '1 change request → amend',
    );

    // Nothing was dispatched: staging is local until the verb is pressed.
    expect(dispatchGuard.blocked).toEqual([]);
  });

  test('arming a row opens its draft card in place, level with that row', async ({
    page,
    dispatchGuard,
  }) => {
    // Composing moved INTO the margin beside the row (Task 8b): there is no
    // composer at the foot to type into, so arming a row must open the card here.
    const targetRow = 'obj-2-1';
    await page.getByTestId(TESTID.commentListItemButton(targetRow)).click();

    const composer = page.getByTestId(TESTID.marginComposer);
    await expect(composer).toBeVisible();
    const composerBox = await box(composer);
    const rowBox = await box(page.getByTestId(TESTID.commentListItem(targetRow)));
    expect(Math.abs(composerBox.y - rowBox.y)).toBeLessThan(24);

    // It opens ready to type, on the row that armed it.
    await expect(composer).toContainText('Objective 2');
    await composer.getByTestId(TESTID.marginComposerInput).getByRole('textbox').fill('Name the metric.');
    await composer.getByTestId(TESTID.marginComposerSubmit).click();

    await expect(page.getByTestId(TESTID.marginRoot)).toContainText('Name the metric.');
    await expect(page.getByTestId(TESTID.submitBar)).toContainText('Amend (1)');

    expect(dispatchGuard.blocked).toEqual([]);
  });

  test('the design header shows no slot path and no committed strip', async ({ page }) => {
    // Task 9 recovered the header space: the project.json address moved into the (?)
    // popover, and the full-width 'COMMITTED · revision N' strip became a chip.
    // RULING P7: assert the strip's ABSENCE by its rendered TEXT — its constant is
    // gone, so there is nothing left to select by.
    await expect(page.getByText('project.json → slots.mission')).toHaveCount(0);
    await expect(page.getByText(/COMMITTED · revision/)).toHaveCount(0);
    await expect(page.getByText(/committed · r\d+/)).toBeVisible();
  });
});
