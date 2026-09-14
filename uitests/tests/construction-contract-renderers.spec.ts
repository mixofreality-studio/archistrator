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

async function open(page: Page, query: string, width = 1600, height = 950): Promise<void> {
  await page.setViewportSize({ width, height });
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
  // A reconstructed attempt gets the one sentence — in the provenance note, after
  // the basis (S3: above the frame it pushed the first op under the action bar) —
  // and no authorship is claimed.
  await expect(
    body
      .getByTestId(TESTID.constructionDetailProvenanceNote)
      .getByTestId(TESTID.constructionArtifactReconstructedNote)
  ).toHaveText(
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

test('Code Review on a RECONSTRUCTED attempt: one line, no CODE frame; the contract only as REFERENCE', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=construction&k=codeReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(review).toBeVisible();
  // B3: a reconstructed attempt reviewed nothing anyone watched. No frame claims it did.
  await expect(review.getByTestId(TESTID.constructionCodeReviewNoView)).toHaveText(
    'No code view in this stage.'
  );
  await expect(review.getByTestId(TESTID.constructionCodeReviewCommit)).toHaveCount(0);
  expect(await roles(review)).toEqual(['REFERENCE']);
  await expect(review).not.toContainText('COMMITTED NOW');
  await expect(review.getByTestId(TESTID.constructionDetailVerdict)).toBeVisible();
  // "The contract below is the one committed today" would name a contract this
  // body does not show, so the note carries no such sentence here.
  await expect(page.getByTestId(TESTID.constructionArtifactReconstructedNote)).toHaveCount(0);
  expect(dispatchGuard.blocked).toEqual([]);
});

/**
 * Rewrite the project READ (GET responses only): each mutator edits the wire's
 * activity rows before the SPA sees them. One handler, so several edits compose.
 */
async function serveRead(
  page: Page,
  mutate: (rows: Record<string, Record<string, unknown>>) => void
): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as {
      ActivityConstruction?: Record<string, Record<string, unknown>>;
    };
    mutate(wire.ActivityConstruction ?? {});
    await route.fulfill({ response, json: wire });
  });
}

const SHA = '98eeae5806ed1ad2631accdb82cc9bfe73545882';

/** An OBSERVED Code Review attempt on `row`, pointing at SHA. */
function observedCodeReview(row: Record<string, unknown>, id: string): void {
  const attempts = (row['attempts'] as Record<string, unknown>[] | undefined) ?? [];
  row['attempts'] = [
    ...attempts.filter((a) => a['task'] !== 'codeReview'),
    {
      attemptId: `${id}:codeReview:1`,
      task: 'codeReview',
      phase: 'construction',
      attempt: 1,
      actor: 'human',
      outcome: 'passed',
      evidence: { kind: 'git', ref: SHA },
      provenance: { origin: 'observed' },
    },
  ];
}

test('Code Review on an OBSERVED attempt not owed reads REVIEWED, sourced to its commit and attempt', async ({
  page,
  dispatchGuard,
}) => {
  await serveRead(page, (rows) => {
    const row = rows[MANAGER];
    if (row === undefined) throw new Error(`no row ${MANAGER} in the read`);
    observedCodeReview(row, MANAGER);
  });
  await open(page, `a=${MANAGER}&p=construction&k=codeReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(review.getByTestId(TESTID.constructionCodeReviewCommit)).toContainText(SHA);
  await expect.poll(() => roles(review)).toEqual(['REVIEWED', 'REFERENCE']);
  await expect(review.getByTestId(TESTID.constructionArtifactSource).first()).toHaveText(
    `evidence · git ${SHA} · attempt 1`
  );
  await expect(review.getByTestId(TESTID.constructionCodeReviewNoView)).toHaveCount(0);
  await expect(review).not.toContainText('COMMITTED NOW');
  expect(dispatchGuard.blocked).toEqual([]);
});

test('Code Review owed now over a RECONSTRUCTED attempt is still one line — never UNDER REVIEW', async ({
  page,
  dispatchGuard,
}) => {
  // The gate IS owed (the pane says so), but the attempt it would decide was
  // reconstructed: nobody reviewed that commit, so no CODE frame may claim it.
  await serveOwedGate(page, MANAGER, 'construction');
  await open(page, `a=${MANAGER}&p=construction&k=codeReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/AWAITING YOU/i);
  await expect(review.getByTestId(TESTID.constructionCodeReviewNoView)).toHaveText(
    'No code view in this stage.'
  );
  await expect(review.getByTestId(TESTID.constructionCodeReviewCommit)).toHaveCount(0);
  expect(await roles(review)).toEqual(['REFERENCE']);
  await expect(review).not.toContainText('UNDER REVIEW');
  expect(dispatchGuard.blocked).toEqual([]);
});

