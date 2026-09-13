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
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

const COMMITTED_LABEL = /Begin construction|Resume construction/;

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
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
      .map((el) => (el.getAttribute('data-testid') ?? '').slice('construction-begin-candidate-'.length))
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

interface Harness {
  trapped: string[];
  /** Timestamps of every project read the page made. */
  reads: number[];
  /** While true, project reads are answered 503 in the browser. */
  holdReads: { on: boolean };
}

async function harness(
  page: Page,
  answer: (route: import('@playwright/test').Route) => Promise<void>
): Promise<Harness> {
  const h: Harness = { trapped: [], reads: [], holdReads: { on: false } };
  await page.route('**/execute-next-activity/**', async (route) => {
    h.trapped.push(route.request().url());
    await answer(route);
  });
  await page.route('**/system-design/get-project/archistrator**', async (route) => {
    h.reads.push(Date.now());
    if (h.holdReads.on) {
      await route.fulfill({ status: 503, contentType: 'application/json', body: '{}' });
    } else {
      await route.continue();
    }
  });
  return h;
}

async function dispatchOnce(page: Page): Promise<number> {
  await openDialog(page);
  const at = Date.now();
  await page.getByTestId(TESTID.constructionBeginConfirmDispatch).click();
  return at;
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

test('a 500 says the outcome is unknown, and Begin waits for a refreshed project', async ({
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

  // The refreshed project answers — and Begin says what IT says.
  h.holdReads.on = false;
  const begin = page.getByTestId(TESTID.constructionBegin);
  await expect(begin).toHaveText(/Begin construction/, { timeout: 10_000 });
  await expect(begin).toBeEnabled();
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

  h.holdReads.on = false;
  await expect(page.getByTestId(TESTID.constructionBegin)).toBeEnabled({ timeout: 10_000 });
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
