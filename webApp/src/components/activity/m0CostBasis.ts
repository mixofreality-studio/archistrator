/**
 * WHETHER THE M0 GATE'S COST RODE ON ASSUMED NUMBERS, and the sentence that says so.
 *
 * ── The server says it; this module only decides whether to show it ─────────
 * The Project-Design compute defaults any planning-assumption family the founder
 * never authored and proceeds — a project that cannot reach its own cost-approval
 * gate cannot be told what it would cost. Approving M0 binds the plan of record and
 * starts spending, so a cost computed on numbers nobody showed the founder is the
 * one way this screen can mislead.
 *
 * The compute records what it defaulted, in its own words, on the sdpReview task's
 * ATTEMPT `Detail` (`defaultedDetail`, written by `sdpComputeStrategy.Produce`), and
 * as of stage 4b2 that field reaches the client on the revision the gate is about
 * (`DeliveryTaskRevisionView.detail`, carried from the DECISIVE attempt). So the
 * notice is a READ of what the server said, rendered verbatim — never a second
 * derivation of the Manager's defaulting rules, which is the copy the design-health
 * move exists to prevent.
 *
 * ── THE SLOT-READING PROXY IS GONE, and what it could never see ─────────────
 * Until now this module inferred the defaulting from the committed planning-
 * assumptions slot: no slot 8 at all ⇒ the whole document was defaulted. That proxy
 * answered exactly one of the two real cases and was SILENT for the other — the
 * PER-FAMILY fills (`resolvePlanningAssumptions`, on an authored slot whose
 * `terms.computeCost` or `declaredUsage` is its vocabulary's unknown member), which
 * is the case that actually ran on this repo. A notice that is silent on the live
 * case is the defect it was written to close.
 *
 * ── AN ABSENT DETAIL IS "NOTHING WAS ASSUMED" ───────────────────────────────
 * It is not a missing value and it is not broken plumbing. `defaultedDetail` returns
 * the EMPTY STRING when the defaulted list is empty, and the wire omits an empty
 * detail rather than sending one. On this repo today the detail is legitimately
 * absent, because removing revenue share (stage 4b2 Task 7) made slot 8 fully
 * authored and the compute now defaults nothing — that emptiness is the measured
 * proof the uncomputable-SDP finding is closed at its source. Silence is also the
 * right rendering on its own terms: a notice that always shows is a notice nobody
 * reads.
 *
 * Pure and React-free so `node --test` loads it directly.
 */

/** Just enough of the revision the M0 gate is about. */
export interface M0Revision {
  /** `DeliveryTaskRevisionOutcome`'s wire string. */
  outcome: string;
  /** The decisive attempt's own sentence, verbatim. Omitted, never empty. */
  detail?: string | null | undefined;
}

/**
 * The line the M0 review body renders, or the EMPTY STRING when there is nothing to
 * say. The caller decides WHERE this belongs (the M0 gate only, never a read-only
 * history); this decides WHETHER there is a basis to name.
 */
export function m0CostBasisNotice(revision: M0Revision | undefined): string {
  if (revision === undefined) return '';
  // A FAILED revision's detail is the failure's own message, not a cost basis. The
  // lifecycle already says the compute failed, loudly; repeating its error text under
  // a heading that promises "what your cost was computed on" would misfile it.
  if (revision.outcome === 'failed') return '';
  return revision.detail ?? '';
}
