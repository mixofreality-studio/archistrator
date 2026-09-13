/**
 * construction-begin-confirm.spec — Begin/Resume is decided once, and Begin asks
 * before it dispatches (fix round A, designer P0-3 + review I3; hardened by the
 * fix-A review, I1/I2).
 *
 * WHAT WENT WRONG BEFORE
 * ----------------------
 * The label was guessed from the rows: 23 activities carry backfilled
 * (reconstructed) attempts no pump ever ran, so the console read "Resume
 * construction" on a project whose pump had never started — and because the rows
 * arrive a beat after the page, it read "Begin" first and flipped to "Resume"
 * about 1.1s after load. The label is now the project read's constructionStarted
 * (fix round B, item 7), computed once on the server — which replaced probing one
 * construction-session endpoint per activity on every load. The seeded project has
 * never been run, so the one label is "Begin construction", and it is never shown
 * before the project read is in.
 *
 * Then the fix-A review found a double-click on the dispatch button sent TWO
 * execute-next-activity POSTs, each with a fresh tickID — two pump workflows.
 *
 * SAFETY: the shared dispatch guard (support/dispatchGuard) aborts EVERY non-GET
 * before any navigation. On top of it, every execute-next-activity request is
 * TRAPPED by a page.route installed before the page is opened, so no case here
 * can dispatch to the live server — including a mutant that makes Cancel, Escape
 * or the backdrop confirm. The trapped calls are what each case asserts on.
 *
 * Gated like construction-tracker.spec.ts: needs the seeded "archistrator"
 * construction-phase project behind the SPA proxy.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { TESTID } from './support/testids.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const COMMITTED_LABEL = /Begin construction|Resume construction/;

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

interface Trapped {
  url: string;
  tickID: string | undefined;
}

/** Abort every dispatch before it leaves the browser, and record it. MUST run
 *  before the page is opened, so not even the first request can slip past. */
async function trapDispatches(page: Page): Promise<Trapped[]> {
  const trapped: Trapped[] = [];
  await page.route('**/execute-next-activity/**', async (route) => {
    const body = route.request().postDataJSON() as { tickID?: string } | null;
    trapped.push({ url: route.request().url(), tickID: body?.tickID });
    await route.abort();
  });
  return trapped;
}

/** The activities the server reports with no stored record — read from the same
 *  get-project wire the console reads, so the expectation is never hardcoded. */
