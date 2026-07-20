/**
 * Pure poll-cadence logic for the Phase-1 session-state query (useSessionState).
 * Extracted so the QA-incident cadence rules (2026-07-15 gtdapp:1, 2026-07-16
 * F-QA2-28, F-QA2-48, F-QA2-50) are unit-testable without a react-query harness.
 *
 * DECISION TABLE (stage = last-known data's stage, error = last fetch error):
 *
 *   stage          | error            | interval | why
 *   ---------------|------------------|----------|---------------------------------
 *   REST (committed| (any)            | stop     | only a user action changes it,
 *   / withdrawn)   |                  |          | and that action invalidates the query
 *   FAILURE GATE   | none             | 8s       | refused / draftFailed are human
 *   (refused /     |                  |          | gates, NOT stop states (F-QA2-50):
 *   draftFailed)   |                  |          | Retry resumes the SAME session
 *                  |                  |          | server-side (failed → redrafting →
 *                  |                  |          | awaitingReview in seconds, no new
 *                  |                  |          | CI job) and the retry mutation's
 *                  |                  |          | single invalidation refetch RACES
 *                  |                  |          | that transition — keep watching so
 *                  |                  |          | the polled stage wins within one
 *                  |                  |          | interval
 *   awaitingReview | none             | 8s       | the GATE is NOT terminal (F-QA2-48):
 *                  |                  |          | the server-side stage can move without
 *                  |                  |          | this tab (another tab's decision, or a
 *                  |                  |          | submit whose response was lost after
 *                  |                  |          | the signal was delivered) — keep
 *                  |                  |          | watching, slowly: the human is the
 *                  |                  |          | actor here, so 2s would be noise
 *   live           | none             | 2s       | the normal live-session poll
 *   live/gate      | 404 (no session) | 4s       | server says the session is GONE —
 *                  |                  |          | re-probe at the no-session cadence
 *   live/gate      | other            | 5s       | TRANSIENT fault (5xx / ECONNREFUSED
 *                  |                  |          | mid-poll): degraded poll, self-heals
 *   (no data)      | none             | stop     | pristine mount — first fetch in flight
 *   (no data)      | 404 (no session) | 4s       | the gentle no-session probe (below)
 *   (no data)      | other            | 5s       | bootstrap fetch hit a transient fault —
 *                  |                  |          | keep probing so the view ever loads
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
 *
 * Why the FAILURE gates must KEEP polling (F-QA2-50, 2026-07-16): draftFailed
 * (and its sync sibling refused) host the Retry button. Retry resumes the SAME
 * session server-side WITHOUT dispatching a new CI job, so the stage moves
 * failed → (brief redrafting) → awaitingReview within seconds — decoupled from
 * the mutation's HTTP round-trip. Under the old "terminal: stop" rule the retry's
 * ONLY escape hatch was the mutation's single invalidateQueries refetch; any
 * outcome of that one refetch that left the evaluation on the cached failed stage
 * (raced ahead of the server's transition, or hit a transient fault — the
 * terminal check ran BEFORE the error branch) froze the query PERMANENTLY, and
 * the founder watched a stale generating view for 4+ minutes while the server sat
 * at the review gate — only a hard reload (whose mount fetch ignores the frozen
 * interval) recovered. The failure gates now watch at the slow gate cadence: the
 * polled server stage always wins within one interval, reload never required.
 */
// Runtime imports carry explicit .ts extensions (allowImportingTsExtensions) so this
// module also loads under node:test's type-stripping (sessionPolling.test.ts).
import { ApiError } from '../contracts/errors.ts';
import { REVIEWABLE_STAGE } from '../contracts/types.ts';
import type { SessionStage } from '../contracts/types.ts';

