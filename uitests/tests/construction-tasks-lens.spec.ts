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
 * SAFETY: the shared dispatch guard (support/dispatchGuard.ts), both its layers,
 * because neither alone closes review I2 (measured, tasks round 2):
 *  - a CONTEXT route aborts every non-GET no page route answers; a page's
 *    `unrouteAll` cannot remove it;
 *  - a request a test HOLDS open goes through `dispatchGuard.hold()`, and is
 *    aborted BEFORE anything is unrouted (`page.unrouteAll` and the fixture's
 *    teardown both abort holds first). When `unrouteAll` removes a page handler
 *    still holding a request, Playwright lets that request go on to the network —
 *    past the context route too.
 * Nothing reaches the server but GET reads.
 */
import { test, expect } from './support/dispatchGuard.js';
import type { Page, Route } from '@playwright/test';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

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

/** Mutable session stages per activity, served for the session route. */
type Stages = Record<string, number | undefined>;

async function serveOwed(
  page: Page,
  stages: Stages,
  only?: readonly string[],
  /** Further edits to the same read (one handler, so none is bypassed). */
  alsoEdit?: (wire: Wire) => void,
  /** Serve these rows under another project id: the archistrator read, re-keyed in
   *  the browser (the two-project pin). */
  projectId = 'archistrator'
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
  await page.route(`**/system-design/get-project/${projectId}**`, async (route) => {
    const response = await route.fetch(
      projectId === 'archistrator'
        ? undefined
        : {
            url: route
              .request()
              .url()
              .replace(`/get-project/${projectId}`, '/get-project/archistrator'),
          }
    );
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
  await page.route(`**/get-session-state/${projectId}/**`, async (route: Route) => {
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
        projectId,
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

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

test.afterEach(async ({ page }) => {
  // The guard's `unrouteAll` aborts every held request FIRST, while every route is
  // still in place (I2): unrouting a handler that still holds one would send it on
  // to the network. The console re-reads the project every 10s (review I3), so a
  // route handler can be mid-fetch when a test ends; that is teardown, not a
  // failure. The context route outlives this.
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});

test('a write is aborted by the context guard, even once every page route is gone (review I2)', async ({
  page,
  dispatchGuard,
}) => {
  await openTasks(page);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  // SAFETY: the probe path does not exist on the server, so a failed guard could
  // only ever reach a 404 (and the GET-only proxy in front of it).
  const probe = '/api/v1/__tasks-lens-guard-probe';
  const outcome = await page.evaluate(async (url) => {
    try {
      return `status ${String((await fetch(url, { method: 'POST', body: '{}' })).status)}`;
    } catch {
      return 'aborted';
    }
  }, probe);
  expect(outcome).toBe('aborted');
  expect(dispatchGuard.blocked.filter((b) => b.endsWith(probe))).toEqual([`POST ${BASE}${probe}`]);
});

test('a write a test still holds when it ends is aborted by teardown, never let out (review I2)', async ({
  page,
  dispatchGuard,
}) => {
  await openTasks(page);
  // SAFETY: the probe path does not exist on the server; were it let out, it could
  // only reach the GET-only proxy, which refuses it.
  const probe = '/api/v1/__tasks-lens-hold-probe';
  const hold = dispatchGuard.hold();
  let held = 0;
  await page.route(`**${probe}`, (route) => {
    held += 1;
    return hold.handle(route, async () => {
      await route.fulfill({ status: 200, json: {} });
    });
  });
  const outcome = page.evaluate(async (url) => {
    try {
      return `status ${String((await fetch(url, { method: 'POST', body: '{}' })).status)}`;
    } catch {
      return 'aborted';
    }
  }, probe);
  await expect.poll(() => held).toBe(1);
  // Exactly what teardown does when a test fails with the request still held.
  await dispatchGuard.abortHolds();
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  expect(await outcome).toBe('aborted');
});

test('live: nothing is owed, and the lens says so without probing a session', async ({ page }) => {
  const sessionGets: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('get-session-state')) sessionGets.push(r.url());
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).toContainText('Nothing needs you.');
  // Every activity in one bucket (designer final pass, item 1): the waiting,
  // failed and unclassified clauses appear only when above zero.
  await expect(page.getByTestId(TESTID.constructionTasksEmptyCounts)).toHaveText(
    /^\d+ eligible(?: · \d+ waiting on dependencies)? · \d+ in flight · \d+ blocked · \d+ done(?: · \d+ failed)?(?: · \d+ unclassified)?$/
  );
  // The in-card Begin is a link that says what it would start with (designer P2).
  await expect(page.getByTestId(TESTID.constructionTasksResume)).toHaveText(
    /^(Begin|Resume) construction — \d+ eligible$/
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

test('a probe that fails is not an all-clear; "Couldn\'t check" holds through re-asks, and Retry asks at once (designer B1)', async ({
  page,
}) => {
  test.setTimeout(60_000);
  // Only the RUNNING row is started, and its session route answers 500 every time.
  await serveOwed(page, {}, [RUNNING]);
  const sessionGets: number[] = [];
  await page.route('**/get-session-state/archistrator/**', async (route) => {
    sessionGets.push(Date.now());
    await route.fulfill({
      status: 500,
      json: { error: 'session store unavailable' },
    });
  });
  await openTasks(page);
  const unchecked = page.getByTestId(TESTID.constructionTasksUnchecked);
  const retry = page.getByTestId(TESTID.constructionTasksUncheckedRetry);
  const couldnt = "Couldn't check 1 in-flight activity";
  await expect(unchecked).toContainText(couldnt, { timeout: 15_000 });
  await expect(page.getByText('Nothing needs you.')).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).not.toContainText('Nothing needs');
  // The TASKS badge cannot say "nothing" either: "?" while a probe is unchecked.
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('?');

  // It HOLDS for 12s, through the probe's own re-ask: never "Checking…", never a
  // Retry that vanishes (designer re-check B1, which saw it blink every ~4s).
  const windowStart = sessionGets.length;
  for (const end = Date.now() + 12_000; Date.now() < end; ) {
    expect(await unchecked.textContent()).toContain(couldnt);
    expect(await retry.count()).toBe(1);
    await page.waitForTimeout(200);
  }
  // …and it backs off: one re-ask (a fetch and its one retry) in the window, not
  // the 3s live cadence.
  const reasks = sessionGets.length - windowStart;
  expect(reasks).toBeGreaterThanOrEqual(1);
  expect(reasks).toBeLessThanOrEqual(2);

  // Retry asks at once — within 1s, far inside the backoff — and says so meanwhile.
  await expect(retry).toHaveText('Retry');
  const before = sessionGets.length;
  const clickedAt = Date.now();
  await retry.click();
  await expect.poll(() => sessionGets.length, { timeout: 1_000 }).toBeGreaterThan(before);
  expect((sessionGets[before] ?? Infinity) - clickedAt).toBeLessThan(1_000);
  await expect(retry).toHaveText('Retrying…');
  await expect(retry).toBeDisabled();
  // Failed again: still "Couldn't check", and Retry is back.
  await expect(retry).toHaveText('Retry', { timeout: 10_000 });
  await expect(unchecked).toContainText(couldnt);
  await expect(page.getByText('Nothing needs you.')).toHaveCount(0);
});

test('a probe still in flight reads "Checking…", never the all-clear', async ({
  page,
  dispatchGuard,
}) => {
  await serveOwed(page, {}, [RUNNING]);
  const hold = dispatchGuard.hold();
  await page.route('**/get-session-state/archistrator/**', (route) =>
    hold.handle(route, async () => {
      await route.fulfill({
        status: 404,
        json: { error: 'no construction session' },
      });
    })
  );
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksUnchecked)).toContainText(
    'Checking 1 in-flight activity…'
  );
  await expect(page.getByText('Nothing needs you.')).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('?');
  // Once the probe answers (a dormant-pump 404 — an established absence), it is clear.
  hold.release();
  await expect(page.getByTestId(TESTID.constructionTasksEmpty)).toContainText(
    'Nothing needs you.',
    { timeout: 10_000 }
  );
  // …and so is the badge: nothing owed, nothing unchecked, no badge at all.
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveCount(0);
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
      json: {
        projectId: 'archistrator',
        activityId: GATE,
        stage: STAGE.awaitingApproval,
      },
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
      await route.fulfill({
        status: 404,
        json: { error: 'no construction session' },
      });
      return;
    }
    await route.fulfill({
      json: {
        projectId: 'archistrator',
        activityId: GATE,
        stage: STAGE.awaitingApproval,
      },
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
  await expect(
    page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'cant-turn-off'))
  ).toHaveText('Can’t be turned off');
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'stop-asking'))).toHaveCount(
    0
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
  // The shape reads the activity's contract through the contract join
  // (componentId → contractKey): billingStateAccess commits 6 ops.
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'shape'))).toHaveText(
    'Contract · 6 ops'
  );
  // An unknown shape says nothing before "no CI record": no "—" token (designer P2).
  // design-health-engine commits no contract, so its row carries no shape at all.
  await expect(
    page.getByTestId(TESTID.constructionTasksCell(`${TAKEOVER}:takeover`, 'shape'))
  ).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'ci'))).toHaveText(
    'no CI record'
  );
});

