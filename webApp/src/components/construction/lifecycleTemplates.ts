/**
 * App-A per-kind life-cycle templates for the five construction activity
 * KINDS (SERVICE / FRONTEND / TESTING / DEPLOYMENT / DOCUMENTATION).
 *
 * The id/phase/name/weight/tasks per kind is GENERATED (lifecycleTemplates.gen.ts,
 * server/cmd/gen-uiprofiles) straight from the server's single canonical
 * Profile (server/internal/resourceaccess/projectstate/projectstateaccess.go) —
 * the same 5-phase Method vocabulary (Requirements/DetailedDesign/TestPlan/
 * Construction/Integration) the App-A earned-value formula uses uniformly
 * across every activity kind, weighted per kind. TESTING now generates all
 * five variant profiles (GENERATED_TESTING_VARIANTS), not one representative
 * shape.
 *
 * RESOLVED DRIFT (2026-07-10, step-9 cleanup): this file previously
 * hand-authored a richer, kind-specific phase breakdown (e.g. 8 phases for
 * SERVICE: SRS/STP/Detailed-design/Contract/Construction/Review/Integration/
 * Blackbox) "ported verbatim from the frozen UX mock"
 * (methodpoc/designs/aiarch/ux-mock/src/data/activities.ts) — an EARLIER
 * prototype than the server's later-ratified canonical-5-phase Profile.
 * Names, phase counts, and weights had diverged completely (e.g. SERVICE's
 * hand "Detailed design"+"Service contract" weights summed to 30 against the
 * server's DetailedDesign weight of 20) and the per-phase done/active state
 * was derived from a hand per-kind index table over the coarse BuildStatus
 * enum — never from the server's real per-phase completion data. The server
 * Profile (fewer, canonical phases) is architecturally authoritative: adopted
 * here. Exit-criterion prose has no server-side source (the server Profile
 * carries no such field — ProfilePhase is a generated contract type, not
 * something to bolt UI copy onto) — kept here as a small hand table keyed by
 * the 5 canonical phases (was ~29 kind-specific entries; now 5 generic ones).
 *
 * RESOLVED (2026-09-09, Stage A Task 8): this file used to carry a helper
 * (and a companion lookup table) that reverse-engineered which canonical
 * phase was "active" from the coarse 8-member BuildStatus and presented that
 * guess unmarked as fact — fabrication, ruled out explicitly by the Stage A
 * design spec. Both are deleted. `phaseStateFor` now derives `done`/`active`
 * from the activity's REAL current phase (ConstructionRow.currentLifecyclePhase, reported
 * straight from the server, never inferred) instead of a status-derived
 * guess; with no real current phase reported it marks nothing active and
 * nothing done, except the one non-guessed terminal fact: `integrated` means
 * every phase is done.
 */

import type { BuildStatus } from '../../contracts/constructionAdapters';
import type { ActivityKind } from './KindBadge';
import {
  GENERATED_TEMPLATES,
  type LifecyclePhase,
  type GeneratedPhase,
} from './lifecycleTemplates.gen';

// ---------------------------------------------------------------------------
// Template shape (static — no done/active, those are derived at render time).
// ---------------------------------------------------------------------------

export interface PhaseTemplate extends GeneratedPhase {
  exitCriterion: string;
}

/** A phase with its derived done/active state for a specific activity status. */
export interface PhaseState extends PhaseTemplate {
  done: boolean;
  active: boolean;
}

// ---------------------------------------------------------------------------
// Exit-criterion prose — generic per canonical phase (no server source; the
// server's weighted Profile subset differs per kind, but what "done" MEANS
// for a given canonical phase does not).
// ---------------------------------------------------------------------------

const EXIT_CRITERIA: Record<LifecyclePhase, string> = {
  requirements: 'The requirement/brief for this activity is captured and approved',
  detailed_design: 'Detailed design (contract / UI concept / provisioning spec) is approved',
  test_plan: "This activity's slice of the test plan is written",
  construction: 'Construction is code-complete and self-verified',
  integration: 'Reviewed, wired into the integrated system, and converged',
};

function withExitCriterion(phases: readonly GeneratedPhase[]): readonly PhaseTemplate[] {
  return phases.map((p) => ({ ...p, exitCriterion: EXIT_CRITERIA[p.phase] }));
}

