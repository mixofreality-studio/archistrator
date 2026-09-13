/**
 * What the console says after a human decides a gate (Stage C Task 5). Pure, so
 * node:test pins it.
 *
 * EVIDENCE, NOT ACKNOWLEDGEMENT (spec §6)
 * ---------------------------------------
 * A 200 from submit-phase-decision means the server relayed a signal; it does not
 * mean the workflow took it. The workflow drops, silently, any decision keyed to a
 * gate it is not waiting on (receivePhaseDecision) — and the client cannot tell a
 * lifecycle-phase gate from the local merge hold, which is keyed "merge" (plan
 * DC3/Q2). So the row says "resumed" only once the per-activity session has LEFT
 * `awaitingApproval`, and a decision still waiting after RESUME_TIMEOUT_MS reads
 * "did not land", loudly. "A swallowed approval is worse than no button."
 *
 * ONE GATE OCCURRENCE PER DECISION (review C2)
 * --------------------------------------------
 * A record answers the occurrence it was made on (hooks/gateOccurrences.ts: the
 * `epoch` the session was observed entering the gate). Once a later occurrence
 * opens — the redraft's next round after a send-back, the next phase's gate after
 * an approve — the record is retired: it says nothing about the new gate, which
 * is a fresh decision with Approve and Send back on.
 *
 * DERIVED, NOT STORED
 * -------------------
 * What happened on the wire lives in the QueryClient's mutation cache
 * (decisionRecords.ts), so it survives a remount (review C1); everything else is
 * derived here at render from the observed gate and the clock, so no effect ever
 * sets state to follow the workflow (the React Compiler rule this codebase holds).
 *
 * A failed request follows fix C's rule (beginControl.dispatchOutcomeFor): a 4xx is
 * a rejection, nothing was decided; a 5xx or no answer is an UNKNOWN outcome — the
 * signal may have been delivered — so the row keeps watching the gate, says
 * "resumed" if it clears, and keeps Approve / Send back off until a session read
 * REQUESTED after the failure has answered (the Begin ruling, review I1). A read
 * counts from when it was asked for, not when it arrived: one already in flight
 * when the failure came back describes the gate from before it, and must not
 * re-enable the buttons (tasks round 2).
 *
 * An unknown outcome is held the same way on a NEWER occurrence: a record is
 * retired by a later occurrence only once a read requested after its answer has
 * come back (tasks round-2 review, minor; decisionViewFor).
 *
 * A POST STILL ON THE WIRE HOLDS ITS ACTIVITY (tasks round 2, review I1)
 * ----------------------------------------------------------------------
 * A record retires once a later occurrence opens, even while its own request is
 * still pending. The new gate then read as undecided, with Approve enabled, while
 * the one-click guard refused every click: an enabled button that did nothing. So
 * while ANY decision for an activity is on the wire, that activity's Approve and
 * Send back stay off, and say why (gateControlFor).
 */
import type { ConstructionStage } from '../../../contracts/types';
import type { LensSelection } from '../lens/useLensSelection';
import { dispatchOutcomeFor, type DispatchOutcome } from '../lens/beginControl.ts';
import { CANONICAL_PHASE_NAME, isLifecyclePhase } from '../detail/bodies/taskBriefing.ts';

/** How long an accepted decision may sit at the gate before it reads "did not land". */
export const RESUME_TIMEOUT_MS = 12_000;
/** How long a resumed row stays in place before it leaves (spec §6: ~30s). */
export const RESUMED_LINGER_MS = 30_000;

export type GateDecision = 'approve' | 'sendBack';

/** What happened on the wire, as the mutation cache records it. */
export interface DecisionRecord {
  /** The owed item's key. */
  key: string;
  activityId: string;
  decision: GateDecision;
  /** The gate occurrence this decision answered (gateOccurrences.ts). */
  epoch: number;
  /** The lifecycle phase the decision was sent against — what "approved" names. */
  gatedPhase: string;
  /** When the human decided (the request went out). */
  decidedAt?: number;
  /** When the server ANSWERED (success, or a failure whose outcome is unknown). */
  sentAt?: number;
  /** The failure, when the request did not come back clean. */
  error?: { status?: number | undefined; message: string };
}