test('folded beside the pane, WAITING reads on one line; the drawer offers the next decision', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  // 1280: the pane takes the room and each row folds (designer P2).
  await openTasks(page, 1280);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  const dash = page
    .getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'waiting'))
    .getByText('—', { exact: true });
  const round = page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'round'));
  const a = await dash.boundingBox();
  const b = await round.boundingBox();
  expect(a && b ? Math.abs(a.y + a.height / 2 - (b.y + b.height / 2)) : 99).toBeLessThan(6);
  // Below 1200px the pane is a drawer over the table: its footer names the next one.
  await openTasks(page, 1100);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  const next = page.getByTestId(TESTID.constructionDetailNextDecision);
  await expect(next).toContainText('Next decision →');
  const target = (await next.textContent())?.split('→ ')[1]?.trim() ?? '';
  expect(target).not.toBe(GATE);
  await next.click();
  await expect(page).toHaveURL(new RegExp(`a=${target}`));
});

const REVIEW_ONLY =
  'Retry and re-queue unlock once a verification run shows your note reaching the agent. Until then, steer from GitHub or the MCP override_activity tool.';

test('steer-needed and failed rows show the PM’s steer actions LOCKED: disabled, with the reason, and nothing is sent', async ({
  page,
}) => {
  // Every write the page tries is recorded (the dispatch guard aborts it anyway).
  const writes: string[] = [];
  page.on('request', (r) => {
    if (r.method() !== 'GET') writes.push(`${r.method()} ${r.url()}`);
  });
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
  // PM Q3: Retry… · Review · ⋯ Skip… on the steer, Re-queue… · Review on the
  // failure — each disabled with the locked reason, Review the live primary, and
  // never the cut Takeover / Reassign.
  const steerKey = `${TAKEOVER}:takeover`;
  const failedKey = `${FAILED}:failed`;
  const locked = [
    [TESTID.constructionTasksSteer(steerKey, 'retry'), 'Retry…'],
    [TESTID.constructionTasksSteer(steerKey, 'skip'), 'Skip…'],
    [TESTID.constructionTasksSteer(failedKey, 'requeue'), 'Re-queue…'],
  ] as const;
  for (const [id, label] of locked) {
    const b = page.getByTestId(id);
    await expect(b).toHaveText(new RegExp(`${label}$`));
    await expect(b).toBeDisabled();
    await expect(b).toHaveAttribute('data-reason', REVIEW_ONLY);
    // A disabled button takes no click: forced, it still sends nothing.
    await b.click({ force: true });
  }
  await expect(page.getByTestId(TESTID.constructionTasksSteer(failedKey, 'retry'))).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionTasksSteer(steerKey, 'requeue'))).toHaveCount(0);
  for (const key of [steerKey, failedKey]) {
    const review = page.getByTestId(TESTID.constructionTasksReview(key));
    await expect(review).toBeEnabled();
    await expect(review).toHaveAttribute('data-variant', 'contained');
  }
  for (const name of [/Takeover/, /Reassign/]) {
    await expect(page.getByRole('button', { name })).toHaveCount(0);
  }

  await page.getByTestId(TESTID.constructionTasksReview(`${TAKEOVER}:takeover`)).click();
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/steer needed/i);
  await expect(page.getByTestId(TESTID.constructionDetailOwedReason)).toContainText(
    'three builds failed the contract tests'
  );
  await expect(page.getByTestId(TESTID.constructionDetailReviewOnlyNote)).toHaveText(REVIEW_ONLY);
  // Run is always present (spec §7.8, §9.3): here disabled, with the review-only
  // reason (tasks merge review I2 ruling). Nothing else is offered.
  const run = page.getByTestId(TESTID.constructionDetailAction('run'));
  await expect(run).toBeVisible();
  await expect(run).toBeDisabled();
  await expect(run).toHaveAttribute('data-reason', REVIEW_ONLY);
  for (const id of ['retry', 'skip']) {
    const b = page.getByTestId(TESTID.constructionDetailAction(id));
    await expect(b).toBeDisabled();
    await expect(b).toHaveAttribute('data-reason', REVIEW_ONLY);
  }
  await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toHaveCount(0);
  await expect(page.getByTestId(TESTID.constructionDetailAction('sendBack'))).toHaveCount(0);

  await page.getByTestId(TESTID.constructionTasksReview(`${FAILED}:failed`)).click();
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/^failed$/i);
  await expect(page.getByTestId(TESTID.constructionDetailOwedReason)).toContainText(
    'the provisioning pipeline ran past its budget'
  );
  await expect(page.getByTestId(TESTID.constructionDetailReviewOnlyNote)).toBeVisible();
  await expect(run).toBeVisible();
  await expect(run).toBeDisabled();
  await expect(run).toHaveAttribute('data-reason', REVIEW_ONLY);
  await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toHaveCount(0);
  const requeue = page.getByTestId(TESTID.constructionDetailAction('requeue'));
  await expect(requeue).toBeDisabled();
  await expect(requeue).toHaveAttribute('data-reason', REVIEW_ONLY);
  await expect(page.getByTestId(TESTID.constructionDetailAction('retry'))).toHaveCount(0);
  for (const name of [/Takeover/, /Reassign/]) {
    await expect(page.getByRole('button', { name })).toHaveCount(0);
  }
  expect(writes).toEqual([]);
});

