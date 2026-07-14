/**
 * Pure copy mapping for the Construction Tracker's "role line" — the honest
 * label rendered for the ONE in-flight activity the tracker supervises: who
 * (the dispatched workerClass) is doing what (the Method phase they are
 * currently in) to which activity (its title).
 *
 * Both inputs are already-flowing dispatched state, never invented here:
 *  - `workerClass` comes straight off the committed Phase-2 activity-list entry
 *    for the active activity (ModelActivityItem.workerClass, schema.ts:649) —
 *    the same roster id RoleAvatar's PROP_FOR/PRESENTATION_FOR key on, so
 *    (unlike ../design/roleLine.ts's ActiveRole → roster-id SEED_FOR map) no
 *    seed translation is needed: the RoleAvatar seed IS the workerClass,
 *    verbatim.
 *  - `phase` comes from ConstructionRow.phase (contracts/types.ts), wired
 *    straight through from the server's ActivityMethodPhase — set at the
 *    RecordPhaseStarted / RecordPhaseCompleted dispatch boundaries
 *    (projectstateaccess.go), never derived or inferred client-side. It is
 *    present once the pump has recorded at least one phase for the activity;
 *    absent (or an unrecognized value, e.g. from an older/newer server) before
 *    that lands — in which case this honestly omits the phase verb rather
 *    than guessing one (see the pinned fallback below).
 *
 * No timers, no inference: the returned line only restates the reported
 * dispatch-boundary state. Kept side-effect-free and React-free so it is
 * unit-testable in isolation (see constructionRoleLine.test.ts).
 */
import type { CanonicalPhase } from './lifecycleTemplates.gen';

export interface ConstructionRoleLine {
  /** RoleAvatar `seed` — the workerClass verbatim; the roster ids match. */
  seed: string;
  /** The honest sentence, e.g. "Junior developer is constructing Build the web client". */
  text: string;
}

/** Method-phase → present-progressive verb (task brief §C4 copy). */
const PHASE_VERB: Record<CanonicalPhase, string> = {
  requirements: 'scoping',
  detailed_design: 'designing',
  test_plan: 'planning tests for',
  construction: 'constructing',
  integration: 'integrating',
};

const CANONICAL_PHASES = new Set<string>(Object.keys(PHASE_VERB));

function verbFor(phase: string | undefined): string | undefined {
  if (phase === undefined || !CANONICAL_PHASES.has(phase)) return undefined;
  return PHASE_VERB[phase as CanonicalPhase];
}

/** Words in the worker-class roster that are acronyms, rendered upper-case. */
const ACRONYM_WORDS = new Set(['qa', 'ui', 'ux']);

/**
 * "junior-developer" -> "Junior developer"; "qa-engineer" -> "QA engineer"
 * (roster acronyms — qa/ui/ux — render upper-case rather than sentence-case).
 * An empty/unassigned workerClass (no real data to report) honestly resolves
 * to "Someone" rather than fabricating a specific role.
 */
export function humanizeWorkerClass(workerClass: string): string {
  const words = workerClass.split('-').filter((w) => w.length > 0);
  if (words.length === 0) return 'Someone';
  const cased = words.map((w, i) => {
    if (ACRONYM_WORDS.has(w)) return w.toUpperCase();
    return i === 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w;
  });
  return cased.join(' ');
}

/**
 * @param workerClass the active activity's dispatched worker class (roster id,
 *   verbatim — e.g. "junior-developer")
 * @param phase       ConstructionRow.phase for the active activity, or
 *   undefined when no construction row has been recorded for it yet
 * @param title       the activity's title (callers fall back to its id first,
 *   same as ActivityRowView / toActivityListView)
 */
export function constructionRoleLine(
  workerClass: string,
  phase: string | undefined,
  title: string
): ConstructionRoleLine {
  const role = humanizeWorkerClass(workerClass);
  const verb = verbFor(phase);
  return {
    seed: workerClass,
    text: verb !== undefined ? `${role} is ${verb} ${title}` : `${role} is working on ${title}`,
  };
}
