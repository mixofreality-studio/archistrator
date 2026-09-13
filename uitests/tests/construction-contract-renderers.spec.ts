/**
 * construction-contract-renderers.spec — the contract diagrams are back, placed
 * per phase and task, labelled honestly (renderers slice 1; designer ruling
 * renderers-placement.md, architect ruling design-renderer-data.md §3.1).
 *
 * The regression this pins: a state reset emptied every construction row's
 * `produced[]`, the activity → contract join read nothing else, and every C-*
 * row said "No service contract is recorded" beneath an evidence line naming the
 * contract — while 29 contracts sat on the wire. No uitest caught it. The join
 * now reads the committed activity list's componentId → the component's
 * contractKey → serviceContracts, and these cases pin, per phase:
 *
 *   - Detailed Design (phase, its Design task, a Not started row, Observed only)
 *     shows the full ServiceContractView on Code;
 *   - Design Review wraps it above the verdict, UNDER REVIEW only when the gate
 *     is owed now on an observed attempt;
 *   - a bare click shows the summary card and no canvas; a construction task a
 *     one-line REFERENCE; Code Review the commit, with the contract as REFERENCE;
 *   - the Component tab draws the architecture's neighbours;
 *   - the frame never carries the provenance hatch;
 *   - the gap states: a real gap (C-design-health-engine), none by design
 *     (R-github), no component (N-STP) — three different sentences;
 *   - the Test Plan phase: the honest empty state plus system test coverage;
 *   - the focus view (`&focus=1`), and summary-only below 600px.
 *
 * Gated like the other construction specs: the seeded "archistrator" project
 * behind the SPA proxy. SAFETY: the shared dispatch guard aborts every non-GET;
 * the two cases that fake a read edit only GET responses.
 */
import type { Locator, Page, Route } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const MANAGER = 'C-construction-manager';
const ENGINE = 'C-review-engine';
/** Rowless: no attempt recorded at all, so every task reads Not started. */
const NOT_STARTED = 'C-billing-state-access';
const GAP = 'C-design-health-engine';
const RESOURCE = 'R-github';

// Wire ordinals (enums.gen.ts), as construction-graph-owed.spec uses them.
const STAGE_AWAITING_APPROVAL = 7;
const BUILD_IN_REVIEW = 1;

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

async function open(page: Page, query: string, width = 1600): Promise<void> {
  await page.setViewportSize({ width, height: 950 });
  await gotoApp(page, `/project/archistrator/construction?lens=list&${query}`);
  // Below 600px the modal drawer covers the whole list; wait for the pane instead.
  await expect(
    page.getByTestId(width < 600 ? TESTID.constructionDetailBody : TESTID.constructionListTree)
  ).toBeVisible({ timeout: 15_000 });
}

function pane(page: Page): Locator {
  return page.getByTestId(TESTID.constructionDetailBody);
}

/** The role labels on every artifact frame in `scope`, in order. */
async function roles(scope: Locator): Promise<string[]> {
  return scope.getByTestId(TESTID.constructionArtifactRole).allInnerTexts();
}