test('the list lens reads the same owed set: "Awaiting me" is the gate, the steer and the failure', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible({
    timeout: 15_000,
  });
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
  await expect(page.getByTestId(TESTID.constructionTasksPolicySummary)).toContainText(
    'checkpoints'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'why'))).toContainText(
    'Preset'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'stop-asking'))).toHaveText(
    'Stop asking me about this class of thing → review policy'
  );
  // A Detailed Design gate is no risk-floor case: "stop asking" is not hedged.
  await expect(
    page.getByTestId(TESTID.constructionTasksCell(GATE_KEY, 'stop-asking-hedge'))
  ).toHaveCount(0);
  // A steer and a failure are not policy questions: nothing to turn off.
  await expect(
    page.getByTestId(TESTID.constructionTasksCell(`${TAKEOVER}:takeover`, 'stop-asking'))
  ).toHaveCount(0);
});

test('"stop asking" on a construction gate says the risk floor may still ask (round 2, designer)', async ({
  page,
}) => {
  // The same preset, with the gate on Construction: the one phase the risk floor
  // holds under every policy (a contract touching deploy, spend or schema).
  await serveOwed(page, initialStages(), undefined, (wire) => {
    wire.reviewPolicy = { gatedPhasesByType: {}, preset: 'checkpoints' };
    const row = wire.ActivityConstruction?.[GATE];
    if (row !== undefined) row['CurrentPhase'] = 'construction';
  });
  await openTasks(page);
  // eslint-disable-next-line no-restricted-syntax -- the construction gate's key carries a ledger round this spec does not fix; the row is found by its reason and activity instead.
  const row = page.locator(`[data-testid^="construction-tasks-row-${GATE}:"][data-reason="gate"]`);
  await expect(row).toHaveCount(1);
  const key = ((await row.getAttribute('data-testid')) ?? '').replace(
    'construction-tasks-row-',
    ''
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(key, 'why'))).toContainText('Preset');
  await expect(page.getByTestId(TESTID.constructionTasksCell(key, 'stop-asking'))).toHaveText(
    'Stop asking me about this class of thing → review policy'
  );
  await expect(page.getByTestId(TESTID.constructionTasksCell(key, 'stop-asking-hedge'))).toHaveText(
    '…the risk floor may still ask'
  );
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
  // …and LOOKS off: the same half-opacity mark as Expand (tasks round 2, designer).
  // The mark is laid on the box that holds the switch and its label; what the
  // operator sees is the SWITCH drawn at half opacity, so that is what is read: the
  // product of every opacity from the switch (its one test id) up to the page.
  const observedSwitch = page.getByTestId(TESTID.constructionLensObservedOnly);
  const renderedOpacity = (): Promise<number> =>
    observedSwitch.evaluate((el) => {
      let opacity = 1;
      for (let e: Element | null = el; e !== null; e = e.parentElement) {
        opacity *= Number(getComputedStyle(e).opacity);
      }
      return opacity;
    });
  await expect.poll(renderedOpacity).toBe(0.5);
  // Back on the list, both come back and the Sort menu returns.
  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(page.getByTestId(TESTID.constructionLensSort)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionLensSortRanked)).toHaveCount(0);
  await expect(page.getByLabel('Observed only')).toBeEnabled();
  await expect.poll(renderedOpacity).toBe(1);
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
  // The verdict block says what is missing plainly; the engineering is on hover (P1-7).
  await expect(page.getByTestId(TESTID.constructionDetailVerdict)).toContainText(
    'Reviewer verdicts aren’t recorded yet — this is who was asked, not what they said.'
  );
  const sendBack = page.getByTestId(TESTID.constructionDetailAction('sendBack'));
  await expect(sendBack).toBeEnabled();
  await sendBack.click();
  // While the composer is open its own send is the only one: the bar's Send back
  // steps aside (tasks round 2, designer).
  await expect(sendBack).toHaveCount(0);
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
    /^You sent this back at \d\d:\d\d;/
  );
});

