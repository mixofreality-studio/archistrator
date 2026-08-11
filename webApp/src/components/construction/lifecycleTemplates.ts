/**
 * App-A per-kind life-cycle templates for the five construction activity
 * KINDS (SERVICE / FRONTEND / TESTING / DEPLOYMENT / DOCUMENTATION).
 *
 * The id/phase/name/weight per kind is GENERATED (lifecycleTemplates.gen.ts,
 * server/cmd/gen-uiprofiles) straight from the server's single canonical
 * Profile (server/internal/resourceaccess/projectstate/activityprofile.go) —
 * the same 5-phase Method vocabulary (Requirements/DetailedDesign/TestPlan/
 * Construction/Integration) the App-A earned-value formula uses uniformly
 * across every activity kind, weighted per kind.
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
 * EARMARK: TESTING kind renders one representative profile (TestVariantPlan)
 * regardless of the activity's actual testing variant — the server tracks 5
 * distinct variant profiles (Harness/Perf/SystemTest/QAProcess/Plan) with
 * different weights, not surfaced here. Matches today's behavior
 * (ActivityLifecyclePanel calls phaseStateFor(kind, status) with no variant
 * argument); a variant-aware lifecycle panel is a follow-up, not this cleanup.
 */

import type { BuildStatus } from '../../contracts/constructionAdapters';
import type { ActivityKind } from './KindBadge';
import {
  GENERATED_TEMPLATES,
  type CanonicalPhase,
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

const EXIT_CRITERIA: Record<CanonicalPhase, string> = {
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
  },
];

// ---------------------------------------------------------------------------
// Status → active-phase derivation
//
// Each BuildStatus maps onto a target CANONICAL phase (the Method phase it
// implies is in flight). Because different kinds' Profiles carry different
// SUBSETS of the 5 canonical phases (e.g. DEPLOYMENT has no Requirements or
// TestPlan phase), the target snaps forward to the next canonical phase the
// kind's profile actually carries — generic over every kind, no per-kind
// index table required.
// ---------------------------------------------------------------------------

const CANONICAL_ORDER: readonly CanonicalPhase[] = [
  'requirements',
  'detailed_design',
  'test_plan',
  'construction',
  'integration',
];

// `failed` joins integrated/blocked/not-started as a NON-in-flight status: the
// pump durably gave up, so no lifecycle phase is active for it.
type InFlightStatus = Exclude<BuildStatus, 'integrated' | 'blocked' | 'not-started' | 'failed'>;

const STATUS_TARGET_PHASE: Record<InFlightStatus, CanonicalPhase> = {
  eligible: 'requirements',
  'in-detailed-design': 'detailed_design',
  'in-construction': 'construction',
  'in-review': 'integration',
};

/** The index within `phases` that is active for `status`, or null (nothing active: done, failed, or not started). */
function activeIdxFor(phases: readonly PhaseTemplate[], status: BuildStatus): number | null {
  if (
    status === 'integrated' ||
    status === 'blocked' ||
    status === 'not-started' ||
    status === 'failed'
  ) {
    return null;
  }

  const targetPhase = STATUS_TARGET_PHASE[status];
  const startAt = CANONICAL_ORDER.indexOf(targetPhase);
  for (let i = startAt; i < CANONICAL_ORDER.length; i++) {
    const idx = phases.findIndex((p) => p.phase === CANONICAL_ORDER[i]);
    if (idx !== -1) return idx;
  }
  return null; // no phase at or after the target is tracked for this kind
}

/**
 * Derive per-phase `{done, active}` state from the kind's template + the
 * activity's committed BuildStatus. Pure function — no fabrication.
 *
 * `integrated` → all phases done; `blocked`/`not-started`/`failed` → none
 * done/active (a terminally failed activity has no phase still in flight).
 */
export function phaseStateFor(kind: ActivityKind, status: BuildStatus): PhaseState[] {
  // Runtime-tolerant lookup: TS proves `kind` is an ActivityKind, but bad project
  // data could carry an unknown kind — fall back loudly to the neutral template
  // rather than silently borrowing another kind's lifecycle.
  const tpl: readonly PhaseTemplate[] =
    (TEMPLATES as Partial<Record<ActivityKind, readonly PhaseTemplate[]>>)[kind] ?? UNKNOWN_PHASES;

  const allDone = status === 'integrated';
  const activeIdx = allDone ? null : activeIdxFor(tpl, status);

  return tpl.map((p, i) => ({
    ...p,
    done: allDone || (activeIdx !== null && i < activeIdx),
    active: !allDone && activeIdx !== null && i === activeIdx,
  }));
}

/** App A §1.3 progress formula: Σ weights of done phases. */
export function progressPct(phases: PhaseState[]): number {
  return phases.filter((p) => p.done).reduce((acc, p) => acc + p.weight, 0);
}