test('Detailed Design shows the full contract on Code, and the evidence line and the body agree', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`);
  const body = pane(page);
  // The live contradiction this fixes: evidence names the contract, the body denied it.
  await expect(body.getByTestId(TESTID.constructionDetailEvidence)).toContainText(
    'evidence · contract constructionManager'
  );
  const contract = body.getByTestId(TESTID.serviceContractRoot);
  await expect(contract).toBeVisible();
  await expect(contract).toHaveAttribute('data-view', 'code');
  await expect(body.getByTestId(TESTID.serviceContractTabCode)).toHaveAttribute('aria-pressed', 'true');
  await expect(body).not.toContainText('No service contract');

  expect(await roles(body)).toEqual(['COMMITTED NOW']);
  await expect(body.getByTestId(TESTID.constructionArtifactSource)).toHaveText(
    'serviceContracts.constructionManager · current · no revision history recorded'
  );
  // A reconstructed attempt gets the one sentence, and no authorship is claimed.
  await expect(body.getByTestId(TESTID.constructionArtifactReconstructedNote)).toHaveText(
    'The contract below is the one committed today. Nothing recorded links it to this attempt — the attempt was reconstructed, and the contract has no revision history.'
  );
  await expect(body).not.toContainText('written by attempt');
  // The old view's fabricated status chip is gone; the revision slot is one line.
  await expect(body.getByTestId(TESTID.serviceContractStatusChip)).toHaveCount(0);
  await expect(body).not.toContainText('IN-DESIGN');
  await expect(body.getByTestId(TESTID.serviceContractRevisionHistory)).toHaveText(
    'No revision history recorded.'
  );
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the phase itself shows the same contract, and a Not started row still shows its committed contract', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design`);
  await expect(pane(page).getByTestId(TESTID.serviceContractRoot)).toBeVisible();

  await open(page, `a=${NOT_STARTED}&p=detailed_design&k=detailedDesign`);
  const body = pane(page);
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText('NOT STARTED');
  await expect(body.getByTestId(TESTID.constructionArtifactStateLine)).toHaveText(
    'Not started. Nothing has run for this task yet.'
  );
  await expect(body.getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  expect(await roles(body)).toEqual(['COMMITTED NOW']);
  // No attempt, so nothing to disown: no reconstructed sentence.
  await expect(body.getByTestId(TESTID.constructionArtifactReconstructedNote)).toHaveCount(0);
  // The rest of the briefing moved under the artifact, collapsed.
  const about = body.getByTestId(TESTID.constructionArtifactAboutTask);
  await expect(about).toBeVisible();
  await expect(about).not.toHaveAttribute('open', /.*/);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('with Observed only on, the contract stays — project state, not evidence', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`);
  await page.getByTestId(TESTID.constructionLensObservedOnly).click();
  const body = pane(page);
  await expect(page.getByTestId(TESTID.constructionDetailObservedOnlyChip)).toBeVisible();
  await expect(body.getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  await expect(body.getByTestId(TESTID.constructionArtifactSource)).toHaveText(
    'serviceContracts.constructionManager · current · no revision history recorded · shown under Observed only: project state, not evidence'
  );
  expect(await roles(body)).toEqual(['COMMITTED NOW']);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the artifact frame never carries the provenance hatch', async ({ page, dispatchGuard }) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`);
  const body = pane(page);
  // The hatch is on the attempt's note…
  await expect(body.getByTestId(TESTID.constructionDetailProvenanceNote)).toBeVisible();
  const frame = body.getByTestId(TESTID.constructionArtifactFrame);
  await expect(frame).toHaveCount(1);
  // …and nowhere on or inside the frame, which is not inside the note either.
  const hatched = await frame.evaluate((el, noteId) => {
    const nodes = [el, ...Array.from(el.querySelectorAll('*'))];
    const hatch = nodes.some((n) =>
      getComputedStyle(n).backgroundImage.includes('repeating-linear-gradient')
    );
    return { hatch, insideNote: el.closest(`[data-testid="${noteId}"]`) !== null };
  }, TESTID.constructionDetailProvenanceNote);
  expect(hatched).toEqual({ hatch: false, insideNote: false });
  expect(dispatchGuard.blocked).toEqual([]);
});