export type DecisionView =
  | { kind: 'sending' }
  | { kind: 'awaitingResume' }
  | {
      kind: 'resumed';
      gatedPhase: string;
      /** Where the activity is now — only from a project read taken after the resume. */
      lifecyclePhase?: string;
      /** It left the gate for an operator steer, not for work. */
      escalated: boolean;
    }
  | { kind: 'notLanded' }
  /** `watching`: an unknown outcome with no session read newer than the failure. */
  | { kind: 'failed'; outcome: DispatchOutcome; watching: boolean }
  | { kind: 'done' };

/** What the console has observed of the activity's gate. */
export interface ObservedGate {
  /** The latest session stage; `null` where it no longer exists, `undefined` where
   *  there is no answer. */
  stage: ConstructionStage | null | undefined;
  /** The gate occurrence now observed (gateOccurrences.ts). */
  epoch?: number | undefined;
  /** When the latest session read was REQUESTED (0 where that is not known). */
  requestedAt?: number | undefined;
  /** The activity's current phase — present only when a project read fetched after
   *  the gate was left reports it (observedGateFor). */
  lifecyclePhase?: string | undefined;
}

/**
 * The observation for one activity: its gate occurrence, and the project read's
 * current phase ONLY when that read was fetched after the gate was seen left — an
 * older read still names the phase the gate was on, and "now in <that>" would be
 * the stale claim the review found (minor: "only show Now in <phase> from a
 * refetched read").
 */
export function observedGateFor(
  occurrence:
    | { stage: ConstructionStage | null; epoch: number; requestedAt: number; leftAt?: number }
    | undefined,
  read: { at: number; lifecyclePhase: string | undefined }
): ObservedGate {
  if (occurrence === undefined) return { stage: undefined };
  const fresh =
    occurrence.leftAt !== undefined &&
    read.at > occurrence.leftAt &&
    read.lifecyclePhase !== undefined;
  return {
    stage: occurrence.stage,
    epoch: occurrence.epoch,
    requestedAt: occurrence.requestedAt,
    ...(fresh ? { lifecyclePhase: read.lifecyclePhase } : {}),
  };
}

function resumed(r: DecisionRecord, o: ObservedGate): DecisionView {
  return {
    kind: 'resumed',
    gatedPhase: r.gatedPhase,
    escalated: o.stage === 'awaitingTakeover',
    ...(o.lifecyclePhase !== undefined ? { lifecyclePhase: o.lifecyclePhase } : {}),
  };
}

export function decisionViewFor(r: DecisionRecord, o: ObservedGate, now: number): DecisionView {
  // A later occurrence of this activity's gate has opened: the gate this record
  // answered was left, and the record is retired (review C2) — with one exception,
  // below.
  const superseded = o.epoch !== undefined && o.epoch > r.epoch;
  // Left the gate: the session moved on, or no longer exists (the activity ended).
  const leftGate = o.stage === null || (o.stage !== undefined && o.stage !== 'awaitingApproval');
  const lingerOver = r.sentAt !== undefined && now - r.sentAt > RESUMED_LINGER_MS;
  if (r.error !== undefined) {
    const outcome = dispatchOutcomeFor(r.error.status, r.error.message);
    // Only an UNKNOWN outcome can turn into a resume: a 4xx decided nothing, so a
    // gate that clears afterwards cleared for some other reason.
    if (!superseded && outcome.kind === 'unknown' && leftGate)
      return lingerOver ? { kind: 'done' } : resumed(r, o);
    // Asked for after the failure came back — not merely arrived after it.
    const newerRead =
      o.requestedAt !== undefined && r.sentAt !== undefined && o.requestedAt > r.sentAt;
    // An unknown outcome with no read asked for since its answer holds its activity,
    // on a newer occurrence too (tasks round-2 review, minor). The signal may have
    // been delivered, to the gate this record answered or to the one that opened
    // while the request was on the wire; until a read taken after the answer says
    // where the session stands, the gate showing now is not a fresh decision.
    // Retired first, a held POST answered 500 after the gate left and re-opened put
    // an ENABLED Approve on the new gate 26ms later.
    if (outcome.kind === 'unknown' && !newerRead)
      return { kind: 'failed', outcome, watching: true };
    if (superseded) return { kind: 'done' };
    return { kind: 'failed', outcome, watching: false };
  }
  if (superseded) return { kind: 'done' };
  if (r.sentAt === undefined) return { kind: 'sending' };
  if (leftGate) return lingerOver ? { kind: 'done' } : resumed(r, o);
  return now - r.sentAt > RESUME_TIMEOUT_MS ? { kind: 'notLanded' } : { kind: 'awaitingResume' };
}

