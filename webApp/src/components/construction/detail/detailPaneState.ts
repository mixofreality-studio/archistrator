/**
 * Pure state/derivation logic for the shared construction detail pane
 * (DetailPane.tsx) — no React/JSX here. Node's native TS runner cannot load a
 * `.tsx` module at all (`ERR_UNKNOWN_FILE_EXTENSION` — proven empirically,
 * not merely a JSX-transform gap), so every piece of logic the brief's tests
 * exercise lives in this plain `.ts` sibling; DetailPane.tsx re-exports what a
 * caller needs alongside the component, exactly like activityTree.ts sits
 * beside ActivityTreeView.tsx (Tasks 5/6).
 *
 * This module is reachable from DetailPane.test.ts under `node --test`, so
 * every RELATIVE VALUE import below carries an explicit `.ts` extension (the
 * convention wire.ts/lifecycleTemplates.ts already establish); type-only
 * imports are erased before Node ever sees them and need no extension.
 */
import type {
  ActivityBuildStatusRow,
  ConstructionRow,
  EvidenceRefRow,
  TaskAttemptRow,
  RecordOriginRow,
} from '../../../contracts/types';
import type { LensSelection } from '../lens/useLensSelection';
import type { Tokens } from '../../../utilities/theme/themes';
import {
  GENERATED_TEMPLATES,
  GENERATED_TESTING_VARIANTS,
  type GeneratedPhase,
} from '../lifecycleTemplates.gen.ts';
import { EXIT_CRITERIA } from '../lifecycleTemplates.ts';
import type { ProvenanceBearing } from '../provenanceAxis.ts';

// ---------------------------------------------------------------------------
// The beside-content (>= 1200px) layout contract.
//
// It lives here, as a plain exported object rather than an inline `sx`, for one
// reason: it is the ONLY part of this surface no tool can see. typecheck,
// eslint and the whole test suite stay green whether the action bar sits at
// y=475 or y=2000, so the mechanism gets pinned by an assertion in
// DetailPane.test.ts instead of by a comment nobody can run.
//
// THE MECHANISM, measured rather than assumed (review round 1)
// ------------------------------------------------------------
// ConstructionShell wraps this pane in a plain `<Box sx={{flexShrink:0}}>` —
// `construction-lens-detail`. THAT box is the flex item of the content row, and
// the row's default `align-items: stretch` makes it 1989px tall to match the
// content column beside it. This pane is an ordinary BLOCK child inside that
// box, so it is not a flex item at all:
//
//   1. It never receives an explicit `height`, so it is sized by its own
//      content — 315px — and the wrapper's 1989px is not inherited.
//   2. `maxHeight` caps that content height at what fits below the sticky lens
//      toolbar, so a long body scrolls INSIDE the pane (`overflow: hidden`
//      here; the body's own scroller lives in DetailPaneChrome) rather than
//      pushing the action bar off-screen.
//   3. `position: sticky` + `top` then pin those 315px in the viewport as the
//      page scrolls.
//
// The wrapper's stretch is LOAD-BEARING, not the bug: sticky can only travel
// within its containing block, so the 1989px wrapper is exactly what gives this
// pane room to stay pinned for the full scroll. Making
// `construction-lens-detail` `display:flex` + `alignSelf:'flex-start'` — or
// otherwise shrinking it to its content — would collapse that travel and the
// pane would scroll away with the page.
//
// An `alignSelf` on THIS box would be inert (a block child of a non-flex
// parent) and stood here for one round claiming to "stop the stretch"; it is
// deliberately absent, and the test asserts its absence so it cannot come back
// as a mechanism nobody re-measured.
// ---------------------------------------------------------------------------

/** Distance below the scrolling ancestor's top where the pane pins — clears the
 *  lens toolbar (ConstructionShell), which sticks at `top: 0`. */
export const DETAIL_PANE_STICKY_TOP = 76;

/** Bottom breathing room so the pinned pane never touches the viewport edge. */
const DETAIL_PANE_BOTTOM_GAP = 16;

export const WIDE_PANE_SX = {
  display: 'flex',
  position: 'sticky',
  top: DETAIL_PANE_STICKY_TOP,
  maxHeight: `calc(100vh - ${String(DETAIL_PANE_STICKY_TOP)}px - ${String(DETAIL_PANE_BOTTOM_GAP)}px)`,
  overflow: 'hidden',
} as const;

