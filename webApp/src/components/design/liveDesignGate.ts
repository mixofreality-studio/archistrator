/**
 * WHETHER A DESIGN ARTIFACT ACTUALLY AWAITS A HUMAN, read off the LIVE authority.
 *
 * ── The defect this exists to close (stage 4b2 Task 9, Task-5 review finding) ──
 * `SystemDesignView` opened its approve/reject gate on `stage === 'awaitingReview'`.
 * That stage comes from the DERIVED design-artifact session door, which is a
 * projection of one durable `ArtifactSlot` — and since 4b1 retired the co-author
 * workflow its whole vocabulary is `{unknown, committed, withdrawn, draftFailed}`.
 * Nothing can emit `awaitingReview` any more, so the MCP design widget's gate could
 * NEVER open: a founder looking at a draft that was waiting for them was shown a
 * screen with no approve, no send-back and no withdraw.
 *
 * The live authority is `QueryActivityView`, which 4b1 made authoritative for the
 * three design activities: the artifact's REVIEW task carries a revision whose
 * `outcome` is `awaitingHuman` exactly while a human decision is owed on it. That is
 * the same thing the Activity Experience's own review bar reads
 * (`revisionWire?.outcome === 'awaitingHuman'`), so the two surfaces now open their
 * gates on ONE fact rather than on two vocabularies that drifted apart.
 *
 * ── And the failed run's deep link, from the same read ──────────────────────
 * `SessionStateView.failureRunUrl` was a wire field nothing ever set; it is gone.
 * What IS recorded is the failed attempt's own sentence on the DISPATCH task's
 * revision (`DeliveryTaskRevisionView.detail` — "a failed venue's whole sentence
 * including its run URL"), so the panel's "View the failed run" link is read back out
 * of that rather than deleted. Nothing is fabricated: a sentence with no URL in it
 * yields no link, exactly as an absent `failureRunUrl` did.
 *
 * Pure and React-free so `node --test` loads it directly.
 */
import type { ActivityViewWire } from '../activity/activityViewToGraph.ts';

/** What the design screen needs from the activity view, and nothing else. */
export interface LiveDesignGate {
  /** A human decision is owed on this artifact's review task RIGHT NOW. */
  awaitingHuman: boolean;
  /** The failed dispatch attempt's run URL, when its sentence carries one. */
  failedRunUrl: string | undefined;
}

/** No activity view (loading, no such activity, an error) answers "no gate, no link". */
const CLOSED: LiveDesignGate = { awaitingHuman: false, failedRunUrl: undefined };

/**
 * The first absolute http(s) URL in a sentence, or undefined. Trailing sentence
 * punctuation is trimmed — the venue's sentence is prose, not a link list.
 */
function urlIn(sentence: string | null | undefined): string | undefined {
  if (sentence === undefined || sentence === null) return undefined;
  const found = /https?:\/\/[^\s]+/.exec(sentence)?.[0];
  return found === undefined ? undefined : found.replace(/[.,;:)\]]+$/, '');
}

/** The latest revision of one task — revisions arrive oldest first. */
function latestOf(
  view: ActivityViewWire,
  taskId: string
): ActivityViewWire['tasks'][number]['revisions'][number] | undefined {
  const task = view.tasks.find((t) => t.id === taskId);
  return task === undefined ? undefined : task.revisions[task.revisions.length - 1];
}

export function liveDesignGate(
  view: ActivityViewWire | undefined,
  ref: { reviewTaskId: string; dispatchTaskId: string }
): LiveDesignGate {
  if (view === undefined || ref.reviewTaskId.length === 0) return CLOSED;
  const review = latestOf(view, ref.reviewTaskId);
  const dispatch = latestOf(view, ref.dispatchTaskId);
  return {
    awaitingHuman: review?.outcome === 'awaitingHuman',
    // Only a FAILED dispatch's sentence is a failed run. A passed attempt's detail
    // names what it drafted, and linking that as "the failed run" would be a lie the
    // reader has no way to check.
    failedRunUrl: dispatch?.outcome === 'failed' ? urlIn(dispatch.detail) : undefined,
  };
}