/**
 * Approve / Send back stay off: while a decision is on the wire, while an accepted
 * one waits for its resume, and while an unknown outcome has no newer session read
 * to judge it by (review I1). One click, one signal — across a remount too.
 */
export function decisionBusy(view: DecisionView | undefined): boolean {
  if (view === undefined) return false;
  if (view.kind === 'failed') return view.watching;
  return view.kind === 'sending' || view.kind === 'awaitingResume';
}

/** The one line a row (and the pane) shows for a decision. */
export interface FlowNote {
  tone: 'progress' | 'ok' | 'danger';
  text: string;
}

/** Said on a gate while an earlier decision for its activity is still on the wire. */
export const PREVIOUS_STILL_SENDING: FlowNote = {
  tone: 'progress',
  text: 'Previous decision still sending…',
};

/** Approve / Send back for one gate: whether they are off, and the line beside them. */
export interface GateControl {
  busy: boolean;
  note?: FlowNote | undefined;
}

/**
 * The gate's controls from its own record's view and whether ANY decision for the
 * activity is still on the wire (review I1, tasks round 2). A retired record says
 * nothing about the new gate — but while its request is pending the new gate cannot
 * be decided either (the one-click guard is per activity), so the buttons stay off
 * and the line says why, never an enabled button that swallows the click.
 */
export function gateControlFor(
  view: DecisionView | undefined,
  decision: GateDecision | undefined,
  activityPending: boolean
): GateControl {
  const note =
    view !== undefined && decision !== undefined ? decisionNoteFor(view, decision) : undefined;
  if (note !== undefined) return { busy: decisionBusy(view) || activityPending, note };
  return activityPending ? { busy: true, note: PREVIOUS_STILL_SENDING } : { busy: false };
}

function phaseName(lifecyclePhase: string): string {
  return isLifecyclePhase(lifecyclePhase) ? CANONICAL_PHASE_NAME[lifecyclePhase] : lifecyclePhase;
}

export function decisionNoteFor(view: DecisionView, decision: GateDecision): FlowNote | undefined {
  switch (view.kind) {
    case 'sending':
      return {
        tone: 'progress',
        text: decision === 'approve' ? 'Sending your approval…' : 'Sending it back with your note…',
      };
    case 'awaitingResume':
      return { tone: 'progress', text: 'Sent — waiting for the agent to resume…' };
    case 'resumed': {
      if (view.escalated) {
        return {
          tone: 'danger',
          text: 'Escalated — the agent stopped and is asking you how to proceed',
        };
      }
      const gated = phaseName(view.gatedPhase);
      // The server drops the send-back's note today (constructactivity.go never reads
      // sig.Feedback — designer P0-1); the redraft re-runs from its original brief,
      // and the row says so rather than implying the agent read it. Follow-up B1.
      if (decision === 'sendBack') {
        return {
          tone: 'ok',
          text: `Sent back — re-running ${gated} (your note was not delivered)`,
        };
      }
      return {
        tone: 'ok',
        text:
          view.lifecyclePhase !== undefined
            ? `Resumed — now in ${phaseName(view.lifecyclePhase)}`
            : `Resumed — ${gated} approved`,
      };
    }
    case 'notLanded':
      return {
        tone: 'danger',
        text: 'Decision did not land — the gate is still waiting and nothing resumed.',
      };
    case 'failed':
      return view.outcome.kind === 'rejected'
        ? { tone: 'danger', text: `Rejected: ${view.outcome.message}. Nothing was decided.` }
        : {
            tone: 'danger',
            text: `Outcome unknown (${view.outcome.message}) — watching the gate; this row says so if it clears.`,
          };
    case 'done':
      return undefined;
  }
}

