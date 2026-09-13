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
 * DERIVED, NOT STORED
 * -------------------
 * The route stores only what happened on the wire — the decision, when the server
 * answered, and the error if it failed — in event handlers. Everything else is
 * derived here at render from the live session and the clock, so no effect ever
 * sets state to follow the workflow (the React Compiler rule this codebase holds).
 *
 * A failed request follows fix C's rule (beginControl.dispatchOutcomeFor): a 4xx is
 * a rejection, nothing was decided; a 5xx or no answer is an UNKNOWN outcome — the
 * signal may have been delivered — so the row keeps watching the gate, and says
 * "resumed" if it clears.
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

/** What happened on the wire, as the route records it in its event handlers. */
export interface DecisionRecord {
  /** The owed item's key — the decision's own identity. */
  key: string;
  activityId: string;
  decision: GateDecision;
  /** When the server ANSWERED (success, or a failure whose outcome is unknown). */
  sentAt?: number;
  /** The failure, when the request did not come back clean. */
  error?: { status?: number | undefined; message: string };
}

export type DecisionView =
  | { kind: 'sending' }
  | { kind: 'awaitingResume' }
  | { kind: 'resumed'; lifecyclePhase?: string }
  | { kind: 'notLanded' }
  | { kind: 'failed'; outcome: DispatchOutcome }
  | { kind: 'done' };

/** What the live session says now: its stage, `null` where it no longer exists,
 *  `undefined` where there is no answer. */
export interface ObservedGate {
  stage: ConstructionStage | null | undefined;
  /** The activity's current lifecycle phase, for "now in <phase>". */
  lifecyclePhase?: string | undefined;
}

function resumed(o: ObservedGate): DecisionView {
  return o.lifecyclePhase !== undefined
    ? { kind: 'resumed', lifecyclePhase: o.lifecyclePhase }
    : { kind: 'resumed' };
}

export function decisionViewFor(r: DecisionRecord, o: ObservedGate, now: number): DecisionView {
  // Left the gate: the session moved on, or no longer exists (the activity ended).
  const leftGate = o.stage === null || (o.stage !== undefined && o.stage !== 'awaitingApproval');
  const lingerOver = r.sentAt !== undefined && now - r.sentAt > RESUMED_LINGER_MS;
  if (r.error !== undefined) {
    const outcome = dispatchOutcomeFor(r.error.status, r.error.message);
    if (outcome.kind === 'unknown' && leftGate) return lingerOver ? { kind: 'done' } : resumed(o);
    return { kind: 'failed', outcome };
  }
  if (r.sentAt === undefined) return { kind: 'sending' };
  if (leftGate) return lingerOver ? { kind: 'done' } : resumed(o);
  return now - r.sentAt > RESUME_TIMEOUT_MS ? { kind: 'notLanded' } : { kind: 'awaitingResume' };
}

/** A decision is in flight: Approve/Send back stay off so one click sends one signal. */
export function decisionBusy(view: DecisionView | undefined): boolean {
  return view?.kind === 'sending' || view?.kind === 'awaitingResume';
}

/** The one line a row (and the pane) shows for a decision. */
export interface FlowNote {
  tone: 'progress' | 'ok' | 'danger';
  text: string;
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
      const where = view.lifecyclePhase !== undefined ? phaseName(view.lifecyclePhase) : undefined;
      if (decision === 'sendBack') {
        return {
          tone: 'ok',
          text: where !== undefined ? `Sent back — redrafting ${where}` : 'Sent back — redrafting',
        };
      }
      return { tone: 'ok', text: where !== undefined ? `Resumed — now in ${where}` : 'Resumed' };
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
  onApprove: () => void;
  onSendBack: (note: string) => void;
}
