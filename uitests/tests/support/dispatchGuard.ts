/**
 * The shared dispatch guard for the construction specs (fix-D review M5).
 *
 * The construction console sits on real write endpoints: Begin/Resume
 * (execute-next-activity), phase decisions, overrides and the review policy. A
 * spec that forgot its own trap, or a mutant that pressed the wrong button, could
 * dispatch against the live server. Six specs had no trap at all.
 *
 * Import `test` and `expect` from here instead of '@playwright/test'. The guarded
 * `test` is also this module's DEFAULT export, and no spec may import
 * '@playwright/test''s own `test` (fix-H ruling; pinned by
 * meta/suite-rules.spec). The one exception is the seed step, which is the one
 * place a real project may be created. Two layers,
 * because neither alone is enough (measured on the tasks-lens branch, round 2: the
 * guard alone let one held POST out; with the teardown abort, none):
 *
 *  1. The CONTEXT route aborts every non-GET/HEAD no page route answers, bar the
 *     one merged READ that is a POST (see READS below). It is an
 *     AUTO fixture, so it is in place before the test body runs and before any
 *     navigation, and a page's `unrouteAll` cannot remove it. A spec that fakes a
 *     write's outcome (a 500, a 400, a dropped response) registers its own
 *     `page.route` inside the test; page routes take precedence over this context
 *     route, so that handler answers first. GET and HEAD fall through untouched.
 *
 *  2. HOLDS. A page handler that holds a request open (to answer it later) and is
 *     then removed by `unrouteAll` or `unroute` does not fail that request:
 *     Playwright sends it on to the network, past the context route too. So a spec
 *     holds a request only through `dispatchGuard.hold()`, and every hold still
 *     open is ABORTED, and waited for, before anything is unrouted:
 *       - `page.unrouteAll` and `page.unroute` abort every open hold first, so a
 *         spec's own `afterEach` cleanup cannot let one out (afterEach runs before
 *         fixture teardown). Only unrouteAll was patched until the fix-G review
 *         (M1), and a single `page.unroute` let a held write out. A spec that
 *         unroutes mid-test releases its holds first, or they are aborted;
 *       - the fixture's teardown aborts whatever is left, while every route is
 *         still in place. It depends on `page`, so it is torn down before the page.
 *     A held write can never outlive its test, whether the test passes, fails or
 *     times out (pinned by construction-dispatch-guard.spec).
 *
 *  3. THE API CONTEXTS. The `request` fixture, `page.request` and `context.request`
 *     never pass through a route, so neither layer above sees what they send (the
 *     cleanup round found `request` unguarded). The guarded `test` overrides the
 *     `request` fixture, and the dispatchGuard fixture replaces `page.request` and
 *     `context.request`, with a wrapper that applies the same rule
 *     (guardLetsThrough) before anything is sent: GET and HEAD go out, and every
 *     other method REJECTS with REQUEST_GUARD_REFUSAL unless this spec allowlisted
 *     it. A relative URL is resolved against baseURL first, as Playwright would.
 *     Pinned by meta/suite-rules.spec.
 *
 * `dispatchGuard.blocked` lists what the context route aborted ("POST /api/…"), for
 * specs that want to assert on it.
 *
 * LIVE DRAFTING (fix-G review ruling). The live-drafting specs exist to send real
 * drafting writes, so a spec may let a NAMED set of writes through with
 * `test.use({ dispatchGuardAllows: LIVE_DRAFTING_WRITES })`. The default is none.
 * Whatever a spec allows, creating a project and every construction write stay
 * aborted (guardLetsThrough): the one place a project may be created is the seed
 * step (tests/seed/shared-project.setup.ts), which does not use this guard.
 */
import {
  test as base,
  expect,
  type APIRequestContext,
  type Page,
  type Request,
  type Route,
} from '@playwright/test';

/**
 * The Phase-1 co-author loop's writes: request a draft, decide the gate, ask a
 * question, acknowledge a stale basis. Only the live-drafting specs (opt-in,
 * UITESTS_LIVE_DRAFTING) allow these.
 *
 * Stage 4a folded seven `system-design/*` routes into four `delivery/*` ones, and
 * the two that were about STARTING the phase went with them: `start-system-design`
 * and `set-research-input` are steps of `start-project`, which is on
 * NEVER_LET_THROUGH because it also creates projects. `set-review-comment-status`
 * and `advance-phase` are members of `submit-review-decision` now, which is
 * allowed as one route — the decision the body carries is not something a URL
 * regex can separate, and a live-drafting spec that may send a verdict may send
 * the comment-status flip beside it.
 *
 * Each of the four takes `/{projectID}/{activityID}`: the delivery rail addresses
 * an ACTIVITY and its task, not an artifact kind. Pinned by
 * meta/project-creation-guard.spec.
 */
export const LIVE_DRAFTING_WRITES: readonly RegExp[] = [
  /^\/api\/v1\/delivery\/(dispatch-activity-task|submit-review-decision|ask-questions|acknowledge-stale-basis)\/[^/]+\/[^/]+$/,
];

/**
 * A READ the guard always lets out, whatever its HTTP method.
 *
 * Stage 4a merged thirteen per-rail readers into `POST
 * /api/v1/delivery/query-project-view`: the selector is a `ProjectViewQuery` in the
 * BODY, so a URL query string could not carry it. "GET and HEAD pass" was always a
 * proxy for "a read changes nothing", and with this one op it stopped being one —
 * left alone, the context route would abort the project read of EVERY spec and the
 * SPA would render its error state everywhere.
 *
 * It is named EXACTLY, not by a `query-*` prefix: the guard's whole value is that
 * nothing writes by accident, and a pattern would admit the next POST that happens
 * to be spelled like a read. `query-activity-view` is a GET and needs no entry.
 */
