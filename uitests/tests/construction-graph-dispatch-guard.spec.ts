/**
 * construction-graph-dispatch-guard.spec — the graph branch's LOCAL dispatch
 * guard itself (support/graphDispatchGuard; graph round 2, merge prep).
 *
 * A port of construction-ui-rewrite's construction-dispatch-guard.spec for the
 * local equivalent. AT MERGE: this spec and support/graphDispatchGuard.ts both
 * go, the graph specs import support/dispatchGuard, and the shared guard's own
 * spec pins the same four things:
 *
 *  - with no opt-in, every write method is aborted in the browser, before any
 *    navigation; a GET still goes through;
 *  - the guard lists what it aborted;
 *  - a write a test still HOLDS when it fails is aborted in the browser at
 *    cleanup, and never reaches the network.
 *
 * SAFETY: the probe paths do not exist on the server. A guard that failed would
 * only reach a 404, or the GET-only proxy's refusal — never a real write.
 */
import type { Page } from '@playwright/test';
import { test, expect } from './support/graphDispatchGuard.js';
import { skipUnlessServer, skipUnlessConstructionArtifacts, gotoApp } from './support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';
const GRAPH = '/project/archistrator/construction?lens=graph';

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
  await skipUnlessConstructionArtifacts(request, BASE);
});

const PROBE = '/api/v1/__graph-dispatch-guard-probe';

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
  await gotoApp(page, GRAPH);
  const got = await probeMethods(page);
  expect(got).toMatchObject({ POST: 'aborted', PUT: 'aborted', PATCH: 'aborted', DELETE: 'aborted' });
  expect(got['GET']).toMatch(/^status \d+$/);
});

test('the guard lists what it aborted', async ({ page, dispatchGuard }) => {
  await gotoApp(page, GRAPH);
  await probeMethods(page);
  const probes = dispatchGuard.blocked.filter((b) => b.endsWith(PROBE));
  expect(probes.map((b) => b.split(' ')[0])).toEqual(['POST', 'PUT', 'PATCH', 'DELETE']);
});

test('a page unrouteAll cannot remove the guard', async ({ page }) => {
  await gotoApp(page, GRAPH);
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  const got = await probeMethods(page);
  expect(got['POST']).toBe('aborted');
});

// ---------------------------------------------------------------------------
// A held write never outlives its test. When `unrouteAll` removes a page handler
// that is still HOLDING a request, Playwright sends it on to the network, past
// the context route too; a spec's own afterEach runs before fixture teardown. So
// the guard makes `page.unrouteAll` abort every hold first. The first case FAILS
// on purpose with a POST still held, and its afterEach unroutes everything the
// way a failing spec's cleanup would; the second reads what became of the POST.
// ---------------------------------------------------------------------------

const HOLD_PROBE = '/api/v1/__graph-dispatch-guard-hold-probe';
/** "failed" (aborted in the browser) or "response <status>" (it got out). */
const heldOutcome: string[] = [];
let heldSettled: Promise<void> = Promise.resolve();

test.describe('a write a test still holds when it fails', () => {
  test.describe.configure({ mode: 'serial' });

  test.afterEach(async ({ page }) => {
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await Promise.race([heldSettled, page.waitForTimeout(5_000)]);
  });

  test('a forced failure with a POST still held', async ({ page, dispatchGuard }) => {
    test.fail(true, 'forced: this test ends by failing with the POST still held');
    await gotoApp(page, GRAPH);
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
