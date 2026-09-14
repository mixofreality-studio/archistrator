/**
 * WHICH BODY fills the detail pane's one body slot.
 *
 * Task 4 built the pane as an invariant header + an invariant action bar around
 * a single slot. Four bodies fill it, and this module — pure, no React — is the
 * one place that decides between them, so the rule can be read and tested
 * without a renderer in the way (node:test cannot load a `.tsx` module at all;
 * relative VALUE imports therefore carry an explicit `.ts` extension).
 *
 * THE ORDER OF THE QUESTIONS IS THE WHOLE DESIGN
 * ---------------------------------------------
 *  1. Is this thing ABSENT from the activity's profile? A Deployment activity
 *     carries no Test Plan phase; a deep link naming one is answered with "by
 *     design, not missing data" and nothing else. Asked FIRST, because every
 *     later question would otherwise answer it as "no record", which is the one
 *     conflation this stage keeps refusing.
 *  2. Is there NO RECORD? 292 of 384 task rows land here. The unknown body.
 *  3. Is the selected task a GATE? Then a human decision is (or was) owed and
 *     the review body is the honest surface — artifact above, verdict below.
 *  4. Does this activity's classification have an artifact renderer? Then the
 *     artifact body. `deployment` / `documentation` / `integration` are CUT for
 *     this stage and fall back to the unknown body rather than to an empty
 *     frame pretending a renderer exists.
 *  5. Otherwise the episode body — which is also what an ACTIVITY-level
 *     selection gets, because episodes are genuinely activity-level (see
 *     EpisodeBody's caption, which says so out loud).
 *
 * THE ONE EXCEPTION: A COMMITTED TESTING ARTIFACT OUTRANKS "NO RECORD"
 * ---------------------------------------------------------------------
 * `hasBuildEvidence` (question 2's `state`) answers "did this activity's BUILD
 * progress" — attempts, retries, gate outcomes. A system test PLAN's scenarios,
 * or a recorded test RUN, are a different kind of fact: real artifacts that
 * exist on the wire (`project.testingState`) independently of whether the
 * authoring activity itself has ever been attempted. All five testing-kind
 * activities in this project's committed corpus carry `hasBuildEvidence: false`,
 * so question 2 used to swallow N-STP's five committed scenarios at every
 * selection depth — the artifact was real and simply unreachable. So question
 * 2's short-circuit is now bypassed, for `testing:plan`/`testing:systemTest`
 * ONLY, whenever `testingArtifactRendererKeyFor` finds the corresponding
 * artifact actually present — see its own comment for exactly which selections
 * that reaches. The task's own STATE is untouched (the header still reads
 * `Not started`/`Unknown` honestly); only which BODY fills the slot changes.
 * `service`/`uiDesign`/`frontend` never take this branch.
 */
import type { ConstructionRow, ProjectStateWithGit } from '../../../../contracts/types';
import type { LensSelection } from '../../lens/useLensSelection';
import { classify } from '../../artifactClassification.ts';
import type { LifecyclePhase } from '../../lifecycleTemplates.gen.ts';
import type { TaskDetailState } from '../detailPaneState.ts';
import { absenceFor, profileFor } from './taskBriefing.ts';

/** The four bodies, plus the by-design-absent sibling of the unknown one. */
export type DetailBodyKind = 'absent' | 'unknown' | 'review' | 'artifact' | 'episode';

/**
 * The artifact classifications this stage actually renders.
 *
 * A deliberate SUBSET of `Classification`. The three cut kinds
 * (deployment/documentation/integration) and the three testing variants with no
 * authored renderer (harness/perf/qaProcess) are absent on purpose: they resolve
 * to `undefined` and fall back to the unknown body, which says plainly that
 * there is nothing recorded to show. An empty renderer frame would say the
 * opposite.
 */
export type ArtifactBodyKind =
  | 'service'
  | 'uiDesign'
  | 'frontend'
  | 'testing:plan'
  | 'testing:systemTest';

