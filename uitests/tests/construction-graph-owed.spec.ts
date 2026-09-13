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
import type { Page, Route } from '@playwright/test';
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

async function serveOwed(page: Page): Promise<void> {
  const rows: Record<string, Record<string, unknown>> = {
    [GATE]: { BuildStatus: BUILD.inReview, CurrentPhase: 'detailed_design' },
    [TAKEOVER]: { BuildStatus: BUILD.inConstruction, CurrentPhase: 'construction' },
    [FAILED]: {
      BuildStatus: BUILD.failed,
      FailureReason: 3,
      FailureDetail: 'the provisioning pipeline ran past its budget',
    },
    [IN_REVIEW_NOT_OWED]: { BuildStatus: BUILD.inReview, CurrentPhase: 'construction' },
  };
  const stages: Record<string, number> = {
    [GATE]: STAGE.awaitingApproval,
    [TAKEOVER]: STAGE.awaitingTakeover,
    [IN_REVIEW_NOT_OWED]: STAGE.pipelineRunning,
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
        ...(id === TAKEOVER
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