export const SERVICE_PHASES = withExitCriterion(GENERATED_TEMPLATES.service);
export const FRONTEND_PHASES = withExitCriterion(GENERATED_TEMPLATES.frontend);
export const TESTING_PHASES = withExitCriterion(GENERATED_TEMPLATES.testing);
export const DEPLOYMENT_PHASES = withExitCriterion(GENERATED_TEMPLATES.deployment);
export const DOCUMENTATION_PHASES = withExitCriterion(GENERATED_TEMPLATES.documentation);
export const UI_DESIGN_PHASES = withExitCriterion(GENERATED_TEMPLATES.uiDesign);
export const INTEGRATION_PHASES = withExitCriterion(GENERATED_TEMPLATES.integration);

// The kind → template registry. Exhaustive over ActivityKind so a new kind is a
// compile error rather than a silent fall-through to another kind's template.
const TEMPLATES: Record<ActivityKind, readonly PhaseTemplate[]> = {
  service: SERVICE_PHASES,
  frontend: FRONTEND_PHASES,
  testing: TESTING_PHASES,
  deployment: DEPLOYMENT_PHASES,
  documentation: DOCUMENTATION_PHASES,
  uiDesign: UI_DESIGN_PHASES,
  integration: INTEGRATION_PHASES,
};

// Neutral single-phase fallback for an unknown (bad-data) kind — never borrow
// another kind's lifecycle silently.
const UNKNOWN_PHASES: readonly PhaseTemplate[] = [
  {
    id: 'unknown',
    phase: 'construction',
    name: 'Unknown lifecycle',
    exitCriterion: 'No lifecycle template registered for this activity kind',
    weight: 100,
    tasks: [],
  },
];

// ---------------------------------------------------------------------------
// Real current-phase → done/active derivation.
//
// No more guessing. A deleted helper + lookup table used to reverse-engineer
// an "active" canonical phase from the coarse 8-member BuildStatus via a
// hand-authored mapping table and present that guess unmarked — the
// Stage A spec rules this out explicitly. `phaseStateFor` now takes the
// activity's REAL current phase (ConstructionRow.currentLifecyclePhase, wired straight
// through from the server's ActivityMethodPhase at the RecordPhaseStarted /
// RecordPhaseCompleted dispatch boundaries — see constructionRoleLine.ts's
// header for the same field used the same way) instead of re-deriving it
// from an unrelated coarse status proxy. `active` is that real phase, if the
// kind's template carries it; `done` is every phase strictly before it, per
// the Method's own sequential phase order. With no real current phase
// reported yet (absent, or an unrecognized value), nothing is marked done or
// active: absence of data is rendered as absence, never as a plausible guess.
// ---------------------------------------------------------------------------

/**
 * Derive per-phase `{done, active}` state from the kind's template + the
 * activity's committed BuildStatus + its REAL current phase. Pure function —
 * no fabrication.
 *
 * `integrated` → every phase done, nothing active (a terminal fact, not a
 * guess). Otherwise `active` is set only when `currentPhase` names a phase
 * the kind's template actually carries; `done` is every phase before it.
 */
export function phaseStateFor(
  kind: ActivityKind,
  status: BuildStatus,
  currentPhase?: string
): PhaseState[] {
  // Runtime-tolerant lookup: TS proves `kind` is an ActivityKind, but bad project
  // data could carry an unknown kind — fall back loudly to the neutral template
  // rather than silently borrowing another kind's lifecycle.
  const tpl: readonly PhaseTemplate[] =
    (TEMPLATES as Partial<Record<ActivityKind, readonly PhaseTemplate[]>>)[kind] ?? UNKNOWN_PHASES;

  if (status === 'integrated') {
    return tpl.map((p) => ({ ...p, done: true, active: false }));
  }

  // findIndex returns -1 for both an absent currentPhase and one this kind's
  // template does not carry — both honestly resolve to "nothing active".
  const activeIdx = tpl.findIndex((p) => p.phase === currentPhase);

  return tpl.map((p, i) => ({
    ...p,
    done: activeIdx !== -1 && i < activeIdx,
    active: activeIdx !== -1 && i === activeIdx,
  }));
}

/** App A §1.3 progress formula: Σ weights of done phases. */
export function progressPct(phases: PhaseState[]): number {
  return phases.filter((p) => p.done).reduce((acc, p) => acc + p.weight, 0);
}
