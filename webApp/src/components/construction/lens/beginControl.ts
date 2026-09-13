/**
 * The Begin/Resume control's pure rules — label, enabled state, and the list a
 * Begin names before it dispatches anything. No React here, so node:test can pin
 * it (beginControl.test.ts); the route wires it and the dialog renders it.
 *
 * The label is the project read's `constructionStarted`, computed ONCE on the
 * server from the stored head-state (fix round B, item 7) — never counted from rows
 * or attempts here: the backfill filled 23 rows with reconstructed attempts no pump
 * ever ran, and the server already refuses to count those. It replaced probing one
 * construction-session endpoint per committed activity on every load.
 *
 * Two rules the designer pass (P0-3) turned up still hold:
 *
 *  1. The label is decided ONCE. While the project is loading the button is
 *     disabled and says neither "Begin" nor "Resume", so it cannot read "Begin"
 *     and flip to "Resume" after load.
 *  2. Begin is a real dispatch, so it asks first and says what it would start —
 *     the activities with no stored record, read from the rows the server sent,
 *     never a hardcoded list.
 */
import type { ConstructionRows, ConstructionStage } from '../../../contracts/types';

export interface BeginControl {
  label: string;
  disabled: boolean;
  /** Show the spinner: something is in flight (a load or a run). */
  busy: boolean;
  /** The verb the confirm step asks with. */
  verb: 'Begin' | 'Resume';
}

const CHECKING: BeginControl = {
  label: 'Checking construction…',
  disabled: true,
  busy: true,
  verb: 'Begin',
};

export function beginControlFor(input: {
  /** The project read's constructionStarted; `undefined` when there is no project read. */
  constructionStarted: boolean | undefined;
  projectLoading: boolean;
  running: boolean;
  /**
   * A dispatch ended with an UNKNOWN outcome (a 5xx or a dropped response), and
   * Begin is held: there is no pump evidence yet, and the bounded hold has not
   * expired (beginHoldFor). The pump may have started, so the button cannot say
   * Begin.
   */
  awaitingPump?: boolean;
}): BeginControl {
  if (input.running) {
    return { label: 'Construction running…', disabled: true, busy: true, verb: 'Resume' };
  }
  if (input.projectLoading || input.awaitingPump === true) return CHECKING;
  if (input.constructionStarted === undefined) {
    // Loaded, but no project read to answer from: neither word can be claimed, and
    // nothing can sensibly be dispatched.
    return { label: 'Construction state unavailable', disabled: true, busy: false, verb: 'Begin' };
  }
  return input.constructionStarted
    ? { label: 'Resume construction', disabled: false, busy: false, verb: 'Resume' }
    : { label: 'Begin construction', disabled: false, busy: false, verb: 'Begin' };
}

/**
 * What a FAILED dispatch tells the operator (fix-C review, Important).
 *
 * The server starts the pump workflow BEFORE it answers, and it can still answer
 * 5xx after that (a decode failure, a cancelled context, the terminal fallback) —
 * or a proxy or the network can drop the response. So a 5xx or a network error
 * does not mean "nothing started", and "Begin again to retry" on it invited a
 * second pump. Only a 4xx is the server REFUSING the request: nothing started.
 *
 *   - `rejected` — a 4xx. Carries the server's own message where it sent one.
 *   - `unknown`  — anything else: a 5xx, no status at all (the request never got
 *                  an answer), or a status this rule does not recognise.
 */
export type DispatchOutcome =
  | { kind: 'rejected'; message: string }
  | { kind: 'unknown'; message: string };

export function dispatchOutcomeFor(
  /** The HTTP status, or `undefined` when no response arrived (network error). */
  status: number | undefined,
  message: string
): DispatchOutcome {
  const said = message.trim().length > 0 ? message.trim() : 'no reason given';
  if (status !== undefined && status >= 400 && status < 500) {
    return { kind: 'rejected', message: said };
  }
  return { kind: 'unknown', message: said };
}

/** The alert's words for one outcome. The unknown sentence is the review's ruling
 *  verbatim; it never says "retry", because a retry could start a second pump. */
export function dispatchOutcomeCopy(outcome: DispatchOutcome): {
  headline: string;
  detail: string;
} {
  switch (outcome.kind) {
    case 'rejected':
      return {
        headline: `Construction dispatch rejected: ${outcome.message}.`,
        detail: 'The server refused the request, so nothing was started.',
      };
    case 'unknown':
      return {
        headline: 'Outcome unknown — the pump may have started; the list will show it if it did.',
        detail: `The dispatch got no clean answer (${outcome.message}). Begin stays off until the pump shows itself, or ${String(UNKNOWN_OUTCOME_HOLD_MS / 1000)}s pass with no sign of it.`,
      };
  }
}