/**
 * WHICH LIFECYCLE PHASE each renderer's artifact actually belongs to.
 *
 * The renderers are activity-scoped — `ServiceContractView` renders THE service
 * contract, `FrontendArtifactView` the ui-design concept and the built UI.
 * Dispatching on classification alone therefore puts the service contract under
 * `SRS`, a Requirements task, and labels it that task's artifact by placement
 * alone. That is a quieter version of exactly the mis-attribution this stage
 * keeps removing (it is the same mistake the episode caption exists to prevent),
 * so the phase is part of the question: outside its artifact's own phase a task
 * falls through to its EPISODES, which is genuinely what is known about it.
 *
 * Keyed by ArtifactBodyKind, so a new renderer has to state where its artifact
 * lives rather than silently applying everywhere.
 */
const ARTIFACT_PHASES: Record<ArtifactBodyKind, readonly LifecyclePhase[]> = {
  // The service contract is placed by artifactPlacement.ts (the designer's
  // per-phase table), which reads the contract JOIN rather than a phase alone:
  // a missing contract and a contract by design need different bodies.
  service: [],
  // A uiDesign activity's whole output is its Design Concept.
  uiDesign: ['detailed_design'],
  // FrontendArtifactView renders the built ui-code. The frontend's Detailed
  // Design is its client contract, placed by artifactPlacement.ts.
  frontend: ['construction'],
  // The STP is written in Plan Authoring and signed off in Plan Review.
  'testing:plan': ['construction', 'integration'],
  // SystemTestRunView renders the run: execution, then regression & sign-off.
  'testing:systemTest': ['construction', 'integration'],
};

function isArtifactBodyKind(value: string): value is ArtifactBodyKind {
  return Object.prototype.hasOwnProperty.call(ARTIFACT_PHASES, value);
}

/** The canonical phase a task key belongs to — every key sits in exactly one. */
export function lifecyclePhaseOfTask(
  row: ConstructionRow | undefined,
  task: string
): LifecyclePhase | undefined {
  for (const profilePhase of profileFor(row) ?? []) {
    if (profilePhase.tasks.some((tk) => tk.task === task)) return profilePhase.phase;
  }
  return undefined;
}

/**
 * The renderer key for the artifact the SELECTED task's phase produces, or
 * `undefined` when this stage renders nothing for it.
 *
 * Dispatches on the EXISTING `classify(row)` rather than re-deriving a family
 * from `kind`/`variant`: that function already returns the right key and is
 * already what the Artifacts tab dispatches on, and a second implementation of
 * one rule is how this branch's worst bug happened. The phase check on top
 * decides SCOPE, never which renderer — see ARTIFACT_PHASES.
 */
export function artifactRendererKeyFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): ArtifactBodyKind | undefined {
  if (row === undefined) return undefined;
  const classification = classify(row);
  if (classification === undefined || !isArtifactBodyKind(classification)) return undefined;

  // The task's OWN phase, read from the profile — not `selection.lifecyclePhase`,
  // which a stale deep link can disagree with, and not the row's current phase,
  // which describes the activity rather than the selection.
  const lifecyclePhase =
    selection.task !== undefined
      ? lifecyclePhaseOfTask(row, selection.task)
      : selection.lifecyclePhase;
  if (lifecyclePhase === undefined) return undefined;
  return ARTIFACT_PHASES[classification].some((p) => p === lifecyclePhase)
    ? classification
    : undefined;
}

/**
 * Whether the wire actually carries the artifact a testing classification
 * renders — the system test plan's scenarios for `testing:plan`, a recorded
 * test run for `testing:systemTest`. Deliberately independent of
 * `hasBuildEvidence`: see the module comment on why gating a real, committed
 * artifact on build evidence hides true information for this one family.
 */
function recordedTestingArtifactExists(
  classification: 'testing:plan' | 'testing:systemTest',
  project: ProjectStateWithGit | undefined
): boolean {
  const testingState = project?.testingState;
  if (classification === 'testing:plan') {
    return (testingState?.systemTestPlan?.scenarios?.length ?? 0) > 0;
  }
  return (testingState?.testRuns?.length ?? 0) > 0;
}

