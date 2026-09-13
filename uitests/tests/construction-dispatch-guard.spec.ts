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
