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
import type { Page, Route } from '@playwright/test';
import { test, expect, type DispatchGuard } from './support/dispatchGuard.js';
import { requireServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

test.beforeEach(async ({ request }) => {
  await requireServer(request, BASE);
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

// Folded in from the graph branch's local guard spec (integration merge): the
// guard is a CONTEXT route, so a page's own `unrouteAll` cannot take it away.
test('a page unrouteAll cannot remove the guard', async ({ page }) => {
  await gotoApp(page, '/project/archistrator/construction?lens=graph');
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  const got = await probeMethods(page);
  expect(got['POST']).toBe('aborted');
});

// ---------------------------------------------------------------------------
// A held write never outlives its test (orchestrator, from the tasks-lens leak
// demo: the context route alone let one held POST out).
//
// When an unroute removes a page handler that is still HOLDING a request,
// Playwright sends that request on to the network, past the context route too.
// A spec's own cleanup (`afterEach`) runs before any fixture teardown, so the
// guard makes `page.unrouteAll` AND `page.unroute` abort every hold first (fix-G
// review M1: with only unrouteAll patched, a single `page.unroute` let a held
// write out). Each first case below FAILS on purpose with writes still held, and
// its `afterEach` cleans up the way a failing spec's cleanup would, once with
// `unrouteAll` and once with a single `unroute`. The second reads what became of
// the writes.
//
// Each holds TWO writes, under two separate holds (fix-G review M2). With one,
// an abort the guard did not wait for still landed in time, so the pin could not
// tell an awaited abort from an unawaited one: `void abortHolds()` survived.
//
// SAFETY: the probe paths do not exist on the server. Were a POST let out, it
// could only reach the GET-only proxy, which refuses (and logs) it.
// ---------------------------------------------------------------------------

/** What became of each held POST, by probe path, as the page saw it: "failed"
 *  (aborted in the browser) or "response <status>" (it reached the network). */
const heldOutcome: Record<string, string[]> = {};
/** Per probe path: resolves once both held POSTs have settled either way. */
const heldSettled: Record<string, Promise<void>> = {};

/** Fire two POSTs at `probe`, each held by its own hold, then fail the test. */
async function failWithTwoHeld(
  page: Page,
  dispatchGuard: DispatchGuard,
  probe: string
): Promise<void> {
  await gotoApp(page, '/project/archistrator/construction?lens=list');
  const outcomes: string[] = [];
  heldOutcome[probe] = outcomes;
  heldSettled[probe] = new Promise<void>((settle) => {
    const record = (what: string): void => {
      outcomes.push(what);
      if (outcomes.length === 2) settle();
    };
    page.on('requestfailed', (r) => {
      if (r.url().endsWith(probe)) record('failed');
    });
    page.on('response', (r) => {
      if (r.url().endsWith(probe)) record(`response ${String(r.status())}`);
    });
  });
  const holds = [dispatchGuard.hold(), dispatchGuard.hold()];
  let held = 0;
  await page.route(`**${probe}`, (route) => {
    const hold = holds[held];
    held += 1;
    if (hold === undefined) return route.abort();
    return hold.handle(route, () => route.fulfill({ status: 200, json: {} }));
  });
  for (let i = 0; i < 2; i++) {
    void page
      .evaluate((url) => fetch(url, { method: 'POST', body: '{}' }).then(() => undefined), probe)
      .catch(() => undefined);
  }
  await expect.poll(() => held).toBe(2);
  throw new Error('forced failure: the test ends with two POSTs still held');
}

for (const [cleanup, probe] of [
  ['unrouteAll', '/api/v1/__dispatch-guard-hold-probe'],
  ['unroute', '/api/v1/__dispatch-guard-unroute-probe'],
] as const) {
  test.describe(`writes a test still holds when it fails, cleaned up with ${cleanup}`, () => {
    test.describe.configure({ mode: 'serial' });

    // The leak trigger: a spec's cleanup unroutes its page handlers while they
    // still hold requests. Then, with the page still open, wait for those requests
    // to settle: an escaped request's answer must land while the page can still
    // see it. (Measured: without the wait, a POST that reached the GET-only proxy
    // could land after the page closed and be recorded as a failure, so the escape
    // went unseen.)
    test.afterEach(async ({ page }) => {
      if (cleanup === 'unrouteAll') await page.unrouteAll({ behavior: 'ignoreErrors' });
      else await page.unroute(`**${probe}`);
      await Promise.race([heldSettled[probe] ?? Promise.resolve(), page.waitForTimeout(5_000)]);
    });

    test('a forced failure with two POSTs still held', async ({ page, dispatchGuard }) => {
      test.fail(true, 'forced: this test ends by failing with two POSTs still held');
      await failWithTwoHeld(page, dispatchGuard, probe);
    });

    test('both held POSTs were aborted in the browser, and neither reached the network', () => {
      expect(heldOutcome[probe]).toEqual(['failed', 'failed']);
    });
  });
}

// ---------------------------------------------------------------------------
// Fix I (graph review minor): the fixture's TEARDOWN abort, pinned on its own.
//
// The cases above clean up with `unrouteAll` or `unroute`, whose patches abort the
// holds first — so they would still pass with the teardown's own abort removed. Here
// the test ends holding a POST and cleans up NOTHING: no release, no unroute, no
// unrouteAll. Only the guard's teardown can end that hold, and it must end it as an
// abort, never an answer. The route handed to the hold records the guard's abort.
//
// SAFETY: the probe path does not exist on the server; had the POST escaped, the
// GET-only proxy would refuse (and log) it.
// ---------------------------------------------------------------------------

const TEARDOWN_PROBE = '/api/v1/__dispatch-guard-teardown-probe';
/** What the guard did with the held POST, recorded from inside the hold. */
const teardownHold = { held: 0, aborted: 0, answered: 0 };

test.describe('a held write the test never cleans up at all', () => {
  test.describe.configure({ mode: 'serial' });

  test('a test that ends holding a POST, with no release, unroute or unrouteAll', async ({
    page,
    dispatchGuard,
  }) => {
    await gotoApp(page, '/project/archistrator/construction?lens=list');
    const hold = dispatchGuard.hold();
    await page.route(`**${TEARDOWN_PROBE}`, (route) => {
      teardownHold.held += 1;
      // The guard ends a hold through the route it was handed: this one records it.
      const recording = {
        abort: async (errorCode?: string): Promise<void> => {
          teardownHold.aborted += 1;
          await route.abort(errorCode);
        },
      } as unknown as Route;
      return hold.handle(recording, async () => {
        teardownHold.answered += 1;
        await route.fulfill({ status: 200, json: {} });
      });
    });
    void page
      .evaluate((url) => fetch(url, { method: 'POST', body: '{}' }).then(() => undefined), TEARDOWN_PROBE)
      .catch(() => undefined);
    await expect.poll(() => teardownHold.held).toBe(1);
    expect(teardownHold.aborted, 'nothing has aborted it before the test ends').toBe(0);
  });

  test("the guard's teardown aborted it: never answered, never let out", () => {
    expect(teardownHold).toEqual({ held: 1, aborted: 1, answered: 0 });
  });
});
