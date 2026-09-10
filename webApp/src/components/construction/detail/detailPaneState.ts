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
