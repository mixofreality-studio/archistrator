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
 *   any task running       | none  | 2s                | an agent is working; with the
 *                          |       |                   | fork, even beside an owed gate
 *   state running          | none  | 2s                | between tasks
 *   state awaitingHuman    | none  | 8s                | the human is the actor; the poll
 *                          |       |                   | is the safety net (F-QA2-48)
 *   state done / failed    | none  | stop              | terminal; mutations invalidate
 *   state notStarted       | none  | notStartedPollMs  | default stop; a caller that must
 *                          |       |                   | see the sweep dispatch it opts in
 */
// Runtime imports carry explicit .ts extensions so this module also loads under
// node:test's type-stripping (activityViewPolling.test.ts).
import { isNoSessionError } from './sessionPolling.ts';

export const ACTIVITY_LIVE_POLL_MS = 2000;
export const ACTIVITY_GATE_POLL_MS = 8000;
export const ACTIVITY_DEGRADED_POLL_MS = 5000;

/** The slice of an ActivityView the cadence reads (structural, so node:test needs no schema). */
export interface ActivityViewPollInput {
  readonly state: string;
  readonly tasks: readonly { readonly state: string }[];
}

export function activityViewPollIntervalMs(
  data: ActivityViewPollInput | undefined,
  error: unknown,
  notStartedPollMs: number | false = false
): number | false {
  if (error === null || error === undefined) {
    if (data === undefined) return false;
    if (data.tasks.some((t) => t.state === 'running') || data.state === 'running') {
      return ACTIVITY_LIVE_POLL_MS;
    }
    if (data.state === 'awaitingHuman') return ACTIVITY_GATE_POLL_MS;
    if (data.state === 'notStarted') return notStartedPollMs;
    return false;
  }
  // isNoSessionError is "an ApiError with status 404": here, an activity the
  // committed plan does not hold. Any other error is transient: degrade, never
  // stop — a stopped poll is permanent (F-QA2-28) while a degraded one self-heals.
  return isNoSessionError(error) ? false : ACTIVITY_DEGRADED_POLL_MS;
}
