/**
 * The shared dispatch guard for the construction specs (fix-D review M5).
 *
 * The construction console sits on real write endpoints: Begin/Resume
 * (execute-next-activity), phase decisions, overrides and the review policy. A
 * spec that forgot its own trap, or a mutant that pressed the wrong button, could
 * dispatch against the live server. Six specs had no trap at all.
 *
 * Import `test` and `expect` from here instead of '@playwright/test'. Two layers,
 * because neither alone is enough (measured on the tasks-lens branch, round 2: the
 * guard alone let one held POST out; with the teardown abort, none):
 *
 *  1. The CONTEXT route aborts every non-GET/HEAD no page route answers. It is an
 *     AUTO fixture, so it is in place before the test body runs and before any
 *     navigation, and a page's `unrouteAll` cannot remove it. A spec that fakes a
 *     write's outcome (a 500, a 400, a dropped response) registers its own
 *     `page.route` inside the test; page routes take precedence over this context
 *     route, so that handler answers first. GET and HEAD fall through untouched.
 *
 *  2. HOLDS. A page handler that holds a request open (to answer it later) and is
 *     then removed by `unrouteAll` does not fail that request: Playwright sends it
 *     on to the network, past the context route too. So a spec holds a request only
 *     through `dispatchGuard.hold()`, and every hold still open is ABORTED, and
 *     waited for, before anything is unrouted:
 *       - `page.unrouteAll` aborts every hold first, so a spec's own `afterEach`
 *         cleanup cannot let one out (afterEach runs before fixture teardown);
 *       - the fixture's teardown aborts whatever is left, while every route is
 *         still in place. It depends on `page`, so it is torn down before the page.
 *     A held write can never outlive its test, whether the test passes, fails or
 *     times out (pinned by construction-dispatch-guard.spec).
 *
 * `dispatchGuard.blocked` lists what the context route aborted ("POST /api/…"), for
 * specs that want to assert on it.
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
