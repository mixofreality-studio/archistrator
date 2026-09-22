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
 * convention wire.ts already establishes); type-only
 * imports are erased before Node ever sees them and need no extension.
 */
import type {
  ActivityBuildStatusRow,
  ConstructionRow,
  ConstructionStage,
  EvidenceRefRow,
  TaskAttemptRow,
  RecordOriginRow,
} from '../../../contracts/types';
import type { LensSelection } from '../lens/useLensSelection';
import type { Tokens } from '../../../utilities/theme/themes';
import { profileFor } from '../lifecycleProfiles.ts';
import type { ProvenanceBearing, ProvenanceOrigin } from '../provenanceAxis.ts';
import { PANE_MAX_HEIGHT, PANE_STICKY_TOP } from '../lens/lensGeometry.ts';
import { REVIEW_ONLY_NOTE, type OwedMark } from '../tasks/owedChip.ts';
import { steerBarFor, type SteerActionId } from '../tasks/steerActions.ts';

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

// Where the pane pins, and how tall it may be, both come from the lens toolbar
// and the scroller AS MEASURED (ConstructionShell publishes them as CSS custom
// properties; see ../lens/lensGeometry.ts). This used to be a constant 76px under
// a 100vh cap: the toolbar wraps to ~86px at 1280/1366, so the pane slid under it,
// and 100vh ignored the app chrome above the scroller, so the action bar ran off
// the bottom of the screen (designer P0-1).
export const WIDE_PANE_SX = {
  display: 'flex',
  position: 'sticky',
  top: PANE_STICKY_TOP,
  maxHeight: PANE_MAX_HEIGHT,
  overflow: 'hidden',
} as const;

// ---------------------------------------------------------------------------
// Task-detail state — the vocabulary the header's state chip and the action
// bar both key off. Distinct from BuildStatus (construction/status.tsx),
// which is the coarser ACTIVITY-row lens applied to the whole tracker; this
// one describes whatever is CURRENTLY SELECTED (an activity, a phase, or one
// task attempt) in the shared detail pane.
// ---------------------------------------------------------------------------

/**
 * `waiting` is an INTEGRATION-PENDING activity (the server's `pendingResume`,
 * list/pendingResume.ts): part of its lifecycle is done, and the rest waits on its
 * dependencies — nothing runs it and nothing reviews it. It is deliberately NOT
 * `running`: that is what the row used to read, and it held Begin on "Construction
 * running…" with no pump anywhere (rowIsInFlight reads running/awaitingHuman only).
 */
export type TaskDetailState =
  | 'unknown'
  | 'notStarted'
  | 'running'
  | 'waiting'
  | 'awaitingHuman'
  | 'passed'
  | 'failed'
  | 'skipped';