const READS = /^\/api\/v1\/delivery\/query-project-view$/;

/** Never let through, whatever a spec allows: creating or continuing a project
 *  (start-project both creates one and names its operating model), and every
 *  write that drives the construction pump. */
const NEVER_LET_THROUGH =
  /^\/api\/v1\/delivery\/(start-project|execute-next-activity|override-activity|replan-project|set-project-run-state|set-project-execution-policy)(\/|$)/;

/** Whether the guard lets a request through to the network. Pure (pinned by
 *  meta/project-creation-guard.spec). */
export function guardLetsThrough(
  method: string,
  url: string,
  allowed: readonly RegExp[]
): boolean {
  if (method === 'GET' || method === 'HEAD') return true;
  const path = new URL(url).pathname;
  if (READS.test(path)) return true;
  if (NEVER_LET_THROUGH.test(path)) return false;
  return allowed.some((re) => re.test(path));
}

/** What a guarded API context rejects a write with (see layer 3 above). */
export const REQUEST_GUARD_REFUSAL = 'dispatch guard refused an API-context write';

/** The HTTP method each APIRequestContext verb sends; `fetch` names its own. */
const VERB_METHOD: Readonly<Partial<Record<string, string>>> = {
  get: 'GET',
  head: 'HEAD',
  post: 'POST',
  put: 'PUT',
  patch: 'PATCH',
  delete: 'DELETE',
};

/**
 * `request` with guardLetsThrough applied before each call is sent (layer 3). Every
 * member other than the six verbs and `fetch` (dispose, storageState) passes
 * through untouched.
 */
export function guardRequestContext(
  request: APIRequestContext,
  baseURL: string | undefined,
  allowed: readonly RegExp[]
): APIRequestContext {
  const check = (method: string, url: string): void => {
    const absolute = new URL(url, baseURL).toString();
    if (!guardLetsThrough(method, absolute, allowed)) {
      throw new Error(
        `${REQUEST_GUARD_REFUSAL}: ${method} ${absolute}. Specs never write to the ` +
          'server; fake the answer with page.route, or allowlist a live-drafting write ' +
          'with test.use({ dispatchGuardAllows }).'
      );
    }
  };
  return new Proxy(request, {
    get(target, prop): unknown {
      const member: unknown = Reflect.get(target, prop);
      if (typeof member !== 'function') return member;
      const call = member as (this: APIRequestContext, ...args: unknown[]) => unknown;
      if (prop === 'fetch') {
        return async (urlOrRequest: string | Request, options?: { method?: string }) => {
          const isUrl = typeof urlOrRequest === 'string';
          const url = isUrl ? urlOrRequest : urlOrRequest.url();
          const method = options?.method ?? (isUrl ? 'GET' : urlOrRequest.method());
          check(method.toUpperCase(), url);
          return call.call(target, urlOrRequest, options);
        };
      }
      const method = typeof prop === 'string' ? VERB_METHOD[prop] : undefined;
      if (method !== undefined) {
        return async (url: string, options?: unknown) => {
          check(method, url);
          return call.call(target, url, options);
        };
      }
      return call.bind(target);
    },
  });
}

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

export const test = base.extend<{
  dispatchGuard: DispatchGuard;
  /** Writes this spec lets through (LIVE_DRAFTING_WRITES). Default: none. */
  dispatchGuardAllows: readonly RegExp[];
}>({
  dispatchGuardAllows: [[], { option: true }],
  // Layer 3: the `request` fixture never passes through a route.
  request: async ({ request, baseURL, dispatchGuardAllows }, use) => {
    await use(guardRequestContext(request, baseURL, dispatchGuardAllows));
  },
  dispatchGuard: [
    async ({ context, page, baseURL, dispatchGuardAllows }, use) => {
      const blocked: string[] = [];
      let holds: (() => Promise<void>)[] = [];

      // Layer 3 again: page.request and context.request share the context's
      // cookies but not its routes. (Playwright serves one object for both.)
      const api = guardRequestContext(context.request, baseURL, dispatchGuardAllows);
      Object.defineProperty(context, 'request', { value: api, configurable: true });
      Object.defineProperty(page, 'request', { value: api, configurable: true });

      await context.route('**/*', async (route) => {
        const req = route.request();
        const method = req.method();
        if (guardLetsThrough(method, req.url(), dispatchGuardAllows)) {
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

      // Every hold dies as an abort BEFORE any route is removed, by either unroute.
      const unrouteAll = page.unrouteAll.bind(page);
      page.unrouteAll = (async (options?: Parameters<Page['unrouteAll']>[0]) => {
        await abortHolds();
        await unrouteAll(options);
      }) as Page['unrouteAll'];
      const unroute = page.unroute.bind(page);
      page.unroute = (async (...args: Parameters<Page['unroute']>) => {
        await abortHolds();
        await unroute(...args);
      }) as Page['unroute'];

      await use({ blocked, hold, abortHolds });

      await abortHolds();
    },
    { auto: true },
  ],
});

export { expect };

/** The guarded `test` is this module's default export (fix-H ruling). */
export default test;