test('a bare click is the summary card — no canvas — and opens Detailed Design in one click', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}`);
  const body = pane(page);
  const summary = body.getByTestId(TESTID.constructionContractSummary);
  await expect(summary).toBeVisible();
  await expect(summary).toContainText('10 ops');
  await expect(summary).toContainText('+5 more');
  await expect(body.getByTestId(TESTID.serviceContractRoot)).toHaveCount(0);
  expect(await roles(body)).toEqual(['COMMITTED NOW']);

  await body.getByTestId(TESTID.constructionContractSummaryOpen).click();
  await expect(page).toHaveURL(/[?&]p=detailed_design/);
  await expect(pane(page).getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('a construction task carries the contract as a one-line REFERENCE, never as its product', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=construction&k=construction`);
  const body = pane(page);
  await expect(body.getByTestId(TESTID.constructionContractReference)).toHaveText(
    'The contract this code implements → Open'
  );
  expect(await roles(body)).toEqual(['REFERENCE']);
  await expect(body.getByTestId(TESTID.serviceContractRoot)).toHaveCount(0);
  await body.getByTestId(TESTID.constructionContractReferenceOpen).click();
  await expect(page).toHaveURL(/[?&]p=detailed_design/);
  await expect(page).not.toHaveURL(/[?&]k=/);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('Code Review shows the commit under review; the contract is only its REFERENCE', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=construction&k=codeReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(review).toBeVisible();
  const commit = review.getByTestId(TESTID.constructionCodeReviewCommit);
  await expect(commit).toContainText(/[0-9a-f]{40}/);
  await expect(commit).toContainText('No code view in this stage.');
  // The backfilled review is not owed, so nothing reads UNDER REVIEW — and the
  // contract is REFERENCE, never the thing under code review.
  expect(await roles(review)).toEqual(['COMMITTED NOW', 'REFERENCE']);
  await expect(review.getByTestId(TESTID.constructionDetailVerdict)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// Design Review: the contract above the verdict; UNDER REVIEW only when owed
// ---------------------------------------------------------------------------

/** Fake a live gate on `activityId`'s Detailed Design — GET responses only. */
async function serveOwedDesignGate(page: Page, activityId: string): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as {
      ActivityConstruction?: Record<string, Record<string, unknown>>;
    };
    const row = wire.ActivityConstruction?.[activityId];
    if (row === undefined) throw new Error(`no row ${activityId} in the read`);
    // A probe candidate (owedWork.probeCandidatesFor): recorded, started, not
    // completed — so the console asks the session route, which answers a gate.
    Object.assign(row, {
      recorded: true,
      classified: true,
      hasBuildEvidence: true,
      startedAt: '2026-09-12T20:00:00Z',
      BuildStatus: BUILD_IN_REVIEW,
      CurrentPhase: 'detailed_design',
    });
    await route.fulfill({ response, json: wire });
  });
  await page.route('**/get-session-state/archistrator/**', async (route: Route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '');
    if (id !== activityId) {
      await route.fulfill({ status: 404, json: { error: 'no construction session' } });
      return;
    }
    await route.fulfill({
      json: { projectId: 'archistrator', activityId: id, stage: STAGE_AWAITING_APPROVAL },
    });
  });
}

