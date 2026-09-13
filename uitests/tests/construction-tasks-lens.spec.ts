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
  reviewPolicy?: unknown;
}

/** Trap every write before the page can issue one: EVERY non-GET is aborted (the
 *  catch-all, registered first so a test's own answering route still wins), and
 *  each known write route is named on top of it. */
async function trapWrites(page: Page): Promise<void> {
  await page.route('**', (r) => {
    const m = r.request().method();
    return m === 'GET' || m === 'HEAD' ? r.fallback() : r.abort();
  });
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

async function serveOwed(
  page: Page,
  stages: Stages,
  only?: readonly string[],
  /** Further edits to the same read (one handler, so none is bypassed). */
  alsoEdit?: (wire: Wire) => void
): Promise<void> {
  const started = '2026-09-12T20:00:00Z';
  const all: Record<string, WireRow> = {
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
  const edits = Object.fromEntries(
    Object.entries(all).filter(([id]) => only === undefined || only.includes(id))
  );
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
    alsoEdit?.(wire);
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

// The console re-reads the project every 10s (review I3), so a route handler can be
// mid-fetch when a test ends; that is teardown, not a failure.
test.afterEach(async ({ page }) => {
  await page.unrouteAll({ behavior: 'ignoreErrors' });
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
  // The corpus records no review policy: the designer's one-liner (§7.7 amended),
  // with the way to set one, and no summary line repeating it.
  const banner = page.getByTestId(TESTID.constructionTasksPolicyBanner);
  await expect(banner).toContainText('No review policy recorded.');
  await expect(banner).toContainText(
    'Only the risk floor is gated — changes touching deploy, spend or schema always ask you; everything else proceeds without asking.'
  );
  await expect(page.getByTestId(TESTID.constructionTasksPolicyLink)).toHaveText(
    'Set a review policy →'
  );
  await expect(page.getByTestId(TESTID.constructionTasksPolicySummary)).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveCount(0);
  expect(sessionGets).toEqual([]);
});

test('a probe that fails is not an all-clear: no "Nothing needs you", and Retry asks again', async ({
  page,
}) => {
  // Only the RUNNING row is started, and its session route answers 500 every time.
  await serveOwed(page, {}, [RUNNING]);
  const sessionGets: string[] = [];
  await page.route('**/get-session-state/archistrator/**', async (route) => {
    sessionGets.push(route.request().url());
    await route.fulfill({ status: 500, json: { error: 'session store unavailable' } });
  });
  await openTasks(page);
  const unchecked = page.getByTestId(TESTID.constructionTasksUnchecked);
  await expect(unchecked).toContainText("Couldn't check 1 in-flight activity", {
    timeout: 15_000,
  });
  await expect(page.getByText('Nothing needs you.')).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).not.toContainText('Nothing needs');
  const before = sessionGets.length;
  await page.getByTestId(TESTID.constructionTasksUncheckedRetry).click();
  await expect.poll(() => sessionGets.length).toBeGreaterThan(before);
  await expect(page.getByText('Nothing needs you.')).toHaveCount(0);
});

test('a probe still in flight reads "Checking…", never the all-clear', async ({ page }) => {
  await serveOwed(page, {}, [RUNNING]);
  let release: () => void = () => undefined;
  const held = new Promise<void>((r) => {
    release = r;
  });
  await page.route('**/get-session-state/archistrator/**', async (route) => {
    await held;
    await route.fulfill({ status: 404, json: { error: 'no construction session' } }).catch(() => {
      // the page may already be closed when the test releases the hold
    });
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksUnchecked)).toContainText(
    'Checking 1 in-flight activity…'
  );
  await expect(page.getByText('Nothing needs you.')).toHaveCount(0);
  // Once the probe answers (a dormant-pump 404 — an established absence), it is clear.
  release();
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).toContainText(
    'Nothing needs you.',
    { timeout: 10_000 }
  );
});

test('a gate on an activity started after the page loaded appears without a reload (review I3)', async ({
  page,
}) => {
  test.setTimeout(45_000);
  // The first reads show nothing started; then the sweep (or another tab) starts
  // the GATE activity, and its session opens a gate. No Begin, no cascade poll.
  let startedYet = false;
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    const response = await route.fetch();
    const wire = (await response.json()) as Wire;
    const row = wire.ActivityConstruction?.[GATE];
    if (row === undefined) throw new Error(`no row ${GATE} in the read`);
    if (startedYet) {
      Object.assign(row, {
        recorded: true,
        hasBuildEvidence: true,
        classified: true,
        startedAt: '2026-09-12T20:00:00Z',
        BuildStatus: BUILD.inConstruction,
        CurrentPhase: 'detailed_design',
      });
    }
    await route.fulfill({ response, json: wire });
  });
  await page.route('**/get-session-state/archistrator/**', async (route) => {
    await route.fulfill({
      json: { projectId: 'archistrator', activityId: GATE, stage: STAGE.awaitingApproval },
    });
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).toContainText('Nothing needs you.');
  startedYet = true;
  await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('1');
});

