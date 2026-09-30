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
  /**
   * The artifact's DISPATCH task is running right now — a draft or an amendment in
   * flight. Read from the same activity view as {@link awaitingHuman}, because the
   * derived session door lost the `drafting`/`redrafting` stages that used to say it.
   */
  dispatchRunning: boolean;
  /** The failed dispatch attempt's run URL, when its sentence carries one. */
  failedRunUrl: string | undefined;
}

/** No activity view (loading, no such activity, an error) answers "no gate, no link". */
const CLOSED: LiveDesignGate = {
  awaitingHuman: false,
  dispatchRunning: false,
  failedRunUrl: undefined,
};

/**
 * The first absolute http(s) URL in a sentence, or undefined. Trailing sentence
 * punctuation is trimmed — the venue's sentence is prose, not a link list.
 *
 * TWO KNOWN LIMITS, both accepted while the run URL is prose rather than a typed field
 * (Task 9 review, minors): it takes the FIRST http(s) token, so a venue sentence that
 * mentioned a docs link before its run link would link the docs; and it strips a trailing
 * `)`, which truncates the rare legitimate URL that ends in one. Both are cosmetic
 * (a wrong-but-real link, or a short one), and both disappear the day the attempt carries
 * the run URL as its own field — which is the fix, not a cleverer regex here.
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
    dispatchRunning: dispatch?.outcome === 'running',
    // Only a FAILED dispatch's sentence is a failed run. A passed attempt's detail
    // names what it drafted, and linking that as "the failed run" would be a lie the
    // reader has no way to check.
    failedRunUrl: dispatch?.outcome === 'failed' ? urlIn(dispatch.detail) : undefined,
  };
}

/**
 * IS AN AMENDMENT IN FLIGHT ON THIS ARTIFACT — the predicate behind `SystemDesignView`'s
 * `sessionLive`, and therefore behind the stale-basis ACK's refusal (F-GTD-12).
 *
 * The hazard is concrete: a committed slot can only host an AMENDMENT, and acknowledging a
 * stale basis commits to main. Do that while the amendment's review PR is open and the two
 * writes merge-conflict — which is why the popover explains itself ("An amendment is already
 * in flight for this artifact") instead of letting the operator discover it.
 *
 * WHAT IT RESTORES, and what it had lost. The original disjunct was
 * `drafting || awaitingReview || redrafting` on the derived session door. 4b1 retired the
 * co-author workflow and every one of those three lost its producer, so the whole predicate
 * collapsed to `draftFailed` alone — the one state that needs the refusal LEAST, because a
 * failed draft has nothing in flight to conflict with. Task 9 restored the gate half from
 * the live authority (`awaitingHuman`); this restores the other half — a draft or redraft
 * actually RUNNING — from the same read, which is the case the popover's own sentence names.
 *
 * Extracted from the component so `node --test` can hold it: this repo's node suite has no
 * DOM and no JSX loader, and a predicate that lives only inside a `.tsx` is a predicate that
 * can collapse a third time with every gate green.
 */
export function designAmendmentInFlight(input: {
  awaitingHuman: boolean;
  dispatchRunning: boolean;
  /**
   * The derived session door's stage — `draftFailed` is the one member left that says so.
   * Optional because the door itself is: no session read yet is not an amendment in flight.
   */
  stage: string | undefined;
}): boolean {
  return input.awaitingHuman || input.dispatchRunning || input.stage === 'draftFailed';
}
