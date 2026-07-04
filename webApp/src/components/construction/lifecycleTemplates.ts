/**
 * App-A per-kind life-cycle templates for the three construction activity KINDS
 * (SERVICE / FRONTEND / TESTING). These are the real Method process templates
 * (App A Table A-1 spirit): each phase has a binary exit criterion and a weight
 * summing to 100 per kind.
 *
 * Phase ordering and weights are ported verbatim from the frozen UX mock
 * (methodpoc/designs/aiarch/ux-mock/src/data/activities.ts).
 *
 * The deriver `phaseStateFor` maps a committed BuildStatus → per-phase {done, active}
 * without fabricating any data: it reads only the ordinal the status implies.
 */

import type { BuildStatus } from '../../contracts/constructionAdapters';
import type { ActivityKind } from './KindBadge';

// ---------------------------------------------------------------------------
// Template shape (static — no done/active, those are derived at render time).
// ---------------------------------------------------------------------------

export interface PhaseTemplate {
  id: string;
  name: string;
  exitCriterion: string;
  /** % contribution (App A Table A-1); weights sum to 100 per kind. */
  weight: number;
}

/** A phase with its derived done/active state for a specific activity status. */
export interface PhaseState extends PhaseTemplate {
  done: boolean;
  active: boolean;
}

// ---------------------------------------------------------------------------
// SERVICE — 8 phases, weights sum to 100
// App A Fig A-1: SRS → STP slice → Detailed Design → Service Contract →
// Construction (+white-box test client) → Code Review → Integration →
// Black-box unit test.
// ---------------------------------------------------------------------------

export const SERVICE_PHASES: readonly PhaseTemplate[] = [
  { id: 'svc-srs',         name: 'SRS',                  exitCriterion: 'Architect has reviewed the service requirement spec',              weight: 8  },
  { id: 'svc-stp',         name: 'System-test-plan slice',exitCriterion: 'The service\'s slice of the System Test Plan is written',          weight: 7  },
  { id: 'svc-dd',          name: 'Detailed design',       exitCriterion: 'Detailed design (after exploratory construction) approved',        weight: 18 },
  { id: 'svc-contract',    name: 'Service contract',      exitCriterion: 'App-B contract FROZEN by the senior reviewer',                    weight: 12 },
  { id: 'svc-build',       name: 'Construction',          exitCriterion: 'Code complete + white-box test client passes',                    weight: 33 },
  { id: 'svc-review',      name: 'Code review',           exitCriterion: 'reviewEngine reviewer set all PASS',                              weight: 8  },
  { id: 'svc-integration', name: 'Integration',           exitCriterion: 'Wired into the closed-layered call graph',                       weight: 9  },
  { id: 'svc-blackbox',    name: 'Black-box unit test',   exitCriterion: 'Passes the service test plan as a black box',                    weight: 5  },
];

// ---------------------------------------------------------------------------
// FRONTEND — 7 phases, weights sum to 100
// Brief → Concept draft → Iteration rounds → Design-approval gate →
// UI-code construction → Conformance review → Integration.
// ---------------------------------------------------------------------------

export const FRONTEND_PHASES: readonly PhaseTemplate[] = [
  { id: 'fe-brief',       name: 'Brief / requirements',   exitCriterion: 'The surface + persona + core use case captured',                 weight: 8  },
  { id: 'fe-concept',     name: 'Concept draft',          exitCriterion: 'First ui-design preview produced',                              weight: 14 },
  { id: 'fe-iterate',     name: 'Iteration rounds',       exitCriterion: 'Preview converged after co-author feedback',                    weight: 20 },
  { id: 'fe-approve',     name: 'Design-approval gate',   exitCriterion: 'Human design authority APPROVES the concept',                   weight: 12 },
  { id: 'fe-build',       name: 'UI-code construction',   exitCriterion: 'SPA built against the approved design',                        weight: 28 },
  { id: 'fe-conformance', name: 'Conformance review',     exitCriterion: 'ui-designer / ux-reviewer confirm conformance',                 weight: 10 },
  { id: 'fe-integration', name: 'Integration',            exitCriterion: 'Wired into the SPA + the demonstrable E2E',                    weight: 8  },
];