test('a probe that met the dormant 404 asks again, and finds the gate that opened (review I3)', async ({
  page,
}) => {
  test.setTimeout(45_000);
  await serveOwed(page, {}, [GATE]);
  let opened = false;
  await page.route('**/get-session-state/archistrator/**', async (route) => {
    if (!opened) {
      await route.fulfill({ status: 404, json: { error: 'no construction session' } });
      return;
    }
    await route.fulfill({
      json: { projectId: 'archistrator', activityId: GATE, stage: STAGE.awaitingApproval },
    });
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).toContainText('Nothing needs you.');
  opened = true;
  await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toBeVisible({
    timeout: 20_000,
  });
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
  // With no policy recorded, an open gate can only be the risk floor — which no
  // policy edit can turn off, so the row never offers "stop asking" (designer P1-5).
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'why'))).toContainText(
    'Risk floor'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'cant-turn-off'))).toHaveText(
    'Can’t be turned off'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'stop-asking'))).toHaveCount(0);
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

test('steer-needed and failed rows stay review-only: the pane says why and offers nothing yet', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  await openTasks(page);
  const takeoverRow = page.getByTestId(TESTID.constructionTasksRow(`${TAKEOVER}:takeover`));
  const failedRow = page.getByTestId(TESTID.constructionTasksRow(`${FAILED}:failed`));
  // The PM's words for why each stopped; one word for failure, "Failed".
  await expect(takeoverRow).toContainText(
    'Stopped and asking you how to proceed — three builds failed the contract tests'
  );
  await expect(takeoverRow).toContainText('Steer needed');
  await expect(failedRow).toContainText('run timed out — the provisioning pipeline ran past its');
  await expect(failedRow).toContainText('Failed');
  await expect(failedRow).not.toContainText('Stopped');
  // Until follow-up B1 delivers the operator's note: no Retry, Re-queue or Skip,
  // and never the cut Takeover / Reassign.
  for (const name of [/^Retry/, /Re-queue/, /^Skip/, /Takeover/, /Reassign/]) {
    await expect(page.getByRole('button', { name })).toHaveCount(0);
  }

  await page.getByTestId(TESTID.constructionTasksReview(`${TAKEOVER}:takeover`)).click();
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/steer needed/i);
  await expect(page.getByTestId(TESTID.constructionDetailOwedReason)).toContainText(
    'three builds failed the contract tests'
  );
  await expect(page.getByTestId(TESTID.constructionDetailReviewOnlyNote)).toHaveText(
    'Retry and re-queue arrive once your note reaches the agent. Until then, steer from GitHub or the MCP override_activity tool.'
  );
  await expect(page.getByTestId(TESTID.constructionDetailAction('run'))).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toHaveCount(0);

  await page.getByTestId(TESTID.constructionTasksReview(`${FAILED}:failed`)).click();
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/^failed$/i);
  await expect(page.getByTestId(TESTID.constructionDetailOwedReason)).toContainText(
    'the provisioning pipeline ran past its budget'
  );
  await expect(page.getByTestId(TESTID.constructionDetailReviewOnlyNote)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionDetailAction('run'))).toHaveCount(0);
  for (const name of [/^Retry/, /Re-queue/, /^Skip/, /Takeover/, /Reassign/]) {
    await expect(page.getByRole('button', { name })).toHaveCount(0);
  }
});