test('approve is confirmed by the resume, not the click', async ({ page }) => {
  // The resumed row lingers ~30s before it leaves; the last check is after that.
  test.setTimeout(90_000);
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
  // In the operator's words — no ticket names in a tooltip (tasks round 2, designer).
  await expect(run).not.toHaveAttribute('data-reason', /\bB\d\b/);
  // The badge drops the moment the gate clears; the row lingers with its evidence.
  await expect(page.getByTestId(TESTID.constructionLensTasksCount)).toHaveText('2');
  // Once the linger is over the row leaves, and the pane — still on the gate task,
  // which the ledger has no attempt for — says what the live workflow says of the
  // activity, never UNKNOWN (tasks round 2, designer).
  await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toHaveCount(0, {
    timeout: 45_000,
  });
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(
    /^activity running$/i
  );
});

test('a decision on the wire survives a remount: Approve stays off, exactly one POST (review C1)', async ({
  page,
  dispatchGuard,
}) => {
  test.setTimeout(45_000);
  const stages = initialStages();
  await serveOwed(page, stages);
  const posts: string[] = [];
  // Hold the 200 while the console is navigated away and back. If the test fails
  // first, teardown aborts it — it never goes out (review I2).
  const hold = dispatchGuard.hold();
  await page.route('**/submit-phase-decision/**', (route) => {
    posts.push(route.request().url());
    return hold.handle(route, async () => {
      stages[GATE] = STAGE.pipelineRunning;
      await route.fulfill({ status: 200, json: {} });
    });
  });
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  await expect.poll(() => posts.length).toBe(1);
  // Home and back, in-app: the console unmounts and remounts; the QueryClient stays.
  await page.getByTestId(TESTID.designClose).click();
  await expect(page).toHaveURL(/\/home/);
  await page.goBack();
  await expect(page.getByTestId(TESTID.constructionTasksLens)).toBeVisible({
    timeout: 15_000,
  });
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  await expect(approve).toBeDisabled();
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText(
    'Sending your approval'
  );
  // Not "Decided" while the server has not accepted it: the gate still awaits you,
  // and there is no "You approved this at…" lead yet (tasks round 2, designer).
  await expect(page.getByTestId(TESTID.constructionDetailStateChip)).toHaveText(/^awaiting you$/i);
  await expect(page.getByTestId(TESTID.constructionDetailDecisionLead)).toHaveCount(0);
  await approve.evaluate((el) => {
    (el as HTMLButtonElement).click();
  });
  hold.release();
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText('Resumed', {
    timeout: 15_000,
  });
  expect(posts).toHaveLength(1);
});