// ---------------------------------------------------------------------------
// TESTING — 4 phases, weights sum to 100
// System Test Plan → Build harness → Run → Regression.
// ---------------------------------------------------------------------------

export const TESTING_PHASES: readonly PhaseTemplate[] = [
  { id: 'test-plan',       name: 'System Test Plan',  exitCriterion: 'The ways the integrated system can FAIL are enumerated + signed off', weight: 25 },
  { id: 'test-harness',    name: 'Build harness',     exitCriterion: 'Playwright + durable-execution drivers built for the plan',           weight: 35 },
  { id: 'test-run',        name: 'Run',               exitCriterion: 'A separate software-tester runs the plan against the integrated system', weight: 25 },
  { id: 'test-regression', name: 'Regression',        exitCriterion: 'Regression harness guards the demonstrable set continuously',          weight: 15 },
];

// ---------------------------------------------------------------------------
// DEPLOYMENT — 5 phases, weights sum to 100
// App A noncoding activity lifecycle: Provisioning spec (requirements) →
// Detailed design → Apply (construction) → Review → Convergence verification.
// Mirrors the server ActivityTypeDeployment profile (activityprofile.go).
// ---------------------------------------------------------------------------

export const DEPLOYMENT_PHASES: readonly PhaseTemplate[] = [
  { id: 'dep-spec',    name: 'Provisioning spec',       exitCriterion: 'Target environment + resources + rollback captured',              weight: 15 },
  { id: 'dep-design',  name: 'Detailed design',         exitCriterion: 'The provisioning/IaC change is designed + reviewed',             weight: 20 },
  { id: 'dep-apply',   name: 'Apply',                   exitCriterion: 'The change is applied to the target environment',                weight: 35 },
  { id: 'dep-review',  name: 'Review',                  exitCriterion: 'devops / architect approve the applied change',                  weight: 15 },
  { id: 'dep-verify',  name: 'Convergence verification',exitCriterion: 'Environment converged to the declared desired state',           weight: 15 },
];

// ---------------------------------------------------------------------------
// DOCUMENTATION — 5 phases, weights sum to 100
// App A noncoding activity lifecycle: Outline (requirements) → Structure design →
// Authoring (construction) → Review → Publish verification.
// Mirrors the server ActivityTypeDocumentation profile (activityprofile.go).
// ---------------------------------------------------------------------------

export const DOCUMENTATION_PHASES: readonly PhaseTemplate[] = [
  { id: 'doc-outline', name: 'Outline',                 exitCriterion: 'Scope + audience + the sections to write are agreed',            weight: 15 },
  { id: 'doc-design',  name: 'Structure design',        exitCriterion: 'Document structure + source references designed',                weight: 20 },
  { id: 'doc-author',  name: 'Authoring',               exitCriterion: 'The document is drafted against the outline',                    weight: 35 },
  { id: 'doc-review',  name: 'Review',                  exitCriterion: 'architect / co-author confirm the document is accurate',         weight: 15 },
  { id: 'doc-publish', name: 'Publish verification',    exitCriterion: 'Document published + linked from the corpus',                    weight: 15 },
];

// The kind → template registry. Exhaustive over ActivityKind so a new kind is a
// compile error rather than a silent fall-through to TESTING_PHASES.
const TEMPLATES: Record<ActivityKind, readonly PhaseTemplate[]> = {
  service: SERVICE_PHASES,
  frontend: FRONTEND_PHASES,
  testing: TESTING_PHASES,
  deployment: DEPLOYMENT_PHASES,
  documentation: DOCUMENTATION_PHASES,
};

