/**
 * construction-graph-owed.spec — the GRAPH lens reads "a human is owed" from the
 * owed set, exactly as the list does (integration merge of graph-lens into
 * tasks-lens).
 *
 * The tasks side moved that fact onto the owed set (review I4, tasks/owedChip.ts):
 * head-state `in-review` is "some phases complete, not all", true of every
 * activity mid-lifecycle, gated or not. The graph lens still read head-state when
 * the two branches met. The console now hands the lens its owed marks, and every
 * lane's state (`data-state`) and chip come from the list row's own rule. These
 * cases pin that wiring end to end: the node tests pin the rules, not the wire.
 *
 * The owed set is faked in the browser, the way construction-tasks-lens.spec
 * fakes it: the project read's rows are edited, and the session route answers a
 * live gate, a takeover and a running pipeline. DISPATCH SAFETY: the default
 * guarded `test` (support/dispatchGuard) aborts every non-GET before navigation.
 * Nothing reaches the server but GET reads.
 */
import { test, expect } from './support/dispatchGuard.js';
import type { Locator, Page, Route } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// The same dogfood ids construction-tasks-lens.spec uses.
const GATE = 'C-billing-state-access';
const TAKEOVER = 'C-design-health-engine';
const FAILED = 'R-merchant-gateway';
/** In review by head-state, with a running pipeline and nothing owed. */
const IN_REVIEW_NOT_OWED = 'C-merchant-gateway-access';

// Wire ordinals (enums.gen.ts): ConstructionStage and ActivityBuildStatus.
const STAGE = { pipelineRunning: 2, awaitingTakeover: 4, awaitingApproval: 7 } as const;
const BUILD = { inConstruction: 0, inReview: 1, failed: 3 } as const;

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Wire {
  ActivityConstruction?: Record<string, Record<string, unknown>>;
}

interface ServeOptions {
  /** More rows to edit, merged over the defaults. */
  rows?: Record<string, Record<string, unknown>>;
  /** More session stages, merged over the defaults. */
  stages?: Record<string, number>;
  /** Further edits to the same read (one handler, so none is bypassed). */
  alsoEdit?: (wire: Wire) => void;
}

async function serveOwed(page: Page, opts: ServeOptions = {}): Promise<void> {
  const rows: Record<string, Record<string, unknown>> = {
    [GATE]: { BuildStatus: BUILD.inReview, CurrentPhase: 'detailed_design' },
    [TAKEOVER]: { BuildStatus: BUILD.inConstruction, CurrentPhase: 'construction' },
    [FAILED]: {
      BuildStatus: BUILD.failed,
      FailureReason: 3,
      FailureDetail: 'the provisioning pipeline ran past its budget',
    },
    [IN_REVIEW_NOT_OWED]: { BuildStatus: BUILD.inReview, CurrentPhase: 'construction' },
    ...opts.rows,
  };
  const stages: Record<string, number> = {
    [GATE]: STAGE.awaitingApproval,
    [TAKEOVER]: STAGE.awaitingTakeover,
    [IN_REVIEW_NOT_OWED]: STAGE.pipelineRunning,
    ...opts.stages,
  };
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as Wire;
    for (const [id, edit] of Object.entries(rows)) {
      const row = wire.ActivityConstruction?.[id];
      if (row === undefined) throw new Error(`no row ${id} in the read`);
      Object.assign(
        row,
        {
          recorded: true,
          hasBuildEvidence: true,
          classified: true,
          startedAt: '2026-09-12T20:00:00Z',
        },
        edit
      );
    }
    opts.alsoEdit?.(wire);
    await route.fulfill({ response, json: wire });
  });
  await page.route('**/get-session-state/archistrator/**', async (route: Route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '');
    const stage = stages[id];
    if (stage === undefined) {
      await route.fulfill({ status: 404, json: { error: 'no construction session' } });
      return;
    }
    await route.fulfill({
      json: {
        projectId: 'archistrator',
        activityId: id,
        stage,
        ...(stage === STAGE.awaitingTakeover
          ? {
              variance: {
                projectId: 'archistrator',
                activityId: id,
                summary: 'three builds failed the contract tests',
              },
            }
          : {}),
      },
    });
  });
}

async function openGraph(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=graph');
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
}