test('Design Review: the contract above the verdict, COMMITTED NOW when the gate is not owed', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=designReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(review.getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  await expect(review.getByTestId(TESTID.serviceContractTabCode)).toHaveAttribute('aria-pressed', 'true');
  expect(await roles(review)).toEqual(['COMMITTED NOW']);
  // The contract sits ABOVE the verdict.
  const contractBox = await review.getByTestId(TESTID.serviceContractRoot).boundingBox();
  const verdictBox = await review.getByTestId(TESTID.constructionDetailVerdict).boundingBox();
  expect(contractBox !== null && verdictBox !== null && contractBox.y < verdictBox.y).toBe(true);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('UNDER REVIEW appears when the gate is owed now on an observed attempt', async ({
  page,
  dispatchGuard,
}) => {
  // Rowless: no attempt recorded, so the owed gate is the live workflow's own.
  await serveOwedDesignGate(page, NOT_STARTED);
  await open(page, `a=${NOT_STARTED}&p=detailed_design&k=designReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(review.getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  await expect.poll(() => roles(review)).toEqual(['UNDER REVIEW']);
  await expect(review.getByTestId(TESTID.constructionArtifactFrame)).toHaveAttribute(
    'data-role',
    'underReview'
  );
  expect(dispatchGuard.blocked).toEqual([]);
});

test('an owed gate over a RECONSTRUCTED attempt never reads UNDER REVIEW', async ({
  page,
  dispatchGuard,
}) => {
  await serveOwedDesignGate(page, MANAGER);
  await open(page, `a=${MANAGER}&p=detailed_design&k=designReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(review.getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  // The gate IS owed (the pane says so)…
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/AWAITING YOU/i);
  // …but its attempt was reconstructed, so the frame claims only what is committed.
  expect(await roles(review)).toEqual(['COMMITTED NOW']);
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// The Component tab: the architecture's own relationships
// ---------------------------------------------------------------------------

test('the Component tab draws the architecture’s neighbours, and a neighbour click moves the selection', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`);
  const body = pane(page);
  await body.getByTestId(TESTID.serviceContractTabComponent).click();
  await expect(page).toHaveURL(/[?&]av=component/);
  const flow = body.getByTestId(TESTID.serviceContractComponentFlow);
  await expect(flow).toBeVisible();
  expect(Number(await flow.getAttribute('data-edge-count'))).toBeGreaterThan(1);
  await expect(flow.getByTestId(TESTID.archC4Node('construction-manager'))).toBeVisible();
  const neighbour = flow.getByTestId(TESTID.archC4Node('review-engine'));
  await expect(neighbour).toBeVisible();

  await neighbour.click();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${ENGINE}(&|$)`));
  await expect(page).toHaveURL(/[?&]k=detailedDesign/);
  await expect(page).toHaveURL(/[?&]av=component/);
  await expect(pane(page).getByTestId(TESTID.serviceContractComponentFlow)).toHaveAttribute(
    'data-component-id',
    'review-engine'
  );
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the Facets tab names the Client layer only on a client contract', async ({
  page,
  dispatchGuard,
}) => {
  // Every committed contract declares facets, so empty one engine's in the READ.
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as {
      ServiceContracts?: Record<string, Record<string, unknown>>;
    };
    const engine = wire.ServiceContracts?.['reviewEngine'];
    if (engine === undefined) throw new Error('no reviewEngine contract in the read');
    Object.assign(engine, { DataContracts: [], ErrorModel: '', Idempotency: '' });
    await route.fulfill({ response, json: wire });
  });
  await open(page, `a=${ENGINE}&p=detailed_design&av=facets`);
  const empty = pane(page).getByTestId(TESTID.serviceContractFacetsEmpty);
  await expect(empty).toHaveText('This contract declares no data/error/idempotency facets.');
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// The gap states: three different facts, three different sentences
// ---------------------------------------------------------------------------

test('a missing contract and none by design are distinct; no component says nothing', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${GAP}&p=detailed_design`);
  const gap = pane(page).getByTestId(TESTID.constructionContractGap);
  await expect(gap).toContainText('NO CONTRACT COMMITTED');
  await expect(gap).toContainText('this is missing data, not a design choice');
  await expect(gap).toContainText('EvaluateDesignHealth(project, systemModel) → findings');
  await expect(gap).toHaveAttribute('data-tone', 'gap');
  await expect(pane(page).getByTestId(TESTID.serviceContractRoot)).toHaveCount(0);
  // Its real neighbours are still drawn.
  await expect(
    pane(page).getByTestId(TESTID.serviceContractComponentFlow).getByTestId(TESTID.archC4Node('system-design-manager'))
  ).toBeVisible();
  const gapText = await gap.innerText();

  await open(page, `a=${RESOURCE}`);
  const byDesign = pane(page).getByTestId(TESTID.constructionContractByDesign);
  await expect(byDesign).toContainText('NO CONTRACT · BY DESIGN');
  await expect(byDesign).toContainText('Nothing is missing.');
  await expect(byDesign).toHaveAttribute('data-tone', 'byDesign');
  await expect(
    pane(page).getByTestId(TESTID.constructionWhoReachesIt).getByTestId(TESTID.archC4Node('source-control-access'))
  ).toBeVisible();
  expect(await byDesign.innerText()).not.toEqual(gapText);

  await open(page, 'a=N-STP');
  const body = pane(page);
  await expect(page.getByTestId(TESTID.constructionTestPlanView)).toBeVisible();
  for (const id of [
    TESTID.constructionContractGap,
    TESTID.constructionContractByDesign,
    TESTID.constructionContractSummary,
    TESTID.serviceContractRoot,
  ]) {
    await expect(body.getByTestId(id)).toHaveCount(0);
  }
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// The Test Plan phase: the honest empty state plus system test coverage
// ---------------------------------------------------------------------------

test('a manager’s Test Plan: no component plan recorded, direct system coverage below it', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=test_plan&k=stp`);
  const body = pane(page);
  const empty = body.getByTestId(TESTID.constructionComponentTestPlanEmpty);
  await expect(empty).toContainText('NO COMPONENT TEST PLAN RECORDED');
  // The stp attempt was reconstructed, so the backfill clause is there.
  await expect(empty).toContainText('backfilled with no plan behind them');
  const direct = body.getByTestId(TESTID.constructionTestCoverageDirect);
  await expect(direct).toContainText('These are not this component’s own tests.');
  await expect(direct.getByTestId(TESTID.constructionScenarioPicker)).toBeVisible();
  expect(await roles(body)).toEqual(['REFERENCE']);
  await expect(body).not.toContainText(/untested/i);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('an engine’s Test Plan: reached through its manager, never "untested", and the row opens N-STP', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${ENGINE}&p=test_plan&k=stp`);
  const body = pane(page);
  await expect(body.getByTestId(TESTID.constructionTestCoverageDirect)).toHaveCount(0);
  const row = body.getByTestId(TESTID.constructionCoverageReachedRow('STP-UC3'));
  await expect(row).toContainText('reached through constructionManager');
  await expect(body.getByTestId(TESTID.constructionUseCaseFlowsLink)).toContainText(
    'These are designed call chains, not tests.'
  );
  await expect(body).not.toContainText(/untested/i);
  await row.click();
  await expect(page).toHaveURL(/[?&]a=N-STP(&|$)/);
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// The focus view
// ---------------------------------------------------------------------------

test('focus=1 opens the artifact full-viewport with the header and action bar; the lens stays mounted and the poll does not close it', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&av=facets&focus=1`, 1280);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  await expect(focus).toBeVisible();
  const box = await focus.boundingBox();
  expect(box !== null && box.width >= 1279 && box.height >= 949).toBe(true);
  await expect(focus.getByTestId(TESTID.constructionDetailBreadcrumb)).toContainText('Detailed Design');
  await expect(focus.getByTestId(TESTID.constructionDetailActionBar)).toBeVisible();
  await expect(focus.getByTestId(TESTID.serviceContractRoot)).toHaveAttribute('data-view', 'facets');
  // The lens is still mounted underneath.
  await expect(page.getByTestId(TESTID.constructionListTree)).toHaveCount(1);

  // Two poll cycles: the selection, the tab and the layer all survive.
  await page.waitForTimeout(3_500);
  await expect(focus).toBeVisible();
  await expect(focus.getByTestId(TESTID.serviceContractRoot)).toHaveAttribute('data-view', 'facets');
  await expect(page).toHaveURL(/[?&]focus=1/);

  await page.keyboard.press('Escape');
  await expect(focus).toHaveCount(0);
  await expect(page).not.toHaveURL(/[?&]focus=/);
  await expect(page).toHaveURL(new RegExp(`[?&]a=${MANAGER}`));
  expect(dispatchGuard.blocked).toEqual([]);
});