test('a POST still on the wire keeps the re-opened gate busy, and says why (round 2 I1)', async ({
  page,
  dispatchGuard,
}) => {
  test.setTimeout(60_000);
  const stages = initialStages();
  await serveOwed(page, stages);
  const posts: string[] = [];
  // The reviewer's repro: hold the approval on the wire…
  const hold = dispatchGuard.hold();
  await page.route('**/submit-phase-decision/**', (route) => {
    posts.push(route.request().url());
    return hold.handle(route, async () => {
      await route.fulfill({ status: 200, json: {} });
    });
  });
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  await approve.click();
  await expect.poll(() => posts.length).toBe(1);
  const row = page.getByTestId(TESTID.constructionTasksRow(GATE_KEY));
  const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
  // …flip the session to running (the gate is left: the row lingers, still sending)…
  stages[GATE] = STAGE.pipelineRunning;
  await expect(row).toHaveAttribute('data-lingering', 'true', {
    timeout: 10_000,
  });
  // …and back to the gate, which bumps the occurrence and retires the pending record.
  stages[GATE] = STAGE.awaitingApproval;
  await expect(row).toHaveAttribute('data-lingering', 'false', {
    timeout: 10_000,
  });
  // The new gate cannot be decided while the first request is on the wire: the
  // buttons stay off and the line says why — never an enabled Approve that does nothing.
  await expect(flow).toHaveText('Previous decision still sending…');
  await expect(page.getByTestId(TESTID.constructionDetailDecisionFlow)).toHaveText(
    'Previous decision still sending…'
  );
  await expect(approve).toBeDisabled();
  await expect(page.getByTestId(TESTID.constructionDetailAction('sendBack'))).toBeDisabled();
  await approve.evaluate((el) => {
    (el as HTMLButtonElement).click();
  });
  // Once it settles, the re-opened gate is a fresh decision.
  hold.release();
  await expect(approve).toBeEnabled({ timeout: 10_000 });
  await expect(flow).toHaveCount(0);
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
    await answerDecisions(page, () => {
      answeredAt = Date.now();
      // Leave the gate (the redraft, or the next phase's work)…
      stages[GATE] = STAGE.pipelineRunning;
      // …and reach the gate again: round 2, or the next phase's gate.
      setTimeout(() => {
        stages[GATE] = STAGE.awaitingApproval;
      }, 6_000);
      return { status: 200 };
    }, []);
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

// Tasks round-2 review, minor: a record was retired on a newer occurrence BEFORE its
// error was read. The reviewer's repro: hold the POST, let the gate leave and
// re-open, then answer 500 — Approve came back enabled on the new gate 26ms later.
test('a 500 answered after the gate re-opened holds the new gate until a read asked for after it (round-2 review, minor)', async ({
  page,
  dispatchGuard,
}) => {
  test.setTimeout(60_000);
  const stages = initialStages();
  await serveOwed(page, stages);
  // Session reads asked for after the 500 are answered late, so the window between
  // the answer and the first newer read is long enough to watch.
  let stallFrom = Number.POSITIVE_INFINITY;
  await page.route('**/get-session-state/archistrator/**', async (route) => {
    if (Date.now() >= stallFrom) {
      await new Promise((r) => {
        setTimeout(r, 3_000);
      });
    }
    await route.fallback().catch(() => undefined);
  });
  const posts: string[] = [];
  const hold = dispatchGuard.hold();
  await page.route('**/submit-phase-decision/**', (route) => {
    posts.push(route.request().url());
    return hold.handle(route, async () => {
      await route.fulfill({ status: 500, json: { code: 'internal', error: 'boom' } });
    });
  });
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  await approve.click();
  await expect.poll(() => posts.length).toBe(1);
  const row = page.getByTestId(TESTID.constructionTasksRow(GATE_KEY));
  const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
  // The gate is left while the POST is on the wire…
  stages[GATE] = STAGE.pipelineRunning;
  await expect(row).toHaveAttribute('data-lingering', 'true', { timeout: 10_000 });
  // …and re-opens: a newer occurrence.
  stages[GATE] = STAGE.awaitingApproval;
  await expect(row).toHaveAttribute('data-lingering', 'false', { timeout: 10_000 });
  await expect(flow).toHaveText('Previous decision still sending…');
  // Now the 500. The signal may have been delivered, and no read taken since says
  // where the session stands: the re-opened gate is not a fresh decision yet.
  stallFrom = Date.now();
  hold.release();
  await expect(flow).toContainText('Outcome unknown', { timeout: 10_000 });
  for (let i = 0; i < 15; i++) {
    expect(await approve.isDisabled(), `Approve enabled ${String(i * 100)}ms after`).toBe(true);
    await page.waitForTimeout(100);
  }
  // A read asked for after the answer lands and still shows the gate: decide again.
  await expect(approve).toBeEnabled({ timeout: 20_000 });
  await expect(flow).toHaveCount(0);
  expect(posts).toHaveLength(1);
});

// Tasks round-2 review, minor: the route's cache read and its one-click guard each
// built the mutation key themselves, and a key without the project survived every
// test. Both use phaseDecisionFilters now; this drives each call site across two
// projects that share an activity id. SAFETY: the other project exists only in the
// browser (its read is the archistrator read, re-keyed); every POST is held here
// and aborted by the guard's teardown.
test('a decision on the wire in one project never holds the same gate in another (round-2 review, minor)', async ({
  page,
  dispatchGuard,
}) => {
  test.setTimeout(60_000);
  const OTHER = 'tasks-lens-second-project';
  await serveOwed(page, initialStages());
  await serveOwed(page, { [GATE]: STAGE.awaitingApproval }, [GATE], undefined, OTHER);
  const posts: string[] = [];
  const hold = dispatchGuard.hold();
  await page.route('**/submit-phase-decision/**', (route) => {
    posts.push(new URL(route.request().url()).pathname);
    return hold.handle(route, async () => {
      await route.fulfill({ status: 200, json: {} });
    });
  });
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  await expect.poll(() => posts.length).toBe(1);
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toContainText(
    'Sending your approval'
  );
  // In-app to the other project's console: the QueryClient, and the decision on
  // the wire in its mutation cache, come along.
  await page.evaluate((to) => {
    window.history.pushState(null, '', to);
    window.dispatchEvent(new PopStateEvent('popstate', { state: null }));
  }, `/project/${OTHER}/construction?lens=tasks`);
  const review = page.getByTestId(TESTID.constructionTasksReview(GATE_KEY));
  await expect(review).toBeVisible({ timeout: 15_000 });
  await review.click();
  // The cache read: project A's decision is not this gate's record…
  await expect(page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY))).toHaveCount(0);
  const approve = page.getByTestId(TESTID.constructionDetailAction('approve'));
  await expect(approve).toBeEnabled();
  // …and the one-click guard lets this project's own decision go.
  await approve.click();
  await expect.poll(() => posts.length).toBe(2);
  expect(posts[0]).toContain('/submit-phase-decision/archistrator/');
  expect(posts[1]).toContain(`/submit-phase-decision/${OTHER}/`);
});

