/**
 * construction-dispatch-guard.spec — the shared dispatch guard itself (fix-D
 * review M5; support/dispatchGuard).
 *
 * Every construction spec imports `test` from support/dispatchGuard, whose AUTO
 * fixture aborts every non-GET/HEAD request the browser context makes, before any
 * navigation. These cases pin that the guard is there without opting in, that it
 * aborts every write method, that a GET still goes through, and that it records
 * what it aborted.
 *
 * SAFETY: the probe path does not exist on the server. A guard that failed would
 * only ever reach a 404, never a real write.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/dispatchGuard.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

const PROBE = '/api/v1/__dispatch-guard-probe';

async function probeMethods(page: Page): Promise<Record<string, string>> {
  return page.evaluate(async (url) => {
    const out: Record<string, string> = {};
    for (const method of ['POST', 'PUT', 'PATCH', 'DELETE', 'GET']) {
      try {
        out[method] = `status ${String((await fetch(url, { method })).status)}`;
      } catch {
        out[method] = 'aborted';
      }
    }
    return out;
  }, PROBE);
}

test('with no opt-in at all, every non-GET is aborted in the browser and a GET goes through', async ({
  page,
}) => {
  // This test does not ask for the guard: it must be there anyway (auto).
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const got = await probeMethods(page);
  expect(got).toMatchObject({ POST: 'aborted', PUT: 'aborted', PATCH: 'aborted', DELETE: 'aborted' });
  expect(got['GET']).toMatch(/^status \d+$/);
});

test('the guard lists what it aborted', async ({ page, dispatchGuard }) => {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  await probeMethods(page);
  const probes = dispatchGuard.blocked.filter((b) => b.endsWith(PROBE));
  expect(probes.map((b) => b.split(' ')[0])).toEqual(['POST', 'PUT', 'PATCH', 'DELETE']);
});

// ---------------------------------------------------------------------------
// A held write never outlives its test (orchestrator, from the tasks-lens leak
// demo: the context route alone let one held POST out).
//
// When `unrouteAll` removes a page handler that is still HOLDING a request,
// Playwright sends that request on to the network, past the context route too.
// A spec's own cleanup (`afterEach`) runs before any fixture teardown, so the
// guard makes `page.unrouteAll` abort every hold first. The first case below FAILS
// on purpose with a POST still held, and its `afterEach` unroutes everything, the
// way a failing spec's cleanup would. The second reads what became of the POST.
//
// SAFETY: the probe path does not exist on the server. Were the POST let out, it
// could only reach the GET-only proxy, which refuses (and logs) it.
// ---------------------------------------------------------------------------

const HOLD_PROBE = '/api/v1/__dispatch-guard-hold-probe';
/** What became of the held POST, as the page saw it: "failed" (aborted in the
 *  browser) or "response <status>" (it reached the network). */
const heldOutcome: string[] = [];
/** Resolves once the held POST has settled either way. */
let heldSettled: Promise<void> = Promise.resolve();

test.describe('a write a test still holds when it fails', () => {
  test.describe.configure({ mode: 'serial' });

  // The leak trigger: a spec's cleanup unroutes its page handlers while one of them
  // still holds a request. Then, with the page still open, wait for that request to
  // settle: an escaped request's answer must land while the page can still see it.
  // (Measured: without the wait, a POST that reached the GET-only proxy could land
  // after the page closed and be recorded as a failure, so the escape went unseen.)
  test.afterEach(async ({ page }) => {
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await Promise.race([heldSettled, page.waitForTimeout(5_000)]);
  });

  test('a forced failure with a POST still held', async ({ page, dispatchGuard }) => {
    test.fail(true, 'forced: this test ends by failing with the POST still held');
    await gotoApp(page, '/project/archistrator/construction?lens=list');
    heldSettled = new Promise<void>((settle) => {
      page.on('requestfailed', (r) => {
        if (!r.url().endsWith(HOLD_PROBE)) return;
        heldOutcome.push('failed');
        settle();
      });
      page.on('response', (r) => {
        if (!r.url().endsWith(HOLD_PROBE)) return;
        heldOutcome.push(`response ${String(r.status())}`);
        settle();
      });
    });
    const hold = dispatchGuard.hold();
    let held = 0;
    await page.route(`**${HOLD_PROBE}`, (route) => {
      held += 1;
      return hold.handle(route, () => route.fulfill({ status: 200, json: {} }));
    });
    void page
      .evaluate((url) => fetch(url, { method: 'POST', body: '{}' }).then(() => undefined), HOLD_PROBE)
      .catch(() => undefined);
    await expect.poll(() => held).toBe(1);
    throw new Error('forced failure: the test ends with the POST still held');
  });

  test('its held POST was aborted in the browser, and never reached the network', () => {
    expect(heldOutcome).toEqual(['failed']);
  });
});
