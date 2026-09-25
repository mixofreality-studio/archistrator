/**
 * THE ACTIVITY EXPERIENCE (`/project/$projectId/activity/$activityId?task=&rev=`)
 * — one activity's whole lifecycle on one full-screen surface (spec §7.2),
 * driven in the PREVIEW build over
 * `uitests/preview-fixtures/web-client/activity-experience/`.
 *
 * One case per promise §7.2 makes, each over the fixture that exercises it, plus
 * the two §7.5 gaps whose components have no other proof: `ReadOnlyRow` (a
 * read-only surface still enrols its rows' anchors, so a past round's margin card
 * is PLACED) and `OpRow` (a service contract's operations do the same, so a
 * design-review comment on an op sits level with it). Neither branch is reachable
 * under `node --test`, which is why they are asserted here. The same describe
 * block now also pins `OptionRow` (an SDP option row enrols its own anchor), and
 * a case below pins the OTHER half of C1: a rail with no question op offers no
 * way to stage a question.
 *
 * TWO THINGS THE PREVIEW CANNOT SHOW, and what stands in for them:
 *
 *   - THE URL. A preview runs on a MEMORY history — the address bar keeps
 *     `?screen=&state=` and never sees the app's own route — so `?rev` cannot be
 *     read. Its RULE is asserted through the two things a reader actually sees:
 *     the revision select saying `· latest` or `· read-only`, and the read-only
 *     banner. "`rev` absent" IS "the latest, with no banner"; "`rev` written" IS
 *     "an older revision, under the banner".
 *   - AN ARBITRARY ROUTE. A fixture opens at ITS OWN route, so a case cannot
 *     invent a query string. The default-task rule is therefore asserted over a
 *     fixture whose route names NO task, and the "lands where the URL says" half
 *     over one whose route names a task that is NOT the default.
 *
 * `incidents(page)` is asserted EMPTY in every case: without it a screen that
 * rendered half a state over an op no fixture answered would read as a pass.
 */
import { test, expect } from '../support/dispatchGuard.js';
import { TESTID } from '../support/testids.js';
import { fixture, incidents, openState } from '../support/previewShell.js';

/** The one service activity every contract assertion below is about. */
const SERVICE = 'service-fork-sent-back';

test.describe('activity experience: the screen, and where it opens', () => {
  test('opens FULL SCREEN with the eyebrow reading the ACTIVITY, not a phase', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);

    // The full-screen chrome, with the activity's own lifecycle in the spine bar
    // where a design phase would put its slim spine — and no slim spine at all.
    await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleGraph)).toBeVisible();
    await expect(page.getByTestId(TESTID.slimSpine)).toHaveCount(0);

    // The eyebrow names the ACTIVITY and its type. A phase eyebrow would read
    // "PHASE n · …", which is what this screen replaced.
    await expect(page.getByTestId(TESTID.activityEyebrow)).toHaveText(
      'C-BILLING-STATE-ACCESS · SERVICE'
    );

    expect(await incidents(page)).toEqual([]);
    await expect(page.getByTestId(TESTID.previewAlarm)).toHaveCount(0);
    expect(offBundle).toEqual([]);
  });

  test('with no ?task, the default-task rule lands on the awaiting-human gate', async ({
    page,
  }) => {
    // This fixture's route names NO task (`…/activity/projectDesign`), and its
    // one task is the M0 gate awaiting a human.
    expect(fixture('activity-experience', 'project-design-m0').route).not.toContain('task=');

    const offBundle = await openState(page, 'activity-experience', 'project-design-m0');
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleNode('sdpReview'))).toHaveAttribute(
      'aria-current',
      'step'
    );
    // And it opened on the REVIEW body, which is what being at a gate means.
    await expect(page.getByTestId(TESTID.activityReviewBody)).toBeVisible();

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('with ?task= naming a task, it lands where the URL says — not on the default', async ({
    page,
  }) => {
    // `sub-attempts` opens at `?task=construction&rev=1` while its awaiting-human
    // gate is `codeReview`: the default rule would have chosen the gate, so this
    // discriminates "the URL decided" from "the rule decided".
    expect(fixture('activity-experience', 'sub-attempts').route).toContain('task=construction');

    const offBundle = await openState(page, 'activity-experience', 'sub-attempts');
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleNode('construction'))).toHaveAttribute(
      'aria-current',
      'step'
    );
    await expect(page.getByTestId(TESTID.lifecycleNode('codeReview'))).not.toHaveAttribute(
      'aria-current',
      'step'
    );
    // A dispatch task, so the dispatch body — not the gate's review body.
    await expect(page.getByTestId(TESTID.activityDispatchBody)).toBeVisible();
    await expect(page.getByTestId(TESTID.activityReviewBody)).toHaveCount(0);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('clicking a node opens its LATEST revision', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    // `detailedDesign` has two revisions. A plain click opens the latest, and the
    // URL then carries no `rev` at all — which is what "· latest, and no
    // read-only banner" is the reader's view of.
    await page.getByTestId(TESTID.lifecycleNode('detailedDesign')).click();
    await expect(page.getByTestId(TESTID.lifecycleNode('detailedDesign'))).toHaveAttribute(
      'aria-current',
      'step'
    );
    await expect(page.getByTestId(TESTID.lifecycleRevisionSelect)).toContainText(
      'Revision 2 of 2 · latest'
    );
    await expect(page.getByTestId(TESTID.activityHistoryBanner)).toHaveCount(0);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});

