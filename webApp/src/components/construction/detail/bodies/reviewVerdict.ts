/**
 * THE REVIEW VERDICT — and the fact that, today, there isn't one.
 *
 * A gate task is where a human decision is owed, so the review body's second
 * half should be per-reviewer rows: role, PASS / PASS-WITH-NOTES / FAIL, notes.
 * It cannot be, and the reason is a known dropped signal rather than a gap in
 * this surface:
 *
 *   `awaitPhaseDecision` never dereferences `sig.Feedback`. The reviewer's
 *   structured verdict is carried on the signal, is never read, and is never
 *   written to head-state. `ConstructionReviewer` on the wire is
 *   {role, perspective, referenceArtifact?, mayAmend} — there is no verdict
 *   FIELD to render, because no verdict is stored.
 *
 * So this module reports the two things that DO survive, and keeps them apart:
 *
 *   1. THE REVIEWER SET — who the reviewEngine computed should look. Real, and
 *      worth showing: "who was asked" is genuinely known. It is NOT a verdict
 *      and is never styled as one.
 *   2. PROSE on the activity's produced records. Sometimes the only surviving
 *      trace of what a reviewer thought, and rendered stamped
 *      `≈ reconstructed from the produced-record note` — as PROSE, never as a
 *      structured verdict, never with a PASS/FAIL chip synthesized from it.
 *      A note saying "reviewed and merged" is not a recorded PASS, and turning
 *      it into one here would manufacture the exact signal the server dropped.
 *
 * Pure, so the rule survives without a renderer (node:test cannot load `.tsx`).
 */
import type {
  ConstructionReviewSet,
  ConstructionRow,
  ProducedArtifactRow,
} from '../../../../contracts/types';

/** The strongest thing actually recorded about this review. */
export type VerdictSource = 'reviewerSet' | 'reconstructedNote' | 'none';

export interface ReviewerRow {
  role: string;
  perspective: string;
  mayAmend: boolean;
  referenceArtifact?: string;
}

export interface ReconstructedNote {
  /** Index into the row's ORIGINAL `produced` array — the comment anchor's path. */
  index: number;
  kind: string;
  title: string;
  source: string;
  text: string;
}

/**
 * The stamp every reconstructed note carries.
 *
 * Deliberately the same `≈` the provenance axis uses for a reconstructed record:
 * one mark, one meaning, across the list, the pane's provenance chip and here.
 */
export const RECONSTRUCTED_VERDICT_STAMP = '≈ reconstructed from the produced-record note';

// The plain sentence (designer P1-7) and, for the tooltip, the engineering detail
// behind it — the reader first learns WHAT is missing, and only on hover why.
export const REVIEWER_SET_STATEMENT =
  'Reviewer verdicts aren’t recorded yet — this is who was asked, not what they said.';

const REVIEWER_SET_DETAIL =
  'This is the reviewer set the reviewEngine computed — who was asked to look. No per-reviewer verdict is recorded anywhere: the construction gate drops the reviewer feedback it is handed (awaitPhaseDecision never dereferences sig.Feedback), so what they answered was never written down.';

const RECONSTRUCTED_STATEMENT =
  'No verdict was recorded for this review — what survives is prose on the produced records, shown below as prose.';

const RECONSTRUCTED_DETAIL =
  'No structured verdict exists for this review — the construction gate drops the reviewer feedback it is handed (awaitPhaseDecision never dereferences sig.Feedback). What survives is prose on the activity’s produced records, reproduced below AS prose.';

const NONE_STATEMENT = 'No verdict and no reviewer set are recorded for this review.';

const NONE_DETAIL =
  'The construction gate drops the reviewer feedback it is handed (awaitPhaseDecision never dereferences sig.Feedback), so an approved gate leaves no trace of who approved it or why.';

export interface ReviewVerdictView {
  source: VerdictSource;
  /** Who was asked. Never presented as what they answered. */
  reviewers: ReviewerRow[];
  /** Surviving prose, always stamped. Rendered alongside the reviewer set when both exist. */
  notes: ReconstructedNote[];
  /** Present exactly when `notes` is non-empty. */
  stamp?: string;
  statement: string;
  /** The engineering reason, for the statement's tooltip (designer P1-7). */
  detail: string;
}

function noteFrom(artifact: ProducedArtifactRow, index: number): ReconstructedNote {
  return {
    index,
    kind: artifact.kind,
    title: artifact.title,
    source: artifact.source,
    text: artifact.note,
  };
}

export function reviewVerdictFor(
  row: ConstructionRow | undefined,
  reviewSet: ConstructionReviewSet | undefined
): ReviewVerdictView {
  const reviewers: ReviewerRow[] = (reviewSet?.reviewers ?? []).map((r) => ({
    role: r.role,
    perspective: r.perspective,
    mayAmend: r.mayAmend,
    ...(r.referenceArtifact !== undefined ? { referenceArtifact: r.referenceArtifact } : {}),
  }));

  const notes = (row?.produced ?? []).map(noteFrom).filter((n) => n.text.trim().length > 0);

  const source: VerdictSource =
    reviewers.length > 0 ? 'reviewerSet' : notes.length > 0 ? 'reconstructedNote' : 'none';

  return {
    source,
    reviewers,
    notes,
    ...(notes.length > 0 ? { stamp: RECONSTRUCTED_VERDICT_STAMP } : {}),
    statement:
      source === 'reviewerSet'
        ? REVIEWER_SET_STATEMENT
        : source === 'reconstructedNote'
          ? RECONSTRUCTED_STATEMENT
          : NONE_STATEMENT,
    detail:
      source === 'reviewerSet'
        ? REVIEWER_SET_DETAIL
        : source === 'reconstructedNote'
          ? RECONSTRUCTED_DETAIL
          : NONE_DETAIL,
  };
}

// ---------------------------------------------------------------------------
// Comment anchors — item-granular, so a send-back carries per-item feedback
// ---------------------------------------------------------------------------

/**
 * The JSONPath a comment on one reviewer row anchors to.
 *
 * The server treats jsonPath as OPAQUE guidance (it never evaluates it), so the
 * scheme only has to be stable and human-meaningful in a redraft prompt — the
 * same contract CommentContext's own builders work to.
 */
export function reviewerAnchorPath(activityId: string, index: number): string {
  return `$.activityConstruction[${activityId}].reviewSet.reviewers[${String(index)}]`;
}

/** The JSONPath a comment on one produced-record note anchors to. */
export function producedNoteAnchorPath(activityId: string, index: number): string {
  return `$.activityConstruction[${activityId}].produced[${String(index)}].Note`;
}