// ---------------------------------------------------------------------------
// Tasks merge review M2: pins for the survivors of the review's mutation run.
// ---------------------------------------------------------------------------

/** The tree task row's state, read from the row's own `data-task-state` hook. */
async function taskStateOf(page: Page, nodeId: string): Promise<string | null> {
  return page.evaluate(
    (rowId) =>
      document
        .querySelector(`[data-testid="${rowId}"] [data-task-state]`)
        ?.getAttribute('data-task-state') ?? null,
    TESTID.constructionListRow(nodeId)
  );
}

// MP3: with a gate owed, the list's Expand has something to open. TASKS still
// disables it, as a list-only control, not because nothing is current.
test('M2/MP3: with a gate owed, Expand is enabled on the list and stays disabled in TASKS', async ({
  page,
}) => {
  await serveOwed(page, initialStages());
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksRow(GATE_KEY))).toBeVisible();
  const expand = page.getByTestId(TESTID.constructionLensExpandToPhase);
  await page.getByTestId(TESTID.constructionLensButton('list')).click();
  await expect(expand).toBeEnabled({ timeout: 10_000 });
  await page.getByTestId(TESTID.constructionLensButton('tasks')).click();
  await expect(page.getByTestId(TESTID.constructionTasksLens)).toBeVisible();
  await expect(expand).toBeDisabled();
  for (let i = 0; i < 8; i++) {
    expect(await expand.isDisabled(), 'Expand enabled in TASKS').toBe(true);
    await page.waitForTimeout(150);
  }
});