/**
 * The artifact key for a testing-kind row's OWN committed artifact, reached
 * regardless of `hasBuildEvidence` — `undefined` when this row is not a
 * testing:plan/testing:systemTest row, or when the wire carries no such
 * artifact yet (an ordinary no-record testing row is untouched by this and
 * falls through to the unknown body exactly as before).
 *
 * Reachable at three depths:
 *   - the BARE activity row (no phase, no task named) — the plainest click;
 *   - the artifact's OWN phase(s) (Plan Authoring/Plan Review — ARTIFACT_PHASES)
 *     with no task named;
 *   - a TASK within one of those phases.
 * A phase this artifact does NOT belong to (N-STP's own Requirements/
 * "Use-Case Trace" phase, `srs`/`srsReview`) is deliberately NOT widened: this
 * reuses `artifactRendererKeyFor`'s existing phase-membership check for both
 * of the narrower cases, so the scoping rule is defined in exactly one place.
 */
export function testingArtifactRendererKeyFor(
  row: ConstructionRow | undefined,
  selection: LensSelection,
  project: ProjectStateWithGit | undefined
): ArtifactBodyKind | undefined {
  if (row === undefined) return undefined;
  const classification = classify(row);
  if (classification !== 'testing:plan' && classification !== 'testing:systemTest') {
    return undefined;
  }
  if (!recordedTestingArtifactExists(classification, project)) return undefined;

  if (selection.task === undefined && selection.lifecyclePhase === undefined) {
    return classification;
  }
  return artifactRendererKeyFor(row, selection);
}

/** Whether the selected task is its phase's gate — read from the generated profile. */
export function selectedTaskIsGate(
  row: ConstructionRow | undefined,
  selection: LensSelection
): boolean {
  const task = selection.task;
  if (task === undefined) return false;
  const phases = profileFor(row);
  if (phases === undefined) return false;
  for (const profilePhase of phases) {
    const found = profilePhase.tasks.find((tk) => tk.task === task);
    if (found !== undefined) return found.gate;
  }
  return false;
}

/**
 * Pick the body. See the module comment for why the questions are in this
 * order, and for the one exception (a testing-kind row's own committed
 * artifact outranking question 2's "no record" reading).
 */
export function detailBodyFor(
  row: ConstructionRow | undefined,
  selection: LensSelection,
  state: TaskDetailState,
  project?: ProjectStateWithGit,
  /**
   * Whether artifactPlacement.ts placed a PRIMARY artifact here (a committed
   * contract, its honest absence, the test plan body, the commit under review).
   * A committed artifact outranks "no record" for every kind whose artifact
   * resolves, exactly as the testing bypass below does — the contract is project
   * state, not build evidence. Passed in rather than computed so this module
   * stays free of the join's inputs.
   */
  primaryArtifact = false
): DetailBodyKind {
  if (absenceFor(row, selection) !== undefined) return 'absent';

  const testingArtifact = testingArtifactRendererKeyFor(row, selection, project);
  if (testingArtifact !== undefined || primaryArtifact) {
    // A gate task still owes the review surface (artifact + verdict), exactly
    // as question 3 would decide for any other classification — this bypass
    // only removes question 2's block, it does not reorder question 3.
    if (selection.task !== undefined && selectedTaskIsGate(row, selection)) return 'review';
    return 'artifact';
  }

  if (state === 'unknown' || state === 'notStarted') return 'unknown';
  if (selection.task !== undefined) {
    if (selectedTaskIsGate(row, selection)) return 'review';
    return artifactRendererKeyFor(row, selection) !== undefined ? 'artifact' : 'episode';
  }
  // Nothing narrower than the activity is selected. Episodes ARE activity-level,
  // so this is the one selection depth at which the episode body needs no
  // apology for its scope.
  return 'episode';
}