// ---------------------------------------------------------------------------
// Task-detail state — the vocabulary the header's state chip and the action
// bar both key off. Distinct from BuildStatus (construction/status.tsx),
// which is the coarser ACTIVITY-row lens applied to the whole tracker; this
// one describes whatever is CURRENTLY SELECTED (an activity, a phase, or one
// task attempt) in the shared detail pane.
// ---------------------------------------------------------------------------

export type TaskDetailState =
  | 'unknown'
  | 'notStarted'
  | 'running'
  | 'awaitingHuman'
  | 'passed'
  | 'failed';

export const TASK_DETAIL_STATE_LABEL: Record<TaskDetailState, string> = {
  unknown: 'Unknown',
  notStarted: 'Not started',
  running: 'Running',
  awaitingHuman: 'Awaiting you',
  passed: 'Passed',
  failed: 'Failed',
};

/** Token-driven fill for the state chip — no hardcoded colour, every theme recolors it. */
export function taskDetailStateFill(
  t: Tokens,
  s: TaskDetailState
): { fg: string; bg: string; border: string } {
  switch (s) {
    case 'unknown':
      return { fg: t.muted, bg: 'transparent', border: t.line };
    case 'notStarted':
      return { fg: t.muted, bg: 'transparent', border: t.line };
    case 'running':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg, border: t.chatArchitectFg };
    case 'awaitingHuman':
      return { fg: t.awaitingFg, bg: t.awaitingBg, border: t.accent };
    case 'passed':
      return { fg: t.committedFg, bg: t.committedBg, border: t.committedDot };
    case 'failed':
      return { fg: t.dangerFg, bg: t.awaitingBg, border: t.dangerFg };
  }
}

/**
 * The attempts for one task, sorted ascending by attempt NUMBER — never by
 * array position. Figure A-1's ledger is append-only but the server does not
 * guarantee arrival order (see activityTree.ts's identical rule, Task 5).
 */
export function attemptsForTask(
  attempts: readonly TaskAttemptRow[],
  task: string
): TaskAttemptRow[] {
  return attempts.filter((a) => a.task === task).sort((a, b) => a.attempt - b.attempt);
}

function latestMatchingAttempt(
  attempts: readonly TaskAttemptRow[],
  task: string,
  explicitAttempt: number | undefined
): TaskAttemptRow | undefined {
  const forTask = attemptsForTask(attempts, task);
  if (forTask.length === 0) return undefined;
  if (explicitAttempt === undefined) return forTask[forTask.length - 1];
  return forTask.find((a) => a.attempt === explicitAttempt);
}

/**
 * Derive the state of whatever is CURRENTLY SELECTED from real head-state —
 * never a guess:
 *
 *   - a TASK is selected: state comes from its latest attempt (by attempt
 *     NUMBER — see latestMatchingAttempt). No attempt recorded is `unknown`,
 *     the exact "no record" state Task 8's placeholder body teaches — never
 *     a fabricated `notStarted` for a task that may simply not have run yet.
 *   - only a PHASE/ACTIVITY is selected: state comes from the row's own
 *     coarse status. `hasBuildEvidence === false` IS `notStarted` — a row
 *     the server COULD classify but has nothing recorded for yet — distinct
 *     from `classified === false`, which stays `unknown` (the same
 *     distinction ConstructionRow's own doc comments already draw for
 *     BuildStatus's `not-started` vs `unclassified`).
 */
export function taskDetailStateFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): TaskDetailState {
  if (row?.classified !== true) return 'unknown';

  if (selection.task !== undefined) {
    const attempt = latestMatchingAttempt(row.attempts, selection.task, selection.attempt);
    return attempt === undefined ? 'unknown' : stateForOutcome(attempt.outcome, row.status);
  }

  if (!row.hasBuildEvidence) return 'notStarted';
  return stateForRowStatus(row.status);
}

/** The state of one task attempt's outcome, given the row's coarse status —
 *  a pending ('') gate task on an `in-review` row IS `awaitingHuman`; the
 *  same pending outcome elsewhere is plain `running`. Each function below is
 *  a single exhaustive switch AS the entire function body (the established
 *  idiom this codebase already uses — see buildStatusForStage), so a real,
 *  unreachable `default` closes the TS control-flow proof without hiding a
 *  genuinely-missing case: `switch-exhaustiveness-check` still fails the
 *  build the moment a member goes unhandled above it. */