/**
 * The send-back composer's caption (designer P0-1): the note is still required —
 * it rides the decision and is the operator's record of why — but the redraft does
 * not read it yet, and the operator is told so before sending, not after.
 */
export function sendBackCaptionFor(gatedPhase: string): string {
  return `Your note goes with the decision, but this redraft does not read it yet — the agent re-runs ${phaseName(gatedPhase)} from its original brief.`;
}

/** What a decision made on this gate says in the pane while its record lives
 *  (designer P1-4). */
export interface DecidedMark {
  decision: GateDecision;
  /** When the human decided. */
  at: number;
}

/**
 * The decision the pane reports as made: once the server has ACCEPTED it (waiting
 * for its resume) and after it resumed. Not while it is still on the wire — the
 * server may yet refuse it (tasks round 2, designer) — and not for "did not land",
 * a failure, an escalation or a retired record: those say something louder, or
 * nothing.
 */
export function decidedFor(record: DecisionRecord, view: DecisionView): DecidedMark | undefined {
  if (record.decidedAt === undefined) return undefined;
  const made = view.kind === 'awaitingResume' || (view.kind === 'resumed' && !view.escalated);
  return made ? { decision: record.decision, at: record.decidedAt } : undefined;
}

/** The pane's state chip after a decision: "Decided · approved" / "Sent back". */
export function decidedChipLabel(decision: GateDecision): string {
  return decision === 'approve' ? 'Decided · approved' : 'Sent back';
}

function hhmm(at: number): string {
  const d = new Date(at);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/**
 * The pane body's lead line after a decision (designer P1-4): the ledger has no
 * attempt for the gate yet (no live RecordTaskAttempt writer — Stage A earmark), so
 * the body would otherwise read "has not run" right under a decision just made.
 */
export function decisionLeadFor(decided: DecidedMark): string {
  const did = decided.decision === 'approve' ? 'approved this' : 'sent this back';
  return `You ${did} at ${hhmm(decided.at)}; gate decisions are not yet written to the task ledger.`;
}

/** Send back carries the human's words into the redraft, so it needs some (spec §6):
 *  a typed note, or at least one anchored comment. */
export function sendBackReady(note: string, anchoredCount: number): boolean {
  return note.trim().length > 0 || anchoredCount > 0;
}

/**
 * Whether the pane's selection is the gated thing: the activity itself, its gated
 * phase, or that phase's gate task. Selecting another phase or task of the same
 * activity in the list must not offer to decide this gate from there.
 */
export function paneDecisionApplies(
  gate: { lifecyclePhase: string; gateTask?: string | undefined },
  selection: LensSelection
): boolean {
  if (selection.lifecyclePhase !== undefined && selection.lifecyclePhase !== gate.lifecyclePhase) {
    return false;
  }
  return selection.task === undefined || selection.task === gate.gateTask;
}

/**
 * The decision the shared pane can make (DetailPane's `decision` prop), handed down
 * by the route ONLY for an activity the live workflow reports at a gate.
 */
export interface PaneDecision {
  lifecyclePhase: string;
  /** The gate is still open (the item is still owed). False while a just-decided
   *  row lingers: the pane keeps its evidence line but Approve / Send back go off
   *  and the selection stops reading AWAITING YOU. */
  open: boolean;
  gateTask?: string | undefined;
  /** A decision for this gate is in flight — Approve/Send back stay off. */
  busy: boolean;
  /** Anchored comments (CommentProvider) the send-back will carry. */
  anchoredCount: number;
  /** The decision's current line, if one was made. */
  note?: FlowNote | undefined;
  /** A decision made on this gate, while its record lives (decidedFor). */
  decided?: DecidedMark | undefined;
  onApprove: () => void;
  onSendBack: (note: string) => void;
}
