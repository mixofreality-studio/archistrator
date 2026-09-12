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
 */
import type { ConstructionRow } from '../../../../contracts/types';
import type { LensSelection } from '../../lens/useLensSelection';
import { classify } from '../../artifactClassification.ts';
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

const ARTIFACT_BODY_KINDS = new Set<string>([
  'service',
  'uiDesign',
  'frontend',
  'testing:plan',
  'testing:systemTest',
]);

/**
 * The renderer key for this activity's artifact, or `undefined` when this stage
 * has no renderer for it.
 *
 * Dispatches on the EXISTING `classify(row)` rather than re-deriving a family
 * from `kind`/`variant`: that function already returns the right key and is
 * already what the Artifacts tab dispatches on, and a second implementation of
 * one rule is how this branch's worst bug happened.
 */
export function artifactRendererKeyFor(
  row: ConstructionRow | undefined
): ArtifactBodyKind | undefined {
  if (row === undefined) return undefined;
  const classification = classify(row);
  if (classification === undefined) return undefined;
  return ARTIFACT_BODY_KINDS.has(classification) ? (classification as ArtifactBodyKind) : undefined;
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

/** Pick the body. See the module comment for why the questions are in this order. */
export function detailBodyFor(
  row: ConstructionRow | undefined,
  selection: LensSelection,
  state: TaskDetailState
): DetailBodyKind {
  if (absenceFor(row, selection) !== undefined) return 'absent';
  if (state === 'unknown' || state === 'notStarted') return 'unknown';
  if (selection.task !== undefined) {
    if (selectedTaskIsGate(row, selection)) return 'review';
    return artifactRendererKeyFor(row) !== undefined ? 'artifact' : 'episode';
  }
  // Nothing narrower than the activity is selected. Episodes ARE activity-level,
  // so this is the one selection depth at which the episode body needs no
  // apology for its scope.
  return 'episode';
}
