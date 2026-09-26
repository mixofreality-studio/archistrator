/**
 * WHAT EACH VERB SENDS, per activity type (R12) — pure, React-free and tested, so
 * `node --test` loads it directly (hence the explicit `.ts` on the relative VALUE
 * imports).
 *
 * It NAMES A TARGET, never a hook: the container holds one hook per delivery op and
 * maps a target onto it. What used to be a RAIL CHOICE here (three managers
 * answering the same four verbs, two of them missing ops the third had) is now only
 * a question of WHICH OP and, for a decision, which members of
 * `ReviewDecisionInput` it fills. The server reads the rail off the committed
 * activity list and resolves the artifact kind from the (activity, task) the
 * container already has, so no target carries a kind any more.
 *
 * ── What is still asymmetric, and why that is not this file's bug ────────────
 * Stage 4a unified the WRITE SURFACE, not every rail's verbs. `deliveryManager`
 * answers ContractMisuse for, on the CONSTRUCTION rail:
 *   - `ReviewSetCommentStatus` / `ReviewWithdraw` ("no comment-status or withdraw
 *     verb until stage 4b"),
 *   - `AskQuestions` ("no question verb until stage 4b"),
 *   - `AcknowledgeStaleBasis` (same),
 *   - `DispatchActivityTask` ("run/re-run has no op before stage 4b — a send-back
 *     re-dispatches the task").
 * So the R2/GAP-6 asymmetry SURVIVES 4a: it moved from "this manager has no op" to
 * "the one manager refuses this rail", which is the same dead button to a user.
 * This table therefore still withholds Ask, Resolve and the stale exits on a
 * construction gate; `rerun` there is an Override(retry), which is a real op.
 *
 * ── The M0 gate keeps its two special rules (spec §6/R7) ────────────────────
 *   approve  → commits the chosen OPTION (the only intent carrying an optionId),
 *              then ADVANCES; both are SubmitReviewDecision calls.
 *   sendBack → nothing. The plan is DERIVED, so changing it means amending the
 *              Architecture.
 */
import { REVIEW_DECISION_APP_TO_ORDINAL } from '../contracts/enums.gen.ts';
import type { components } from '../contracts/schema';
import { NO_ARTIFACT_KIND } from '../components/activity/activityCopy.ts';
import { SLOT_KIND } from '../components/activity/taskArtifactFor.ts';

/**
 * The wire ordinals this table hands out, named once and typed as the WIRE enum —
 * so a table that named an ordinal outside ReviewDecision would not compile, and
 * the appended members (4 advance, 5 setCommentStatus) cannot drift.
 */
type WireReviewDecision = components['schemas']['DeliveryReviewDecision'];

export const REVIEW_APPROVE = REVIEW_DECISION_APP_TO_ORDINAL.approve as WireReviewDecision;
export const REVIEW_REJECT = REVIEW_DECISION_APP_TO_ORDINAL.reject as WireReviewDecision;
export const REVIEW_ADVANCE = REVIEW_DECISION_APP_TO_ORDINAL.advance as WireReviewDecision;
export const REVIEW_SET_COMMENT_STATUS =
  REVIEW_DECISION_APP_TO_ORDINAL.setCommentStatus as WireReviewDecision;

/**
 * WHICH OP a verb rides.
 *
 *  - `decision`        → SubmitReviewDecision, filling `decision` (and `optionId`
 *                        when `needsOption`).
 *  - `ask`             → AskQuestions.
 *  - `commentStatus`   → SubmitReviewDecision with the comment members, which the
 *                        container fills per comment.
 *  - `dispatch`        → DispatchActivityTask (a design draft or redraft).
 *  - `override`        → OverrideActivity (construction's re-run).
 *  - `acknowledgeStale`→ AcknowledgeStaleBasis.
 *  - `none`            → nothing, carrying the REASON the bar renders.
 */
export type VerbTarget =
  | { kind: 'decision'; decision: WireReviewDecision; needsOption?: boolean }
  | { kind: 'ask' }
  | { kind: 'commentStatus' }
  | { kind: 'dispatch' }
  | { kind: 'override' }
  | { kind: 'acknowledgeStale' }
  | { kind: 'none'; reason: string };

export interface VerbsFor {
  approve: VerbTarget;
  /** `{ kind: 'none' }` for projectDesign (spec R7). */
  sendBack: VerbTarget;
  /** `{ kind: 'none' }` for construction until stage 4b. */
  ask: VerbTarget;
  /** `{ kind: 'none' }` for construction until stage 4b. */
  commentStatus: VerbTarget;
  /** A design redraft, a construction Override(retry), or nothing (M0). */
  rerun: VerbTarget;
  /** The "reviewed — unaffected" exit. `{ kind: 'none' }` for construction. */
  acknowledgeStale: VerbTarget;
  /** The other stale exit: AMEND. A design redraft carrying the reconcile
   *  rationale; for M0 it is a NAVIGATION (amend the Architecture), which the
   *  container owns because this table names ops, not routes. */
  reconcileStale: VerbTarget;
  allowSendBack: boolean;
  approveCopy?: { label: string; consequence: string };
  /** True where approve must also ADVANCE once it has committed (the M0 gate). */
  advanceAfterApprove?: boolean;
}