function lane(page: Page, id: string) {
  return page.getByTestId(TESTID.constructionGraphLane(id));
}

test('an owed lane reads the owed set: a live gate is awaiting you, a takeover a steer, a failure failed', async ({
  page,
}) => {
  await serveOwed(page);
  await openGraph(page);

  // The list's own words, from the one owed vocabulary (tasks/owedChip.ts). The
  // chip is the lane's only text in those words (its CSS uppercases them).
  await expect(lane(page, GATE)).toHaveAttribute('data-state', 'awaitingHuman');
  await expect(lane(page, GATE).getByText(/^awaiting you$/i)).toBeVisible();

  await expect(lane(page, TAKEOVER)).toHaveAttribute('data-state', 'awaitingHuman');
  await expect(lane(page, TAKEOVER).getByText(/^steer needed$/i)).toBeVisible();

  await expect(lane(page, FAILED)).toHaveAttribute('data-state', 'failed');
  await expect(lane(page, FAILED).getByText(/^failed$/i)).toBeVisible();
});

test('head-state in-review with nothing owed is never awaiting you on a lane', async ({ page }) => {
  await serveOwed(page);
  await openGraph(page);
  // Wait until the owed set has landed (the gate lane flips), so the negative
  // below is read against the settled owed set, not before it arrived.
  await expect(lane(page, GATE)).toHaveAttribute('data-state', 'awaitingHuman');
  await expect(lane(page, IN_REVIEW_NOT_OWED)).not.toHaveAttribute('data-state', 'awaitingHuman');
  await expect(
    lane(page, IN_REVIEW_NOT_OWED).getByText(/^(awaiting you|steer needed)$/i)
  ).toHaveCount(0);
});

// ---------------------------------------------------------------------------
// Mutation follow-ups (integration merge): the pins above hold the lane state and
// chip. These hold the rest of the merge's graph wiring: the list-only guard on
// Expand and Sort, the spine segment and the hover card read the owed set too,
// and the list's Expand keeps reading it.
// ---------------------------------------------------------------------------

const GRAPH_LIST_ONLY_REASON =
  "The graph's positions are the architecture's own and every lane already shows its whole lifecycle — sort and expand apply to the list lens.";
/** Entirely backfilled in the dogfood record (construction-tasks-lens.spec MP7). */
const RECONSTRUCTED = 'C-billing-engine';

/** The centre of `locator`'s box. A disabled control takes no pointer events, so
 *  the pointer lands on the tooltip wrapper beneath it. */
async function hoverCentre(page: Page, locator: Locator): Promise<void> {
  const box = await locator.boundingBox();
  if (box === null) throw new Error('nothing to hover');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
}

// On the shared data nothing is in flight, so the list's Expand is off anyway and
// could not tell the graph's guard from its own. With work in flight, it is on in
// the list: in the graph, only the graph's guard keeps it off.
test("with work in flight, the graph keeps Expand and Sort off, and says why in the graph's words", async ({
  page,
}) => {
  await serveOwed(page);
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const expand = page.getByTestId(TESTID.constructionLensExpandToPhase);
  await expect(expand).toBeEnabled({ timeout: 15_000 });

  await page.getByTestId(TESTID.constructionLensButton('graph')).click();
  await expect(page.getByTestId(TESTID.constructionGraphCanvas)).toBeVisible();
  await expect(expand).toBeDisabled();
  const reason = page.getByRole('tooltip').filter({ hasText: GRAPH_LIST_ONLY_REASON });
  await hoverCentre(page, expand);
  await expect(reason).toBeVisible();
  await page.mouse.move(2, 2);
  await expect(reason).toHaveCount(0);

  const sort = page.getByTestId(TESTID.constructionLensSort).getByRole('combobox');
  await expect(sort).toHaveAttribute('aria-disabled', 'true');
  await hoverCentre(page, sort);
  await expect(reason).toBeVisible();
});

