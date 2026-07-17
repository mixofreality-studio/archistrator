/**
 * Pure poll-cadence logic for the Phase-1 session-state query (useSessionState).
 * Extracted so the QA-incident cadence rules (2026-07-15 gtdapp:1, 2026-07-16
 * F-QA2-28) are unit-testable without a react-query harness.
 *
 * DECISION TABLE (stage = last-known data's stage, error = last fetch error):
 *
 *   stage        | error            | interval | why
 *   -------------|------------------|----------|---------------------------------
 *   TERMINAL     | (any)            | stop     | only a user action changes it,
 *                |                  |          | and that action invalidates the query
 *   live         | none             | 2s       | the normal live-session poll
 *   live         | 404 (no session) | 4s       | server says the session is GONE —
 *                |                  |          | re-probe at the no-session cadence
 *   live         | other            | 5s       | TRANSIENT fault (5xx / ECONNREFUSED
 *                |                  |          | mid-poll): degraded poll, self-heals
 *   (no data)    | none             | stop     | pristine mount — first fetch in flight
 *   (no data)    | 404 (no session) | 4s       | the gentle no-session probe (below)
 *   (no data)    | other            | 5s       | bootstrap fetch hit a transient fault —
 *                |                  |          | keep probing so the view ever loads
 *
 * Why the no-session 404 polls (QA incident 2026-07-15, gtdapp:1): a gate approve
 * auto-advances the phase workflow, which AUTO-STARTS the next step's co-author
 * session server-side. The SPA used to cache that 404 forever ("No draft yet /
 * Request draft"), so the founder clicked Request draft into an already-RUNNING
 * session and the signal queued as a stale gate-consumer. Gentle re-probing
 * discovers the auto-started session within seconds. A 404 refetch keeps the query
 * in error state (never back to pending), so the view stays stable — no skeleton
 * flash, no artifact-view remount.
 *
 * Why non-404 errors must KEEP polling (F-QA2-28, 2026-07-16): this query is its
 * own single refresh authority (staleTime Infinity, refetchOnWindowFocus false), so
 * ONE refetchInterval evaluation that returns false stops polling PERMANENTLY —
 * nothing ever re-evaluates it. The old "any other error: no poll" rule latched the
 * view dead after a single transient fetch failure (dev-server restart mid-poll):
 * react-query keeps the last-good data rendered, so the founder watched a stale
 * DRAFTING… scene for a session the server had long since failed. Degraded 5s
 * polling costs one cheap failed fetch per tick against a dead dev server and
 * self-heals the moment the server returns.
 */
// Runtime imports carry explicit .ts extensions (allowImportingTsExtensions) so this
// module also loads under node:test's type-stripping (sessionPolling.test.ts).
import { ApiError } from '../contracts/errors.ts';
import { TERMINAL_STAGES } from '../contracts/types.ts';
import type { SessionStage } from '../contracts/types.ts';

export const POLL_INTERVAL_MS = 2000;

/** The no-session (404) re-probe cadence — gentler than the live-session poll. */
export const NO_SESSION_POLL_INTERVAL_MS = 4000;

/**
 * The transient-fault cadence (any non-404 error): degraded but NEVER stopped, so
 * the view self-heals when the server comes back (F-QA2-28).
 */
export const DEGRADED_POLL_INTERVAL_MS = 5000;

/** True when the query error is the deterministic "no session started yet" 404. */
export function isNoSessionError(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

/**
 * The refetchInterval decision for one observed query state (last data's stage +
 * last error). Returns the poll interval in ms, or false to stop polling.
 * Implements the decision table in the header comment.
 */
export function sessionPollIntervalMs(
  stage: SessionStage | undefined,
  error: unknown
): number | false {
  // Terminal stage: stop. Only a user action (retry / withdraw / new draft) moves a
  // terminal session, and every such mutation invalidates the query.
  if (stage !== undefined && TERMINAL_STAGES.includes(stage)) return false;
  if (error === null || error === undefined) {
    // Healthy: a live stage polls at 2s; a pristine mount (no data, no error — the
    // first fetch is in flight) adds no interval, its settle re-evaluates this.
    return stage === undefined ? false : POLL_INTERVAL_MS;
  }
  // The deterministic "no session" 404 — the gentle probe (also when the last-known
  // stage was live: the server says that session is GONE, so probe for its successor).
  if (isNoSessionError(error)) return NO_SESSION_POLL_INTERVAL_MS;
  // Any other error is transient (5xx, connection refused mid-poll): degrade, never
  // stop — a stopped poll is permanent (F-QA2-28) while a degraded one self-heals.
  return DEGRADED_POLL_INTERVAL_MS;
}