/** The two design activities whose verbs ride the system-design rail. */
const DESIGN_RAIL_TYPES: ReadonlySet<string> = new Set(['requirements', 'architecture']);

/**
 * Why a construction gate offers no thread lifecycle and no question. Named here
 * rather than in `activityCopy.ts` because it is the REASON a target carries, not a
 * sentence any component renders on its own.
 *
 * Stage 4a did NOT close this — `deliveryManager` refuses both on the construction
 * rail "until stage 4b", so the button would dispatch a guaranteed 400.
 */
const NO_CONSTRUCTION_THREAD_OP =
  'A construction review thread cannot yet be resolved, reopened or replied to: the delivery manager has no comment-status or question verb for the construction rail until stage 4b.';

const NO_CONSTRUCTION_STALE_OP =
  'A construction activity has no stale-basis acknowledgement until stage 4b.';

const NO_SDP_SEND_BACK =
  'The M0 gate has no send-back: the plan is derived, so changing it means amending the Architecture.';

const NO_SDP_RERUN =
  'The SDP review is re-derived from the committed plan, not re-run from this gate.';

/** The M0 approval says what it costs, because approving it starts spending. */
const M0_APPROVE_COPY = {
  label: 'Approve plan & cost — start construction',
  consequence: 'Commits the chosen option as the plan of record and starts construction',
} as const;

export function verbsFor(input: {
  type: string;
  variant?: string | undefined;
  taskId: string;
  lifecyclePhase: string;
  /** The RESOLVED kind from `taskFactsFor` (Task 2) — a review task has none of its
   *  own. It no longer chooses an OP; it only says whether this gate judges a design
   *  artifact the SPA knows, which is what decides the design verbs. */
  artifactKind?: string | undefined;
}): VerbsFor {
  if (input.type === 'projectDesign') {
    return {
      approve: { kind: 'decision', decision: REVIEW_APPROVE, needsOption: true },
      sendBack: { kind: 'none', reason: NO_SDP_SEND_BACK },
      // Spec §6: comments AND questions are allowed at M0.
      ask: { kind: 'ask' },
      commentStatus: { kind: 'commentStatus' },
      rerun: { kind: 'none', reason: NO_SDP_RERUN },
      acknowledgeStale: { kind: 'acknowledgeStale' },
      // The M0 plan is DERIVED: it is reconciled by amending what it derives from,
      // which is a navigation to the Architecture activity, not an op.
      reconcileStale: { kind: 'none', reason: NO_SDP_SEND_BACK },
      allowSendBack: false,
      approveCopy: { ...M0_APPROVE_COPY },
      advanceAfterApprove: true,
    };
  }

  if (DESIGN_RAIL_TYPES.has(input.type)) {
    // A design gate must name an artifact the SPA knows, or it judges nothing. The
    // server would resolve the kind from the task on its own, but a task whose
    // resolved kind is unknown here has no renderable artifact either, so refusing
    // loudly is what keeps the bar honest — the same guard the three-rail table
    // made, minus the op choice.
    const slot = input.artifactKind === undefined ? undefined : SLOT_KIND[input.artifactKind];
    if (slot === undefined) {
      const none: VerbTarget = { kind: 'none', reason: NO_ARTIFACT_KIND };
      return {
        approve: none,
        sendBack: none,
        ask: none,
        commentStatus: none,
        rerun: none,
        acknowledgeStale: none,
        reconcileStale: none,
        allowSendBack: false,
      };
    }
    return {
      approve: { kind: 'decision', decision: REVIEW_APPROVE },
      sendBack: { kind: 'decision', decision: REVIEW_REJECT },
      ask: { kind: 'ask' },
      commentStatus: { kind: 'commentStatus' },
      rerun: { kind: 'dispatch' },
      acknowledgeStale: { kind: 'acknowledgeStale' },
      reconcileStale: { kind: 'dispatch' },
      allowSendBack: true,
    };
  }

  // Every other type is a CONSTRUCTION activity. Approve and send back are the
  // same op as everywhere else; the rest the rail still refuses.
  return {
    approve: { kind: 'decision', decision: REVIEW_APPROVE },
    sendBack: { kind: 'decision', decision: REVIEW_REJECT },
    ask: { kind: 'none', reason: NO_CONSTRUCTION_THREAD_OP },
    commentStatus: { kind: 'none', reason: NO_CONSTRUCTION_THREAD_OP },
    rerun: { kind: 'override' },
    acknowledgeStale: { kind: 'none', reason: NO_CONSTRUCTION_STALE_OP },
    reconcileStale: { kind: 'none', reason: NO_CONSTRUCTION_STALE_OP },
    allowSendBack: true,
  };
}