// MP8: the list's task row reads the owed mark. Its own open attempt says the gate
// task is running; only the owed gate makes it AWAITING YOU.
test('M2/MP8: the list task row reads the owed mark: the gate task awaits you', async ({
  page,
}) => {
  await serveOwed(page, initialStages(), undefined, (wire) => {
    const r = wire.ActivityConstruction?.[GATE];
    if (r === undefined) throw new Error(`no row ${GATE} in the read`);
    // An open (outcome '') observed attempt on the gate task: the task is running.
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
  });
  await page.setViewportSize({ width: 1600, height: 950 });
  const node = `${GATE}::detailed_design::designReview`;
  await gotoApp(
    page,
    `/project/archistrator/construction?lens=list&a=${GATE}&p=detailed_design&k=designReview`
  );
  await expect(page.getByTestId(TESTID.constructionListRow(node))).toBeVisible({
    timeout: 15_000,
  });
  await expect.poll(() => taskStateOf(page, node), { timeout: 10_000 }).toBe('awaitingHuman');
});

// MP1: pump evidence reads the owed set. After a Begin answered 500, a row head-state
// still calls in construction, but whose pump stopped on a recorded failure, is not
// the pump: the hold stands.
test('M2/MP1: after a 500, a failure-stopped row is no evidence of the pump: Begin stays held', async ({
  page,
}) => {
  await serveOwed(page, {}, [FAILED], (wire) => {
    const r = wire.ActivityConstruction?.[FAILED];
    if (r !== undefined) r['BuildStatus'] = BUILD.inConstruction;
  });
  const dispatched: string[] = [];
  await page.route('**/execute-next-activity/**', async (route) => {
    dispatched.push(route.request().url());
    await route.fulfill({ status: 500, json: { error: 'boom' } });
  });
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toBeEnabled({ timeout: 15_000 });
  await begin.click();
  await page.getByTestId(TESTID.constructionBeginConfirmDispatch).click();
  await expect.poll(() => dispatched.length).toBe(1);
  await expect(page.getByTestId(TESTID.constructionBeginError)).toBeVisible({ timeout: 10_000 });
  // Several fast polls land after the 500. None of them is evidence.
  for (const end = Date.now() + 8_000; Date.now() < end; ) {
    expect(await begin.isEnabled(), 'Begin enabled beside a failure-stopped row').toBe(false);
    await page.waitForTimeout(150);
  }
});

// MP7: "Observed only" is an evidence VIEW, and never decides whether the pump runs.
// C-billing-engine's record is entirely backfilled; edited to sit in review, its raw
// row is work in flight. The view strips it to NOT STARTED, and Begin must not
// follow the view.
test('M2/MP7: Observed only cannot enable Begin: the label reads the raw rows', async ({
  page,
}) => {
  const RECONSTRUCTED = 'C-billing-engine';
  await serveOwed(page, {}, [], (wire) => {
    const r = wire.ActivityConstruction?.[RECONSTRUCTED];
    if (r === undefined) throw new Error(`no row ${RECONSTRUCTED} in the read`);
    r['BuildStatus'] = BUILD.inReview;
  });
  await page.setViewportSize({ width: 1600, height: 950 });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Construction running…/, { timeout: 15_000 });
  await page.getByRole('switch', { name: 'Observed only' }).check();
  await expect(page.getByRole('switch', { name: 'Observed only' })).toBeChecked();
  for (const end = Date.now() + 3_000; Date.now() < end; ) {
    const s = await begin.evaluate((el) => ({
      label: (el as HTMLElement).innerText.trim(),
      enabled: !(el as HTMLButtonElement).disabled,
    }));
    expect(s.enabled, `Begin enabled under Observed only ("${s.label}")`).toBe(false);
    expect(s.label).toMatch(/Construction running…/);
    await page.waitForTimeout(150);
  }
});

