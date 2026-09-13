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
 * newer than the failure has answered (the Begin ruling, review I1).
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
  /** When the latest session read was fetched. */
  seenAt?: number | undefined;
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
    | { stage: ConstructionStage | null; epoch: number; seenAt: number; leftAt?: number }
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
    seenAt: occurrence.seenAt,
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
  // answered was left, and the record is retired (review C2).
  if (o.epoch !== undefined && o.epoch > r.epoch) return { kind: 'done' };
  // Left the gate: the session moved on, or no longer exists (the activity ended).
  const leftGate = o.stage === null || (o.stage !== undefined && o.stage !== 'awaitingApproval');
  const lingerOver = r.sentAt !== undefined && now - r.sentAt > RESUMED_LINGER_MS;
  if (r.error !== undefined) {
    const outcome = dispatchOutcomeFor(r.error.status, r.error.message);
    // Only an UNKNOWN outcome can turn into a resume: a 4xx decided nothing, so a
    // gate that clears afterwards cleared for some other reason.
    if (outcome.kind === 'unknown' && leftGate)
      return lingerOver ? { kind: 'done' } : resumed(r, o);
    const newerRead = o.seenAt !== undefined && r.sentAt !== undefined && o.seenAt > r.sentAt;
    return { kind: 'failed', outcome, watching: outcome.kind === 'unknown' && !newerRead };
  }
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
      if (decision === 'sendBack') return { tone: 'ok', text: `Sent back — redrafting ${gated}` };
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