test.describe('activity experience: the revision menu — all three affordances', () => {
  // Three separate cases on purpose: all three are shipped affordances, and a
  // single case over one of them would let the other two rot.

  test('right-click on a pip opens it', async ({ page }) => {
    await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    await page.getByTestId(TESTID.lifecycleNode('detailedDesign')).click({ button: 'right' });

    const menu = page.getByTestId(TESTID.lifecycleRevisionMenu);
    await expect(menu).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleRevisionItem('detailedDesign', 1))).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleRevisionItem('detailedDesign', 2))).toBeVisible();
    expect(await incidents(page)).toEqual([]);
  });

  test('the caret on the ACTIVE pill opens it — the touch and discoverability route', async ({
    page,
  }) => {
    await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    // The caret exists only on the active pill, which is the fixture's own task.
    await page.getByTestId(TESTID.lifecycleNodeMenuButton('designReview')).click();

    await expect(page.getByTestId(TESTID.lifecycleRevisionMenu)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleRevisionItem('designReview', 1))).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleRevisionItem('designReview', 2))).toBeVisible();
    expect(await incidents(page)).toEqual([]);
  });

  test('Shift+F10 on a focused pip opens it — the keyboard route', async ({ page }) => {
    await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    await page.getByTestId(TESTID.lifecycleNode('detailedDesign')).focus();
    await page.keyboard.press('Shift+F10');

    await expect(page.getByTestId(TESTID.lifecycleRevisionMenu)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleRevisionItem('detailedDesign', 1))).toBeVisible();
    expect(await incidents(page)).toEqual([]);
  });
});

