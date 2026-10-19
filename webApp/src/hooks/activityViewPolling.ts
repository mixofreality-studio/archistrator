/**
 * Pure poll-cadence logic for the activity-view query (useActivityView) — the
 * sessionPolling.ts pattern, for the Activity Experience's single read.
 *
 * DECISION TABLE (first match wins; data = the last ActivityView, error = the
 * last fetch error):
 *
 *   data                   | error | interval          | why
 *   -----------------------|-------|-------------------|-------------------------------
 *   (any)                  | 404   | stop              | not in the committed plan; a
 *                          |       |                   | plan change invalidates
 *   (any)                  | other | 5s                | transient: degrade, NEVER stop —
 *                          |       |                   | this query is its own refresh
 *                          |       |                   | authority (F-QA2-28)
 *   (none)                 | none  | stop              | pristine mount
 *   unchanged since a      | none  | 2s                | the server applies a decision,
 *   write this screen sent |       |                   | an amendment or an override
 *                          |       |                   | ASYNCHRONOUSLY: the
 *                          |       |                   | read the write's invalidation
 *                          |       |                   | answers can still be the activity
 *                          |       |                   | from before it, so the screen that
 *                          |       |                   | sent it follows until it moves
 *   any task running       | none  | 2s                | an agent is working; with the
 *                          |       |                   | fork, even beside an owed gate
 *   state running          | none  | 2s                | between tasks
 *   state awaitingHuman    | none  | 8s                | the human is the actor; the poll
 *                          |       |                   | is the safety net (F-QA2-48)
 *   state done / failed    | none  | stop              | terminal; mutations invalidate
 *   state notStarted       | none  | notStartedPollMs  | default stop; a caller that must
 *                          |       |                   | see the sweep dispatch it opts in
 */
import { isNoSessionError } from './sessionPolling.ts';

export const ACTIVITY_LIVE_POLL_MS = 2000;
export const ACTIVITY_GATE_POLL_MS = 8000;
export const ACTIVITY_DEGRADED_POLL_MS = 5000;

/** The slice of an ActivityView the cadence reads (structural, so node:test needs no schema). */
export interface ActivityViewPollInput {
  readonly state: string;
  readonly tasks: readonly { readonly state: string; readonly revisions: readonly unknown[] }[];
}

/**
 * What a write moves: the activity's state and each task's state and revision count.
 * The signature a screen takes when it sends a write is the activity "from before it".
 */
export function activityViewSignature(data: ActivityViewPollInput): string {
  return [data.state, ...data.tasks.map((t) => `${t.state}/${String(t.revisions.length)}`)].join(
    '|'
  );
}

export function activityViewPollIntervalMs(
  data: ActivityViewPollInput | undefined,
  error: unknown,
  notStartedPollMs: number | false = false,
  /**
   * The activity's signature (activityViewSignature) when this screen sent it a write
   * it applies asynchronously (a decision, an amendment, an override): while the read
   * still shows it, the write has not landed yet. `undefined` when nothing is owed.
   */
  owedFrom?: string
): number | false {
  // Checked as the no-error case FIRST, not its inverse (the sessionPolling.ts convention).
  if (error === null || error === undefined) {
    if (data === undefined) return false;
    if (owedFrom !== undefined && activityViewSignature(data) === owedFrom)
      return ACTIVITY_LIVE_POLL_MS;
    if (data.tasks.some((t) => t.state === 'running') || data.state === 'running') {
      return ACTIVITY_LIVE_POLL_MS;
    }
    // A halted activity (FIX-003) waits on a human too — the operator's resume.
    if (data.state === 'awaitingHuman' || data.state === 'haltedByFailure')
      return ACTIVITY_GATE_POLL_MS;
    if (data.state === 'notStarted') return notStartedPollMs;
    return false;
  }
  // isNoSessionError is "an ApiError with status 404": here, an activity the
  // committed plan does not hold. Any other error is transient: degrade, never
  // stop — a stopped poll is permanent (F-QA2-28) while a degraded one self-heals.
  return isNoSessionError(error) ? false : ACTIVITY_DEGRADED_POLL_MS;
}
