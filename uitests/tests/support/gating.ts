/**
 * Infra gating — the UI analogue of systemtests' InfraFromEnv + requireStack.
 *
 * The SPA gates its whole tree on `GET /api/userinfo` returning 200 (session
 * probe, GTD parity). In dev mode the Go server injects a dev principal, so a
 * Postgres-backed server answers 200 with no IdP. Two tiers, mirroring
 * systemtests' "structural always-runs / wire-skips-without-stack" split:
 *
 *   1. SERVER REACHABLE — needed by every spec. We probe /api/userinfo through
 *      the SPA origin (the Vite proxy forwards it to the Go server). If it is
 *      not 200, the pure-UI specs SKIP with an annotation rather than failing on
 *      a backend that was never provisioned.
 *
 *   2. LIVE DRAFTING — the co-author flow (request-draft → generating → render →
 *      gate) additionally needs Temporal + a worker provider. That stack is
 *      opt-in via UITESTS_LIVE_DRAFTING, now a four-value flag
 *      (off | replay | WHEN_REQUIRED | live; legacy 1/true ⇒ live) — see
 *      draftingMode() below. Specs needing it SKIP unless the mode is non-off,
 *      exactly as systemtests' wire tests skip without the ARCHISTRATOR_* infra env.
 */
import { test, type Page, type APIRequestContext } from '@playwright/test';

/**
 * Drafting modes (design 2026-06-05). The uitests do not start the Go server, so
 * this only decides whether to RUN the live-drafting specs; the operator brings up
 * the server in the matching worker mode (replay-strict / record-on-miss / live).
 *
 *   off  (or unset) — skip live-drafting specs (pure-UI only).
 *   replay          — run; server replays cassettes strictly (offline, default CI).
 *   WHEN_REQUIRED   — run; server records misses via a real delegate.
 *   live            — run; server hits a real model, cache ignored.
 *
 * Legacy '1' / 'true' are accepted as aliases meaning "run".
 */
export type DraftingMode = 'off' | 'replay' | 'WHEN_REQUIRED' | 'live';

export function draftingMode(): DraftingMode {
  const v = (process.env.UITESTS_LIVE_DRAFTING ?? '').trim();
  switch (v) {
    case 'replay':
      return 'replay';
    case 'WHEN_REQUIRED':
      return 'WHEN_REQUIRED';
    case 'live':
    case '1':
    case 'true':
      return 'live';
    default:
      return 'off';
  }
}

/** True when the live-drafting specs should run (any mode other than off). */
export function liveDraftingEnabled(): boolean {
  return draftingMode() !== 'off';
}

/**
 * serverReachable probes the SPA's `/api/userinfo` (same origin → Vite proxy →
 * Go server). 200 means a dev-mode, Postgres-backed server is answering.
 */
export async function serverReachable(request: APIRequestContext, baseURL: string): Promise<boolean> {
  try {
    const res = await request.get(`${baseURL}/api/userinfo`, {
      headers: { Accept: 'application/json' },
      timeout: 5_000,
    });
    return res.status() === 200;
  } catch {
    return false;
  }
}

/**
 * skipUnlessServer skips the current test (with a clear reason) when the
 * dev-mode server behind the SPA proxy is not reachable. Used by the pure-UI
 * specs so they degrade gracefully on a bare checkout, matching systemtests.
 */
export async function skipUnlessServer(
  request: APIRequestContext,
  baseURL: string,
): Promise<void> {
  const ok = await serverReachable(request, baseURL);
  test.skip(
    !ok,
    'uitests: SPA-proxied server not reachable at /api/userinfo — bring up the dev-mode Go server (Postgres) behind the SPA proxy. See README.',
  );
}

/**
 * skipUnlessLiveDrafting skips when the drafting flag is `off`/unset — the UI
 * analogue of requireStack. Set UITESTS_LIVE_DRAFTING to replay (offline cassette
 * replay), WHEN_REQUIRED, or live to run, with a matching server up (see README).
 */
export function skipUnlessLiveDrafting(): void {
  test.skip(
    !liveDraftingEnabled(),
    'uitests: drafting specs gated off — set UITESTS_LIVE_DRAFTING=replay (offline cassettes) or WHEN_REQUIRED/live with a matching Postgres+Temporal+worker server (see README).',
  );
}

/**
 * gotoApp navigates to a SPA route and waits past the session gate: the app
 * shows `loading-indicator` while probing /api/userinfo, then mounts the route.
 * We wait for the loader to detach (best-effort) so callers can assert on the
 * real screen.
 */
export async function gotoApp(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await page.waitForLoadState('domcontentloaded');
  // The gate loader is transient; if present, let it resolve before asserting.
  const loader = page.getByTestId('loading-indicator');
  if ((await loader.count()) > 0) {
    await loader.first().waitFor({ state: 'detached' }).catch(() => undefined);
  }
}