function stateForOutcome(
  outcome: TaskAttemptRow['outcome'],
  rowStatus: ActivityBuildStatusRow | undefined
): TaskDetailState {
  switch (outcome) {
    case 'passed':
      return 'passed';
    case 'rejected':
    case 'failed':
      return 'failed';
    case 'skipped':
      // A skipped (conditional-not-needed) task is a benign terminal
      // outcome, not a verdict of its own — grouped with `passed` rather
      // than inventing a seventh state for one non-blocking case.
      return 'passed';
    case '':
      return rowStatus === 'in-review' ? 'awaitingHuman' : 'running';
    default:
      return 'unknown';
  }
}

function stateForRowStatus(status: ActivityBuildStatusRow | undefined): TaskDetailState {
  switch (status) {
    case 'integrated':
      return 'passed';
    case 'in-review':
      return 'awaitingHuman';
    case 'in-construction':
      return 'running';
    case 'failed':
      return 'failed';
    case undefined:
      // Not reachable given classified===true && hasBuildEvidence===true per
      // the row's own contract, but the field stays optional on the wire —
      // absence renders as `unknown`, never a guessed status.
      return 'unknown';
  }
}

/** The provenance of whatever is currently selected — a task attempt's own
 *  record, or the row's rolled-up worst origin when no task is selected. */
export function attemptProvenance(
  row: ConstructionRow | undefined,
  selection: LensSelection
): RecordOriginRow | undefined {
  if (row === undefined) return undefined;
  if (selection.task !== undefined) {
    const attempt = latestMatchingAttempt(row.attempts, selection.task, selection.attempt);
    return attempt?.provenance.origin;
  }
  return row.worstOrigin;
}

export const PROVENANCE_LABEL: Record<RecordOriginRow, string> = {
  observed: 'Recorded',
  backfilled: 'Reconstructed',
  synthesized: 'Synthesized',
};

/** The one attempt the pane is currently showing, or none. */
export function selectedAttemptOf(
  row: ConstructionRow | undefined,
  selection: LensSelection
): TaskAttemptRow | undefined {
  if (row === undefined || selection.task === undefined) return undefined;
  return latestMatchingAttempt(row.attempts, selection.task, selection.attempt);
}

/**
 * The node whose provenance the pane's chip reads — the SAME `ProvenanceBearing`
 * shape the LIST lens's rail and group badge already read (provenanceAxis.ts),
 * so the pane and the row it was opened from can never disagree about the same
 * record.
 *
 * THIS IS THE LAUNDERING FIX. On 2026-09-09 the founder ruled that any fully
 * implemented component is done, reviewed and integrated, and a backfill wrote
 * 218 attempts onto 25 activities from that ruling — 21 of which now render 100%
 * with every phase complete. Six of the ten tasks on each of those (srs,
 * srsReview, stp, stpReview, integration, testing) have NO artifact behind them
 * at all; their entire evidence is the ruling, recorded in
 * `provenance.basis`. The list stamps them `≈ RECONSTRUCTED`. Until this
 * function existed the pane — the surface a reader opens precisely to CHECK a
 * row — showed `PASSED` with no mark at all, which is the same laundering one
 * surface over.
 *
 * Scoped exactly as `attemptProvenance` is: the selected attempt when one is
 * selected, every attempt of the selected task when the attempt is implicit, and
 * the whole row (plus the server's own roll-up) when only an activity is. An
 * empty ledger yields `unknown` from `worstOriginOf`, never `observed`.
 */
export function provenanceNodeFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): ProvenanceBearing {
  if (row === undefined) return {};
  if (selection.task !== undefined) {
    if (selection.attempt !== undefined) {
      const attempt = selectedAttemptOf(row, selection);
      return { attempts: attempt === undefined ? [] : [attempt] };
    }
    return { attempts: attemptsForTask(row.attempts, selection.task) };
  }
  return {
    ...(row.worstOrigin !== undefined ? { worstOrigin: row.worstOrigin } : {}),
    attempts: row.attempts,
  };
}