test('an owed gate reads awaiting you on its lifecycle segment too, not only on the lane chip', async ({
  page,
}) => {
  await serveOwed(page, {
    alsoEdit: (wire) => {
      const r = wire.ActivityConstruction?.[GATE];
      if (r === undefined) throw new Error(`no row ${GATE} in the read`);
      // An open (outcome '') observed attempt on the gate task, as MP8 serves it:
      // the tick is running, and only the owed gate makes it awaiting you.
      r['attempts'] = [
        {
          attemptId: `${GATE}:designReview:1`,
          task: 'designReview',
          phase: 'detailed_design',
          attempt: 1,
          actor: 'agent',
          outcome: '',
          evidence: { kind: '', ref: '' },
          provenance: { origin: 'observed' },
        },
      ];
    },
  });
  await openGraph(page);
  await expect(lane(page, GATE)).toHaveAttribute('data-state', 'awaitingHuman');
  await expect(
    page.getByTestId(TESTID.constructionGraphSegment(GATE, 'detailed_design'))
  ).toHaveAttribute('data-state', 'awaitingHuman');
});

test("a reconstructed owed lane's hover-card line carries the owed chip, in the owed words", async ({
  page,
}) => {
  await serveOwed(page, {
    rows: { [RECONSTRUCTED]: { BuildStatus: BUILD.inConstruction, CurrentPhase: 'construction' } },
    stages: { [RECONSTRUCTED]: STAGE.awaitingTakeover },
    alsoEdit: (wire) => {
      // A backfilled row carries no finish stamp the pump never wrote; with the
      // start stamp serveOwed adds, it is a probe candidate and its session is asked.
      const r = wire.ActivityConstruction?.[RECONSTRUCTED];
      if (r === undefined) throw new Error(`no row ${RECONSTRUCTED} in the read`);
      delete r['completedAt'];
    },
  });
  await openGraph(page);
  await expect(lane(page, RECONSTRUCTED)).toHaveAttribute(
    'data-provenance',
    /^(backfilled|synthesized)$/
  );
  await expect(lane(page, RECONSTRUCTED)).toHaveAttribute('data-state', 'awaitingHuman');
  await lane(page, RECONSTRUCTED).hover();
  const line = page
    .getByTestId(TESTID.constructionGraphHoverCard)
    .getByTestId(TESTID.constructionGraphHoverLane(RECONSTRUCTED));
  await expect(line).toBeVisible();
  await expect(line.getByText(/^steer needed$/i)).toBeVisible();
});

/** One fresh pickup: dispatched a minute ago, no evidence yet, so head-state says
 *  not started. `atGate`: its session answers at a gate, else no session. */
async function serveFreshPickup(page: Page, atGate: boolean): Promise<void> {
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as Wire;
    const r = wire.ActivityConstruction?.[GATE];
    if (r === undefined) throw new Error(`no row ${GATE} in the read`);
    Object.assign(r, {
      recorded: true,
      classified: true,
      hasBuildEvidence: false,
      startedAt: new Date(Date.now() - 60_000).toISOString(),
      CurrentPhase: 'detailed_design',
      attempts: [],
    });
    delete r['completedAt'];
    delete r['FailureReason'];
    await route.fulfill({ response, json: wire });
  });
  await page.route('**/get-session-state/archistrator/**', async (route: Route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '');
    if (atGate && id === GATE) {
      await route.fulfill({
        json: { projectId: 'archistrator', activityId: id, stage: STAGE.awaitingApproval },
      });
      return;
    }
    await route.fulfill({ status: 404, json: { error: 'no construction session' } });
  });
}

// The list's Expand reads the owed set (the tasks side's, kept at the merge): a
// fresh pickup owed at a gate is in flight through its owed mark alone.
test("a fresh pickup owed at a gate is in flight through its owed mark alone: the list's Expand opens it", async ({
  page,
}) => {
  await serveFreshPickup(page, true);
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionLensExpandToPhase)).toBeEnabled({
    timeout: 15_000,
  });
});

// The control for the case above: on this fixture, the owed gate is the only
// thing that turns Expand on.
test("the same fresh pickup with no session is not in flight: the list's Expand stays off", async ({
  page,
}) => {
  await serveFreshPickup(page, false);
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const expand = page.getByTestId(TESTID.constructionLensExpandToPhase);
  await expect(expand).toBeVisible({ timeout: 15_000 });
  for (let i = 0; i < 12; i++) {
    expect(await expand.isDisabled(), 'Expand on with nothing in flight').toBe(true);
    await page.waitForTimeout(300);
  }
});