// MP6: "now in <phase>" counts a project read from its REQUEST. A read asked for
// before the gate left still names the gate's own phase, however late it lands.
test('M2/MP6: a project read asked for before the gate left never says "now in" its phase', async ({
  page,
}) => {
  test.setTimeout(90_000);
  const stages = initialStages();
  const phase = { current: 'detailed_design', served: 0 };
  await serveOwed(page, stages, undefined, (wire) => {
    const r = wire.ActivityConstruction?.[GATE];
    if (r !== undefined) r['CurrentPhase'] = phase.current;
    phase.served += 1;
  });
  // Registered after serveOwed, so it sees each project read first. `stall` holds one
  // read until released; `delay` holds each later one 2s, so the stalled read is
  // alone on screen for a while.
  let release: () => void = () => undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const project = { stall: false, stalled: 0, delayMs: 0 };
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    if (project.stall) {
      project.stall = false;
      project.stalled += 1;
      await gate;
    } else if (project.delayMs > 0) {
      await new Promise((r) => {
        setTimeout(r, project.delayMs);
      });
    }
    await route.fallback().catch(() => undefined);
  });
  await answerDecisions(page, () => ({ status: 200 }), []);
  await openTasks(page);
  await page.getByTestId(TESTID.constructionTasksReview(GATE_KEY)).click();
  await page.getByTestId(TESTID.constructionDetailAction('approve')).click();
  const flow = page.getByTestId(TESTID.constructionTasksFlow(GATE_KEY));
  await expect(flow).toContainText('waiting for the agent to resume');
  // A project read is asked for now, before the gate leaves, and held.
  project.stall = true;
  await expect.poll(() => project.stalled, { timeout: 20_000 }).toBe(1);
  // The gate leaves.
  stages[GATE] = STAGE.pipelineRunning;
  await expect(flow).toContainText('Resumed', { timeout: 15_000 });
  // The held read lands now, after the gate left, still naming the gate's phase.
  project.delayMs = 2_000;
  const servedBefore = phase.served;
  release();
  await expect.poll(() => phase.served, { timeout: 10_000 }).toBeGreaterThan(servedBefore);
  phase.current = 'construction';
  for (const end = Date.now() + 1_800; Date.now() < end; ) {
    expect(await flow.textContent()).not.toMatch(/now in/i);
    await page.waitForTimeout(100);
  }
  // A read asked for after the gate left says where the activity is now.
  project.delayMs = 0;
  await expect(flow).toContainText(/now in construction/i, { timeout: 20_000 });
});

// Tasks-lens merge round: the Begin label reads the owed set for "awaiting" (Q4).
// Head-state still says in construction; the pump's own failure record says it
// stopped there. Nothing else is in flight, so Begin is offered, not "running".
test('a recorded failure is not work in flight: Begin stays offered beside it (merge round)', async ({
  page,
}) => {
  await serveOwed(page, {}, [FAILED], (wire) => {
    const r = wire.ActivityConstruction?.[FAILED];
    if (r !== undefined) r['BuildStatus'] = BUILD.inConstruction;
  });
  await openTasks(page);
  await expect(page.getByTestId(TESTID.constructionTasksRow(`${FAILED}:failed`))).toBeVisible();
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction|Continue construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
  // Sampled: it never reads as running beside the stopped activity.
  for (let i = 0; i < 10; i++) {
    await expect(begin).not.toHaveText(/Construction running/);
    await page.waitForTimeout(150);
  }
  // The list's "Expand to current phase" reads the same owed-aware rule: nothing to open.
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionListTree)).toBeVisible();
  await expect(page.getByTestId(TESTID.constructionLensExpandToPhase)).toBeDisabled();
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
  // A 5xx may have been delivered, so Approve waits for a session read REQUESTED
  // after the failure — then, the gate still open, the human may decide again
  // (review I1; round 2: a read counts from its request, not its arrival).
  await expect(page.getByTestId(TESTID.constructionDetailAction('approve'))).toBeEnabled({
    timeout: 10_000,
  });
});