test('the list lens reads the same owed set: "Awaiting me" is the gate, the steer and the failure', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({ timeout: 15_000 });
  // One vocabulary: the steer's row chip says "Steer needed", not "Awaiting you".
  await expect(page.getByTestId(TESTID.constructionListRow(TAKEOVER))).toContainText(
    /steer needed/i
  );
  await page.getByTestId(TESTID.constructionLensScope).getByRole('combobox').click();
  await page.getByRole('option', { name: 'Awaiting me' }).click();
  for (const id of [GATE, TAKEOVER, FAILED]) {
    await expect(page.getByTestId(TESTID.constructionListRow(id))).toBeVisible();
  }
  // Head-state in-review with a merely running session is not awaiting anyone.
  await expect(page.getByTestId(TESTID.constructionListRow(RUNNING))).toHaveCount(0);
});

test('the headline says when the toolbar hides owed decisions (review I4)', async ({ page }) => {
  await serveOwed(page, initialStages());
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksFiltered)).toHaveCount(0);
  await page.getByTestId(TESTID.constructionLensSearch).getByRole('textbox').fill('billing-state');
  await expect(page.getByTestId(TESTID.constructionTasksFiltered)).toHaveText('1 shown · 3 owed');
});

test('a gate a policy rule opened offers "stop asking"; the summary replaces the banner', async ({
  page,
}) => {
  // Record a preset that gates Detailed Design, in the same read as the owed rows.
  await serveOwed(page, initialStages(), undefined, (wire) => {
    wire.reviewPolicy = { gatedPhasesByType: {}, preset: 'checkpoints' };
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksPolicyBanner)).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionTasksPolicySummary)).toContainText('checkpoints');
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'why'))).toContainText(
    'Preset'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'stop-asking'))).toHaveText(
    'Stop asking me about this class of thing → review policy'
  );
  // A steer and a failure are not policy questions: nothing to turn off.
  await expect(
    page.getByTestId(TESTID.constructionTasksCell(`${TAKEOVER}:takeover`, 'stop-asking'))
  ).toHaveCount(0);
});

test('the TASKS toolbar names its own order and disables the list-only controls (designer P1-1)', async ({
  page,
}) => {
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionLensSortRanked)).toHaveText(
    'Ranked: risk floor · blast radius · id'
  );
  await expect(page.getByTestId(TESTID.constructionLensSort)).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionLensExpandToPhase)).toBeDisabled();
  await expect(page.getByLabel('Observed only')).toBeDisabled();
  // Back on the list, both come back and the Sort menu returns.
  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(page.getByTestId(TESTID.constructionLensSort)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionLensSortRanked)).toHaveCount(0);
  await expect(page.getByLabel('Observed only')).toBeEnabled();
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
  // Told before sending: the note rides the decision, but the redraft does not read
  // it yet (designer P0-1; delivery is follow-up B1).
  await expect(page.getByTestId(TESTID.constructionDetailDecisionCaption)).toHaveText(
    'Your note goes with the decision, but this redraft does not read it yet — the agent re-runs Detailed Design from its original brief.'
  );
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
    'Sent back — re-running Detailed Design (your note was not delivered)',
    { timeout: 10_000 }
  );
  // The lingering row reads SENT BACK, not RESUMED (designer P2); the pane says what
  // was decided and when (P1-4).
  const row = page.getByTestId(TESTID.constructionTasksRow(GATE_KEY));
  await expect(row).toHaveAttribute('data-lingering', 'true');
  await expect(row).toContainText('Sent back');
  await expect(row).not.toContainText('Resumed');
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/^sent back$/i);
  await expect(page.getByTestId(TESTID.constructionDetailDecisionLead)).toContainText(
    'You sent back this at'
  );
});