// ---------------------------------------------------------------------------
// After an UNKNOWN outcome: hold Begin until the pump is evidenced (orchestrator
// ruling on the fix-D review, I3).
//
// A newer project read ALONE used to lift the gate. But the first read after a
// 5xx can land before the pump has stored its StartedAt, so it still says "not
// started", and a second Begin there started a second pump. Begin now stays off
// until one of two things happens:
//   - evidence: a read NEWER than the failure says `constructionStarted`, or a
//     session probe newer than the failure shows a live session;
//   - the bounded hold expires with no evidence. Begin then comes back, and the
//     alert says so (holdExpiredCopy).
// ---------------------------------------------------------------------------

/** How long Begin stays held after an unknown outcome with no sign of the pump. */
export const UNKNOWN_OUTCOME_HOLD_MS = 60_000;

/** The session stages at which no pump is running for the session. */
const NO_PUMP_STAGES: ReadonlySet<ConstructionStage> = new Set(['exited', 'paused', 'unknown']);

/**
 * Whether the reads since `at` show the pump: the project says construction
 * started, or a session is live. Only reads NEWER than the failure count, so a
 * read that was already on screen before the dispatch failed is never taken as
 * an answer to it.
 */
export function pumpEvidencedSince(
  at: number,
  reads: {
    /** The project query's `dataUpdatedAt` (0 before any read), and what it said. */
    projectReadAt: number;
    constructionStarted: boolean | undefined;
    /** The session probe's `dataUpdatedAt`, and its stage. Stage is `undefined`
     *  when there is no probe, or the probe established that no session exists. */
    sessionReadAt: number;
    sessionStage: ConstructionStage | undefined;
  }
): boolean {
  const started = reads.projectReadAt > at && reads.constructionStarted === true;
  const live =
    reads.sessionReadAt > at &&
    reads.sessionStage !== undefined &&
    !NO_PUMP_STAGES.has(reads.sessionStage);
  return started || live;
}

/**
 * Where Begin stands after a failed dispatch:
 *   - `none`      — no failure, or a rejection (a 4xx means nothing started);
 *   - `held`      — an unknown outcome, with no evidence and the hold still running;
 *   - `evidenced` — the pump showed itself, so the project read decides the label;
 *   - `expired`   — the hold ran out with no evidence, so Begin is offered again.
 */
export type BeginHold = 'none' | 'held' | 'evidenced' | 'expired';

export function beginHoldFor(
  failure: { outcome: DispatchOutcome; holdExpired: boolean } | null,
  evidenced: boolean
): BeginHold {
  if (failure?.outcome.kind !== 'unknown') return 'none';
  if (evidenced) return 'evidenced';
  return failure.holdExpired ? 'expired' : 'held';
}

/**
 * Whether the button reads "Construction running…" (disabled): a dispatch is
 * pending, or the poll is on for a run the console believes in.
 *
 * A dispatch that SUCCEEDED counts. So does an unknown outcome once the pump is
 * EVIDENCED (fix-E review I1). The pump showed itself, so the button must not
 * fall back to the read's label: a live session while `constructionStarted` is
 * still false read "Begin construction", enabled, with a pump running. A held or
 * expired unknown outcome does not count, and neither does a rejection.
 */
export function beginRunning(input: {
  pending: boolean;
  cascading: boolean;
  failed: boolean;
  hold: BeginHold;
}): boolean {
  if (input.pending) return true;
  if (!input.cascading) return false;
  return !input.failed || input.hold === 'evidenced';
}

/** The alert's words once the hold has expired. The headline is the ruling verbatim. */
export function holdExpiredCopy(outcome: DispatchOutcome): { headline: string; detail: string } {
  return {
    headline: 'No sign the pump started. Begin again?',
    detail: `The dispatch got no clean answer (${outcome.message}), and for ${String(UNKNOWN_OUTCOME_HOLD_MS / 1000)}s since, no project read showed construction started and no session was live.`,
  };
}

export interface DispatchCandidate {
  activityId: string;
  title?: string;
}

/**
 * The activities a Begin could start: every row with NO stored record
 * (`recorded === false`) — the server's own signal, not re-derived from empty
 * fields. A recorded row is already under way (or reconstructed as done), and the
 * pump does not start it again. Sorted by id so the list is stable across polls.
 */
export function notStartedActivities(
  rows: ConstructionRows | undefined,
  titleFor: (activityId: string) => string | undefined
): DispatchCandidate[] {
  return Object.values(rows ?? {})
    .filter((r) => !r.recorded)
    .map((r): DispatchCandidate => {
      const title = titleFor(r.activityId);
      return title !== undefined && title.length > 0
        ? { activityId: r.activityId, title }
        : { activityId: r.activityId };
    })
    .sort((a, b) => a.activityId.localeCompare(b.activityId));
}
