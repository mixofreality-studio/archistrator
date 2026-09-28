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
 * ── What the construction rail now answers, and the ONE thing it still does not ─
 * Stage 4a unified the WRITE SURFACE and left five construction paths answering a
 * guaranteed 400 ("until stage 4b"). Stage 4b1 made all five real, each by its own
 * mechanism, so this table no longer withholds them:
 *   - `ReviewSetCommentStatus` → `SetTaskCommentStatus` on the round the COMMENT is
 *     on (resolve / reopen),
 *   - `ReviewWithdraw` → `WithdrawReviewRound` (a round no one is judging),
 *   - `AskQuestions` → `AskTaskQuestions`, which lands the questions on the round's
 *     thread carried by an abstention. There is no agent answerer yet: a human
 *     answers a construction question (Task 12, D4),
 *   - `DispatchActivityTask` → `RedraftTask`, which withdraws the unjudged round and
 *     re-opens the judged pair at revision n+1 (Task 12, D6) — a REAL dispatch, not
 *     the Override(retry) this table used to route a re-run through,
 *   - approve / send back → `SubmitTaskDecision`, which works for the first time.
 * The ONE survivor is `AcknowledgeStaleBasis`: it is a SEMANTIC refusal on this rail
 * (`NO_CONSTRUCTION_STALE_OP`), not a missing op — see that constant.
 *
 * `OverrideActivity` is no longer any verb of this table. It kept the variance loop
 * and gained a second meaning (re-open a finished activity), and the two are
 * activity-scoped rather than gate-scoped — `activityOverride.ts` owns them.
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
 *  - `ask`             → AskQuestions (folding a reply into its question text when
 *                        `foldReplies` — the Phase-2 ledger refuses a `replyTo`).
 *  - `commentStatus`   → SubmitReviewDecision with the comment members, which the
 *                        container fills per comment.
 *  - `dispatch`        → DispatchActivityTask (a design draft, or a construction
 *                        redraft that re-opens the judged pair at revision n+1).
 *  - `acknowledgeStale`→ AcknowledgeStaleBasis.
 *  - `none`            → nothing, carrying the REASON the bar renders.
 */
export type VerbTarget =
  | { kind: 'decision'; decision: WireReviewDecision; needsOption?: boolean }
  | { kind: 'ask'; foldReplies?: boolean }
  | { kind: 'commentStatus' }
  | { kind: 'dispatch' }
  | { kind: 'acknowledgeStale' }
  | { kind: 'none'; reason: string };

export interface VerbsFor {
  approve: VerbTarget;
  /** `{ kind: 'none' }` for projectDesign (spec R7). */
  sendBack: VerbTarget;
  /** Every rail has the question op now; `{ kind: 'none' }` only where the gate
   *  judges an artifact kind the SPA does not know. */
  ask: VerbTarget;
  /** Every rail has the comment-status op now — same exception as `ask`. */
  commentStatus: VerbTarget;
  /** A redraft on every rail, or nothing (M0: the plan is re-derived, not re-run). */
  rerun: VerbTarget;
  /** The "reviewed — unaffected" exit. `{ kind: 'none' }` for construction, whose
   *  rounds carry no artifact SLOT to clear a basis flag on. */
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
 * Why a construction gate offers neither stale exit — the ONE refusal that survived
 * stage 4b1, and it is SEMANTIC, not a missing op.
 *
 * `StaleBasis` is a field on an artifact SLOT, and the verb that clears it takes the
 * kind of the slot it clears. A construction task's `artifactKind` names its own WORK
 * PRODUCT (`srs`, `detailedDesign`, `construction`, `integration`, `stp`) and none of
 * those is one of the seventeen design slots, so a construction round is KINDLESS and
 * the verb has no slot to name; clearing the Architecture's flag from a construction
 * activity would un-stale it for every other activity too, which is the architect's
 * decision on the design rail. `deliveryManager` therefore answers FailedPrecondition
 * naming the missing datum (Task 12, D3), which is a refusal the button must not
 * provoke.
 */
const NO_CONSTRUCTION_STALE_OP =
  'A stale basis is acknowledged on the design activity that owns the slot: a construction round judges its own work product, names no artifact slot, and so has no basis flag to clear.';

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
      // Spec §6: comments AND questions are allowed at M0. A FOLLOW-UP question folds
      // its reply into its own text: the Phase-2 ledger refuses a replyTo outright
      // (pdCheckNoReplyTo, RULING P13), so sending one turns "reply in an M0 question
      // thread, then Ask" into a 400 — the same asymmetry `needsOption` answers for the
      // decision, answered the same way (askEntriesFor).
      ask: { kind: 'ask', foldReplies: true },
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

  // Every other type is a CONSTRUCTION activity. Every verb but the stale
  // acknowledgement is the same op as everywhere else now (stage 4b1 Task 12): the
  // re-run is a real `RedraftTask`, which withdraws the round nobody judged and
  // re-opens the judged pair at revision n+1 — not another attempt at the same
  // revision, which is the VARIANCE loop's shape and is the child's own business.
  return {
    approve: { kind: 'decision', decision: REVIEW_APPROVE },
    sendBack: { kind: 'decision', decision: REVIEW_REJECT },
    ask: { kind: 'ask' },
    commentStatus: { kind: 'commentStatus' },
    rerun: { kind: 'dispatch' },
    acknowledgeStale: { kind: 'none', reason: NO_CONSTRUCTION_STALE_OP },
    // The AMEND exit is a redraft, which this rail now has; it is unreachable in
    // practice for the same reason the acknowledgement is refused (only a SLOT
    // reports a stale basis, and no construction gate judges one), and it is the
    // honest op for the day one does.
    reconcileStale: { kind: 'dispatch' },
    allowSendBack: true,
  };
}