test.describe('activity experience: reading an older revision', () => {
  test('choosing a revision in the select moves the screen onto it', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    // The head first: the latest, no banner, and a live decision bar.
    await expect(page.getByTestId(TESTID.lifecycleRevisionSelect)).toContainText('· latest');
    await expect(page.getByTestId(TESTID.submitBar)).toBeVisible();

    await page.getByTestId(TESTID.lifecycleRevisionSelect).click();
    await page.getByTestId(TESTID.lifecycleRevisionOption(1)).click();

    // `?rev=1` is written, and the screen says so in the reader's own terms.
    await expect(page.getByTestId(TESTID.lifecycleRevisionSelect)).toContainText('· read-only');
    await expect(page.getByTestId(TESTID.activityHistoryBanner)).toBeVisible();

    // And Back to latest clears it again.
    await page.getByTestId(TESTID.activityBackToLatest).click();
    await expect(page.getByTestId(TESTID.activityHistoryBanner)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.lifecycleRevisionSelect)).toContainText('· latest');

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('a non-latest revision is read-only: the banner, its resolved threads expanded, NO submit bar', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    await page.getByTestId(TESTID.lifecycleRevisionSelect).click();
    await page.getByTestId(TESTID.lifecycleRevisionOption(1)).click();

    const banner = page.getByTestId(TESTID.activityHistoryBanner);
    await expect(banner).toContainText('Revision 1 of 2');
    await expect(banner).toContainText('read-only');
    // The artifact is the CURRENT one and the caption says so (GAP-5), and the
    // round's own send-back note is what the reader came for.
    await expect(page.getByTestId(TESTID.activityHistoryCaption)).toBeVisible();
    await expect(page.getByTestId(TESTID.activityRevisionNote)).toBeVisible();

    // A resolved thread is the POINT of a past round, so its card stays open —
    // the reply is on screen without a click.
    await expect(page.getByTestId(TESTID.marginCard('r1c1'))).toContainText(
      'The doc comment now names the two callers'
    );

    // Nothing that would change anything is rendered — not disabled, ABSENT.
    await expect(page.getByTestId(TESTID.submitBar)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.marginResolve('r1c1'))).toHaveCount(0);
    await expect(page.getByTestId(TESTID.marginReopen('r1c1'))).toHaveCount(0);
    await expect(page.getByTestId(/^comment-list-item-button-/)).toHaveCount(0);
    // The route's provider still renders its invisible probe span; what matters
    // is that nothing beneath the disabled one can arm an anchor, so it is EMPTY.
    await expect(page.getByTestId(TESTID.commentArmedAnchor)).toHaveAttribute(
      'data-anchor-path',
      ''
    );

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('the M0 history: read-only, with its option cards INERT and the choice said in words', async ({
    page,
  }) => {
    // This fixture opens directly on `?task=sdpReview&rev=1` — a decided M0 round
    // under a second one that is still open.
    const offBundle = await openState(page, 'activity-experience', 'project-design-m0-history');
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    await expect(page.getByTestId(TESTID.activityHistoryBanner)).toContainText('Revision 1 of 2');
    await expect(page.getByTestId(TESTID.submitBar)).toHaveCount(0);

    // Read-only is INERT, not disabled: the option cards lose the radio role and
    // every tab stop rather than keeping a control that does nothing.
    await expect(page.getByTestId(/^sdp-option-/).first()).toBeVisible();
    await expect(page.getByRole('radio')).toHaveCount(0);
    await expect(page.getByRole('radiogroup')).toHaveCount(0);
    // With the radio state gone, the filled dot and the border are colour alone —
    // so the chosen option says the word.
    await expect(page.getByText('CHOSEN', { exact: true })).toHaveCount(1);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});

test.describe('activity experience: what each body says', () => {
  test('a return arc is drawn only where a pair has been sent back', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();

    // Read a task with ONE revision, so the active pill carries no `↻` badge of
    // its own and the only one left on the strip is the RETURN ARC's — the arc
    // over the detailedDesign ↔ designReview pair, which was sent back once.
    await page.getByTestId(TESTID.lifecycleNode('stp')).click();
    await expect(page.getByTestId(TESTID.lifecycleRevisionSelect)).toHaveText('REVISION 1');
    await expect(page.getByText('↻2', { exact: true })).toHaveCount(1);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('…and nowhere on an activity that was never sent back', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', 'done');
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await expect(page.getByTestId(TESTID.lifecycleGraph)).toBeVisible();

    // Every task passed at revision 1: no arc, and no revision badge anywhere.
    await expect(page.getByText(/↻/)).toHaveCount(0);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('the Project Design M0 gate has an Approve verb and NO send-back anywhere, overflow included', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'activity-experience', 'project-design-m0');
    await expect(page.getByTestId(TESTID.activityReviewBody)).toBeVisible();

    await expect(page.getByTestId(TESTID.submitBarPrimary)).toHaveText(
      'Approve plan & cost — start construction'
    );
    await expect(page.getByTestId(TESTID.submitBarConsequence)).toContainText(
      'Commits the chosen option as the plan of record'
    );
    // The plan is DERIVED, so it is amended, never returned (spec R7). The
    // overflow is not merely empty — it is not rendered at all.
    await expect(page.getByTestId(TESTID.submitBarMenuButton)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.submitBarMenu)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.submitBarMenuItem('sendBack'))).toHaveCount(0);
    // What stands in its place: a navigation to the activity that CAN change it.
    await expect(page.getByTestId(TESTID.activityAmendArchitecture)).toBeVisible();

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  // C1. `resolveSubmitVerb` used to answer a staged QUESTION with an `ask` verb
  // BEFORE it looked at whether the rail had a question op — and ~30 of the 32
  // gates have none (`constructionManager` has no AskQuestions, R2/GAP-6). One
  // staged question therefore replaced Approve AND Send back with a button the
  // container's ask handler returns early from: a dead end on the gate. The fix
  // is a pair, and this case pins the half a unit test cannot reach — the
  // composer that stages the question is not there to stage it.

  test('a construction gate offers NO way to stage a question, and keeps its primary verb', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityReviewBody)).toBeVisible();
    // The gate is live and awaiting a human, which is what makes the verb below
    // the thing at stake.
    await expect(page.getByTestId(TESTID.submitBarPrimary)).toBeVisible();

    // Open the draft composer the only way this screen offers one without a row.
    await page.getByTestId(TESTID.marginAddNote).click();
    await expect(page.getByTestId(TESTID.marginComposer)).toBeVisible();

    // The composer IS open (its change-request toggle is there), and the Question
    // toggle is ABSENT — not disabled. A control that cannot be used is not one.
    await expect(page.getByTestId(TESTID.marginComposerChangeRequest)).toBeVisible();
    await expect(page.getByTestId(TESTID.marginComposerQuestion)).toHaveCount(0);

    // And the bar still carries the verb the reviewer came for.
    await expect(page.getByTestId(TESTID.submitBarPrimary)).toBeVisible();
    await expect(page.getByTestId(TESTID.submitBarNotice)).toHaveCount(0);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('…while the M0 gate, which HAS a question op, still offers the toggle', async ({ page }) => {
    // The discrimination: the composer is not question-less everywhere. Spec §6
    // allows comments AND questions at M0, and `projectDesignAskQuestions` is the
    // op behind it.
    const offBundle = await openState(page, 'activity-experience', 'project-design-m0');
    await expect(page.getByTestId(TESTID.activityReviewBody)).toBeVisible();

    await page.getByTestId(TESTID.marginAddNote).click();
    await expect(page.getByTestId(TESTID.marginComposer)).toBeVisible();
    await expect(page.getByTestId(TESTID.marginComposerQuestion)).toBeVisible();

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('a gate whose reviewer set could not be proposed shows the engine refusal, not an empty strip', async ({
    page,
  }) => {
    const view = fixture('activity-experience', 'review-set-error').ops[
      'constructionQueryActivityView'
    ]?.result as { reviewSetError: string };
    expect(view.reviewSetError).toBeTruthy();

    const offBundle = await openState(page, 'activity-experience', 'review-set-error');
    await expect(page.getByTestId(TESTID.activityReviewersStrip)).toBeVisible();
    // Verbatim — a refusal paraphrased is a refusal nobody can act on.
    await expect(page.getByTestId(TESTID.activityReviewSetError)).toContainText(
      view.reviewSetError
    );

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('the sub-attempt line appears only where a revision took more than one attempt', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'activity-experience', 'sub-attempts');
    await expect(page.getByTestId(TESTID.activityDispatchBody)).toBeVisible();
    await expect(page.getByTestId(TESTID.activitySubAttempts)).toHaveText(
      '3 attempts before this revision reached the gate'
    );
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('…and nowhere on a revision that reached its gate first time', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', 'done');
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await page.getByTestId(TESTID.lifecycleNode('construction')).click();
    await expect(page.getByTestId(TESTID.activityDispatchBody)).toBeVisible();
    await expect(page.getByTestId(TESTID.activitySubAttempts)).toHaveCount(0);
    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});

test.describe('activity experience: §7.5 — the anchors a margin card is placed by', () => {
  test('gap 1 · ReadOnlyRow: in a read-only history, a card anchored to a row is PLACED', async ({
    page,
  }) => {
    const offBundle = await openState(page, 'activity-experience', 'project-design-m0-history');
    await expect(page.getByTestId(TESTID.activityHistoryBanner)).toBeVisible();

    const unplaced = page.getByTestId(TESTID.marginUnplaced);
    // `m0r1c2` is anchored to an activity-list row. The whole surface is
    // read-only — CommentableList renders its inert branch — and the card is
    // still PLACED, which is only true because that branch enrols each row's
    // anchor (ReadOnlyRow).
    const placed = page.getByTestId(TESTID.marginCard('m0r1c2'));
    await expect(placed).toBeVisible();
    await expect(unplaced.getByTestId(TESTID.marginCard('m0r1c2'))).toHaveCount(0);

    // …and so is a card anchored to an SDP OPTION row: `OptionRow` enrols
    // `sdpOptionAnchor(solutionKind)` (final fix wave, I4 — before it, the one
    // surface whose whole job is choosing between options could not place a
    // single comment on one).
    await expect(page.getByTestId(TESTID.marginCard('m0r1c1'))).toBeVisible();
    await expect(unplaced.getByTestId(TESTID.marginCard('m0r1c1'))).toHaveCount(0);

    // The control, and the reason this is a discrimination and not a tautology:
    // `m0r1c3` points at an option kind The Method never assembles, so no row can
    // ever enrol it and the card lands in the unanchored group, which leads the
    // margin.
    const orphan = page.getByTestId(TESTID.marginCard('m0r1c3'));
    await expect(unplaced.getByTestId(TESTID.marginCard('m0r1c3'))).toHaveCount(1);
    // Placed means an OFFSET, not a slot in a list: the card sits level with a
    // row far down the artifact, well below the group that leads the column.
    const placedBox = await placed.boundingBox();
    const orphanBox = await orphan.boundingBox();
    expect(placedBox?.y ?? 0).toBeGreaterThan(orphanBox?.y ?? 0);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });

  test('gap 2 · OpRow: a thread anchored to a contract operation is PLACED', async ({ page }) => {
    const offBundle = await openState(page, 'activity-experience', SERVICE);
    await expect(page.getByTestId(TESTID.activityScreen)).toBeVisible();
    await page.getByTestId(TESTID.lifecycleRevisionSelect).click();
    await page.getByTestId(TESTID.lifecycleRevisionOption(1)).click();
    await expect(page.getByTestId(TESTID.serviceContractRoot)).toBeVisible();

    const unplaced = page.getByTestId(TESTID.marginUnplaced);
    // `r1c1` is anchored under `contractOpAnchor(component, signature)` — the
    // same enrolment ContractSignatureList's OpRow puts on every operation row,
    // which is what lets the card sit level with the op it is about.
    await expect(page.getByTestId(TESTID.marginCard('r1c1'))).toBeVisible();
    await expect(unplaced.getByTestId(TESTID.marginCard('r1c1'))).toHaveCount(0);
    // The control: the round's other comment points at a struct field the
    // artifact's view does not enrol, so it belongs in the unanchored group.
    await expect(unplaced.getByTestId(TESTID.marginCard('r1c2'))).toHaveCount(1);

    expect(await incidents(page)).toEqual([]);
    expect(offBundle).toEqual([]);
  });
});
