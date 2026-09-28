/**
 * WHETHER THE M0 GATE'S COST RODE ON ASSUMED NUMBERS, and the sentence that says so.
 *
 * ── WHAT THE SERVER RECORDS, AND WHERE (measured, stage 4b1 Task 14) ────────
 * The compute records the families it had to default in the sdpReview task's ATTEMPT
 * `Detail` (`defaultedDetail`, written by `sdpComputeStrategy.Produce`). That field is
 * NOT ON THE WIRE: `DeliveryTaskRevisionView` carries `attemptIds` and nothing else of
 * an attempt, and no view in the OAS carries an attempt's Detail — grepped across the
 * delivery contract. So this module reads the defaulting from the one place a client
 * CAN see it, the committed planning-assumptions slot itself:
 *
 *   - NO slot 8 at all ⇒ `defaultPlanningAssumptions` filled every family, which is
 *     the whole-slot default and the case the compute's `defaulted` list reports as
 *     seven families at once.
 *
 * THERE USED TO BE A SECOND ARM, and it could never fire (final fix wave, F3). It read a
 * COMMITTED slot 8 whose `notes` began with the compute's own `Derived defaults:` prefix —
 * "the platform wrote this document on a previous run". Nothing writes that:
 * `projectDesignComputedKinds()` deliberately EXCLUDES `KindPlanningAssumptions`, so the
 * compute never commits slot 8 at all. The arm read as coverage of the defaulted case while
 * covering nothing, which is worse than the silence below, so it is gone.
 *
 * WHAT THIS CANNOT SEE, stated where it matters rather than in a report only: the
 * PER-FAMILY fills (`resolvePlanningAssumptions` — an authored slot whose
 * `terms.revenueShare` or `declaredUsage` is its vocabulary's unknown member) leave no
 * trace on any view. The attempt `Detail` is their only record, and until the wire
 * carries it this notice is silent for them. It NEVER guesses: re-deriving the
 * server's per-field default rules here would be a second copy of a rule the Manager
 * owns, which is the defect the design-health move exists to prevent.
 *
 * Pure and React-free so `node --test` loads it directly.
 */
import type { ArtifactSlotView } from '../../contracts/types.ts';
import { assumedCostBasis } from './activityCopy.ts';

/** The slot the M0 cost is priced from. */
const PLANNING_ASSUMPTIONS = 'planningAssumptions';

/**
 * The families the notice names. Not a list of field names: the two cases this can
 * distinguish are both whole-document, and the compute's own word for that case is
 * "every planning assumption".
 */
const EVERY_FAMILY = 'every planning assumption';

/**
 * The line the M0 review body renders, or the EMPTY STRING when nothing was assumed.
 *
 * Empty is the common answer and it must stay empty: a notice that always shows is a
 * notice nobody reads, and "nothing was assumed" is noise on every project whose
 * founder authored their own numbers.
 */
export function m0CostBasisNotice(slots: readonly ArtifactSlotView[]): string {
  const slot = slots.find((s) => s.kind === PLANNING_ASSUMPTIONS);
  if (slot === undefined) return assumedCostBasis(EVERY_FAMILY);
  return '';
}