// Neutral single-phase fallback for an unknown (bad-data) kind — never borrow
// another kind's lifecycle silently.
const UNKNOWN_PHASES: readonly PhaseTemplate[] = [
  { id: 'unknown', name: 'Unknown lifecycle', exitCriterion: 'No lifecycle template registered for this activity kind', weight: 100 },
];

// ---------------------------------------------------------------------------
// Status → phase-ordinal mapping
//
// Each BuildStatus implies an "active index" in the ordered template. Phases
// before that index are done; the active index phase is in-flight; phases after
// are pending. Mapping per kind:
//
// SERVICE (8 phases):
//   integrated        → all done (idx 0..7 done, none active)
//   in-review         → done 0..4 (SRS..Construction), Code review active (idx 5)  Σ done = 78%
//   in-construction   → done 0..3 (SRS..Service contract), Construction active (idx 4)  Σ done = 45%
//   in-detailed-design→ done 0..1 (SRS..STP), Detailed design active (idx 2)  Σ done = 15%
//   eligible          → SRS active (idx 0), none done  Σ done = 0%
//   blocked / not-started → no phase done or active
//
// FRONTEND (7 phases):
//   integrated        → all done
//   in-review         → done 0..4 (Brief..UI-code), Conformance review active (idx 5)  Σ = 82%
//   in-construction   → done 0..3 (Brief..Design-approval), UI-code active (idx 4)  Σ = 54%
//   in-detailed-design→ done 0..1 (Brief..Concept), Iteration active (idx 2)  Σ = 22%
//   eligible          → Brief active (idx 0), none done
//   blocked / not-started → none
//
// TESTING (4 phases):
//   integrated        → all done
//   in-review / in-construction → done 0..1 (Plan..Harness), Run active (idx 2)  Σ = 60%
//   in-detailed-design→ done 0 (Plan), Harness active (idx 1)  Σ = 25%
//   eligible          → Plan active (idx 0), none done
//   blocked / not-started → none
// ---------------------------------------------------------------------------

function activeIdxForStatus(kind: ActivityKind, status: BuildStatus): number | null {
  if (kind === 'testing') {
    switch (status) {
      case 'integrated':         return null;
      case 'in-review':
      case 'in-construction':    return 2;
      case 'in-detailed-design': return 1;
      case 'eligible':           return 0;
      case 'blocked':
      case 'not-started':        return null;
    }
  }
  // deployment and documentation share a 5-phase (spec→design→apply→review→verify)
  // mapping. Review is the penultimate phase (idx 3); apply/authoring is idx 2.
  if (kind === 'deployment' || kind === 'documentation') {
    switch (status) {
      case 'integrated':         return null;
      case 'in-review':          return 3;
      case 'in-construction':    return 2;
      case 'in-detailed-design': return 1;
      case 'eligible':           return 0;
      case 'blocked':
      case 'not-started':        return null;
    }
  }
  // service and frontend share the same phase-index mapping
  switch (status) {
    case 'integrated':         return null; // all done sentinel: see phaseStateFor
    case 'in-review':          return 5;
    case 'in-construction':    return 4;
    case 'in-detailed-design': return 2;
    case 'eligible':           return 0;
    case 'blocked':
    case 'not-started':        return null;
  }
}

/**
 * Derive per-phase `{done, active}` state from the kind's template + the
 * activity's committed BuildStatus. Pure function — no fabrication.
 *
 * `integrated` → all phases done; `blocked`/`not-started` → none done/active.
 */
export function phaseStateFor(
  kind: ActivityKind,
  status: BuildStatus,
): PhaseState[] {
  // Runtime-tolerant lookup: TS proves `kind` is an ActivityKind, but bad project
  // data could carry an unknown kind — fall back loudly to the neutral template
  // rather than silently borrowing another kind's lifecycle.
  const tpl: readonly PhaseTemplate[] =
    (TEMPLATES as Partial<Record<ActivityKind, readonly PhaseTemplate[]>>)[kind] ?? UNKNOWN_PHASES;

  const allDone = status === 'integrated';
  const activeIdx = allDone ? null : activeIdxForStatus(kind, status);

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
