/**
 * construction-tasks-lens.spec — Stage C, the TASKS lens driven in a real browser.
 *
 *  - Live: the committed corpus owes nothing, so the lens says "Nothing needs you."
 *    with eligible · in flight · blocked, carries the degraded-policy banner, shows
 *    no TASKS badge, and asks for no session at all (no activity is in flight).
 *  - Owed rows: the REAL project read with four rows edited in the browser (the
 *    fix-D pattern) and the per-activity session route fulfilled for them — a gate,
 *    a variance awaiting a steer, a terminal failure, and an in-review row whose
 *    session is merely running, which must NOT appear (spec §1: head-state
 *    in-review is not a gate).
 *  - Deciding: [Review] opens the shared pane on the gate task, which reads AWAITING
 *    YOU; Send back needs a note and carries it; the row says "Resumed" only once
 *    the session leaves the gate, says "did not land" when it does not, and tells a
 *    4xx rejection from a 5xx unknown outcome.
 *
 * SAFETY: every write route — execute-next-activity, submit-phase-decision,
 * override-activity, pause-project, update/set-review-policy — is TRAPPED before the
 * page opens: aborted, or answered here. Nothing reaches the server but GET reads.
 */
import { test, expect, type Page, type Route } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const GATE = 'C-billing-state-access';
const TAKEOVER = 'C-design-health-engine';
const FAILED = 'R-merchant-gateway';
const RUNNING = 'C-merchant-gateway-access';
const GATE_KEY = `${GATE}:gate`;

// Wire ordinals (enums.gen.ts): ConstructionStage and ActivityBuildStatus.
const STAGE = {
  pipelineRunning: 2,
  awaitingTakeover: 4,
  awaitingApproval: 7,
} as const;
const BUILD = { inConstruction: 0, inReview: 1, failed: 3 } as const;

interface WireRow {
  [k: string]: unknown;
}
interface Wire {
  ActivityConstruction?: Record<string, WireRow>;
}

/** Trap every write before the page can issue one. */
async function trapWrites(page: Page): Promise<void> {
  for (const pat of [
    '**/execute-next-activity/**',
    '**/override-activity/**',
    '**/pause-project/**',
    '**/update-review-policy/**',
    '**/set-review-policy/**',
    '**/submit-phase-decision/**',
  ]) {
    await page.route(pat, (r) => r.abort());
  }
}

/** Mutable session stages per activity, served for the session route. */
type Stages = Record<string, number | undefined>;

async function serveOwed(page: Page, stages: Stages): Promise<void> {
  const started = '2026-09-12T20:00:00Z';
  const edits: Record<string, WireRow> = {
    [GATE]: { BuildStatus: BUILD.inReview, CurrentPhase: 'detailed_design' },
    [TAKEOVER]: {
      BuildStatus: BUILD.inConstruction,
      CurrentPhase: 'construction',
    },
    [FAILED]: {
      BuildStatus: BUILD.failed,
      FailureReason: 3,
      FailureDetail: 'the provisioning pipeline ran past its budget',
    },
    [RUNNING]: { BuildStatus: BUILD.inReview, CurrentPhase: 'construction' },
  };
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as Wire;
    for (const [id, e] of Object.entries(edits)) {
      const row = wire.ActivityConstruction?.[id];
      if (row === undefined) throw new Error(`no row ${id} in the read`);
      Object.assign(
        row,
        {
          recorded: true,
          hasBuildEvidence: true,
          classified: true,
          startedAt: started,
        },
        e
      );
    }
    await route.fulfill({ response, json: wire });
  });
  await page.route('**/get-session-state/archistrator/**', async (route: Route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '');
    const stage = stages[id];
    if (stage === undefined) {
      await route.fulfill({
        status: 404,
        json: { error: 'no construction session' },
      });
      return;
    }
    await route.fulfill({
      json: {
        projectId: 'archistrator',
        activityId: id,
        stage,
        ...(id === GATE
          ? {
              reviewSet: {
                reviewers: [
                  {
                    role: 'system-architect',
                    perspective: 'architecture',
                    mayAmend: true,
                  },
                  {
                    role: 'product-manager',
                    perspective: 'product',
                    mayAmend: false,
                  },
                ],
              },
            }
          : {}),
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

function initialStages(): Stages {
  return {
    [GATE]: STAGE.awaitingApproval,
    [TAKEOVER]: STAGE.awaitingTakeover,
    [RUNNING]: STAGE.pipelineRunning,
  };
}

async function openTasks(page: Page, width = 1600): Promise<void> {
  await page.setViewportSize({ width, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=tasks');
  await expect(page.getByTestId(TESTID.constructionTasksLens)).toBeVisible({
    timeout: 15_000,
  });
}

/** Answer submit-phase-decision here (registered last, so it wins over the abort). */
async function answerDecisions(
  page: Page,
  answer: (body: Record<string, unknown>) => { status: number; json?: unknown },
  sent: Record<string, unknown>[]
): Promise<void> {
  await page.route('**/submit-phase-decision/**', async (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    sent.push(body);
    const a = answer(body);
    await route.fulfill({ status: a.status, json: a.json ?? {} });
  });
}

test.beforeEach(async ({ page, request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
  await trapWrites(page);
});

test('live: nothing is owed, and the lens says so without probing a session', async ({ page }) => {
  const sessionGets: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('get-session-state')) sessionGets.push(r.url());
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).toContainText('Nothing needs you.');
  await expect(page.getByTestId(TESTID.constructionTasksEmptyCounts)).toHaveText(
    /^\d+ eligible · \d+ in flight · \d+ blocked$/
  );
  // The corpus records no review policy: the banner says what the server does.
  await expect(page.getByTestId(TESTID.constructionTasksPolicyBanner)).toContainText(
    'No review policy recorded'
  );
  await expect(page.getByTestId(TESTID.constructionTasksPolicyBanner)).toContainText('risk floor');
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveCount(0);
  expect(sessionGets).toEqual([]);
});

test('owed rows come from the live stage, risk floor first; a running in-review row is not owed', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('3');
  // eslint-disable-next-line no-restricted-syntax -- counting EVERY owed row regardless of key is a structural assertion no single testid can express; each row is then named by its own TESTID below.
  const rows = page.locator('[data-testid^="construction-tasks-row-"]');
  await expect(rows).toHaveCount(3);
  const order = await rows.evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')));
  expect(order[0]).toBe(TESTID.constructionTasksRow(GATE_KEY));
  expect(order).not.toContain(TESTID.constructionTasksRow(`${RUNNING}:gate`));
  // With no policy recorded, an open gate can only be the risk floor.
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'why'))).toContainText(
    'Risk floor'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'who'))).toContainText(
    'system-architect'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'blast'))).toContainText(
    '↓'
  );
  // No gate-open time reaches the client: WAITING is said to be unknown, not guessed.
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'waiting'))).toContainText(
    '—'
  );
  await expect(page.getByTestId(TESTID.constructionTasksRow(`${TAKEOVER}:takeover`))).toContainText(
    'three builds failed the contract tests'
  );
  await expect(page.getByTestId(TESTID.constructionTasksRow(`${FAILED}:failed`))).toContainText(
    'ran past its budget'
  );
  // No PR is recorded, so there is no GitHub link — never a dead one.
  await expect(page.getByTestId(TESTID.constructionTasksGitHub(GATE_KEY))).toHaveCount(0);
});