test('Code Review owed now on an observed attempt reads UNDER REVIEW — never COMMITTED NOW', async ({
  page,
  dispatchGuard,
}) => {
  await serveOwedGate(page, NOT_STARTED, 'construction', (row) => {
    observedCodeReview(row, NOT_STARTED);
  });
  await open(page, `a=${NOT_STARTED}&p=construction&k=codeReview`);
  const review = page.getByTestId(TESTID.constructionDetailBodyReview);
  await expect.poll(() => roles(review)).toEqual(['UNDER REVIEW', 'REFERENCE']);
  await expect(review.getByTestId(TESTID.constructionCodeReviewCommit)).toContainText(SHA);
  await expect(review).not.toContainText('COMMITTED NOW');
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// Design Review: the contract above the verdict; UNDER REVIEW only when owed
// ---------------------------------------------------------------------------

/** Fake a live gate on `activityId`'s Detailed Design — GET responses only. */
async function serveOwedDesignGate(page: Page, activityId: string): Promise<void> {
  await serveOwedGate(page, activityId, 'detailed_design');
}

/** Fake a live gate on `activityId` at `phase`, plus an optional row edit — GET only. */
async function serveOwedGate(
  page: Page,
  activityId: string,
  phase: string,
  edit?: (row: Record<string, unknown>) => void
): Promise<void> {
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
      CurrentPhase: phase,
    });
    edit?.(row);
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

test('S3: the pane’s Component tab lists callers and callees as text rows; a row moves the selection and Back returns', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`);
  const body = pane(page);
  await body.getByTestId(TESTID.serviceContractTabComponent).click();
  await expect(page).toHaveURL(/[?&]av=component/);
  const view = body.getByTestId(TESTID.serviceContractComponentFlow);
  await expect(view).toHaveAttribute('data-mode', 'list');
  expect(Number(await view.getAttribute('data-edge-count'))).toBeGreaterThan(1);
  // Text, not a canvas: the diagram fit to 0.31 in the pane.
  await expect(view.getByTestId(TESTID.archC4Node('review-engine'))).toHaveCount(0);
  // eslint-disable-next-line no-restricted-syntax -- a structural assertion: no xyflow canvas exists in the pane at all
  await expect(body.locator('.react-flow')).toHaveCount(0);
  await expect(view).toContainText('CALLED BY');
  await expect(view).toContainText('CALLS');
  const row = view.getByTestId(TESTID.serviceContractNeighbourRow('review-engine'));
  await expect(row).toContainText('ReviewEngine');
  // Where it goes, said at its end.
  await expect(row).toContainText(`→ ${ENGINE}`);
  const px = await row.evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
  expect(px, 'a neighbour row reads at pane size').toBeGreaterThanOrEqual(11);
  // "Open diagram in focus view" stays.
  await expect(view.getByTestId(TESTID.serviceContractOpenFocus)).toBeVisible();

  await row.click();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${ENGINE}(&|$)`));
  await expect(page).toHaveURL(/[?&]k=detailedDesign/);
  await expect(page).toHaveURL(/[?&]av=component/);
  await expect(pane(page).getByTestId(TESTID.serviceContractComponentFlow)).toHaveAttribute(
    'data-component-id',
    'review-engine'
  );
  // A jump to another activity PUSHES a history entry: Back returns to where we were.
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${MANAGER}(&|$)`));
  await expect(pane(page).getByTestId(TESTID.serviceContractComponentFlow)).toHaveAttribute(
    'data-component-id',
    'construction-manager'
  );
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S3: the focus view draws the Component diagram; a neighbour click hops, no toolbar survives, and Back returns', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&av=component&focus=1`, 1600);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  const flow = focus.getByTestId(TESTID.serviceContractComponentFlow);
  await expect(flow).toHaveAttribute('data-mode', 'canvas');
  await expect(flow.getByTestId(TESTID.archC4Node('construction-manager'))).toBeVisible();
  const neighbour = flow.getByTestId(TESTID.archC4Node('review-engine'));
  await expect(neighbour).toBeVisible();

  await neighbour.click();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${ENGINE}(&|$)`));
  await expect(page).toHaveURL(/[?&]focus=1/);
  await expect(focus.getByTestId(TESTID.serviceContractComponentFlow)).toHaveAttribute(
    'data-component-id',
    'review-engine'
  );
  // Polish 9: the clicked node's Comment toolbar does not survive the hop.
  await page.waitForTimeout(600);
  // eslint-disable-next-line no-restricted-syntax -- xyflow's NodeToolbar portal carries no testid; counting its generated class is the only structural check
  expect(await page.locator('.react-flow__node-toolbar').count()).toBe(0);
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${MANAGER}(&|$)`));
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the Component view shows no utility: they are one muted line, "Utilities it uses"', async ({
  page,
  dispatchGuard,
}) => {
  const utilities = ['logging', 'diagnostics', 'message-bus', 'security'];
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&av=component`);
  const view = pane(page).getByTestId(TESTID.serviceContractComponentFlow);
  await expect(view.getByTestId(TESTID.serviceContractNeighbourRow('review-engine'))).toBeVisible();
  for (const utility of utilities) {
    await expect(view.getByTestId(TESTID.serviceContractNeighbourRow(utility))).toHaveCount(0);
  }
  const line = view.getByTestId(TESTID.serviceContractUtilitiesLine);
  await expect(line).toHaveText(/^Utilities it uses: /);
  await expect(line).toContainText('Logging');

  // …and the focus view's diagram draws none either.
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&av=component&focus=1`, 1600);
  const flow = page
    .getByTestId(TESTID.constructionFocusView)
    .getByTestId(TESTID.serviceContractComponentFlow);
  await expect(flow.getByTestId(TESTID.archC4Node('review-engine'))).toBeVisible();
  for (const utility of utilities) {
    await expect(flow.getByTestId(TESTID.archC4Node(utility))).toHaveCount(0);
  }
  await expect(flow.getByTestId(TESTID.serviceContractUtilitiesLine)).toHaveText(
    /^Utilities it uses: /
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
  // Its real neighbour, as a row (S4): the call it makes and the activity it opens.
  const caller = pane(page)
    .getByTestId(TESTID.serviceContractComponentFlow)
    .getByTestId(TESTID.serviceContractNeighbourRow('system-design-manager'));
  await expect(caller).toContainText('EvaluateDesignHealth(…)');
  await expect(caller).toContainText('→ C-system-design-manager');
  const gapText = await gap.innerText();

  await open(page, `a=${RESOURCE}`);
  const byDesign = pane(page).getByTestId(TESTID.constructionContractByDesign);
  await expect(byDesign).toContainText('NO CONTRACT · BY DESIGN');
  await expect(byDesign).toContainText('Nothing is missing.');
  await expect(byDesign).toHaveAttribute('data-tone', 'byDesign');
  await expect(
    pane(page)
      .getByTestId(TESTID.constructionWhoReachesIt)
      .getByTestId(TESTID.serviceContractNeighbourRow('source-control-access'))
  ).toContainText('→ C-source-control-access');
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
  // "is not recorded" — the console reads project state, not what anyone wrote (polish 7).
  await expect(empty).toContainText('is not recorded.');
  await expect(empty).not.toContainText('has not been written');
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
  // Where it goes, said at its end (polish 6).
  await expect(row).toContainText('→ N-STP');
  await row.click();
  await expect(page).toHaveURL(/[?&]a=N-STP(&|$)/);
  // B2: at THIS row's scenario, not the plan's first (STP-UC1).
  await expect(page).toHaveURL(/[?&]sc=STP-UC3(&|$)/);
  const picker = page
    .getByTestId(TESTID.constructionTestPlanView)
    .getByTestId(TESTID.constructionScenarioPicker);
  await expect(picker).toContainText('STP-UC3');
  await expect(picker).not.toContainText('STP-UC1');
  // A jump to another activity PUSHES a history entry: Back returns to this Test Plan.
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${ENGINE}(&|$)`));
  await expect(page).toHaveURL(/[?&]k=stp(&|$)/);
  await expect(
    pane(page).getByTestId(TESTID.constructionCoverageReachedRow('STP-UC3'))
  ).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('the scenario deep link opens N-STP at that scenario, and a pick writes it back', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, 'a=N-STP&sc=STP-UC4');
  const plan = page.getByTestId(TESTID.constructionTestPlanView);
  const picker = plan.getByTestId(TESTID.constructionScenarioPicker);
  await expect(picker).toContainText('STP-UC4');
  // A pick is written back to the link (a replace), so a poll or a reload keeps it.
  await picker.click();
  await page.getByRole('option', { name: /^STP-UC2 · / }).click();
  await expect(page).toHaveURL(/[?&]sc=STP-UC2(&|$)/);
  await expect(picker).toContainText('STP-UC2');
  // A scenario the plan does not hold is ignored: the first one shows, never nothing.
  await open(page, 'a=N-STP&sc=STP-UC9');
  await expect(page.getByTestId(TESTID.constructionTestPlanView).getByTestId(TESTID.constructionScenarioPicker)).toContainText('STP-UC1');
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

// ---------------------------------------------------------------------------
// Renderers S2 — the designer's check on S1 (B1–B3 and polish 1–9)
// ---------------------------------------------------------------------------

test('B1: the pane lists the Code tab as HTML signatures — ≥ 11px at every width, no canvas', async ({
  page,
  dispatchGuard,
}) => {
  for (const width of [1100, 1280, 1366, 1600]) {
    await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, width);
    const body = pane(page);
    await expect(body.getByTestId(TESTID.serviceContractSignatureList)).toBeVisible();
    await expect(body.getByTestId(TESTID.serviceContractCodeCanvas)).toHaveCount(0);
    const first = body.getByTestId(TESTID.serviceContractOpSignature).first();
    await expect(first).toContainText('ExecuteNextActivity(');
    const px = await first.evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
    expect(px, `signature font at ${String(width)}`).toBeGreaterThanOrEqual(11);
    await expect(body.getByTestId(TESTID.serviceContractOpenFocus)).toBeVisible();
  }
  expect(dispatchGuard.blocked).toEqual([]);
});

test('B1/S3: at 1280×800 and 1366×768 the first op is above the action bar; the "committed today" sentence is in the note’s disclosure', async ({
  page,
  dispatchGuard,
}) => {
  for (const [width, height] of [
    [1280, 800],
    [1366, 768],
  ] as const) {
    await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, width, height);
    const body = pane(page);
    // The note is condensed in the pane — still there, basis one click away.
    const note = body.getByTestId(TESTID.constructionDetailProvenanceNote);
    await expect(note).toHaveAttribute('data-condensed', 'true');
    // The sentence rides the basis, folded, not above the frame.
    const sentence = note.getByTestId(TESTID.constructionArtifactReconstructedNote);
    await expect(sentence).toHaveCount(1);
    await expect(sentence).toBeHidden();
    const first = await body.getByTestId(TESTID.serviceContractOpSignature).first().boundingBox();
    const bar = await page.getByTestId(TESTID.constructionDetailActionBar).first().boundingBox();
    expect(first).not.toBeNull();
    expect(bar).not.toBeNull();
    if (first === null || bar === null) return;
    expect(
      first.y + first.height,
      `the first op ends above the action bar at ${String(width)}×${String(height)}`
    ).toBeLessThanOrEqual(bar.y);
    expect(first.y + first.height).toBeLessThanOrEqual(height);
    // One click away.
    await note.getByTestId(TESTID.constructionDetailProvenanceDisclosure).click();
    await expect(sentence).toBeVisible();
  }
  expect(dispatchGuard.blocked).toEqual([]);
});

test('B1: an op row expands inline into its request, response and error tables', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, 1280);
  const body = pane(page);
  const row = body.getByTestId(TESTID.serviceContractOpRow(0));
  await expect(row).toHaveAttribute('aria-expanded', 'false');
  await row.click();
  await expect(row).toHaveAttribute('aria-expanded', 'true');
  const structs = body.getByTestId(TESTID.serviceContractOpStructs);
  await expect(structs).toHaveCount(1);
  for (const label of ['REQUEST', 'RESPONSE', 'ERROR']) await expect(structs).toContainText(label);
  await expect(structs).toContainText('PumpResult');
  await expect(structs).toContainText('fwm.Error');
  expect(dispatchGuard.blocked).toEqual([]);
});

/** The share of each expanded struct card that lands on the code canvas's frame. */
async function cardsOnCanvas(canvas: Locator): Promise<number[]> {
  return canvas
    .getByTestId(TESTID.serviceContractCodeCanvasFrame)
    .evaluate((frame, cardId) => {
      const f = frame.getBoundingClientRect();
      return Array.from(frame.querySelectorAll(`[data-testid="${cardId}"]`)).map((card) => {
        const b = card.getBoundingClientRect();
        const w = Math.max(0, Math.min(b.right, f.right) - Math.max(b.left, f.left));
        const h = Math.max(0, Math.min(b.bottom, f.bottom) - Math.max(b.top, f.top));
        return (w * h) / (b.width * b.height);
      });
    }, TESTID.serviceContractStructCard);
}

/** The code canvas's zoom, off xyflow's viewport transform. */
async function canvasScale(canvas: Locator): Promise<number> {
  // eslint-disable-next-line no-restricted-syntax -- the zoom lives on xyflow's generated viewport transform
  const transform = await canvas.locator('.react-flow__viewport').getAttribute('style');
  return Number(/scale\(([0-9.]+)\)/.exec(transform ?? '')?.[1] ?? '0');
}

test('N1: at 1280 and 1366 the focus view lists — the widest expansion does not fit — and the list expands inline', async ({
  page,
  dispatchGuard,
}) => {
  for (const width of [1280, 1366]) {
    await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&focus=1`, width);
    const focus = page.getByTestId(TESTID.constructionFocusView);
    await expect(focus.getByTestId(TESTID.serviceContractSignatureList)).toBeVisible();
    await expect(focus.getByTestId(TESTID.serviceContractCodeCanvas)).toHaveCount(0);
    // S4: the copy is actionable — the window it needs, or collapse the side panel.
    await expect(focus.getByTestId(TESTID.serviceContractCanvasNeedsRoom)).toContainText(
      'Needs a window about 1712 px wide — or collapse the side panel'
    );
    // S2 drew the canvas here and the expanded structs landed 0% / 22% on it.
    // The list shows the same structs, inline, all of them.
    await focus.getByTestId(TESTID.serviceContractOpRow(0)).click();
    const structs = focus.getByTestId(TESTID.serviceContractOpStructs);
    await expect(structs).toContainText('PumpResult');
    await expect(structs).toBeInViewport();
  }
  expect(dispatchGuard.blocked).toEqual([]);
});

