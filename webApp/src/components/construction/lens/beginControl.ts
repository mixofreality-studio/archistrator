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
import { rowIsInFlight } from '../list/activityScope.ts';
import type { OwedMarks } from '../tasks/owedChip.ts';

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

/** Whether a probed session stage is a live pump. `undefined` is no probe, or a
 *  probe that established no session exists. */
function sessionIsLive(stage: ConstructionStage | undefined): boolean {
  return stage !== undefined && !NO_PUMP_STAGES.has(stage);
}

/**
 * Whether the STATE shows construction in flight (fix-F review, root-cause
 * ruling): any activity whose OWED-AWARE row state is running or awaiting a human
 * (the one rowIsInFlight), or any live session.
 *
 * "Awaiting" comes from the live owed set (tasks/owedChip.ts, Q4), never from
 * head-state: in-review reads running, a live gate or a steer reads awaiting, and
 * both are in flight. A recorded failure reads failed, which is not: the pump has
 * stopped on it.
 *
 * This, not a timer, is what says a pump is running. The 30s no-progress
 * watchdog used to decide it: 30s after the last integration it handed the label
 * back to the read, and an ENABLED Begin or Resume stood beside a live session.
 */
export function constructionInFlight(state: {
  rows: ConstructionRows | undefined;
  /** The live owed set, keyed by activity id; omitted, nothing is owed. */
  owed?: OwedMarks | undefined;
  /** A live probed session's stage, if any (newestLiveSession). */
  sessionStage: ConstructionStage | undefined;
}): boolean {
  return anyRowInFlight(state.rows, state.owed) || sessionIsLive(state.sessionStage);
}

/** Whether any row's owed-aware state is running or awaiting a human (rowIsInFlight). */
export function anyRowInFlight(rows: ConstructionRows | undefined, owed?: OwedMarks): boolean {
  return Object.values(rows ?? {}).some((r) => rowIsInFlight(r, owed?.get(r.activityId)));
}

/** One probed session read: its stage (`null` where no session exists, `undefined`
 *  where there is no answer yet) and when it was REQUESTED (0 where unknown). */
export interface SessionRead {
  stage: ConstructionStage | null | undefined;
  requestedAt: number;
}

/**
 * The newest-REQUESTED live session among the console's probes, or undefined when
 * none is live. The console probes every activity the pump started and has not
 * finished (owedWork.probeCandidatesFor), so "a session is live" is any of them;
 * and if any live read was requested after a failure, the newest one was too, so
 * pumpEvidencedSince judges this one read for all of them.
 */
export function newestLiveSession(
  reads: readonly SessionRead[]
): { stage: ConstructionStage; requestedAt: number } | undefined {
  let newest: { stage: ConstructionStage; requestedAt: number } | undefined;
  for (const r of reads) {
    if (r.stage === null || r.stage === undefined || !sessionIsLive(r.stage)) continue;
    if (newest === undefined || r.requestedAt > newest.requestedAt) {
      newest = { stage: r.stage, requestedAt: r.requestedAt };
    }
  }
  return newest;
}

/**
 * Whether the reads since `at` show the pump: the project shows an activity in
 * flight, or NEWLY says construction started, or a session is live. Only reads
 * REQUESTED after the failure count — by when they were asked for, not when they
 * arrived (hooks/readRequestTimes). A read already on screen before the dispatch
 * failed is never taken as an answer to it, and neither is one that was already
 * on its way: it describes the project from before the failure, however late it
 * lands. A read with no known request time (0) never counts.
 *
 * Evidence must be something that CHANGED after the dispatch (fix-G review I1).
 * constructionStarted counts only when it was false at the failure: on a project
 * already started it was true before the dispatch, so a Resume answered 5xx was
 * "evidenced" by the very next read, its alert wiped and Resume offered again,
 * 4.4s after the 500, beside a pump that may be running.
 */
export function pumpEvidencedSince(
  at: number,
  reads: PickupReads & {
    /** What the shown project read said about construction having started. */
    constructionStarted: boolean | undefined;
    /** What the read on screen at the failure said (BeginFailure.startedAtFailure). */
    startedAtFailure: boolean | undefined;
  }
): boolean {
  const newlyStarted = reads.startedAtFailure === false && reads.constructionStarted === true;
  const started = reads.projectRequestedAt > at && newlyStarted;
  return started || pickupEvidencedSince(at, reads);
}