test('the list no longer mounts a phase-gate panel; the decision lives in the pane', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({
    timeout: 15_000,
  });
  await expect(page.getByTestId(TESTID.constructionPhaseGatePanel)).toHaveCount(0);
});

test('[Review] opens the pane on the gate task, awaiting you; send back needs a note and carries it', async ({
  page,
}) => {
  const stages = initialStages();
  await serveOwed(page, stages);
  const sent: Record<string, unknown>[] = [];
  await answerDecisions(
    page,
    () => {
      stages[GATE] = STAGE.pipelineRunning;
      return { status: 200 };
    },
    sent
  );
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await expect(page).toHaveURL(/k=designReview/);
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/awaiting you/i);
  const sendBack = page.getByTestId(TESTID.constructionDetailAction('sendBack'));
  await expect(sendBack).toBeEnabled();
  await sendBack.click();
  const send = page.getByTestId(TESTID.constructionDetailDecisionSendBack);
  await expect(send).toBeDisabled();
  await page.getByTestId(TESTID.constructionDetailDecisionNote).fill('   ');
  await expect(send).toBeDisabled();
  await page.getByTestId(TESTID.constructionDetailDecisionNote).fill('The ops list drops refund.');
  await expect(send).toBeEnabled();
  await send.click();
  await expect.poll(() => sent.length).toBe(1);
  expect(sent[0]?.['phase']).toBe('detailed_design');
  expect(JSON.stringify(sent[0]?.['feedback'])).toContain('The ops list drops refund.');
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText(
    'Sent back',
    {
      timeout: 10_000,
    }
  );
});

test('approve is confirmed by the resume, not the click', async ({ page }) => {
  const stages = initialStages();
  await serveOwed(page, stages);
  const sent: Record<string, unknown>[] = [];
  await answerDecisions(
    page,
    () => {
      stages[GATE] = STAGE.pipelineRunning;
      return { status: 200 };
    },
    sent
  );
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  // One click, one signal — even three clicks in one task.
  await approve.evaluate((el) => {
    for (let i = 0; i < 3; i += 1) (el as HTMLButtonElement).click();
  });
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText('Resumed', {
    timeout: 10_000,
  });
  expect(sent).toHaveLength(1);
  // Resumed is no longer owed: the row says so in place, the header stops counting
  // it, and the pane keeps the evidence line with no decision to make.
  await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toHaveAttribute(
    'data-lingering',
    'true'
  );
  await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toContainText('Resumed');
  await expect(page.getByTestId(TESTID.constructionTasksHeadline)).toContainText('2 decisions');
  await expect(page.getByTestId(TESTID.constructionDetailDecisionFlow)).toContainText('Resumed');
  // Nothing is owed on this selection any more, so the decision actions are gone
  // (Stage B: they appear only where a decision is owed); Run stays.
  await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionDetailAction('run'))).toBeEnabled();
  // The badge drops the moment the gate clears; the row lingers with its evidence.
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('2');
});

test('an approval the gate never takes reads "did not land", loudly', async ({ page }) => {
  test.setTimeout(45_000);
  const stages = initialStages();
  await serveOwed(page, stages);
  await answerDecisions(page, () => ({ status: 200 }), []); // accepted, but the gate stays put
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
  await expect(flow).toContainText('waiting for the agent to resume');
  await expect(flow).toContainText('did not land', { timeout: 20_000 });
  await expect(page.getByTestId(TESTID.constructionDetailDecisionFlow)).toHaveAttribute(
    'data-tone',
    'danger'
  );
});

test('a 4xx is a rejection; a 5xx is an unknown outcome', async ({ page }) => {
  const stages = initialStages();
  await serveOwed(page, stages);
  let status = 400;
  await answerDecisions(page, () => ({ status, json: { error: 'no gate for that phase' } }), []);
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
  await expect(flow).toContainText('Rejected');
  status = 500;
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  await expect(flow).toContainText('Outcome unknown');
});