test('N1: with room for the canvas, "Open diagram in focus view" draws it, and an expanded op’s struct cards land ≥ 90% on it at ≥ 0.9', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, 1760);
  await pane(page).getByTestId(TESTID.serviceContractOpenFocus).click();
  const focus = page.getByTestId(TESTID.constructionFocusView);
  let canvas = focus.getByTestId(TESTID.serviceContractCodeCanvas);
  await expect(canvas).toBeVisible();
  // eslint-disable-next-line no-restricted-syntax -- the op rows live inside xyflow's node; data-op is their only handle
  await canvas.locator('[data-op]').first().click();
  // ExecuteNextActivity: ProjectID, string → PumpResult, fwm.Error.
  await expect(canvas.getByTestId(TESTID.serviceContractStructCard)).toHaveCount(4);
  // The fit is by width (S4): below 1.0 for the widest expansion, never below 0.9.
  await expect.poll(() => canvasScale(canvas), { message: 'the fit settles below 1.0' }).toBeLessThan(1);
  expect(await canvasScale(canvas)).toBeGreaterThanOrEqual(0.899);
  await expect
    .poll(async () => Math.min(...(await cardsOnCanvas(canvas))), {
      message: 'every struct card lands ≥ 90% on the canvas',
    })
    .toBeGreaterThanOrEqual(0.9);
  // S3: the primitive and alias params are one row each; only PumpResult is a struct.
  await expect(canvas.getByTestId(TESTID.serviceContractParamRow)).toHaveCount(3);
  expect(await canvas.getByTestId(TESTID.serviceContractStructName).allInnerTexts()).toEqual([
    'PumpResult',
  ]);

  // A tall expansion (6 cards): the canvas grows to hold it rather than clip it.
  await open(page, 'a=C-autoscaler-engine&p=detailed_design&k=detailedDesign&focus=1', 1760);
  canvas = page.getByTestId(TESTID.constructionFocusView).getByTestId(TESTID.serviceContractCodeCanvas);
  // eslint-disable-next-line no-restricted-syntax -- the op rows live inside xyflow's node; data-op is their only handle
  await canvas.locator('[data-op]').first().click();
  await expect(canvas.getByTestId(TESTID.serviceContractStructCard)).toHaveCount(6);
  await expect
    .poll(async () => Math.min(...(await cardsOnCanvas(canvas))), {
      message: 'every card of the tall expansion lands ≥ 90% on the canvas',
    })
    .toBeGreaterThanOrEqual(0.9);
  expect(await canvasScale(canvas)).toBeGreaterThanOrEqual(0.899);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('B1: a focus view too narrow for the canvas lists the signatures and says why', async ({
  page,
  dispatchGuard,
}) => {
  for (const [width, copy] of [
    [1100, 'Needs a window about 1712 px wide — or collapse the side panel'],
    // Below 600px the rail stacks above the artifact: nothing to collapse here.
    [500, 'Needs a window about 1392 px wide, with the side panel collapsed'],
  ] as const) {
    await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&focus=1`, width);
    const focus = page.getByTestId(TESTID.constructionFocusView);
    await expect(focus.getByTestId(TESTID.serviceContractSignatureList)).toBeVisible();
    await expect(focus.getByTestId(TESTID.serviceContractCodeCanvas)).toHaveCount(0);
    await expect(focus.getByTestId(TESTID.serviceContractCanvasNeedsRoom)).toHaveText(copy);
    await expect(focus.getByTestId(TESTID.serviceContractOpenFocus)).toHaveCount(0);
  }
  await expect(page.getByTestId(TESTID.constructionFocusRailToggle)).toHaveCount(0);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S3: a primitive or alias param is one row in the list, never a struct with its type as a header', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, 1280);
  const body = pane(page);
  await body.getByTestId(TESTID.serviceContractOpRow(0)).click();
  const structs = body.getByTestId(TESTID.serviceContractOpStructs);
  // ExecuteNextActivity(projectID: ProjectID, tickID: string) → (PumpResult, fwm.Error)
  const params = structs.getByTestId(TESTID.serviceContractParamRow);
  await expect(params).toHaveCount(3);
  await expect(params.nth(0)).toContainText('projectID');
  await expect(params.nth(1)).toContainText('tickID');
  await expect(params.nth(1)).toContainText('string');
  await expect(params.nth(2)).toContainText('fault');
  // The only header is the one real struct's.
  expect(await structs.getByTestId(TESTID.serviceContractStructName).allInnerTexts()).toEqual([
    'PumpResult',
  ]);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S3: below 600px the focus rail condenses its note, the "committed today" sentence inside the disclosure', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=designReview&focus=1`, 500);
  const rail = page.getByTestId(TESTID.constructionFocusView).getByTestId(TESTID.constructionFocusRail);
  const note = rail.getByTestId(TESTID.constructionDetailProvenanceNote);
  await expect(note).toHaveAttribute('data-condensed', 'true');
  const sentence = note.getByTestId(TESTID.constructionArtifactReconstructedNote);
  await expect(sentence).toBeHidden();
  await note.getByTestId(TESTID.constructionDetailProvenanceDisclosure).click();
  await expect(sentence).toBeVisible();
  // The verdict still judges the artifact from the rail.
  await expect(rail.getByTestId(TESTID.constructionDetailVerdict)).toHaveCount(1);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 1: the focus rail carries the provenance note, the "nothing links it" sentence and the verdict', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=designReview&focus=1`, 1280);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  const rail = focus.getByTestId(TESTID.constructionFocusRail);
  await expect(rail.getByTestId(TESTID.constructionDetailProvenanceNote)).toBeVisible();
  await expect(rail.getByTestId(TESTID.constructionDetailProvenanceNote)).not.toHaveAttribute(
    'data-condensed',
    'true'
  );
  await expect(rail.getByTestId(TESTID.constructionArtifactReconstructedNote)).toBeVisible();
  await expect(rail.getByTestId(TESTID.constructionDetailVerdict)).toBeVisible();
  // The artifact column is the artifact alone.
  const rootBox = await focus.getByTestId(TESTID.serviceContractRoot).boundingBox();
  const railBox = await rail.boundingBox();
  expect(rootBox !== null && railBox !== null && rootBox.x > railBox.x + railBox.width - 1).toBe(true);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 3: with the focus view open the pane body is unmounted and no DOM id repeats', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=designReview&av=component&focus=1`, 1600);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  await expect(focus.getByTestId(TESTID.serviceContractComponentFlow)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionFocusPlaceholder)).toHaveText(
    'Showing in focus view.'
  );
  // One artifact, one note, one verdict on the page — not a second copy under the layer.
  for (const id of [
    TESTID.serviceContractRoot,
    TESTID.constructionDetailProvenanceNote,
    TESTID.constructionDetailVerdict,
    TESTID.constructionArtifactFrame,
  ]) {
    await expect(page.getByTestId(id)).toHaveCount(1);
  }
  const dupes = await page.evaluate(() => {
    const seen = new Map<string, number>();
    for (const el of Array.from(document.querySelectorAll('[id]'))) {
      seen.set(el.id, (seen.get(el.id) ?? 0) + 1);
    }
    return [...seen.entries()].filter(([, n]) => n > 1).map(([id]) => id);
  });
  expect(dupes).toEqual([]);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 3: two canvases on one page — the graph lens and the focus view — share no DOM id', async ({
  page,
  dispatchGuard,
}) => {
  // The graph lens is a React Flow canvas; the focus view's Component tab is
  // another (S3: the pane lists, so the focus view is where it draws). With
  // xyflow's default instance id both minted `pattern-1`, `react-flow__node-desc-1`, ….
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(
    page,
    `/project/archistrator/construction?lens=graph&a=${MANAGER}&p=detailed_design&k=detailedDesign&av=component&focus=1`
  );
  await expect(
    page
      .getByTestId(TESTID.constructionFocusView)
      .getByTestId(TESTID.serviceContractComponentFlow)
  ).toHaveAttribute('data-mode', 'canvas', { timeout: 15_000 });
  // eslint-disable-next-line no-restricted-syntax -- counting xyflow's own root class: the assertion is that two canvases exist
  await expect.poll(() => page.locator('.react-flow').count()).toBeGreaterThanOrEqual(2);
  const dupes = await page.evaluate(() => {
    const seen = new Map<string, number>();
    for (const el of Array.from(document.querySelectorAll('[id]'))) {
      seen.set(el.id, (seen.get(el.id) ?? 0) + 1);
    }
    return [...seen.entries()].filter(([, n]) => n > 1).map(([id]) => id);
  });
  expect(dupes).toEqual([]);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 4: entering the focus view lands on its heading, and no tooltip shows', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign`, 1280);
  await pane(page).getByTestId(TESTID.constructionArtifactFocus).click();
  const heading = page.getByTestId(TESTID.constructionFocusHeading);
  await expect(heading).toBeVisible();
  await expect
    .poll(() => heading.evaluate((el) => document.activeElement === el))
    .toBe(true);
  await page.waitForTimeout(800);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 5: in the narrow pane more than three cases are a dropdown', async ({
  page,
  dispatchGuard,
}) => {
  for (const width of [1100, 1600]) {
    await open(page, 'a=N-STP&sc=STP-UC3', width);
    const plan = page.getByTestId(TESTID.constructionTestPlanView);
    // STP-UC3 carries 5 cases: no chips, one picker.
    const picker = plan.getByTestId(TESTID.constructionCasePicker);
    await expect(picker).toBeVisible();
    // eslint-disable-next-line no-restricted-syntax -- a structural assertion: no case chip exists at all (the menu's items mount only when it opens)
    await expect(plan.locator('[data-testid^="construction-case-chip-"]')).toHaveCount(0);
    await expect(plan.getByTestId(TESTID.constructionActiveCase)).toBeVisible();
  }
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 7: the SPA Flows task speaks of flows, not stubbed callees', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${SPA}&p=test_plan&k=stp`, 1280);
  const empty = pane(page).getByTestId(TESTID.constructionComponentTestPlanEmpty);
  await expect(empty).toContainText('flow plan');
  await expect(empty).toContainText('is not recorded.');
  await expect(empty).not.toContainText(/callees|stubbed|has not been written/);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('polish 8: SPA Construction says NO SURFACES RECORDED, naming web-client, even Not started', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${SPA}&p=construction&k=construction`, 1280);
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText('NOT STARTED');
  const empty = pane(page).getByTestId(TESTID.constructionFrontendNoSurfaces);
  await expect(empty).toContainText('NO SURFACES RECORDED');
  await expect(empty).toContainText("web-client's UI design records no surfaces");
  expect(await iframesIn(page.getByTestId(TESTID.constructionDetailBody))).toBe(0);
  expect(dispatchGuard.blocked).toEqual([]);
});

// ---------------------------------------------------------------------------
// Renderers S4 — the designer's check on S3
// ---------------------------------------------------------------------------

/** The share of a locator's box that lies inside another's (0 when either is absent). */
async function overlap(a: Locator, b: Locator): Promise<number> {
  const [x, y] = [await a.boundingBox(), await b.boundingBox()];
  if (x === null || y === null) return 0;
  const w = Math.max(0, Math.min(x.x + x.width, y.x + y.width) - Math.max(x.x, y.x));
  const h = Math.max(0, Math.min(x.y + x.height, y.y + y.height) - Math.max(x.y, y.y));
  return (w * h) / (x.width * x.height);
}

test('S4: the gap and Resource frames list in the pane — the call, the activity, REACHED BY — at 1280 and in the 500px drawer', async ({
  page,
  dispatchGuard,
}) => {
  for (const width of [1280, 500]) {
    await open(page, `a=${GAP}&p=detailed_design`, width);
    const flow = pane(page).getByTestId(TESTID.serviceContractComponentFlow);
    await expect(flow).toHaveAttribute('data-mode', 'list');
    // eslint-disable-next-line no-restricted-syntax -- a structural assertion: no xyflow canvas in the pane at all
    await expect(pane(page).locator('.react-flow')).toHaveCount(0);
    await expect(flow).toContainText('CALLED BY · 1');
    const row = flow.getByTestId(TESTID.serviceContractNeighbourRow('system-design-manager'));
    await expect(row).toContainText('EvaluateDesignHealth(…)');
    await expect(row).toContainText('→ C-system-design-manager');
    await expect(flow.getByTestId(TESTID.serviceContractOpenFocus)).toBeVisible();

    await open(page, `a=${RESOURCE}`, width);
    const reaches = pane(page).getByTestId(TESTID.constructionWhoReachesIt);
    await expect(reaches).toHaveAttribute('data-mode', 'list');
    await expect(reaches).toContainText('who reaches this resource');
    await expect(reaches).toContainText('REACHED BY · 1');
    // A Resource calls nothing: the empty CALLS side is not drawn.
    await expect(reaches).not.toContainText('CALLS');
    await expect(reaches).not.toContainText('CALLED BY');
    await expect(
      reaches.getByTestId(TESTID.serviceContractNeighbourRow('source-control-access'))
    ).toContainText('→ C-source-control-access');
  }
  // The row is a navigation: it pushes, and Back returns.
  await open(page, `a=${RESOURCE}`, 1280);
  await pane(page)
    .getByTestId(TESTID.constructionWhoReachesIt)
    .getByTestId(TESTID.serviceContractNeighbourRow('source-control-access'))
    .click();
  await expect(page).toHaveURL(/[?&]a=C-source-control-access(&|$)/);
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`[?&]a=${RESOURCE}(&|$)`));
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: in focus the gap and by-design statement sit above a diagram drawn at ≤ 1.0, as tall as it is, the call on its edge', async ({
  page,
  dispatchGuard,
}) => {
  for (const width of [1760, 1280]) {
    await open(page, `a=${GAP}&p=detailed_design&focus=1`, width);
    const focus = page.getByTestId(TESTID.constructionFocusView);
    const statement = focus.getByTestId(TESTID.constructionContractGap);
    await expect(statement).toContainText('NO CONTRACT COMMITTED');
    const flow = focus.getByTestId(TESTID.serviceContractComponentFlow);
    await expect(flow).toHaveAttribute('data-mode', 'canvas');
    const [s, f] = [await statement.boundingBox(), await flow.boundingBox()];
    expect(s !== null && f !== null && s.y + s.height <= f.y, 'the statement is above the diagram').toBe(true);
    await expect(flow.getByText('EvaluateDesignHealth(…)')).toBeVisible();
    // Never larger than drawn, and the canvas is the drawing's height — not 640.
    await expect.poll(() => canvasScale(flow)).toBeGreaterThan(0);
    expect(await canvasScale(flow)).toBeLessThanOrEqual(1);
    // eslint-disable-next-line no-restricted-syntax -- FlowCanvas's own frame carries the fitted height; xyflow's wrapper has no testid
    const fitted = flow.locator('[data-canvas-height]');
    await expect.poll(async () => Number(await fitted.getAttribute('data-canvas-height'))).toBeGreaterThan(150);
    expect(Number(await fitted.getAttribute('data-canvas-height')), 'a two-node fact on a canvas its own size').toBeLessThan(400);
  }

  await open(page, `a=${RESOURCE}&focus=1`, 1760);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  const byDesign = focus.getByTestId(TESTID.constructionContractByDesign);
  await expect(byDesign).toContainText('NO CONTRACT · BY DESIGN');
  const reaches = focus.getByTestId(TESTID.constructionWhoReachesIt);
  // The Resource's own caption in focus too, never "who calls this component".
  await expect(reaches).toContainText('who reaches this resource');
  await expect(reaches).not.toContainText('who calls this component');
  const [b, r] = [await byDesign.boundingBox(), await reaches.boundingBox()];
  expect(b !== null && r !== null && b.y + b.height <= r.y).toBe(true);
  await expect.poll(() => canvasScale(reaches)).toBeGreaterThan(0);
  expect(await canvasScale(reaches)).toBeLessThanOrEqual(1);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: at 1512 the rail open lists and says how to get the diagram; collapsing it draws it, and the choice is remembered', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&focus=1`, 1512);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  await expect(focus).toHaveAttribute('data-rail', 'open');
  await expect(focus.getByTestId(TESTID.serviceContractCodeCanvas)).toHaveCount(0);
  await expect(focus.getByTestId(TESTID.serviceContractCanvasNeedsRoom)).toHaveText(
    'Needs a window about 1712 px wide — or collapse the side panel'
  );
  await focus.getByTestId(TESTID.serviceContractCanvasNeedsRoomCollapse).click();
  await expect(focus).toHaveAttribute('data-rail', 'collapsed');
  await expect(focus.getByTestId(TESTID.constructionFocusRail)).toBeHidden();
  const canvas = focus.getByTestId(TESTID.serviceContractCodeCanvas);
  await expect(canvas).toBeVisible();
  // eslint-disable-next-line no-restricted-syntax -- the op rows live inside xyflow's node; data-op is their only handle
  await canvas.locator('[data-op]').first().click();
  await expect(canvas.getByTestId(TESTID.serviceContractStructCard)).toHaveCount(4);
  await expect
    .poll(async () => Math.min(...(await cardsOnCanvas(canvas))), { message: 'every card on the canvas' })
    .toBeGreaterThanOrEqual(0.9);
  expect(await canvasScale(canvas)).toBeGreaterThanOrEqual(0.899);
  // Approve / Send back stay in reach: the action bar moved under the artifact.
  await expect(focus.getByTestId(TESTID.constructionDetailActionBar)).toBeVisible();

  // Per viewer, remembered: a reload keeps it collapsed.
  await page.reload();
  await expect(page.getByTestId(TESTID.constructionFocusView)).toHaveAttribute('data-rail', 'collapsed');
  await expect(page.getByTestId(TESTID.serviceContractCodeCanvas)).toBeVisible();
  // The toggle brings it back, and the list with it.
  const toggle = page.getByTestId(TESTID.constructionFocusRailToggle);
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click();
  await expect(page.getByTestId(TESTID.constructionFocusView)).toHaveAttribute('data-rail', 'open');
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByTestId(TESTID.constructionFocusRail)).toBeVisible();
  await expect(page.getByTestId(TESTID.serviceContractSignatureList)).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: the threshold, pinned either side — 1712 with the rail open, 1392 collapsed', async ({
  page,
  dispatchGuard,
}) => {
  const q = `a=${MANAGER}&p=detailed_design&k=detailedDesign&focus=1`;
  const mode = (): Promise<string | null> =>
    // eslint-disable-next-line no-restricted-syntax -- the Code tab's form is a data attribute on its measured wrapper
    page.getByTestId(TESTID.constructionFocusView).locator('[data-code-mode]').getAttribute('data-code-mode');
  for (const [width, want] of [
    [1711, 'list'],
    [1712, 'canvas'],
  ] as const) {
    await open(page, q, width);
    await expect.poll(mode, { message: `open rail at ${String(width)}` }).toBe(want);
  }
  await page.getByTestId(TESTID.constructionFocusRailToggle).click();
  for (const [width, want] of [
    [1391, 'list'],
    [1392, 'canvas'],
  ] as const) {
    await open(page, q, width);
    await expect(page.getByTestId(TESTID.constructionFocusView)).toHaveAttribute('data-rail', 'collapsed');
    await expect.poll(mode, { message: `collapsed rail at ${String(width)}` }).toBe(want);
  }
  await open(page, q, 1391);
  await expect(page.getByTestId(TESTID.serviceContractCanvasNeedsRoom)).toHaveText(
    'Needs a window about 1392 px wide'
  );
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: with the rail collapsed, a Design Review keeps its verdict in reach — a header chip that opens the rail', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=designReview&focus=1`, 1512);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  const railVerdict = focus.getByTestId(TESTID.constructionFocusRail).getByTestId(TESTID.constructionDetailVerdict);
  // Open: the verdict is in the rail, and no chip.
  await expect(railVerdict).toBeVisible();
  await expect(focus.getByTestId(TESTID.constructionFocusVerdictChip)).toHaveCount(0);
  await focus.getByTestId(TESTID.constructionFocusRailToggle).click();
  await expect(focus).toHaveAttribute('data-rail', 'collapsed');
  const chip = focus.getByTestId(TESTID.constructionFocusVerdictChip);
  await expect(chip).toBeVisible();
  await expect(chip).toHaveText(/^VERDICT · /);
  await expect(chip).not.toHaveText(/PASS|FAIL|APPROVED/);
  await expect(focus.getByTestId(TESTID.constructionDetailActionBar)).toBeVisible();
  await chip.click();
  await expect(focus).toHaveAttribute('data-rail', 'open');
  await expect(railVerdict).toBeVisible();
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: before an op is expanded the interface sits at the top of a canvas its own size, under one caption', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&focus=1`, 1760);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  const canvas = focus.getByTestId(TESTID.serviceContractCodeCanvas);
  const iface = canvas.getByTestId(TESTID.serviceContractCodeInterfaceNode);
  const frame = canvas.getByTestId(TESTID.serviceContractCodeCanvasFrame);
  await expect(iface).toBeVisible();
  const gap = async (): Promise<number> => {
    const [i, f] = [await iface.boundingBox(), await frame.boundingBox()];
    return i !== null && f !== null ? Math.round(i.y - f.y) : -1;
  };
  await expect.poll(gap, { message: 'the interface node is top-aligned' }).toBeLessThanOrEqual(20);
  expect(await gap()).toBeGreaterThanOrEqual(0);
  const [i, f] = [await iface.boundingBox(), await frame.boundingBox()];
  expect(i !== null && f !== null && f.height - i.height <= 40, 'no empty band below the node').toBe(true);
  // One "Click an op" in the whole focus view.
  await expect(focus.getByText(/Click an op/)).toHaveCount(1);
  await expect(canvas.getByTestId(TESTID.serviceContractCodeCanvasCaption)).toContainText('10 ops');
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: the autoscaler’s "returns" labels sit clear of the interface node', async ({ page, dispatchGuard }) => {
  await open(page, 'a=C-autoscaler-engine&p=detailed_design&k=detailedDesign&focus=1', 1760);
  const canvas = page.getByTestId(TESTID.constructionFocusView).getByTestId(TESTID.serviceContractCodeCanvas);
  // eslint-disable-next-line no-restricted-syntax -- the op rows live inside xyflow's node; data-op is their only handle
  await canvas.locator('[data-op]').first().click();
  await expect(canvas.getByTestId(TESTID.serviceContractStructCard)).toHaveCount(6);
  const iface = canvas.getByTestId(TESTID.serviceContractCodeInterfaceNode);
  // eslint-disable-next-line no-restricted-syntax -- xyflow draws edge labels as SVG text with no testid
  const labels = canvas.locator('.react-flow__edge-text', { hasText: /^returns/ });
  await expect(labels).toHaveCount(2);
  await expect.poll(() => overlap(labels.nth(0), iface), { message: '"returns" clear of the interface' }).toBe(0);
  expect(await overlap(labels.nth(1), iface), '"returns error" clear of the interface').toBe(0);
  expect(await overlap(labels.nth(0), labels.nth(1)), 'the two labels apart').toBe(0);
  expect(dispatchGuard.blocked).toEqual([]);
});

test('S4: a struct’s field table keeps the type beside the name, and draws no empty note column', async ({
  page,
  dispatchGuard,
}) => {
  await open(page, `a=${MANAGER}&p=detailed_design&k=detailedDesign&focus=1`, 1366);
  const focus = page.getByTestId(TESTID.constructionFocusView);
  await focus.getByTestId(TESTID.serviceContractOpRow(0)).click();
  // PumpResult: dispatched bool, ActivityID ActivityID — no field has a note.
  const rows = focus.getByTestId(TESTID.serviceContractOpStructs).getByTestId(TESTID.serviceContractFieldRow);
  await expect(rows).toHaveCount(2);
  const cells = await rows.evaluateAll((trs) =>
    trs.map((tr) => {
      const tds = Array.from(tr.children).map((td) => td.getBoundingClientRect());
      return { n: tds.length, nameW: tds[0]?.width ?? 0, rowW: tr.getBoundingClientRect().width };
    })
  );
  for (const c of cells) {
    expect(c.n, 'name and type only').toBe(2);
    expect(c.nameW / c.rowW, 'the name column is as narrow as its names').toBeLessThan(0.3);
  }
  expect(dispatchGuard.blocked).toEqual([]);
});