test('approve is confirmed by the resume, not the click', async ({ page }) => {
  const stages = initialStages();
  await serveOwed(page, stages);
  const sent: Record<string, unknown>[] = [];
  await answerDecisions(
    page,
    () => {
      // The workflow takes a while to leave the gate after the 200 (review I5): the
      // decision must stay busy through that window, not only until the answer.
      setTimeout(() => {
        stages[GATE] = STAGE.pipelineRunning;
      }, 5_000);
      return { status: 200 };
    },
    sent
  );
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  // With a live decision behind the selection, Approve is the filled primary and
  // Send back is outlined (designer P1-2).
  await expect(approve).toHaveAttribute('data-variant', 'contained');
  await expect(page.getByTestId(TESTID.constructionDetailAction('sendBack'))).toHaveAttribute(
    'data-variant',
    'outlined'
  );
  // One click, one signal — even three clicks in one task.
  await approve.evaluate((el) => {
    for (let i = 0; i < 3; i += 1) (el as HTMLButtonElement).click();
  });
  const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
  await expect(flow).toContainText('waiting for the agent to resume');
  // Answered 200, gate not yet left: still busy, and another click sends nothing.
  await expect(approve).toBeDisabled();
  await approve.evaluate((el) => {
    (el as HTMLButtonElement).click();
  });
  await expect(flow).toContainText('Resumed', { timeout: 15_000 });
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
  // The pane says what was decided, and that the ledger does not hold it yet (P1-4).
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(
    /^decided · approved$/i
  );
  await expect(page.getByTestId(TESTID.constructionDetailDecisionLead)).toContainText(
    'gate decisions are not yet written to the task ledger'
  );
  // Nothing is owed on this selection any more, so the decision actions are gone
  // (Stage B: they appear only where a decision is owed). Run stays — a text button,
  // disabled with its reason, never a silent no-op (designer P1-2).
  await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toHaveCount(0);
  const run = page.getByTestId(TESTID.constructionDetailAction('run'));
  await expect(run).toBeDisabled();
  await expect(run).toHaveAttribute('data-reason', /not wired/i);
  // The badge drops the moment the gate clears; the row lingers with its evidence.
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('2');
});

test('a decision on the wire survives a remount: Approve stays off, exactly one POST (review C1)', async ({
  page,
}) => {
  test.setTimeout(45_000);
  const stages = initialStages();
  await serveOwed(page, stages);
  const posts: string[] = [];
  let release: () => void = () => undefined;
  const held = new Promise<void>((r) => {
    release = r;
  });
  // Hold the 200 while the console is navigated away and back.
  await page.route('**/submit-phase-decision/**', async (route) => {
    posts.push(route.request().url());
    await held;
    stages[GATE] = STAGE.pipelineRunning;
    await route.fulfill({ status: 200, json: {} }).catch(() => undefined);
  });
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  await expect.poll(() => posts.length).toBe(1);
  // Home and back, in-app: the console unmounts and remounts; the QueryClient stays.
  await page.getByTestId(TESTID.designClose).click();
  await expect(page).toHaveURL(/\/home/);
  await page.goBack();
  await expect(page.getByTestId(TESTID.constructionTasksLens)).toBeVisible({ timeout: 15_000 });
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  await expect(approve).toBeDisabled();
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText(
    'Sending your approval'
  );
  await approve.evaluate((el) => {
    (el as HTMLButtonElement).click();
  });
  release();
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText('Resumed', {
    timeout: 15_000,
  });
  expect(posts).toHaveLength(1);
});

for (const decision of ['sendBack', 'approve'] as const) {
  test(`${decision} → the gate opens again: a new decision, never a stale "did not land" (review C2)`, async ({
    page,
  }) => {
    test.setTimeout(60_000);
    const stages = initialStages();
    await serveOwed(page, stages);
    let answeredAt = 0;
    await answerDecisions(
      page,
      () => {
        answeredAt = Date.now();
        // Leave the gate (the redraft, or the next phase's work)…
        stages[GATE] = STAGE.pipelineRunning;
        // …and reach the gate again: round 2, or the next phase's gate.
        setTimeout(() => {
          stages[GATE] = STAGE.awaitingApproval;
        }, 6_000);
        return { status: 200 };
      },
      []
    );
    await openTasks(page);
    await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
    if (decision === 'approve') {
      await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
    } else {
      await page.getByTestId(TESTID.constructionDetailAction('sendBack')).click();
      await page.getByTestId(TESTID.constructionDetailDecisionNote).fill('Tighten the ops list.');
      await page.getByTestId(TESTID.constructionDetailDecisionSendBack).click();
    }
    const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
    await expect(flow).toContainText(decision === 'approve' ? 'Resumed' : 'Sent back', {
      timeout: 10_000,
    });
    // The new occurrence retires the old record: the row is a fresh decision.
    await expect(flow).toHaveCount(0, { timeout: 15_000 });
    // Past the old decision's resume timeout, it still must not claim "did not land".
    await page.waitForTimeout(Math.max(0, answeredAt + 13_000 - Date.now()));
    await expect(flow).toHaveCount(0);
    await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toHaveAttribute(
      'data-lingering',
      'false'
    );
    await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toBeEnabled();
  });
}

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