/**
 * Where a reader can go to check the selected attempt for themselves.
 *
 * `present: false` is the honest answer for the six ruling-derived tasks per
 * widened activity, whose `evidence` arrives as `{kind: '', ref: ''}`: there is
 * genuinely nothing to open. The pane renders that as "no evidence recorded"
 * rather than inventing a target — a dead link would imply a record exists and
 * merely failed to load.
 *
 * `undefined` (no attempt at all) is a THIRD answer and stays distinct: nothing
 * has been attempted, so there is not even an evidence field to be empty.
 */
export interface EvidencePointer {
  kind: EvidenceRefRow['kind'];
  ref: string;
  present: boolean;
}

export function evidencePointerFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): EvidencePointer | undefined {
  const attempt = selectedAttemptOf(row, selection);
  if (attempt === undefined) return undefined;
  const { kind, ref } = attempt.evidence;
  return { kind, ref, present: kind.length > 0 && ref.length > 0 };
}

// ---------------------------------------------------------------------------
// Phase/task metadata for the header's "exit criterion + Table A-1 weight" —
// resolved from the generated per-kind (or, for testing, per-VARIANT) profile,
// never hand-authored per task (the same rule Task 8's body follows).
// ---------------------------------------------------------------------------

export interface ResolvedPhaseTask {
  phaseName?: string;
  phaseWeight?: number;
  exitCriterion?: string;
  taskLabel?: string;
}

function profileFor(row: ConstructionRow): readonly GeneratedPhase[] | undefined {
  if (row.kind === undefined) return undefined;
  if (row.kind === 'testing') {
    return row.variant !== undefined
      ? GENERATED_TESTING_VARIANTS[row.variant]
      : GENERATED_TEMPLATES.testing;
  }
  return GENERATED_TEMPLATES[row.kind];
}

/**
 * Resolve the selected phase/task's display metadata. Falls back to the
 * row's own `currentLifecyclePhase` when nothing is explicitly selected (the
 * only reachable case today — task-granular selection lands in Task 6), so
 * the header is not blank the moment an activity node is clicked.
 */
export function resolvePhaseTask(
  row: ConstructionRow | undefined,
  selection: LensSelection
): ResolvedPhaseTask {
  if (row === undefined) return {};
  const phases = profileFor(row);
  if (phases === undefined) return {};
  const phaseId = selection.lifecyclePhase ?? row.currentLifecyclePhase;
  const phase = phases.find((p) => p.phase === phaseId);
  if (phase === undefined) return {};
  const task =
    selection.task !== undefined ? phase.tasks.find((tk) => tk.task === selection.task) : undefined;
  return {
    phaseName: phase.name,
    phaseWeight: phase.weight,
    exitCriterion: EXIT_CRITERIA[phase.phase],
    ...(task !== undefined ? { taskLabel: task.label } : {}),
  };
}

/** `activity › lifecycle phase › task · attempt N` — every part optional
 *  except the activity, so the breadcrumb degrades gracefully rather than
 *  ever rendering a broken separator. */
export function breadcrumbFor(
  activityLabel: string,
  phaseName: string | undefined,
  taskLabel: string | undefined,
  attempt: number | undefined
): string {
  const parts = [activityLabel];
  if (phaseName !== undefined) parts.push(phaseName);
  if (taskLabel !== undefined) {
    parts.push(attempt !== undefined ? `${taskLabel} · attempt ${String(attempt)}` : taskLabel);
  }
  return parts.join(' › ');
}

// ---------------------------------------------------------------------------
// The action bar — invariant across every body. `run` (id 'run') is present
// and ENABLED in every state, per the founder's standing ruling that failure
// is never terminal: there is no condition under which it is absent or
// disabled. Approve/send-back appear only where a human decision is owed.
// ---------------------------------------------------------------------------

export interface DetailAction {
  id: 'run' | 'approve' | 'sendBack';
  label: string;
  disabled: boolean;
}

const RUN_ACTION: DetailAction = { id: 'run', label: '↻ Run this task', disabled: false };

export function detailActionsFor(state: TaskDetailState): DetailAction[] {
  // `state` is intentionally unused in the condition below beyond the single
  // `awaitingHuman` check — this is the whole point: no other branch may
  // touch `run`'s presence or `disabled` flag.
  const actions: DetailAction[] = [RUN_ACTION];
  if (state === 'awaitingHuman') {
    actions.push({ id: 'approve', label: 'Approve', disabled: false });
    actions.push({ id: 'sendBack', label: 'Send back', disabled: false });
  }
  return actions;
}