async function unrecordedActivityIds(
  request: import('@playwright/test').APIRequestContext
): Promise<string[]> {
  const res = await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`, {
    headers: { Accept: 'application/json' },
  });
  expect(res.status()).toBe(200);
  const data = (await res.json()) as {
    ActivityConstruction?: Record<string, { ActivityID: string; recorded: boolean }>;
  };
  return Object.values(data.ActivityConstruction ?? {})
    .filter((r) => !r.recorded)
    .map((r) => r.ActivityID)
    .sort();
}

async function openConsole(page: Page): Promise<void> {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toBeVisible({ timeout: 15_000 });
  await expect(begin).toHaveText(/Begin construction/);
  await expect(begin).toBeEnabled();
}

async function openDialog(page: Page): Promise<string> {
  await page.getByTestId(TESTID.constructionBegin).click();
  const dialog = page.getByTestId(TESTID.constructionBeginConfirm);
  await expect(dialog).toBeVisible();
  const tick = await dialog.getAttribute('data-tick-id');
  expect(tick, 'each opening carries its tickID').toBeTruthy();
  return tick ?? '';
}

test('Begin/Resume commits to one label, and Begin names what it would dispatch before dispatching nothing', async ({
  page,
  request,
}) => {
  const trapped = await trapDispatches(page);
  const sessionProbes: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('/construction/get-session-state/')) sessionProbes.push(r.url());
  });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toBeVisible({ timeout: 15_000 });

  // Sample the button through the load. It may say it is checking (disabled), but
  // it may never offer Begin or Resume while disabled-for-loading, and once it is
  // enabled it must hold ONE label — the old Begin→Resume flip fails here.
  // ONE atomic DOM read per sample: reading the label and the enabled state as two
  // separate calls can straddle the Checking→Begin transition and pair the old
  // label with the new state — a false flip the button never showed.
  const samples: { label: string; enabled: boolean }[] = [];
  for (let i = 0; i < 25; i++) {
    samples.push(
      await begin.evaluate((el) => ({
        label: (el as HTMLElement).innerText.trim(),
        enabled: !(el as HTMLButtonElement).disabled,
      }))
    );
    await page.waitForTimeout(100);
  }
  const enabledLabels = new Set(samples.filter((s) => s.enabled).map((s) => s.label));
  expect(enabledLabels.size, `labels once enabled: ${[...enabledLabels].join(' | ')}`).toBe(1);
  for (const s of samples.filter((x) => !x.enabled)) {
    expect(s.label).not.toMatch(COMMITTED_LABEL);
  }

  // The seeded project has never been run, so Begin — even though 23 activities
  // carry reconstructed attempts: the server's constructionStarted says so.
  await expect(begin).toHaveText(/Begin construction/);
  await expect(begin).toBeEnabled();
  const wire = (await (
    await request.get(`${BASE}/api/v1/system-design/get-project/archistrator`, {
      headers: { Accept: 'application/json' },
    })
  ).json()) as { constructionStarted?: boolean };
  expect(wire.constructionStarted).toBe(false);
  // The label came from that read alone: not one per-activity session probe.
  expect(sessionProbes, 'session probes on load').toEqual([]);

  await openDialog(page);
  const expected = await unrecordedActivityIds(request);
  expect(expected.length).toBeGreaterThan(0);
  for (const id of expected) {
    await expect(page.getByTestId(TESTID.constructionBeginCandidate(id))).toBeVisible();
  }
  const named = await page.evaluate(() =>
    Array.from(document.querySelectorAll('[data-testid^="construction-begin-candidate-"]'))
      .map((el) =>
        (el.getAttribute('data-testid') ?? '').slice('construction-begin-candidate-'.length)
      )
      .sort()
  );
  expect(named).toEqual(expected);

  await page.getByTestId(TESTID.constructionBeginConfirmCancel).click();
  await expect(page.getByTestId(TESTID.constructionBeginConfirm)).toBeHidden();
  await page.waitForTimeout(300);
  expect(trapped).toEqual([]);
});

test('Escape and a backdrop click close the confirm without dispatching', async ({ page }) => {
  const trapped = await trapDispatches(page);
  await openConsole(page);
  const dialog = page.getByTestId(TESTID.constructionBeginConfirm);

  const first = await openDialog(page);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();

  const second = await openDialog(page);
  // Each opening mints its own tickID: a key reused across openings would let a
  // later confirm be mistaken for a retry of an earlier one.
  expect(second).not.toBe(first);
  // The dialog's container, outside the paper, is its backdrop.
  await page.mouse.click(8, 8);
  await expect(dialog).toBeHidden();

  await page.waitForTimeout(300);
  expect(trapped).toEqual([]);
});

test('a double-click on dispatch sends exactly ONE request, carrying the opening’s tickID', async ({
  page,
}) => {
  const trapped = await trapDispatches(page);
  await openConsole(page);

  const tick = await openDialog(page);
  await page.getByTestId(TESTID.constructionBeginConfirmDispatch).dblclick();
  await page.waitForTimeout(800);

  expect(trapped, `trapped: ${JSON.stringify(trapped)}`).toHaveLength(1);
  expect(trapped[0]?.tickID).toBe(tick);
});

// Fix-B review M1. A dblclick is two separate input tasks, so React re-renders
// between them and the dispatch button's own disabled state stops the second —
// which left the console's in-flight ref unpinned (a mutant removing it passed).
// Three clicks inside ONE task land before React flushes anything: every one sees
// the dialog still open and un-pressed, so only the ref stands between them and
// three POSTs. (The server's one-pump-per-project rule is the correctness guarantee;
// this pins the client's UX debounce.)
test('three clicks in one task send exactly ONE request', async ({ page }) => {
  const trapped = await trapDispatches(page);
  await openConsole(page);

  const tick = await openDialog(page);
  await page.getByTestId(TESTID.constructionBeginConfirmDispatch).evaluate((el) => {
    const button = el as HTMLButtonElement;
    button.click();
    button.click();
    button.click();
  });
  await page.waitForTimeout(800);

  expect(trapped, `trapped: ${JSON.stringify(trapped)}`).toHaveLength(1);
  expect(trapped[0]?.tickID).toBe(tick);
});

// ---------------------------------------------------------------------------
// A FAILED dispatch (fix-B review M3, then the fix-C review's Important). The
// server starts the pump BEFORE it answers and can still answer 5xx after that,
// or the response can be dropped — so a 5xx or a network error says "outcome
// unknown", keeps polling, and Begin stays off until a project read newer than
// the failure answers. Only a 4xx is "rejected", with the server's own message.
//
// SAFETY: the dispatch route is FULFILLED (500 / 400) or ABORTED in the browser.
// Nothing reaches the server. Project READS may be answered 503 in the browser to
// hold the refresh back; otherwise they go through untouched (a GET).
// ---------------------------------------------------------------------------

const UNKNOWN_HEADLINE =
  'Outcome unknown — the pump may have started; the list will show it if it did.';

interface WireRow {
  ActivityID: string;
  classified: boolean;
  hasBuildEvidence: boolean;
  BuildStatus: number;
  /** The pump's own record is stored (a pickup writes it). */
  recorded?: boolean;
  /** When the pump picked the activity up (RFC 3339). */
  startedAt?: string;
}
interface WireProject {
  constructionStarted?: boolean;
  ActivityConstruction?: Record<string, WireRow>;
}

interface Harness {
  trapped: string[];
  /** Timestamps of every project read the page made. */
  reads: number[];
  /** Timestamps of every project read answered with the project (not held). */
  served: number[];
  /** The edit each served read carried (null: served as the server sent it), in
   *  serve order: how a case knows a read carrying its edit has been served. */
  servedEdits: (((wire: WireProject) => void) | null)[];
  /** While true, project reads are answered 503 in the browser. */
  holdReads: { on: boolean };
  /** When set, the real project read is served with this edit applied, in the
   *  browser. The read itself still goes to the server; nothing is written. */
  edit: { fn: ((wire: WireProject) => void) | null };
}

async function harness(
  page: Page,
  answer: (route: import('@playwright/test').Route) => Promise<void>
): Promise<Harness> {
  const h: Harness = {
    trapped: [],
    reads: [],
    served: [],
    servedEdits: [],
    holdReads: { on: false },
    edit: { fn: null },
  };
  await page.route('**/execute-next-activity/**', async (route) => {
    h.trapped.push(route.request().url());
    await answer(route);
  });
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    h.reads.push(Date.now());
    if (h.holdReads.on) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: '{}',
      });
      return;
    }
    const edit = h.edit.fn;
    if (edit === null) {
      await route.continue();
    } else {
      // A poll read can still be in flight when the test ends. Only "the page has
      // closed" is ignored here; any other failure still fails the test.
      try {
        const response = await route.fetch();
        const wire = (await response.json()) as WireProject;
        edit(wire);
        await route.fulfill({ response, json: wire });
      } catch (err) {
        if (page.isClosed() || /has been closed/.test(String(err))) return;
        throw err;
      }
    }
    h.served.push(Date.now());
    h.servedEdits.push(edit);
  });
  return h;
}

/** Edit one row of the read to what a pump pick-up looks like: classified, with
 *  build evidence, and BuildStatus 0 (BuildInConstruction), so the console
 *  probes its session. */
function inConstruction(activityId: string): (wire: WireProject) => void {
  return (wire) => {
    const row = wire.ActivityConstruction?.[activityId];
    if (row === undefined) throw new Error(`no row ${activityId} in the read`);
    row.classified = true;
    row.hasBuildEvidence = true;
    row.BuildStatus = 0;
    // A pickup is the pump's own record: stored, with its StartedAt. That record is
    // what the console probes a session for (owedWork.probeCandidatesFor).
    row.recorded = true;
    row.startedAt = PICKUP_AT;
  };
}

/** When the stubbed pump picked an activity up (any fixed instant will do). */
const PICKUP_AT = '2026-09-13T00:00:00Z';

/** Edit one row of the read to sit at the human code-review gate: BuildStatus 1
 *  (BuildInReview). Its row state is "awaiting you", which is work in flight. */
function inReview(activityId: string): (wire: WireProject) => void {
  return (wire) => {
    const row = wire.ActivityConstruction?.[activityId];
    if (row === undefined) throw new Error(`no row ${activityId} in the read`);
    row.classified = true;
    row.hasBuildEvidence = true;
    row.BuildStatus = 1;
  };
}

/** The server accepted the dispatch. Answered in the browser; nothing is sent. */
const SUCCESS = { status: 200, contentType: 'application/json', body: '{}' };

/**
 * A harness whose dispatch SUCCEEDS, with the pump's pickup of PICKED showing in
 * every read from the answer on. The pickup lands with the answer here, so these
 * cases pin the in-flight state. The gap BEFORE a first read shows the pickup is
 * pinned by the fix-H cases below, on the plain `harness` with a SUCCESS answer.
 */
async function harnessPickingUp(page: Page): Promise<Harness> {
  const holder: { h?: Harness } = {};
  const h = await harness(page, async (route) => {
    if (holder.h !== undefined) holder.h.edit.fn = inConstruction(PICKED);
    await route.fulfill(SUCCESS);
  });
  holder.h = h;
  return h;
}

async function dispatchOnce(page: Page): Promise<number> {
  await openDialog(page);
  const at = Date.now();
  await page.getByTestId(TESTID.constructionBeginConfirmDispatch).click();
  return at;
}

/** Sample the button for `ms`: it must read "Construction running…" and be disabled
 *  throughout (fix-E review I1: evidence reads as running, not as clickable). */
async function expectRunning(page: Page, ms: number): Promise<void> {
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Construction running…/, { timeout: 10_000 });
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const s = await begin.evaluate((el) => ({
      label: (el as HTMLElement).innerText.trim(),
      enabled: !(el as HTMLButtonElement).disabled,
    }));
    expect(s.enabled, `Begin enabled while the pump is evidenced ("${s.label}")`).toBe(false);
    expect(s.label).toMatch(/Construction running…/);
    await page.waitForTimeout(150);
  }
}

/** Sample the button for `ms`: it must never be enabled, nor claim Begin/Resume. */
async function expectBeginHeldOff(page: Page, ms: number): Promise<void> {
  const begin = page.getByTestId(TESTID.constructionBegin);
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const s = await begin.evaluate((el) => ({
      label: (el as HTMLElement).innerText.trim(),
      enabled: !(el as HTMLButtonElement).disabled,
    }));
    expect(s.enabled, `Begin enabled while the refresh is held ("${s.label}")`).toBe(false);
    expect(s.label).not.toMatch(COMMITTED_LABEL);
    await page.waitForTimeout(150);
  }
}

test('a 500 says the outcome is unknown, and a newer read ALONE does not lift Begin’s hold', async ({
  page,
}) => {
  const h = await harness(page, (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ code: 'internal', error: 'decode pump decision' }),
    })
  );
  await openConsole(page);
  h.holdReads.on = true;
  const sentAt = await dispatchOnce(page);

  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-outcome', 'unknown');
  await expect(alert).toContainText(UNKNOWN_HEADLINE);
  await expect(alert).not.toContainText(/again|retry/i);
  // Severity is error, pinned by MUI's own class.
  await expect(alert).toHaveClass(/MuiAlert-colorError/);
  // No read has answered since: Begin stays off, and keeps polling.
  await expectBeginHeldOff(page, 2_500);
  // Dismissing the alert hides it WITHOUT lifting the gate.
  await alert.getByRole('button', { name: 'Close' }).click();
  await expect(alert).toBeHidden();
  await expectBeginHeldOff(page, 1_000);
  expect(h.reads.filter((t) => t > sentAt).length, 'the project is re-read').toBeGreaterThan(1);

  // The refreshed project answers. That is NOT enough (fix-D review I3): it says
  // "not started", but the pump may not have stored its StartedAt yet. Newer reads
  // arrive, and Begin stays held. The evidence and the expiry are pinned below.
  h.holdReads.on = false;
  const servedBefore = h.served.length;
  await expect
    .poll(() => h.served.length, {
      timeout: 10_000,
      message: 'newer reads answered',
    })
    .toBeGreaterThan(servedBefore + 1);
  await expectBeginHeldOff(page, 2_500);
  expect(h.trapped).toHaveLength(1);
});

test('a dropped response (network error) is an unknown outcome too', async ({ page }) => {
  const h = await harness(page, (route) => route.abort('failed'));
  await openConsole(page);
  h.holdReads.on = true;
  const sentAt = await dispatchOnce(page);

  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-outcome', 'unknown');
  await expect(alert).toContainText(UNKNOWN_HEADLINE);
  await expectBeginHeldOff(page, 1_500);
  expect(h.reads.filter((t) => t > sentAt).length, 'the project is re-read').toBeGreaterThan(0);

  // A newer read alone does not lift the hold (fix-D review I3).
  h.holdReads.on = false;
  const servedBefore = h.served.length;
  await expect.poll(() => h.served.length, { timeout: 10_000 }).toBeGreaterThan(servedBefore);
  await expectBeginHeldOff(page, 1_500);
  await expect(alert).toHaveAttribute('data-hold', 'held');
  expect(h.trapped).toHaveLength(1);
});

test('a 400 is a rejection: the server’s message, Begin back at once, and no polling', async ({
  page,
}) => {
  const h = await harness(page, (route) =>
    route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ code: 'contract_misuse', error: 'empty tickId' }),
    })
  );
  await openConsole(page);
  const sentAt = await dispatchOnce(page);

  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-outcome', 'rejected');
  await expect(alert).toContainText('Construction dispatch rejected: empty tickId.');
  await expect(alert).toContainText('nothing was started');
  await expect(alert).not.toContainText('Outcome unknown');
  await expect(alert).toHaveClass(/MuiAlert-colorError/);
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 5_000 });
  await expect(begin).toBeEnabled();
  // The failure refreshed the project even though nothing polls after a rejection.
  const readsAfter = h.reads.filter((t) => t > sentAt).length;
  expect(readsAfter, 'the project is re-read on a failure').toBeGreaterThan(0);
  await page.waitForTimeout(3_500);
  expect(h.reads.filter((t) => t > sentAt).length, 'a rejection does not keep polling').toBe(
    readsAfter
  );

  // The dismiss works.
  await alert.getByRole('button', { name: 'Close' }).click();
  await expect(alert).toBeHidden();
  expect(h.trapped).toHaveLength(1);
});

// ---------------------------------------------------------------------------
// Fix round E: dispatch outcomes that cannot lie (the fix-D review).
//
// SAFETY: the same as above. Dispatches are answered in the browser (a 500, a
// 400, or held and then answered). Session probes for one activity are answered
// in the browser. Reads go to the server as GETs, and are at most edited in the
// browser.
// ---------------------------------------------------------------------------

const SERVER_500 = {
  status: 500,
  contentType: 'application/json',
  body: JSON.stringify({ code: 'internal', error: 'decode pump decision' }),
};
/** A bare status, the way a proxy's 502/503/504 arrives: Content-Length 0. */
const EMPTY_BODY = { headers: { 'content-length': '0' }, body: '' };
/** A classified row with build evidence, to be "picked up" in the read. */
const PICKED = 'C-billing-engine';

test('I2: an EMPTY-body 400 is a rejection, not a success', async ({ page }) => {
  // openapi-fetch returns `error: undefined` for an empty body. The status decides.
  const h = await harness(page, (route) => route.fulfill({ status: 400, ...EMPTY_BODY }));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-outcome', 'rejected');
  await expect(alert).toContainText(
    'Construction dispatch rejected: request failed with status 400.'
  );
  await expect(page.getByTestId(TESTID.constructionBegin)).toHaveText(/Begin construction/);
  expect(h.trapped).toHaveLength(1);
});

test('I2: an EMPTY-body 500 (a proxy’s bare 5xx) is an unknown outcome, not a success', async ({
  page,
}) => {
  const h = await harness(page, (route) => route.fulfill({ status: 500, ...EMPTY_BODY }));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-outcome', 'unknown');
  await expect(alert).toHaveAttribute('data-hold', 'held');
  await expect(alert).toContainText(UNKNOWN_HEADLINE);
  await expect(alert).toContainText('request failed with status 500');
  await expectBeginHeldOff(page, 1_000);
  expect(h.trapped).toHaveLength(1);
});

test('I1: a 500 answered after 33s finds the poll still running, and newer reads keep arriving', async ({
  page,
  dispatchGuard,
}) => {
  // The browser clock is faked, so 33s pass at once while the dispatch is held
  // unanswered. The 30s progress watchdog used to stop the poll right here, and
  // the unknown outcome never turned it back on: Begin sat on "Checking…" forever.
  // The dispatch is held through the guard, so a failure here aborts it (never
  // lets it out) in teardown.
  await page.clock.install();
  const hold = dispatchGuard.hold();
  const h = await harness(page, (route) => hold.handle(route, () => route.fulfill(SERVER_500)));
  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => h.trapped.length).toBe(1);

  await page.clock.fastForward(33_000);
  const whilePending = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 8_000,
      message: 'reads while the dispatch is pending',
    })
    .toBeGreaterThan(whilePending + 1);

  hold.release();
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-outcome', 'unknown', {
    timeout: 10_000,
  });

  // Another 31s: past the progress window again, but inside the 60s hold. Only a
  // read can bring the evidence that lifts the hold, so the poll must go on.
  await page.clock.fastForward(31_000);
  const whileHeld = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 8_000,
      message: 'reads while Begin is held',
    })
    .toBeGreaterThan(whileHeld + 1);
  await expectBeginHeldOff(page, 1_000);
});

test('I3: the hold expiring with no sign of the pump brings Begin back, asking "Begin again?"', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });
  // Dismissed during the hold. The expiry is news, so the alert comes back for it.
  await alert.getByRole('button', { name: 'Close' }).click();
  await expect(alert).toBeHidden();

  // 50s in: reads have kept answering "not started", and Begin is still held.
  await page.clock.fastForward(50_000);
  await expectBeginHeldOff(page, 1_000);
  expect(h.served.length, 'newer reads answered during the hold').toBeGreaterThan(1);

  // Past 60s with no evidence.
  await page.clock.fastForward(12_000);
  await expect(alert).toBeVisible();
  await expect(alert).toHaveAttribute('data-hold', 'expired');
  await expect(alert).toContainText('No sign the pump started. Begin again?');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/);
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('I3: evidence lifts the hold: a read says construction started with nothing in flight, so the failure leaves and Resume is offered', async ({
  page,
}) => {
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });
  await expectBeginHeldOff(page, 1_500);

  // The pump stores its StartedAt, and the next read says so: evidence. But no
  // activity is in flight and no session is live, and the STATE decides the label
  // (fix-F review, root-cause ruling). The failure has done its job and leaves
  // memory, so its alert goes, and the button is the read's own: Resume.
  h.edit.fn = (wire) => {
    wire.constructionStarted = true;
  };
  await expect(alert).toBeHidden({ timeout: 10_000 });
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/);
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('R2: a live session while constructionStarted is false reads as running, at 31s and at 90s', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  const live = { on: false };
  const probes: string[] = [];
  await page.route(`**/construction/get-session-state/archistrator/${PICKED}**`, async (route) => {
    probes.push(route.request().url());
    await route.fulfill(
      live.on
        ? {
            status: 200,
            json: { projectId: 'archistrator', activityId: PICKED, stage: 2 },
          }
        : { status: 404, json: { code: 'not_found', error: 'no session' } }
    );
  });
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });

  // The pump picks up PICKED: the read shows it in construction, and its session
  // is live (pipelineRunning). constructionStarted stays false, so the session is
  // the only evidence.
  live.on = true;
  h.edit.fn = inConstruction(PICKED);
  await expect(alert).toHaveAttribute('data-hold', 'evidenced', {
    timeout: 10_000,
  });
  // The read still says constructionStarted: false. The label must not fall back
  // to it: that was an ENABLED "Begin construction" beside a live session.
  await expectRunning(page, 2_000);
  expect(probes.length, 'the session was probed').toBeGreaterThan(0);

  // 31s with no integration: the no-progress watchdog fires. It used to hand the
  // label back to the read, an enabled "Begin construction" (fix-F review R2). It
  // may only slow the poll now; the state still shows the pump, so it is running.
  await page.clock.fastForward(31_000);
  await expectRunning(page, 1_500);
  // 90s: past the unknown-outcome hold too. Evidence ended the hold, so it never
  // expires, and the state still decides.
  await page.clock.fastForward(59_000);
  await expectRunning(page, 1_500);
  await expect(alert).toHaveAttribute('data-hold', 'evidenced');
  // The poll never stopped: only a read can say the work has ended.
  const before = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 12_000,
      message: 'reads while work is in flight',
    })
    .toBeGreaterThan(before);
  expect(h.trapped).toHaveLength(1);
});

// M4 (fix-D review) pinned that a failed dispatch re-reads a session probe that
// had settled as "no session". Under fix G that case cannot be reached: the rows
// the console probes are the ones the pump started and has not finished (the owed
// set's probes, since the tasks-lens merge), and such a row in construction is
// work in flight, so Begin reads "Construction running…" and cannot be pressed
// beside it. What stays reachable, and is pinned here: the ROW STATE alone holds
// Begin off. The session probe has settled as absent (a 404), so no session term
// helps it. (The refresh still invalidates the session probes; the fix-G report
// records that this is no longer reachable from Begin.)
test('M4 (fix G): a row in construction holds Begin off on its own, with its session probe settled as absent', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.abort());
  h.edit.fn = inConstruction(PICKED);
  const probes: number[] = [];
  await page.route(`**/construction/get-session-state/archistrator/${PICKED}**`, async (route) => {
    probes.push(Date.now());
    await route.fulfill({
      status: 404,
      json: { code: 'not_found', error: 'no session' },
    });
  });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeVisible({
    timeout: 15_000,
  });
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(0);
  await expectRunning(page, 1_500);
  await page.clock.fastForward(31_000);
  await expectRunning(page, 1_500);
  expect(h.trapped).toEqual([]);
});

// Final review minor, pinned end to end (inflight-residual round, mutant T5): the
// confirm dialog was opened against an ENABLED Begin, and work shows up in flight
// while it is open. The dialog's own dispatch button knows nothing of that, so the
// console re-checks Begin at confirm time (onBegin → beginConfirmAllowed). Without
// the re-check this press would be a second pump one click away; with it, nothing
// is sent. Every dispatch is trapped here, and aborted, so none can leave the page.
test('T5: work goes in flight while the confirm is open, so confirming sends nothing', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.abort());
  await page.route(`**/construction/get-session-state/archistrator/${PICKED}**`, async (route) => {
    await route.fulfill(NO_SESSION);
  });
  await openConsole(page);
  const begin = page.getByTestId(TESTID.constructionBegin);
  await openDialog(page);

  // The state flips while the dialog is open: the next read shows a row in
  // construction. Nothing is in flight before it, so the read refreshes on the 10s
  // freshness cadence; the clock is moved past it.
  const edit = inConstruction(PICKED);
  h.edit.fn = edit;
  await page.clock.fastForward(11_000);
  await expect.poll(() => h.servedEdits.includes(edit), { timeout: 10_000 }).toBe(true);
  // The control behind the dialog is off now; the dialog is still open, and its
  // dispatch button is still enabled.
  await expect(begin).toHaveText(/Construction running…/, { timeout: 10_000 });
  await expect(begin).toBeDisabled();
  const dispatch = page.getByTestId(TESTID.constructionBeginConfirmDispatch);
  await expect(dispatch).toBeEnabled();

  await dispatch.click();
  await page.waitForTimeout(800);
  expect(h.trapped, `trapped: ${JSON.stringify(h.trapped)}`).toEqual([]);
  await expect(begin).toHaveText(/Construction running…/);
});

// Tasks-lens merge round: the session term is reachable again. The console probes
// every activity the pump started and has not finished (the owed set's probes). One
// picked up a moment ago has its stored record and StartedAt but no build evidence
// yet, so its row reads NOT STARTED, and only its live session says the pump runs.
test('a fresh pickup with no build evidence yet: its live session alone holds Begin off (merge round)', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.abort());
  h.edit.fn = (wire) => {
    const row = wire.ActivityConstruction?.[PICKED];
    if (row === undefined) throw new Error(`no row ${PICKED} in the read`);
    row.classified = true;
    row.recorded = true;
    row.startedAt = PICKUP_AT;
    row.hasBuildEvidence = false;
  };
  // pipelineRunning (2) while the pump works; exited (6) once the workflow ends — a
  // finished session reports its terminal stage (a later 404 keeps the last one).
  const live = { stage: 2 };
  const probes: number[] = [];
  await page.route(`**/construction/get-session-state/archistrator/${PICKED}**`, async (route) => {
    probes.push(Date.now());
    await route.fulfill({
      status: 200,
      json: { projectId: 'archistrator', activityId: PICKED, stage: live.stage },
    });
  });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toBeVisible({ timeout: 15_000 });
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(0);
  await expectRunning(page, 1_500);
  // The session ends. Nothing is in flight by state now, so Begin is offered.
  live.stage = 6;
  const asked = probes.length;
  await page.clock.fastForward(4_000);
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(asked);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toEqual([]);
});

// Tasks merge review I1: until a fresh pickup's probe ANSWERS, nobody knows whether
// its pump runs. A probe still pending, or one that failed without answering, is
// in flight; only an answer ("no session") gives Begin back.
function freshPickup(wire: WireProject): void {
  const row = wire.ActivityConstruction?.[PICKED];
  if (row === undefined) throw new Error(`no row ${PICKED} in the read`);
  row.classified = true;
  row.recorded = true;
  row.startedAt = PICKUP_AT;
  row.hasBuildEvidence = false;
}

const NO_SESSION = { status: 404, json: { code: 'not_found', error: 'no session' } };

test('I1 (tasks merge review): a fresh pickup whose probe is still PENDING holds Begin off until it answers', async ({
  page,
  dispatchGuard,
}) => {
  const h = await harness(page, (route) => route.abort());
  h.edit.fn = freshPickup;
  const hold = dispatchGuard.hold();
  const probes: number[] = [];
  await page.route(`**/construction/get-session-state/archistrator/${PICKED}**`, (route) => {
    probes.push(Date.now());
    return hold.handle(route, () => route.fulfill(NO_SESSION));
  });
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeVisible({ timeout: 15_000 });
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(0);
  // The probe is on the wire and says nothing yet: running, never an enabled Begin.
  await expectRunning(page, 3_000);
  // It answers: no session. Nothing is in flight, so Begin is offered.
  hold.release();
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toEqual([]);
});

/**
 * Sample one copy of the Begin label for `ms`: while a probe keeps failing, the
 * true state is unknown, so it reads "Checking construction…" and is disabled —
 * never "Construction running…" (a pump nobody has seen) and never an enabled
 * Begin or Resume (tasks merge-2 ruling (a), fix I).
 */
async function expectChecking(page: Page, testId: string, ms: number): Promise<void> {
  const copy = page.getByTestId(testId);
  await expect(copy).toHaveText(/Checking construction…/, { timeout: 10_000 });
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const s = await copy.evaluate((el) => ({
      label: (el as HTMLElement).innerText.trim(),
      enabled: !(el as HTMLButtonElement).disabled,
    }));
    expect(s.enabled, `"${s.label}" enabled while a probe keeps failing`).toBe(false);
    expect(s.label).toMatch(/Checking construction…/);
    await page.waitForTimeout(150);
  }
}

/** A fresh pickup whose session probe answers 500 until `answer.failing` is false. */
async function failingProbe(page: Page): Promise<{ answer: { failing: boolean }; probes: number[] }> {
  const answer = { failing: true };
  const probes: number[] = [];
  await page.route(`**/construction/get-session-state/archistrator/${PICKED}**`, async (route) => {
    probes.push(Date.now());
    await route.fulfill(
      answer.failing ? { status: 500, json: { error: 'session store unavailable' } } : NO_SESSION
    );
  });
  return { answer, probes };
}

test('I1 (tasks merge review): a fresh pickup whose probe answers 500 holds Begin off through the re-asks, until one answers', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.abort());
  h.edit.fn = freshPickup;
  const { answer, probes } = await failingProbe(page);
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeVisible({ timeout: 15_000 });
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(0);
  // A probe that fails is no answer: the button checks, disabled (fix I). It used
  // to read "Construction running…", claiming a pump nobody had seen.
  await expectChecking(page, TESTID.constructionBegin, 1_500);
  // Through the errored probe's backoff and its re-ask, still checking.
  const asked = probes.length;
  await page.clock.fastForward(12_000);
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(asked);
  await expectChecking(page, TESTID.constructionBegin, 1_500);
  // A re-ask answers: no session. Begin comes back.
  answer.failing = false;
  await page.clock.fastForward(25_000);
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 15_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toEqual([]);
});

// Both copies of the label (fix I): the header's button and the tasks lens's own
// Begin/Resume in its "nothing needs you" state, which reads the same control.
test('fix I: while a probe keeps failing, BOTH copies of the label read "Checking construction…", disabled, until one answers', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.abort());
  h.edit.fn = freshPickup;
  const { answer, probes } = await failingProbe(page);
  await gotoApp(page, '/project/archistrator/construction?lens=tasks');
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeVisible({ timeout: 15_000 });
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(0);
  await expect(page.getByTestId(TESTID.constructionTasksResume)).toBeVisible({ timeout: 10_000 });
  await expectChecking(page, TESTID.constructionBegin, 1_500);
  await expectChecking(page, TESTID.constructionTasksResume, 1_500);
  // Through a re-ask that fails again: both still check.
  const asked = probes.length;
  await page.clock.fastForward(12_000);
  await expect.poll(() => probes.length, { timeout: 10_000 }).toBeGreaterThan(asked);
  await expectChecking(page, TESTID.constructionBegin, 1_000);
  await expectChecking(page, TESTID.constructionTasksResume, 1_000);
  // A re-ask answers: no session. Both copies hand the decision back to the read.
  answer.failing = false;
  await page.clock.fastForward(25_000);
  for (const copy of [TESTID.constructionBegin, TESTID.constructionTasksResume]) {
    await expect(page.getByTestId(copy)).toHaveText(COMMITTED_LABEL, { timeout: 15_000 });
    await expect(page.getByTestId(copy)).toBeEnabled();
  }
  expect(h.trapped).toEqual([]);
});

// ---------------------------------------------------------------------------
// Fix round F: a remount must not drop the hold (fix-E review I2).
//
// The console is left IN-APP (the router follows a history change, the document
// stays), so module memory survives exactly as it does for a real operator who
// clicks away to the design pages and back. SAFETY: as above; the dispatch is
// answered in the browser, and the design page's reads are GETs.
// ---------------------------------------------------------------------------

const DESIGN_PATH = '/project/archistrator/design/system';

async function markDocument(page: Page): Promise<void> {
  await page.evaluate(() => {
    (window as unknown as { __sameDocument?: boolean }).__sameDocument = true;
  });
}

async function expectSameDocument(page: Page): Promise<void> {
  expect(
    await page.evaluate(
      () => (window as unknown as { __sameDocument?: boolean }).__sameDocument === true
    ),
    'no reload: the trip happened in-app'
  ).toBe(true);
}

/** Leave the console for the design page, in-app, and wait until it is unmounted. */
async function awayToDesign(page: Page): Promise<void> {
  await page.evaluate((to) => {
    window.history.pushState(null, '', to);
    window.dispatchEvent(new PopStateEvent('popstate', { state: null }));
  }, DESIGN_PATH);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible({
    timeout: 15_000,
  });
  await expect(page.getByTestId(TESTID.constructionBegin)).toHaveCount(0);
}

/** Come back to the console with the browser's Back (a popstate, in-app). */
async function backToConsole(page: Page): Promise<void> {
  await page.evaluate(() => {
    window.history.back();
  });
  await expect(page).toHaveURL(/\/project\/archistrator\/construction/);
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeVisible({
    timeout: 15_000,
  });
}

test('I2: after a 500, a trip to /design and back keeps Begin held and the alert shown, and the 60s expiry still runs', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });
  // The mutation has SETTLED before the console is left (fix-F review): its refresh
  // is done, so the label is the hold's "Checking…", not a pending dispatch's
  // "running". Nothing is pending on return, and the state shows nothing in flight,
  // so only the failure in module memory can restart the remounted console's poll.
  await expect(page.getByTestId(TESTID.constructionBegin)).toHaveText(/Checking construction…/);

  await markDocument(page);
  await awayToDesign(page);
  await page.clock.fastForward(5_000);
  await backToConsole(page);
  await expectSameDocument(page);

  // Still held, still said.
  await expect(alert).toBeVisible();
  await expect(alert).toHaveAttribute('data-hold', 'held');
  await expect(alert).toContainText(UNKNOWN_HEADLINE);
  await expectBeginHeldOff(page, 1_500);
  // The remounted console polls again: only a read can bring the evidence.
  const readsBack = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 8_000,
      message: 'reads after the remount',
    })
    .toBeGreaterThan(readsBack + 1);

  // The hold runs from the FAILURE, not from the remount: 5s away + 57s here.
  await page.clock.fastForward(57_000);
  await expect(alert).toHaveAttribute('data-hold', 'expired', {
    timeout: 10_000,
  });
  await expect(alert).toContainText('No sign the pump started. Begin again?');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/);
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('I2: a hold that runs out while the console is away has expired when it comes back', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });
  // Dismissed before leaving. The expiry is news, so it comes back for it.
  await alert.getByRole('button', { name: 'Close' }).click();
  await expect(alert).toBeHidden();

  await awayToDesign(page);
  await page.clock.fastForward(62_000);
  await backToConsole(page);

  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-hold', 'expired');
  await expect(alert).toContainText('No sign the pump started. Begin again?');
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('I2: a 500 that lands while the console is away is still recorded, and a pending dispatch reads as running after a remount', async ({
  page,
  dispatchGuard,
}) => {
  const hold = dispatchGuard.hold();
  const h = await harness(page, (route) => hold.handle(route, () => route.fulfill(SERVER_500)));
  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => h.trapped.length).toBe(1);

  // Away and back while the dispatch is unanswered: the new console must not offer
  // Begin, because the first dispatch is still in flight.
  await awayToDesign(page);
  await backToConsole(page);
  await expectRunning(page, 1_000);

  // Away again, and the 500 lands while the console is unmounted.
  await awayToDesign(page);
  hold.release();
  await page.waitForTimeout(500);
  await backToConsole(page);

  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toBeVisible({ timeout: 10_000 });
  await expect(alert).toHaveAttribute('data-outcome', 'unknown');
  await expect(alert).toHaveAttribute('data-hold', 'held');
  await expectBeginHeldOff(page, 1_500);
  const readsBack = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 8_000,
      message: 'reads after the remount',
    })
    .toBeGreaterThan(readsBack + 1);
  expect(h.trapped).toHaveLength(1);
});

test('I2 + I1: after a remount, evidence of work in flight still reads as running', async ({
  page,
}) => {
  // A remounted console has no progress of its own to go by, and does not need
  // any: evidence arriving after the trip is an activity in flight, and the state
  // says "running", not the read's clickable label.
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });

  await awayToDesign(page);
  await backToConsole(page);
  await expect(alert).toHaveAttribute('data-hold', 'held');

  h.edit.fn = inConstruction(PICKED);
  await expect(alert).toHaveAttribute('data-hold', 'evidenced', {
    timeout: 10_000,
  });
  await expectRunning(page, 3_000);
  expect(h.trapped).toHaveLength(1);
});

// ---------------------------------------------------------------------------
// Fix round G: the label follows STATE, not timers (fix-F review, root-cause
// ruling). "Construction running…", disabled, shows whenever an activity's row
// state is running or awaiting a human, or a session is live, on every path. The
// 30s no-progress watchdog only slows the poll. Begin/Resume are enabled only when
// nothing is in flight and no hold stands.
//
// SAFETY: as above. Every dispatch is answered in the browser (a 500 or a 200);
// reads go to the server as GETs, at most edited in the browser.
// ---------------------------------------------------------------------------

test('R1b: evidence that arrives at 35s still reads as running past 60s, and at 95s', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });

  // 35s held, past the watchdog's 30s. Then the pump shows itself: it picks PICKED
  // up and stores its StartedAt.
  await page.clock.fastForward(35_000);
  await expectBeginHeldOff(page, 1_000);
  h.edit.fn = (wire) => {
    inConstruction(PICKED)(wire);
    wire.constructionStarted = true;
  };
  await expect(alert).toHaveAttribute('data-hold', 'evidenced', {
    timeout: 10_000,
  });
  await expectRunning(page, 1_500);

  // 61s since the failure: past the watchdog and the hold. It used to read
  // "running" for about 1.5s here and then offer an enabled Resume.
  await page.clock.fastForward(26_000);
  await expectRunning(page, 2_000);
  await expect(alert).toHaveAttribute('data-hold', 'evidenced');
  await page.clock.fastForward(34_000);
  await expectRunning(page, 2_000);
  const before = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 12_000,
      message: 'reads while work is in flight',
    })
    .toBeGreaterThan(before);
  expect(h.trapped).toHaveLength(1);
});

test('a success with an activity running reads as running at 45s; with nothing in flight, Resume is offered', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harnessPickingUp(page);
  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => h.trapped.length).toBe(1);
  await expectRunning(page, 1_500);

  // 45s with no integration: the watchdog has fired. It only slowed the poll.
  await page.clock.fastForward(45_000);
  await expectRunning(page, 2_000);
  await expect(page.getByTestId(TESTID.constructionBeginError)).toHaveCount(0);

  // The work ends: nothing in flight by state, and the pump did start. The slow
  // poll sees it, and the button is the read's own.
  h.edit.fn = (wire) => {
    wire.constructionStarted = true;
  };
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/, { timeout: 12_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('a success answered after 33s pending still polls fast for the pickup', async ({
  page,
  dispatchGuard,
}) => {
  // The watchdog never runs while a dispatch is pending, but it times from Begin. A
  // success answered past its 30s window used to leave the fast poll to stop at
  // once, and with nothing yet in flight there was no slow poll either: the pickup
  // in a later read was never seen. The answer re-arms the window.
  await page.clock.install();
  const hold = dispatchGuard.hold();
  const h = await harness(page, (route) => hold.handle(route, () => route.fulfill(SUCCESS)));
  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => h.trapped.length).toBe(1);
  await page.clock.fastForward(33_000);
  hold.release();
  // The answer's refresh reads "nothing in flight". Wait past several watchdog
  // ticks (1.5s each): without the re-arm the first tick ends the fast poll, and
  // with nothing in flight nothing polls at all. With it, reads keep coming.
  await page.waitForTimeout(4_500);
  const mark = Date.now();
  await page.waitForTimeout(3_500);
  expect(
    h.reads.filter((t) => t > mark).length,
    'reads after the answer, past the watchdog’s first ticks'
  ).toBeGreaterThan(0);
  // The pump then picks PICKED up, and only a later read can show it.
  h.edit.fn = inConstruction(PICKED);
  await expectRunning(page, 1_000);
  expect(h.trapped).toHaveLength(1);
});

test('an evidenced failure leaves memory once nothing is in flight: the alert goes, and a remount does not bring it back', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  await openConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });

  // Evidence, with work in flight: the failure stays while the pump runs.
  h.edit.fn = inConstruction(PICKED);
  await expect(alert).toHaveAttribute('data-hold', 'evidenced', {
    timeout: 10_000,
  });
  await expectRunning(page, 1_000);
  await expect(alert).toBeVisible();

  // The work ends. The failure has done its job: it leaves memory.
  h.edit.fn = (wire) => {
    wire.constructionStarted = true;
  };
  await expect(alert).toBeHidden({ timeout: 10_000 });
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/);
  await expect(begin).toBeEnabled();

  // A remount finds nothing in memory: no flash of the old alert, and no hold.
  await markDocument(page);
  await awayToDesign(page);
  await backToConsole(page);
  await expectSameDocument(page);
  const until = Date.now() + 1_500;
  while (Date.now() < until) {
    expect(await alert.count(), 'the stale alert came back on remount').toBe(0);
    await page.waitForTimeout(150);
  }
  await expect(begin).toHaveText(/Resume construction/);
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('a remount after a SUCCESSFUL dispatch still reads as running while work is in flight, and keeps polling', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harnessPickingUp(page);
  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => h.trapped.length).toBe(1);
  await expectRunning(page, 1_000);

  await markDocument(page);
  await awayToDesign(page);
  await backToConsole(page);
  await expectSameDocument(page);
  // The remounted console has no cascade of its own, no pending dispatch and no
  // failure in memory. The state alone says running, and the poll goes on.
  await expectRunning(page, 1_500);
  await page.clock.fastForward(31_000);
  await expectRunning(page, 1_500);
  const before = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 12_000,
      message: 'reads after the remount',
    })
    .toBeGreaterThan(before);

  // And when the work ends, the remounted console sees it.
  h.edit.fn = (wire) => {
    wire.constructionStarted = true;
  };
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/, { timeout: 12_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

// ---------------------------------------------------------------------------
// Fix round H: after a SUCCESS, the same bounded hold covers the gap before the
// pickup shows (fix-G report, concern 1). A success says the pump was started, not
// that a read shows it yet; until one does, nothing is in flight by state. So
// Begin/Resume stay "Construction running…" until a read requested after the
// success shows work in flight, or 60s pass with no sign of it. (Until B1's
// server-side "pump open" read.)
//
// SAFETY: as above. The dispatch is answered 200 in the browser; nothing is sent.
// ---------------------------------------------------------------------------

/** Wait until the success has SETTLED: its refresh read and one more are served. */
async function awaitSettledSuccess(h: Harness): Promise<void> {
  await expect.poll(() => h.trapped.length).toBe(1);
  const at = h.served.length;
  await expect
    .poll(() => h.served.length, { timeout: 10_000, message: 'reads after the answer' })
    .toBeGreaterThan(at + 1);
}

/**
 * Serve `fn` as the read's edit, and wait until a read CARRYING it has been served.
 * Reads land every ~2.5-3s (the 1.5s poll plus the fetch), so a case that swapped
 * its edits faster than that could replace the pickup before any read showed it,
 * and never exercise the pickup at all (measured while pinning fix H).
 */
async function serveEdit(h: Harness, fn: (wire: WireProject) => void): Promise<void> {
  h.edit.fn = fn;
  await expect
    .poll(() => h.servedEdits.includes(fn), {
      timeout: 10_000,
      message: 'a read carrying the edit',
    })
    .toBe(true);
}

test('a success, then a gap before the pickup: the button reads running throughout, and past the pickup', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SUCCESS));
  await openConsole(page);
  await dispatchOnce(page);
  // The answer's refresh and the polls after it read NOTHING in flight: the gap.
  // Nothing is pending by now, and the state shows no work, so only the hold says
  // "running". It used to be an enabled "Begin construction" here.
  await awaitSettledSuccess(h);
  await expectRunning(page, 3_000);

  // 32s into the gap: past the watchdog's 30s. Still held, and still reading fast.
  await page.clock.fastForward(32_000);
  await expectRunning(page, 1_500);
  const before = h.reads.length;
  await expect
    .poll(() => h.reads.length, { timeout: 6_000, message: 'reads during the gap' })
    .toBeGreaterThan(before + 1);

  // The pickup shows. Running still, now by state: the pickup ended the hold.
  await serveEdit(h, inConstruction(PICKED));
  await expectRunning(page, 2_000);

  // The work ends, still inside the success's 60s: nothing in flight, and no hold
  // standing, because the pickup already ended it. Resume at once, not at 60s.
  h.edit.fn = (wire) => {
    wire.constructionStarted = true;
  };
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/, { timeout: 12_000 });
  await expect(begin).toBeEnabled();
  await expect(page.getByTestId(TESTID.constructionBeginError)).toHaveCount(0);
  expect(h.trapped).toHaveLength(1);
});

test('a success whose pickup never shows holds Begin for 60s, then gives it back', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SUCCESS));
  await openConsole(page);
  await dispatchOnce(page);
  await awaitSettledSuccess(h);
  await expectRunning(page, 1_500);

  // The clock keeps real time between jumps, so the jumps leave margin: 50s here
  // (plus the few real seconds above) is short of 60s, and 12s more is past it.
  await page.clock.fastForward(50_000);
  await expectRunning(page, 1_500);
  // Past 60s since the success, with no read showing the pickup: the hold runs out.
  await page.clock.fastForward(12_000);
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
  // A success is not a failure: nothing to alert about.
  await expect(page.getByTestId(TESTID.constructionBeginError)).toHaveCount(0);
  expect(h.trapped).toHaveLength(1);
});

test('a trip to /design and back during the gap keeps the hold, and its 60s still run from the success', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SUCCESS));
  await openConsole(page);
  await dispatchOnce(page);
  // Settled before leaving: nothing is pending on return, and the state shows
  // nothing in flight, so only the success in module memory can hold Begin.
  await awaitSettledSuccess(h);
  await expectRunning(page, 1_000);

  await markDocument(page);
  await awayToDesign(page);
  await page.clock.fastForward(20_000);
  await backToConsole(page);
  await expectSameDocument(page);

  await expectRunning(page, 1_500);
  // The remounted console polls for the pickup.
  const back = h.reads.length;
  await expect
    .poll(() => h.reads.length, { timeout: 8_000, message: 'reads after the remount' })
    .toBeGreaterThan(back + 1);

  // 20s away + 42s here: past 60s from the SUCCESS, not from the remount.
  await page.clock.fastForward(42_000);
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('a success picked up and finished well inside 60s gives Resume back with the next read, not at 60s', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SUCCESS));
  await openConsole(page);
  await dispatchOnce(page);
  await awaitSettledSuccess(h);

  // The pickup shows: the hold has done its job, and the state decides from here.
  await serveEdit(h, inConstruction(PICKED));
  await expectRunning(page, 1_500);

  // The work ends seconds after the success, far inside its 60s. Nothing is in
  // flight, and the pickup already ended the hold (its record left memory), so
  // Resume comes back with the next read. Kept, the record would hold it to 60s.
  h.edit.fn = (wire) => {
    wire.constructionStarted = true;
  };
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

test('with no dispatch at all, an activity in review reads as running, past 30s, until it leaves review', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.abort());
  h.edit.fn = inReview(PICKED);
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toBeVisible({ timeout: 15_000 });
  // In review is work in flight (the one rowIsInFlight): it is owed a human
  // decision, and the pump is not idle while it waits.
  await expectRunning(page, 1_500);
  await page.clock.fastForward(31_000);
  await expectRunning(page, 1_500);
  // No Begin armed a cascade here; the slow in-flight poll still re-reads.
  const before = h.reads.length;
  await expect
    .poll(() => h.reads.length, {
      timeout: 12_000,
      message: 'reads while in review',
    })
    .toBeGreaterThan(before);

  // The real read: nothing in flight, never started.
  h.edit.fn = null;
  await expect(begin).toHaveText(/Begin construction/, { timeout: 12_000 });
  await expect(begin).toBeEnabled();
  expect(h.trapped).toEqual([]);
});

// ---------------------------------------------------------------------------
// Fix-G review I1: evidence must be something that CHANGED after the dispatch. On
// a project already started, constructionStarted was true before a Resume, so a
// read saying so proves nothing about it. A Resume answered 500 used to be
// "evidenced" by the next read: its alert wiped, and Resume offered again 4.4s
// after the 500, beside a pump that may be running.
//
// SAFETY: as above. The dispatch is answered 500 in the browser; reads go to the
// server as GETs, edited in the browser to read as a project already started.
// ---------------------------------------------------------------------------

/** Every read says construction has started: a project already run. */
function alreadyStarted(wire: WireProject): void {
  wire.constructionStarted = true;
}

async function openStartedConsole(page: Page): Promise<void> {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/, { timeout: 15_000 });
  await expect(begin).toBeEnabled();
}

test('I1: on a project already started, a Resume answered 500 stays held, with its alert, until work shows in flight', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  h.edit.fn = alreadyStarted;
  await openStartedConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });

  // Reads keep arriving, and every one says constructionStarted. None is evidence:
  // sampled well past the 4.4s the review measured.
  const before = h.served.length;
  await expectBeginHeldOff(page, 6_000);
  expect(h.served.length, 'reads after the failure').toBeGreaterThan(before + 1);
  await expect(alert).toBeVisible();
  await expect(alert).toHaveAttribute('data-hold', 'held');
  await expect(alert).toContainText(UNKNOWN_HEADLINE);
  await page.clock.fastForward(40_000);
  await expectBeginHeldOff(page, 1_500);
  await expect(alert).toHaveAttribute('data-hold', 'held');

  // Work shows in flight: that changed after the dispatch.
  h.edit.fn = (wire) => {
    alreadyStarted(wire);
    inConstruction(PICKED)(wire);
  };
  await expect(alert).toHaveAttribute('data-hold', 'evidenced', { timeout: 10_000 });
  await expectRunning(page, 1_500);
  expect(h.trapped).toHaveLength(1);
});

test('I1: on a project already started, with nothing changing, a Resume 500 holds for 60s, then asks again', async ({
  page,
}) => {
  await page.clock.install();
  const h = await harness(page, (route) => route.fulfill(SERVER_500));
  h.edit.fn = alreadyStarted;
  await openStartedConsole(page);
  await dispatchOnce(page);
  const alert = page.getByTestId(TESTID.constructionBeginError);
  await expect(alert).toHaveAttribute('data-hold', 'held', { timeout: 10_000 });

  // The clock keeps real time between jumps: 50s here is short of 60s, 12s more past it.
  await page.clock.fastForward(50_000);
  await expectBeginHeldOff(page, 1_500);
  await expect(alert).toHaveAttribute('data-hold', 'held');
  await page.clock.fastForward(12_000);
  await expect(alert).toHaveAttribute('data-hold', 'expired', { timeout: 10_000 });
  await expect(alert).toContainText('No sign the pump started. Begin again?');
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Resume construction/);
  await expect(begin).toBeEnabled();
  expect(h.trapped).toHaveLength(1);
});

// ---------------------------------------------------------------------------
// Fix-G review M3: an in-app switch to a second project reused the console's
// component, and its Begin in-flight ref and cascade carried over. With a dispatch
// still pending on the first project, the second's enabled Begin did nothing and
// said nothing. The console is now keyed by project.
//
// SAFETY: as above. Both dispatches are answered in the browser (the first held,
// then aborted at teardown; the second aborted). The second project is the first's
// real read, served in the browser at another id; nothing is written.
// ---------------------------------------------------------------------------

const SECOND_PROJECT = 'uitest-second-console';

test('M3: switching in-app to a second project gives it its own Begin, even with a dispatch pending on the first', async ({
  page,
  dispatchGuard,
}) => {
  const hold = dispatchGuard.hold();
  const dispatched: string[] = [];
  await page.route('**/execute-next-activity/**', async (route) => {
    dispatched.push(new URL(route.request().url()).pathname);
    if (dispatched.length === 1) return hold.handle(route, () => route.fulfill(SUCCESS));
    await route.abort();
  });
  // The second project reads as the first, under its own id.
  await page.route(`**/system-design/get-project/${SECOND_PROJECT}**`, async (route) => {
    try {
      const url = route.request().url().replace(SECOND_PROJECT, 'archistrator');
      await route.fulfill({ response: await route.fetch({ url }) });
    } catch (err) {
      if (page.isClosed() || /has been closed/.test(String(err))) return;
      throw err;
    }
  });

  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => dispatched.length).toBe(1);
  await expectRunning(page, 1_000);

  // In-app to the second project's console: the same route, another id.
  await page.evaluate((to) => {
    window.history.pushState(null, '', to);
    window.dispatchEvent(new PopStateEvent('popstate', { state: null }));
  }, `/project/${SECOND_PROJECT}/construction?lens=list`);
  await expect(page).toHaveURL(new RegExp(`/project/${SECOND_PROJECT}/construction`));
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 15_000 });
  await expect(begin).toBeEnabled();

  // Its Begin dispatches for IT: the first project's pending dispatch does not
  // swallow it.
  await dispatchOnce(page);
  await expect.poll(() => dispatched.length, { message: 'the second project dispatched' }).toBe(2);
  expect(dispatched[1]).toContain(`/execute-next-activity/${SECOND_PROJECT}`);
});

// ---------------------------------------------------------------------------
// Fix I (fix-H review minors). SAFETY: as above; the dispatch is answered in the
// browser, and nothing is sent.
// ---------------------------------------------------------------------------

/** The pump answered 200 and started nothing: its quiet tick, nothing eligible. */
const NOTHING_DISPATCHED = {
  status: 200,
  contentType: 'application/json',
  body: '{"dispatched":false}',
};

test('fix I: a 200 saying dispatched:false holds nothing, and says "Nothing to dispatch — no activity is eligible"', async ({
  page,
}) => {
  const h = await harness(page, (route) => route.fulfill(NOTHING_DISPATCHED));
  await openConsole(page);
  await dispatchOnce(page);
  await expect.poll(() => h.trapped.length).toBe(1);
  await expect(page.getByTestId(TESTID.constructionBeginNote)).toHaveText(
    'Nothing to dispatch — no activity is eligible'
  );
  // No success hold: the button is the read's again at once, not "Construction
  // running…" for 60s, and it stays so.
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(COMMITTED_LABEL, { timeout: 10_000 });
  const until = Date.now() + 4_000;
  while (Date.now() < until) {
    await expect(begin).toBeEnabled({ timeout: 1 });
    await expect(begin).toHaveText(COMMITTED_LABEL, { timeout: 1 });
    await page.waitForTimeout(250);
  }
  // A quiet tick is not a failure.
  await expect(page.getByTestId(TESTID.constructionBeginError)).toHaveCount(0);
  expect(h.trapped).toHaveLength(1);
});

test('fix I (mutant F): the success is recorded BEFORE its refresh, so the refresh read alone can show the pickup', async ({
  page,
}) => {
  // Only the read served first after the dispatch — the success's own refresh —
  // carries the pickup; every read after it shows the work already ended. Were the
  // success recorded after its refresh, that read would predate the record and
  // could never count as the pickup, and Begin would stay held for the full 60s.
  const pickup = { shown: 0 };
  const holder: { h?: Harness } = {};
  const h = await harness(page, async (route) => {
    if (holder.h !== undefined) {
      holder.h.edit.fn = (wire) => {
        pickup.shown += 1;
        if (pickup.shown === 1) inConstruction(PICKED)(wire);
        else wire.constructionStarted = true;
      };
    }
    await route.fulfill(SUCCESS);
  });
  holder.h = h;
  await openConsole(page);
  await dispatchOnce(page);
  const begin = page.getByTestId(TESTID.constructionBegin);
  // Well inside the 60s hold, with no clock jump: the pickup was seen on the
  // refresh, and the next read found nothing in flight.
  await expect(begin).toHaveText(/Resume construction/, { timeout: 15_000 });
  await expect(begin).toBeEnabled();
  expect(pickup.shown, 'the pickup rode on one read, and later reads followed').toBeGreaterThan(1);
  await expect(page.getByTestId(TESTID.constructionBeginError)).toHaveCount(0);
  expect(h.trapped).toHaveLength(1);
});