/** The reads the pump's evidence is judged from, each with when it was REQUESTED. */
export interface PickupReads {
  /** When the shown project read was REQUESTED (0 where unknown). */
  projectRequestedAt: number;
  /** Whether that read shows any activity in flight (rowIsInFlight). */
  rowsInFlight: boolean;
  /** When the shown session read was REQUESTED, and its stage. Stage is
   *  `undefined` when there is no probe, or the probe established that no
   *  session exists. */
  sessionRequestedAt: number;
  sessionStage: ConstructionStage | undefined;
}

/**
 * Whether the reads since `at` show the pump PICKING WORK UP: the project read
 * shows an activity in flight, or the probed session is live. Counted from reads
 * requested after `at`, as pumpEvidencedSince is.
 *
 * `constructionStarted` is deliberately not evidence here. After a successful
 * Resume it was already true before the dispatch (the fix-G review's I1 rule:
 * evidence must have CHANGED after the dispatch), and even after a first Begin it
 * says the pump started, not that any work is in flight yet (fix-G report,
 * concern 1).
 */
export function pickupEvidencedSince(at: number, reads: PickupReads): boolean {
  const picked = reads.projectRequestedAt > at && reads.rowsInFlight;
  const live = reads.sessionRequestedAt > at && sessionIsLive(reads.sessionStage);
  return picked || live;
}

// ---------------------------------------------------------------------------
// After a SUCCESSFUL dispatch: hold Begin until the pickup shows (fix-G report,
// concern 1; orchestrator ruling, fix H).
//
// A success means the pump was started, not that a read shows it yet. Between the
// answer and the first read that shows the pickup, nothing is in flight by state
// and no failure hold stands, so an enabled Begin/Resume stood beside a pump that
// had just been started. The same bounded hold as an unknown outcome covers that
// gap: Begin/Resume stay "Construction running…" until a read requested after the
// success shows work in flight (pickupEvidencedSince), or UNKNOWN_OUTCOME_HOLD_MS
// pass with no sign of it. It stands until B1's server-side "pump open" read can
// say directly whether the pump is running.
// ---------------------------------------------------------------------------

/**
 * Whether a successful dispatch still holds Begin for the pickup: it is recorded
 * (the record leaves memory on evidence or when the hold expires), and no read
 * since it shows the pickup.
 */
export function awaitingPickup(dispatched: { at: number } | null, reads: PickupReads): boolean {
  return dispatched !== null && !pickupEvidencedSince(dispatched.at, reads);
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
 * pending, the STATE shows construction in flight (constructionInFlight), or a
 * successful dispatch is still awaiting its pickup (awaitingPickup).
 *
 * It is decided the same way on every path: after a success, after an unknown
 * outcome, after a remount, and with no dispatch at all. No timer enters it
 * (fix-F review, root-cause ruling); the pickup hold is bounded by its record
 * leaving memory, not by a timer read here. The unknown-outcome hold sits on top
 * of it (beginControlFor's `awaitingPump`), so Begin and Resume are enabled only
 * when nothing is pending, nothing is in flight, and no hold stands.
 */
export function beginRunning(input: {
  pending: boolean;
  inFlight: boolean;
  awaitingPickup: boolean;
}): boolean {
  return input.pending || input.inFlight || input.awaitingPickup;
}

/** The project poll while a Begin is fresh, pending or held: fast enough to animate the cascade. */
export const CASCADE_POLL_MS = 1500;
/** The project poll while the state shows work in flight but the cascade has gone
 *  quiet: slower, and never off, because only a read can say the work ended. */
export const IN_FLIGHT_POLL_MS = 5000;

/**
 * The project read's poll interval. The 30s no-progress watchdog clears
 * `cascading`, and that is ALL it does: it slows the poll, and never decides the
 * label or whether Begin is enabled.
 *
 *   - fast while a dispatch is pending, while a failure or a success in memory
 *     still awaits the pump (so a remounted console polls for the evidence), or
 *     while cascading;
 *   - slow while the state shows work in flight, so "Construction running…" ends
 *     when the work does;
 *   - off otherwise.
 */
export function consolePollMs(input: {
  pending: boolean;
  awaitsPump: boolean;
  cascading: boolean;
  inFlight: boolean;
}): number | false {
  if (input.pending || input.awaitsPump || input.cascading) return CASCADE_POLL_MS;
  return input.inFlight ? IN_FLIGHT_POLL_MS : false;
}

/**
 * Whether a failure has served its purpose and leaves memory (fix-F review): it
 * was evidenced, and the state now shows nothing in flight. Kept, a remount
 * would flash it and bring back a stale "Outcome unknown" alert. A held or expired
 * failure stays (the hold and "Begin again?" still apply), and so does a rejection
 * until it is dismissed.
 */
export function failureLeavesMemory(hold: BeginHold, inFlight: boolean): boolean {
  return hold === 'evidenced' && !inFlight;
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