/**
 * The poll-REST stages: the only stages with NO in-place server-side transition
 * (a committed or withdrawn session never moves again — amendments and fresh
 * drafts start a NEW session the mutations invalidate into view). Deliberately a
 * SUBSET of the contract's TERMINAL_STAGES: refused / draftFailed are terminal at
 * the Manager but are FAILURE GATES here (F-QA2-50) — Retry resumes the same
 * session, so they keep watching (see the decision table).
 */
const REST_STAGES: readonly SessionStage[] = ['committed', 'withdrawn'];

/** The human failure gates: terminal-at-the-Manager, but Retry moves them in place. */
const FAILURE_GATE_STAGES: readonly SessionStage[] = ['refused', 'draftFailed'];

export const POLL_INTERVAL_MS = 2000;

/** The no-session (404) re-probe cadence — gentler than the live-session poll. */
export const NO_SESSION_POLL_INTERVAL_MS = 4000;

/**
 * The review-gate cadence (F-QA2-48): awaitingReview is NOT a stop state. A gate
 * parked in this tab still moves server-side — another tab decides, or a decision
 * submit loses its RESPONSE after the Temporal signal was delivered (the QA2
 * incident: a Send back 503'd, the redraft was actually running, and the stopped
 * poll meant the SPA never rendered it until a hard reload). Slow, because at the
 * gate the human is the expected actor — the poll is a safety net, not a driver.
 */
export const GATE_POLL_INTERVAL_MS = 8000;

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
 * Whether the SPA may treat "no session" as ESTABLISHED and render the
 * destructive no-session surface ("No draft yet / Request draft"), discarding
 * the working view (QA incident 2026-07-19, gtdapp kind=4).
 *
 * A 404 is authoritative only while the query holds NO session data — the
 * probe never found a session, so there is nothing to lose. A 404 that
 * arrives on a REFETCH while a session view is cached means the server's
 * session store vanished under it (observed live: the local stack's Temporal
 * died and a foreign dev server took over its port, so every poll 404'd and
 * the founder's wizard reset to the beginning mid-use-case). react-query
 * keeps the last-good data on a failed refetch, so keep RENDERING that view:
 * the no-session probe cadence (sessionPollIntervalMs → 4s) keeps watching,
 * a recovered store restores the live poll, and any successor session
 * replaces the view. Server-side the same incident is now a typed 5xx (a
 * foreign backend can no longer masquerade as "no session"), so this rule is
 * defense-in-depth for whatever genuinely wipes a session store.
 */
export function isSessionAbsent(hasSessionData: boolean, error: unknown): boolean {
  return !hasSessionData && isNoSessionError(error);
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
  // REST stage (committed / withdrawn): stop. No in-place transition exists — a new
  // draft or amendment starts a NEW session, and that mutation invalidates the query.
  // NOT the failure gates (F-QA2-50): Retry moves those in place, so one stopped
  // evaluation racing the single invalidation refetch froze the view until reload.
  if (stage !== undefined && REST_STAGES.includes(stage)) return false;
  if (error === null || error === undefined) {
    // Healthy: the review gate and the failure gates watch slowly (F-QA2-48 /
    // F-QA2-50 — the human is the actor, the poll is the safety net), any other
    // live stage polls at 2s; a pristine mount (no data, no error — the first
    // fetch is in flight) adds no interval, its settle re-evaluates this.
    if (stage === undefined) return false;
    return stage === REVIEWABLE_STAGE || FAILURE_GATE_STAGES.includes(stage)
      ? GATE_POLL_INTERVAL_MS
      : POLL_INTERVAL_MS;
  }
  // The deterministic "no session" 404 — the gentle probe (also when the last-known
  // stage was live: the server says that session is GONE, so probe for its successor).
  if (isNoSessionError(error)) return NO_SESSION_POLL_INTERVAL_MS;
  // Any other error is transient (5xx, connection refused mid-poll): degrade, never
  // stop — a stopped poll is permanent (F-QA2-28) while a degraded one self-heals.
  return DEGRADED_POLL_INTERVAL_MS;
}