test('F opens the focus view from the pane, and closing it returns focus to the Focus button', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`);
  const button = pane(page).getByTestId(TESTID.constructionArtifactFocus);
  await button.focus();
  await page.keyboard.press('f');
  const focus = page.getByTestId(TESTID.constructionFocusView);
  await expect(focus).toBeVisible();
  await expect(page).toHaveURL(/[?&]focus=1/);
  await focus.getByTestId(TESTID.constructionFocusClose).click();
  await expect(focus).toHaveCount(0);
  await expect
    .poll(() =>
      button.evaluate((el) => document.activeElement === el),
      { message: 'focus returned to the Focus button' }
    )
    .toBe(true);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('below 600px the drawer shows summary cards only, and Focus is where the diagram draws', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, 500);
  const body = pane(page);
  await expect(body.getByTestId(TESTID.constructionContractSummary)).toBeVisible();
  await expect(body.getByTestId(TESTID.serviceContractRoot)).toHaveCount(0);
  await body.getByTestId(TESTID.constructionArtifactFocus).click();
  const focus = page.getByTestId(TESTID.constructionFocusView);
  await expect(focus.getByTestId(TESTID.serviceContractRoot)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// The frontend renderer: the app refuses to be framed, so nothing frames it
// ---------------------------------------------------------------------------

const SPA = 'U-SPA-web-client';

/** Give the SPA row an observed construction attempt and `produced` records — GET only. */
async function serveSpaConstruction(page: Page, produced: Record<string, unknown>[]): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as {
      ActivityConstruction?: Record<string, Record<string, unknown>>;
    };
    const row = wire.ActivityConstruction?.[SPA];
    if (row === undefined) throw new Error(`no row ${SPA} in the read`);
    Object.assign(row, {
      recorded: true,
      classified: true,
      hasBuildEvidence: true,
      Produced: produced,
      attempts: [
        {
          attemptId: `${SPA}:construction:1`,
          task: 'construction',
          phase: 'construction',
          attempt: 1,
          actor: 'agent',
          outcome: 'passed',
          evidence: { kind: '', ref: '' },
          provenance: { origin: 'observed' },
        },
      ],
    });
    await route.fulfill({ response, json: wire });
  });
}

async function iframesIn(scope: Locator): Promise<number> {
  // eslint-disable-next-line no-restricted-syntax -- a structural assertion: no iframe element exists at all, and an iframe has no testid to count by once it is gone
  return scope.locator('iframe').count();
}

test('a built surface opens in a new tab — no same-origin iframe renders', async ({
  page,
  dispatchGuard,
}) => {
  await serveSpaConstruction(page, [
    {
      Kind: 'ui-code',
      Title: 'Construction console',
      Source: '/project/archistrator/construction',
      Produced: true,
      Note: '',
    },
  ]);
  await open(page, `a=${SPA}&p=construction&k=construction`);
  const view = pane(page).getByTestId(TESTID.constructionFrontendView);
  await expect(view).toBeVisible();
  const link = view.getByTestId(TESTID.constructionFrontendOpenLink);
  await expect(link).toHaveAttribute('href', '/project/archistrator/construction');
  await expect(link).toHaveAttribute('target', '_blank');
  expect(await iframesIn(page.getByTestId(TESTID.constructionDetailBody))).toBe(0);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('with no surface recorded the frontend renderer says so, and frames nothing', async ({
  page,
  dispatchGuard,
}) => {
  await serveSpaConstruction(page, []);
  await open(page, `a=${SPA}&p=construction&k=construction`);
  const empty = pane(page).getByTestId(TESTID.constructionFrontendNoSurfaces);
  await expect(empty).toContainText('NO SURFACES RECORDED');
  await expect(empty).toContainText('the console does not guess routes');
  expect(await iframesIn(page.getByTestId(TESTID.constructionDetailBody))).toBe(0);
  expect(dispatchGuard.blocked).toEqual([]);
});
