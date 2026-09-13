/**
 * The shared dispatch guard for the construction specs (fix-D review M5).
 *
 * The construction console sits on real write endpoints: Begin/Resume
 * (execute-next-activity), phase decisions, overrides and the review policy. A
 * spec that forgot its own trap, or a mutant that pressed the wrong button, could
 * dispatch against the live server. Six specs had no trap at all.
 *
 * This `test` aborts EVERY non-GET/HEAD request the browser context makes. It is an
 * AUTO fixture, so the route is in place before the test body runs and before any
 * navigation. Import `test` and `expect` from here instead of '@playwright/test'.
 *
 * A spec that needs to fake a write's outcome (a 500, a 400, a dropped response)
 * registers its own `page.route` inside the test. Page routes take precedence
 * over this context route, so that handler answers first. GET and HEAD fall
 * through to the network untouched.
 *
 * `dispatchGuard.blocked` lists what the guard aborted ("POST /api/…"), for specs
 * that want to assert on it.
 */
import { test as base, expect } from '@playwright/test';

export interface DispatchGuard {
  /** Every non-GET/HEAD request the guard aborted, as "METHOD url". */
  blocked: string[];
}

export const test = base.extend<{ dispatchGuard: DispatchGuard }>({
  dispatchGuard: [
    async ({ context }, use) => {
      const guard: DispatchGuard = { blocked: [] };
      await context.route('**/*', async (route) => {
        const req = route.request();
        const method = req.method();
        if (method === 'GET' || method === 'HEAD') {
          await route.fallback();
          return;
        }
        guard.blocked.push(`${method} ${req.url()}`);
        await route.abort('blockedbyclient');
      });
      await use(guard);
    },
    { auto: true },
  ],
});

export { expect };