export const TASK_DETAIL_STATE_LABEL: Record<TaskDetailState, string> = {
  unknown: 'Unknown',
  notStarted: 'Not started',
  running: 'Running',
  // The generic word; a pending row's own chip says which phase ("Integration
  // pending", pendingResume.pendingChipLabel).
  waiting: 'Pending',
  awaitingHuman: 'Awaiting you',
  passed: 'Passed',
  failed: 'Failed',
  skipped: 'Skipped',
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
    case 'skipped':
      // Skipped did not run: it earns no fill, and above all not the success one.
      return { fg: t.muted, bg: 'transparent', border: t.line };
    case 'running':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg, border: t.chatArchitectFg };
    case 'waiting':
      // Unfilled, like every state that is not happening now — but in ink, so
      // "part-built, waiting" does not read as the muted "not started".
      return { fg: t.ink, bg: 'transparent', border: t.muted };
    case 'awaitingHuman':
      // The awaiting tone, never the accent (designer palette ruling).
      return { fg: t.awaitingFg, bg: t.awaitingBg, border: t.awaitingFg };
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
 *     NUMBER — see latestMatchingAttempt). With no attempt, noAttemptStateFor
 *     (below) decides: `unknown` only where the history cannot say, else
 *     `notStarted`.
 *   - only a PHASE/ACTIVITY is selected: state comes from the row's own
 *     coarse status. `hasBuildEvidence === false` IS `notStarted` — a row
 *     the server COULD classify but has nothing recorded for yet — distinct
 *     from `classified === false`, which stays `unknown` (the same
 *     distinction ConstructionRow's own doc comments already draw for
 *     BuildStatus's `not-started` vs `unclassified`).
 *
 * (That rule is documented on noAttemptStateFor, next.)
 */
/**
 * The state of a task that has NO attempt, from its activity's row — ONE rule for
 * the pane, the list's task rows and the pane's provenance chip (designer final
 * items, replacing re-check N3):
 *
 *   - `unknown` only where the surface genuinely cannot tell "never ran" from
 *     "ran before anyone recorded it": the row is UNCLASSIFIED, or it has build
 *     evidence but ZERO attempts — its history predates per-task capture.
 *   - `notStarted` everywhere else: a classified row with no evidence (nothing
 *     has happened), or a row with at least one attempt (its per-task history
 *     is complete, so a task missing from it has not run).
 */
export type NoAttemptState = 'unknown' | 'notStarted';

export function noAttemptStateFor(
  row: Pick<ConstructionRow, 'classified' | 'hasBuildEvidence' | 'attempts'> | undefined
): NoAttemptState {
  if (row?.classified !== true) return 'unknown';
  if (row.hasBuildEvidence && row.attempts.length === 0) return 'unknown';
  return 'notStarted';
}

export function taskDetailStateFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): TaskDetailState {
  if (row?.classified !== true) return 'unknown';

  if (selection.task !== undefined) {
    const attempt = latestMatchingAttempt(row.attempts, selection.task, selection.attempt);
    if (attempt === undefined) return noAttemptStateFor(row);
    return outcomeStateOf(attempt.outcome);
  }

  // Integration-pending (the server's pendingResume): its coarse status says
  // in-review, which would read `running`. Nothing runs it; it waits.
  if (row.pendingResume !== undefined) return 'waiting';
  if (!row.hasBuildEvidence) return 'notStarted';
  return stateForRowStatus(row.status);
}

/**
 * What ONE attempt's outcome says on its own, with no row context.
 *
 * This is the ONE outcome mapping on the construction surface: the pane
 * (taskDetailStateFor, above), the list's tier-3 task state (activityTree.ts) and
 * the list's expanded attempt ledger all read it, so the three cannot disagree
 * about the same attempt.
 *
 * A pending ('') outcome is `running`, on a gate task too: head-state never says a
 * human is awaited; only the live owed set does (tasks/owedChip.ts, the Stage C
 * plan's Q4), handed to the pane as its `decision` / `owed` props.
 *
 * `skipped` stays `skipped`. A skipped task did not run, and the success mark
 * is not lent to work that never happened (activityRowPresentation.ts,
 * skippedIsNotPassed, records the full reasoning).
 *
 * A single exhaustive switch as the whole function body — the idiom this
 * codebase already uses (see buildStatusForStage). The unreachable `default`
 * closes the TS control-flow proof without hiding a missing case, because
 * `switch-exhaustiveness-check` still fails the build the moment a member goes
 * unhandled above it. A junk wire value is "we do not know", never a guess.
 */
export type OutcomeState = 'unknown' | 'running' | 'passed' | 'failed' | 'skipped';

export function outcomeStateOf(outcome: TaskAttemptRow['outcome']): OutcomeState {
  switch (outcome) {
    case 'passed':
      return 'passed';
    case 'rejected':
    case 'failed':
      return 'failed';
    case 'skipped':
      return 'skipped';
    case '':
      return 'running';
    default:
      return 'unknown';
  }
}

