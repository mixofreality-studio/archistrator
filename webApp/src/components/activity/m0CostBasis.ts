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
 *     seven families at once;
 *   - slot 8 committed whose `notes` begin with the platform's own
 *     `defaultPlanningAssumptionsNote` prefix ⇒ the document the compute wrote on a
 *     previous run, not one a human authored.
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
import { narrowProject } from '../../contracts/projectAdapters.ts';
import type { ArtifactSlotView, ProjectArtifactModelEnvelope } from '../../contracts/types.ts';
import { assumedCostBasis, defaultedCostBasis } from './activityCopy.ts';

/** The slot the M0 cost is priced from. */
const PLANNING_ASSUMPTIONS = 'planningAssumptions';

/**
 * The opening words of `defaultPlanningAssumptionsNote` — the compute's own signature
 * on a planning-assumptions document nobody authored. A PREFIX match, because the
 * sentence after it names the defaults and will be edited as they change; the words
 * that say WHERE the document came from are what this depends on.
 */
export const PLATFORM_DEFAULTS_NOTE_PREFIX = 'Derived defaults:';

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
  const notes = notesOf(slot);
  if (notes.startsWith(PLATFORM_DEFAULTS_NOTE_PREFIX)) return defaultedCostBasis(EVERY_FAMILY);
  return '';
}

/**
 * The committed document's OWN `notes` — `PlanningAssumptions.Notes`, which is what
 * the compute writes its provenance into — and not `ArtifactSlotView.notes`, which is
 * the slot's separate commentary. Narrowed through the envelope's `kind` (the same
 * cast the container makes for the activity list: the Phase-1 and Phase-2 envelopes
 * are one wire shape with two TS spellings), so a slot whose envelope disagrees with
 * its own kind answers the empty note rather than throwing on the screen a founder is
 * trying to approve a plan on.
 */
function notesOf(slot: ArtifactSlotView): string {
  const envelope = slot.model as unknown as ProjectArtifactModelEnvelope | undefined;
  return narrowProject(envelope, PLANNING_ASSUMPTIONS)?.notes ?? '';
}
