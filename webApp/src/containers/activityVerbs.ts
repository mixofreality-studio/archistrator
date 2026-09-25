/**
 * WHICH OP EACH VERB DISPATCHES, per activity type (R12) — pure, React-free and
 * tested, so `node --test` loads it directly (hence the explicit `.ts` on the
 * relative VALUE imports).
 *
 * It NAMES A TARGET; it does not call a hook. The container maps a target onto
 * its hook, which is the only layer allowed to. Keeping the rule here rather than
 * in a `switch` inside the container is what lets the table be read and asserted
 * without a QueryClient in the way — and the table is not obvious: three rails
 * (construction, system design, project design) answer the same four verbs, and
 * two of the three are missing ops the third has.
 *
 * ── The three rails ─────────────────────────────────────────────────────────
 *   construction (everything but the three design activities)
 *       approve / send back  → SubmitPhaseDecision, keyed on the TASK's own
 *                              lifecycle phase (not the activity's current one)
 *       resolve / reopen     → NOTHING. `constructionManager` has no
 *                              SetReviewCommentStatus and no AskQuestions
 *                              (R2/GAP-6): the unified rail is read-unified and
 *                              write-asymmetric until stage 4's
 *                              SubmitReviewDecision.
 *   requirements / architecture (activities 1–2)
 *       every verb            → the system-design rail, keyed on the RESOLVED
 *                              artifact kind.
 *   projectDesign (activity 3)
 *       approve               → SubmitSDPDecision, then AdvanceToConstruction
 *       ask / resolve / reopen → the PROJECT-DESIGN rail: AskQuestions and
 *                              SetReviewCommentStatus both exist there (spec §6:
 *                              "comments and questions allowed" at M0).
 *       send back             → NOTHING (spec R7): the plan is derived, so
 *                              changing it means amending the Architecture.
 *
 * ── The kind is an INPUT, and it is the RESOLVED one ────────────────────────
 * A review task carries no `artifactKind` of its own — it carries `reviews`, and
 * the dispatch it judges is what names the artifact (`artifactKindOf`,
 * activityViewToGraph.ts). The container feeds `taskFactsFor(view, taskId)
 * .artifactKind`. Reading `lifecycles.gen.ts`'s raw `task.artifactKind` here
 * would hand every design review `undefined` and silently strip its approve
 * verb, so this refuses loudly instead of inventing one.
 */
import type { ArtifactKind, ArtifactKindFull, ProjectArtifactKind } from '../contracts/types.ts';
import { SDP_REVIEW_KIND } from '../contracts/types.ts';
import { NO_ARTIFACT_KIND } from '../components/activity/activityCopy.ts';
import { SLOT_KIND } from '../components/activity/taskArtifactFor.ts';

export type VerbTarget =
  | { kind: 'constructionPhaseDecision'; lifecyclePhase: string }
  | { kind: 'designReviewDecision'; artifactKind: ArtifactKind }
  | { kind: 'sdpDecision' }
  /**
   * The Phase-2 question rail (`projectDesignAskQuestions`). Separate from
   * `sdpDecision` because it is a different op with a different body — the
   * decision commits an option, this one appends question entries to the M0
   * review ledger without deciding anything.
   */
  | { kind: 'projectAsk'; artifactKind: ProjectArtifactKind }
  | { kind: 'none'; reason: string };

export interface VerbsFor {
  approve: VerbTarget;
  /** `{ kind: 'none' }` for projectDesign (spec R7). */
  sendBack: VerbTarget;
  /** `{ kind: 'none' }` for construction (R2/GAP-6) — no `AskQuestions` op there. */
  ask: VerbTarget;
  /** `{ kind: 'none' }` for construction (R2/GAP-6). */
  commentStatus: VerbTarget;
  rerun: VerbTarget;
  allowSendBack: boolean;
  approveCopy?: { label: string; consequence: string };
}

/**
 * The Phase-1 slot kinds a DESIGN review decides on, in the vocabulary
 * `systemDesignSubmitReviewDecision` speaks. `SLOT_KIND` (taskArtifactFor.ts) is
 * the Go-cased → app-string half of the same journey and is not repeated here;
 * this second hop is what keeps `designReviewDecision.artifactKind` a genuine
 * Phase-1 `ArtifactKind` with no cast. `sdpReview` is deliberately absent: it is
 * a Phase-2 kind, and the projectDesign branch answers before this is reached.
 */
const DESIGN_DECISION_KIND: Readonly<Partial<Record<ArtifactKindFull, ArtifactKind>>> = {
  mission: 'mission',
  glossary: 'glossary',
  volatilities: 'volatilities',
  coreUseCases: 'coreUseCases',
  system: 'system',
};

/** The two design activities whose verbs ride the SYSTEM DESIGN rail. */
const DESIGN_RAIL_TYPES: ReadonlySet<string> = new Set(['requirements', 'architecture']);

/**
 * Why a construction gate offers no thread lifecycle (R2/GAP-6). Named here
 * rather than in `activityCopy.ts` because it is the REASON a target carries,
 * not a sentence any component renders on its own.
 */
const NO_CONSTRUCTION_THREAD_OP =
  'A construction review thread cannot yet be resolved, reopened or replied to: the construction manager has no comment-status op.';

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
  /** The RESOLVED kind from `taskFactsFor` (Task 2) — a review task has none of its own. */
  artifactKind?: string | undefined;
}): VerbsFor {
  if (input.type === 'projectDesign') {
    const sdp: VerbTarget = { kind: 'sdpDecision' };
    return {
      approve: sdp,
      sendBack: { kind: 'none', reason: NO_SDP_SEND_BACK },
      // Spec §6: comments AND questions are allowed at M0. The op has always
      // existed (`projectDesignAskQuestions`); what was missing was the hook.
      ask: { kind: 'projectAsk', artifactKind: SDP_REVIEW_KIND },
      // The Phase-2 rail DOES have SetReviewCommentStatus — the M0 gate can
      // resolve and reopen its own threads even though it can never send back.
      commentStatus: sdp,
      rerun: { kind: 'none', reason: NO_SDP_RERUN },
      allowSendBack: false,
      approveCopy: { ...M0_APPROVE_COPY },
    };
  }

  if (DESIGN_RAIL_TYPES.has(input.type)) {
    const slot = input.artifactKind === undefined ? undefined : SLOT_KIND[input.artifactKind];
    const kind = slot === undefined ? undefined : DESIGN_DECISION_KIND[slot];
    const target: VerbTarget =
      kind === undefined
        ? { kind: 'none', reason: NO_ARTIFACT_KIND }
        : { kind: 'designReviewDecision', artifactKind: kind };
    return {
      approve: target,
      sendBack: target,
      ask: target,
      commentStatus: target,
      rerun: target,
      allowSendBack: kind !== undefined,
    };
  }

  // Every other type is a CONSTRUCTION activity: one rail, one op, keyed on the
  // task's own lifecycle phase. Its artifact kind ('SRS', 'DetailedDesign',
  // 'Construction', 'Integration', 'STP') names no slot and is not consulted —
  // a service gate never routes through the design op.
  const phase: VerbTarget = {
    kind: 'constructionPhaseDecision',
    lifecyclePhase: input.lifecyclePhase,
  };
  return {
    approve: phase,
    sendBack: phase,
    ask: { kind: 'none', reason: NO_CONSTRUCTION_THREAD_OP },
    commentStatus: { kind: 'none', reason: NO_CONSTRUCTION_THREAD_OP },
    rerun: phase,
    allowSendBack: true,
  };
}
