/**
 * The GRAPH branch's local dispatch guard — merge prep (graph round 2, item 9).
 *
 * construction-ui-rewrite carries the shared fixture, `support/dispatchGuard.ts`
 * (fix-D review M5; teardown abort added in tasks round 2). This branch does not
 * have it yet, so its graph specs use this LOCAL EQUIVALENT with the same two
 * layers and the same `test` / `expect` exports. AT MERGE: delete this file and
 * change each graph spec's import from './support/graphDispatchGuard.js' to
 * './support/dispatchGuard.js' — nothing else changes.
 *
 * Why not a `page.route` in each spec's beforeEach (what the graph specs did):
 * a page route is registered after the context exists and can be removed by
 * `page.unrouteAll`; and a request a page handler HOLDS open is sent on to the
 * network when that handler is removed. Two layers close both:
 *
 *  1. The CONTEXT route aborts every non-GET/HEAD no page route answers. It is an
 *     AUTO fixture, in place before the test body and before any navigation, and
 *     a page's `unrouteAll` cannot remove it. A spec faking a write's outcome
 *     registers its own `page.route` inside the test; page routes take
 *     precedence, so that handler answers first. GET and HEAD fall through.
 *
 *  2. HOLDS. A spec holds a request only through `dispatchGuard.hold()`. Every
 *     hold still open is ABORTED, and waited for, before anything is unrouted:
 *     `page.unrouteAll` aborts every hold first, and the fixture's teardown
 *     aborts whatever is left while every route is still in place. A held write
 *     can never outlive its test, whether it passes, fails or times out.
 *
 * `dispatchGuard.blocked` lists what the context route aborted ("POST /api/…").
 */
import { test as base, expect, type Page, type Route } from '@playwright/test';

/** One held request's release (see `DispatchGuard.hold`). */
export interface Hold {
  /** Let every request this hold handles be answered with its `answer`. */
  release: () => void;
  /** Hold `route` until released (then `answer` runs), or until the hold is
   *  aborted (then the route is aborted). Return this from the page handler. */
  handle: (route: Route, answer: () => Promise<void>) => Promise<void>;
}

export interface DispatchGuard {
  /** Every non-GET/HEAD the context route aborted, as "METHOD url". */
  blocked: string[];
  /** A new hold. The only way a spec may hold a request open. */
  hold: () => Hold;
  /** Abort every hold still open, and wait until each is. Teardown and
   *  `page.unrouteAll` call it first; a spec may too. */
  abortHolds: () => Promise<void>;
}

export const test = base.extend<{ dispatchGuard: DispatchGuard }>({
  dispatchGuard: [
    async ({ context, page }, use) => {
      const blocked: string[] = [];
      let holds: (() => Promise<void>)[] = [];

      await context.route('**/*', async (route) => {
        const req = route.request();
        const method = req.method();
        if (method === 'GET' || method === 'HEAD') {
          await route.fallback();
          return;
        }
        blocked.push(`${method} ${req.url()}`);
        await route.abort('blockedbyclient');
      });

      const abortHolds = async (): Promise<void> => {
        const open = holds;
        holds = [];
        for (const abortHeld of open) await abortHeld();
      };

      const hold = (): Hold => {
        let settle: (answer: boolean) => void = () => undefined;
        const released = new Promise<boolean>((resolve) => {
          settle = resolve;
        });
        const handled: Promise<void>[] = [];
        holds.push(async () => {
          settle(false);
          await Promise.all(handled);
        });
        return {
          release: () => {
            settle(true);
          },
          handle: (route, answer) => {
            const done = released.then(async (go) => {
              if (go) await answer();
              else await route.abort('blockedbyclient').catch(() => undefined);
            });
            handled.push(done);
            return done;
          },
        };
      };

      // Every hold dies as an abort BEFORE any route is removed.
      const unrouteAll = page.unrouteAll.bind(page);
      page.unrouteAll = (async (options?: Parameters<Page['unrouteAll']>[0]) => {
        await abortHolds();
        await unrouteAll(options);
      }) as Page['unrouteAll'];

      await use({ blocked, hold, abortHolds });

      await abortHolds();
    },
    { auto: true },
  ],
});

export { expect };