function stateForRowStatus(status: ActivityBuildStatusRow | undefined): TaskDetailState {
  switch (status) {
    case 'integrated':
      return 'passed';
    // `in-review` means "some phases complete, not all" (spec §1) — mid-lifecycle,
    // gated or not. Whether a human is awaited is the owed set's to say.
    case 'in-review':
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
 * 214 attempts onto 23 activities from that ruling, and all 23 now render 100%
 * with every phase complete. On the 19 ten-task activities among them, six of
 * the tasks (srs, srsReview, stp, stpReview, integration, testing) have, bar a
 * handful, NO artifact behind them at all; their entire evidence is the ruling, recorded in
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

/**
 * Resolve the selected phase/task's display metadata: the selected phase
 * (and, with a task selected, that task's label). With only an activity
 * selected it falls back to the row's own `currentLifecyclePhase`, so the
 * header is not blank the moment an activity node is clicked.
 */
export function resolvePhaseTask(
  row: ConstructionRow | undefined,
  selection: LensSelection
): ResolvedPhaseTask {
  if (row === undefined) return {};
  const phases = profileFor(row.kind, row.variant);
  if (phases === undefined) return {};
  const phaseId = selection.lifecyclePhase ?? row.currentLifecyclePhase;
  const phase = phases.find((p) => p.phase === phaseId);
  if (phase === undefined) return {};
  const task =
    selection.task !== undefined ? phase.tasks.find((tk) => tk.task === selection.task) : undefined;
  return {
    phaseName: phase.name,
    phaseWeight: phase.weight,
    exitCriterion: phase.exitCriterion,
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

function countOf(n: number, noun: string): string {
  return `${String(n)} ${noun}${n === 1 ? '' : 's'}`;
}

/**
 * The header's count line when no single TASK is selected (designer P1-6).
 *
 * The attempt selector only means something for one task, so an activity or a
 * phase selection used to show "NO ATTEMPTS" — beside a PASSED chip, on an
 * activity with ten attempts. This says what the selection actually holds:
 *
 *   - an activity: every attempt on it · the phases its profile draws;
 *   - a phase: that phase's attempts · the tasks the list draws under it (a
 *     conditional task counts only once an attempt makes it render — the same
 *     rule the tree follows).
 *
 * `undefined` when a task is selected: its attempt selector speaks for it.
 */
export function selectionSummaryFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): string | undefined {
  if (row === undefined || selection.task !== undefined) return undefined;
  const phases = profileFor(row.kind, row.variant);
  const phaseId = selection.lifecyclePhase;
  if (phaseId === undefined) {
    const phaseCount = phases?.length ?? row.phases.length;
    return `${countOf(row.attempts.length, 'attempt')} · ${countOf(phaseCount, 'phase')}`;
  }
  const inPhase = row.attempts.filter((a) => a.phase === phaseId);
  const phase = phases?.find((p) => p.phase === phaseId);
  if (phase === undefined) return countOf(inPhase.length, 'attempt');
  const rendered = phase.tasks.filter(
    (tk) => !tk.conditional || inPhase.some((a) => a.task === tk.task)
  );
  return `${countOf(inPhase.length, 'attempt')} · ${countOf(rendered.length, 'task')}`;
}

// ---------------------------------------------------------------------------
// The action bar — invariant across every body. `run` (id 'run') is present in
// every state (the founder's ruling that the app always shows how to retry),
// but it is DISABLED WITH ITS REASON (designer P1-2, orchestrator ruling
// 2026-09-12): nothing here can start work yet — the pane ignored the click —
// and an enabled button that does nothing is the lie this rewrite removes.
// Approve/send-back appear only where a human decision is owed.
// ---------------------------------------------------------------------------

export interface DetailAction {
  id: 'run' | 'approve' | 'sendBack' | SteerActionId;
  label: string;
  disabled: boolean;
  /** Why a disabled action is disabled — said on hover, never left to guess. */
  reason?: string;
}

/** Why Run is off: the console cannot start a task (follow-ups B1/B2 give it the
 *  verbs — retry with the operator's note delivered, and re-queue). The words say
 *  that in the operator's terms: no ticket names in a tooltip (tasks round 2). */
export const RUN_NOT_WIRED_REASON =
  'Not wired yet — the pump starts work on its own; starting it from here comes later.';

/** The session stages at which the activity's workflow is doing work. */
const LIVE_RUNNING_STAGES: ReadonlySet<ConstructionStage> = new Set<ConstructionStage>([
  'dispatching',
  'pipelineRunning',
  'reviewing',
]);

export const LIVE_RUNNING_LABEL = 'Activity running';

/**
 * The pane chip where the ledger cannot place the selection but the activity's live
 * session can (tasks round 2, designer). After a decided gate's row lingers out,
 * its gate task has no attempt yet (no live RecordTaskAttempt writer), and the chip
 * read UNKNOWN beside an activity the workflow reports running. It now says what the
 * workflow says — of the ACTIVITY, since nothing yet says it of the task.
 */
export function liveChipFor(
  state: TaskDetailState,
  liveStage: ConstructionStage | null | undefined
): { label: string; state: TaskDetailState } | undefined {
  if (state !== 'unknown' || liveStage === undefined || liveStage === null) return undefined;
  return LIVE_RUNNING_STAGES.has(liveStage)
    ? { label: LIVE_RUNNING_LABEL, state: 'running' }
    : undefined;
}

/**
 * The run action, named for what is selected (designer re-check B2). It used to be
 * the constant "↻ Run this task" on an activity, on a phase, and on work nothing
 * had ever run — a retry mark on a first run. It now says what it runs ("this
 * activity" / "this phase" / "this task"), and carries ↻ (run AGAIN) only where
 * the selection holds at least one attempt, ▶ otherwise.
 *
 * Presence does not depend on any of that: `run` is in the action bar in every
 * state (detailActionsFor), whatever its label says. Its `disabled` here is only
 * the default; detailActionsFor turns it off, with RUN_NOT_WIRED_REASON, until the
 * console can start work.
 */
export function runActionFor(
  row: ConstructionRow | undefined,
  selection: LensSelection
): DetailAction {
  const attempts = row?.attempts ?? [];
  let scope: 'activity' | 'phase' | 'task';
  let attempted: boolean;
  if (selection.task !== undefined) {
    scope = 'task';
    attempted = attempts.some((a) => a.task === selection.task);
  } else if (selection.lifecyclePhase !== undefined) {
    scope = 'phase';
    attempted = attempts.some((a) => a.phase === selection.lifecyclePhase);
  } else {
    scope = 'activity';
    attempted = attempts.length > 0;
  }
  return { id: 'run', label: `${attempted ? '↻' : '▶'} Run this ${scope}`, disabled: false };
}

/**
 * The provenance chip's words when "Observed only" hid attempts in the selection
 * (designer re-check B1): the record exists and was set aside, which is not the
 * same fact as UNRECORDED — so the chip names the toggle and the count instead.
 */
export function observedOnlyChipLabel(hiddenCount: number): string {
  return `OBSERVED ONLY · ${String(hiddenCount)} reconstructed hidden`;
}

/**
 * Which provenance chips the pane's header draws — the GRADE chip and, beside it,
 * the "Observed only" hidden-count chip (fix-C review; designer final items).
 *
 * The grade chip states what the row it was opened from states, so it stays —
 * including on a MIXED row under "Observed only", where the observed attempts that
 * remain still grade as RECORDED and the hidden-count chip sits beside it instead
 * of replacing it. It is dropped only where the grade would be `unknown`
 * (UNRECORDED) and that word would be false:
 *
 *   - the toggle hid this selection's whole record — it exists and was set aside
 *     (re-check B1), so the hidden-count chip speaks alone;
 *   - a selected task reads NOT STARTED — its row's history is complete (or
 *     nothing has happened), so there is no record to grade: hollow ○, no chip.
 */
export interface HeaderProvenanceChips {
  grade: boolean;
  /** How many attempts "Observed only" hid in the selection; 0 draws no chip. */
  hidden: number;
}

export function headerProvenanceChipsFor(input: {
  origin: ProvenanceOrigin;
  hiddenCount: number;
  state: TaskDetailState;
  taskSelected: boolean;
}): HeaderProvenanceChips {
  const hidden = Math.max(0, input.hiddenCount);
  const nothingToGrade = input.origin === 'unknown';
  const gradeWouldLie =
    nothingToGrade && (hidden > 0 || (input.taskSelected && input.state === 'notStarted'));
  return { grade: !gradeWouldLie, hidden };
}

export function detailActionsFor(state: TaskDetailState, run: DetailAction): DetailAction[] {
  // `state` is intentionally unused beyond the single `awaitingHuman` check: no
  // branch may touch `run`'s presence, and none may make it look live.
  const actions: DetailAction[] = [];
  if (state === 'awaitingHuman') {
    actions.push({ id: 'approve', label: 'Approve', disabled: false });
    actions.push({ id: 'sendBack', label: 'Send back', disabled: false });
  }
  // Last: the decision is the primary act where one is owed.
  actions.push({ ...run, disabled: true, reason: RUN_NOT_WIRED_REASON });
  return actions;
}

/** Why Approve / Send back are off — said on hover (final review minor: an owed gate
 *  with no lifecycle phase disabled both with no reason at all). */
export const DECISION_NO_PHASE_REASON =
  'The activity reports no current phase, so there is no gate to send a decision to.';
export const DECISION_ELSEWHERE_REASON = 'Select the gated phase to decide it.';
export const DECISION_CLOSED_REASON = 'This gate has been decided; it no longer waits on you.';
export const DECISION_BUSY_REASON = 'A decision on this gate is on its way.';

/**
 * Approve / Send back's enabled state and, when off, why. They act only where a live
 * decision backs them: a decision the route handed down (it hands none for a gate
 * with no lifecycle phase — no decision can be addressed to it), that applies to the
 * selection, is still open, and has none on the wire.
 */
export function decisionActionState(
  decision: { open: boolean; busy: boolean } | undefined,
  applies: boolean
): { disabled: boolean; reason?: string } {
  if (decision === undefined) return { disabled: true, reason: DECISION_NO_PHASE_REASON };
  if (!applies) return { disabled: true, reason: DECISION_ELSEWHERE_REASON };
  if (!decision.open) return { disabled: true, reason: DECISION_CLOSED_REASON };
  if (decision.busy) return { disabled: true, reason: DECISION_BUSY_REASON };
  return { disabled: false };
}

/**
 * The action bar on a REVIEW-ONLY selection: a steer-needed or failed activity.
 * Its steer actions come first (Retry… and Skip…, or Re-queue…; PM Q3), each as
 * steerActions.ts says, which is disabled with its reason while they are locked.
 * Run is still in the bar, last, because Run is always present (spec §7.8, §9.3;
 * tasks merge review I2 ruling). It is disabled, with the review-only reason.
 * Never Approve or Send back, and never the cut Takeover or Reassign.
 */
export function reviewOnlyActionsFor(
  run: DetailAction,
  mark: Pick<OwedMark, 'reason'> | undefined
): DetailAction[] {
  const steer = mark !== undefined ? (steerBarFor(mark)?.actions ?? []) : [];
  return [
    ...steer.map(
      (a): DetailAction => ({
        id: a.id,
        label: a.label,
        disabled: a.disabled,
        ...(a.reason !== undefined ? { reason: a.reason } : {}),
      })
    ),
    { ...run, disabled: true, reason: REVIEW_ONLY_NOTE },
  ];
}
