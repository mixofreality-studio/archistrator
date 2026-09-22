/**
 * The LIST lens's tree derivation: activity › lifecycle phase › Figure A-1 task.
 *
 * This module is the heart of the construction rewrite and it is a PURE
 * function — no React, no hooks, no DOM. ActivityTreeView.tsx (Task 6) is
 * presentation over what `buildActivityTree` returns, so every rule below is
 * decided ONCE, here, and tested without a renderer in the way. It is a plain
 * `.ts` sibling for the same reason detailPaneState.ts is: Node's native
 * type-stripping test runner cannot load a `.tsx` module at all
 * (`ERR_UNKNOWN_FILE_EXTENSION` — proven empirically). Relative VALUE imports
 * therefore carry an explicit `.ts` extension (the wire.ts convention);
 * type-only imports are erased before Node
 * ever sees them and need none.
 *
 * The inversion this module exists to perform
 * -------------------------------------------
 * The row set comes from the activity's PROFILE, never from its stored data.
 * Figure A-1 × the activity's kind (or, for testing, its variant) is
 * deterministic, so an activity with zero recorded history still expands into a
 * complete, correct, QUIET Method skeleton: nothing missing, nothing fabricated.
 * It reads as "here is the work, none of it has happened" — which is true —
 * rather than as an empty container, or as twelve green lies.
 *
 * What this module deliberately does NOT do
 * -----------------------------------------
 *  1. It does not GUESS a profile. An unclassified row (`kind` absent — the
 *     server could not derive a type and refuses to guess) yields zero phases
 *     and zero tasks. A default profile there would launder "no answer" into a
 *     plausible-but-wrong lifecycle.
 *  2. It does not RE-DERIVE phase completion from the attempt ledger. The
 *     server already derived it, correctly, and distinguishes "the gate
 *     rejected" from "no gate attempt exists" — see ConstructionRow.phases.
 *     Two implementations of one rule is precisely how this branch's worst bug
 *     happened (Stage A: ordinal inference reported the exact OPPOSITE of the
 *     truth for a row whose completion was non-monotonic). The server is the
 *     single authority; this module reads.
 *  3. It does not RE-ORDER or RE-LABEL anything. Phase order is Method order,
 *     task order is execution order, and both arrive from the generated
 *     profile. Sorting them here would be a hand-mirror of the server — the
 *     defect class this codebase has already paid for twice.
 *  4. It does not report 0% for an activity it knows nothing about. An unknown
 *     denominator is not a zero numerator: if ANY profile phase is unreported,
 *     `percentComplete` is `undefined`.
 */
import type {
  ActivityBuildStatusRow,
  ConstructionRow,
  Layer,
  PhaseRow,
  RecordOriginRow,
  TaskAttemptRow,
  TestingVariantName,
} from '../../../contracts/types';
import { profileFor, type GeneratedPhase, type GeneratedTask } from '../lifecycleProfiles.ts';
import { outcomeStateOf, type OutcomeState } from '../detail/detailPaneState.ts';

/** The activity kinds the server can classify — the profile registry's key set. */
export type ClassifiedKind = NonNullable<ConstructionRow['kind']>;

// ---------------------------------------------------------------------------
// Tier 3 — one Figure A-1 task
// ---------------------------------------------------------------------------

/**
 * A task's state, derived from its LATEST attempt and nothing else — through
 * detailPaneState.outcomeStateOf, the ONE outcome mapping the pane and the
 * attempt ledger read too, so `skipped` stays `skipped` here as well.
 *
 * `unknown` (no attempt recorded) is a first-class answer, not a placeholder:
 * the list renders it chip-less, because chip-less IS the signal.
 */
export type TaskState = OutcomeState;

/** One attempt, plus whether a later attempt has superseded it. */
export interface TaskAttemptNode extends TaskAttemptRow {
  /** True for every attempt but the highest-numbered one. */
  superseded: boolean;
}

export interface TaskNode {
  /** Tree-unique id: `<activityId>::<lifecyclePhase>::<task>`. */
  nodeId: string;
  activityId: string;
  /** The generated task key (`srsReview`, `codeReview`, …) — the server's vocabulary. */
  task: string;
  /** This profile's generated display label — never hand-authored here. */
  label: string;
  /** The book's own Figure A-1 name for the task (shown beside `label` when the
   *  profile renames it, and matched by search so "code review" still finds a
   *  test plan's renamed gate). */
  bookLabel: string;
  /** True when this task's success IS the phase's binary exit criterion (App A). */
  gate: boolean;
  /** True when the profile emits this task only if a real attempt exists. */
  conditional: boolean;
  /** The generated phase id this task belongs to — see GeneratedPhase.phase for why
   *  this is `string` rather than the narrower `LifecyclePhase`. */
  lifecyclePhase: string;
  state: TaskState;
  /** Ascending by attempt NUMBER. Empty when nothing was ever recorded. */
  attempts: TaskAttemptNode[];
  /** The highest-numbered attempt, or absent when the ledger is empty. */
  latestAttempt?: TaskAttemptNode;
  /** `attempts.length` — the `↻N` counter's magnitude. */
  attemptCount: number;
}

// ---------------------------------------------------------------------------
// Tier 2 — one App-A lifecycle phase
// ---------------------------------------------------------------------------

/**
 * A phase's binary exit state as the SERVER reported it.
 *
 * `unknown` — the profile carries this phase but the server reported nothing
 * about it — is deliberately distinct from `incomplete`. "We have no record"
 * and "the gate has not passed" are different facts, and collapsing them is the
 * conflation this rewrite exists to prevent.
 */
export type PhaseCompletion = 'complete' | 'incomplete' | 'unknown';

export interface PhaseNode {
  /** Tree-unique id: `<activityId>::<lifecyclePhase>`. */
  nodeId: string;
  activityId: string;
  /** The generated profile id (`service-requirements`, …) — NOT tree-unique. */
  id: string;
  /** The generated phase id — see GeneratedPhase.phase for why this is `string`
   *  rather than the narrower `LifecyclePhase` (a design row's phase, e.g.
   *  `mission`/`architecture`/`sdp`, is never one of the five canonical ids). */
  phase: string;
  /** The generated per-kind display name (`UX Requirements`, `Smoke Pass`, …). */
  name: string;
  /** Table A-1 % contribution. From the PROFILE, never from the stored row. */
  weight: number;
  /** This profile phase's own exit criterion (generated: lifecycles.gen.ts's
   *  LifecyclePhaseDef.exitCriterion, from method-assets lifecycles.json). */
  exitCriterion: string;
  completion: PhaseCompletion;
  /** The server's `completedAt`, when it reported one. */
  completedAt?: string;
  tasks: TaskNode[];
}

// ---------------------------------------------------------------------------
// Tier 1 — one activity
// ---------------------------------------------------------------------------

/**
 * Per-activity facts that do NOT live on the construction head-state row —
 * effort, float and criticality come from the project network, and the human
 * label from the activity list. Joined by activity id and left ABSENT when the
 * caller has nothing: a fabricated zero float would read as "on the critical
 * path", which is the loudest possible lie on this surface.
 */
export interface ActivityMeta {
  label?: string;
  effortDays?: number;
  float?: number;
  onCriticalPath?: boolean;
  /** The server's float-criticality band, passed through untouched. */
  band?: string;
  /** ModelActivityItem.componentId, joined the same way as `label` — one of
   *  Task 11 search's three matched fields (activity id / title / componentId).
   *  Absent for a project-wide activity (it builds no single component), and
   *  for any row the committed activity list does not carry. */
  componentId?: string;
}

export interface ActivityNode {
  /** Tree-unique id: the activity id itself. */
  nodeId: string;
  activityId: string;
  /** `meta.label` when the caller supplied one, else the activity id. */
  label: string;
  kind?: ClassifiedKind;
  variant?: TestingVariantName;
  /** `!row.classified` — the server could not derive a type for this activity. */
  unclassified: boolean;
  /** Whether the server resolved ANY phase completions. Not the same as unclassified. */
  hasBuildEvidence: boolean;
  status?: ActivityBuildStatusRow;
  currentLifecyclePhase?: string;
  layer?: Layer;
  layerBand?: 'layered' | 'projectWide';
  worstOrigin?: RecordOriginRow;
  /** See ActivityMeta.componentId. */
  componentId?: string;
  /** In profile (Method) order. Empty for an unclassified activity. */
  phases: PhaseNode[];
  /**
   * Σ Table-A-1 weights of COMPLETE phases, or `undefined` when any profile
   * phase is `unknown` (and always for an unclassified activity).
   */
  percentComplete?: number;
  /**
   * How many stored phases named something this activity's profile does not
   * carry. Non-zero is a DATA DEFECT in the committed project state, surfaced
   * as a count rather than rendered as an extra row.
   */
  offProfilePhaseCount: number;
  /** How many task rows this activity emitted (conditionals only when real). */
  taskCount: number;
  /** Σ over tasks of `max(0, attemptCount - 1)` — the retries, not the attempts. */
  retryCount: number;
  effortDays?: number;
  float?: number;
  onCriticalPath?: boolean;
  band?: string;
  /** The source row, so a consumer never has to re-look-up head-state by id. */
  row: ConstructionRow;
}

export interface BuildActivityTreeOptions {
  /** Per-activity network/activity-list facts, keyed by activity id. */
  meta?: Readonly<Record<string, ActivityMeta>>;
}

// ---------------------------------------------------------------------------
// Attempts
// ---------------------------------------------------------------------------

/**
 * The attempts recorded for one task, ascending by attempt NUMBER.
 *
 * Matched on `task` alone, never on `task` + `phase`: every task key in the
 * generated profiles belongs to exactly one canonical phase, and a ledger entry
 * whose `phase` drifted from the profile's would then silently vanish from the
 * tree rather than showing up where the work actually belongs.
 *
 * Sorted by number rather than trusted in array order: the ledger is
 * append-only but arrival order is not guaranteed, and a ledger written out of
 * order must not be able to flip which attempt counts as current.
 */
function attemptsForTask(attempts: readonly TaskAttemptRow[], task: string): TaskAttemptRow[] {
  return attempts.filter((a) => a.task === task).sort((a, b) => a.attempt - b.attempt);
}

function buildTaskNode(
  activityId: string,
  lifecyclePhase: string,
  task: GeneratedTask,
  ledger: readonly TaskAttemptRow[]
): TaskNode {
  const ordered = attemptsForTask(ledger, task.task);
  const lastIndex = ordered.length - 1;
  const attempts: TaskAttemptNode[] = ordered.map((a, i) => ({ ...a, superseded: i < lastIndex }));
  const latestAttempt = attempts[lastIndex];

  return {
    nodeId: `${activityId}::${lifecyclePhase}::${task.task}`,
    activityId,
    task: task.task,
    label: task.label,
    bookLabel: task.bookLabel,
    gate: task.gate,
    conditional: task.conditional,
    lifecyclePhase,
    // No attempt at all is `unknown`.
    state: latestAttempt === undefined ? 'unknown' : outcomeStateOf(latestAttempt.outcome),
    attempts,
    ...(latestAttempt !== undefined ? { latestAttempt } : {}),
    attemptCount: attempts.length,
  };
}

// ---------------------------------------------------------------------------
// Phases
// ---------------------------------------------------------------------------

/** The server's per-phase completion, keyed by canonical phase name. */
function storedByPhase(phases: readonly PhaseRow[]): Map<string, PhaseRow> {
  const byPhase = new Map<string, PhaseRow>();
  for (const p of phases) byPhase.set(p.phase, p);
  return byPhase;
}

function completionOf(stored: PhaseRow | undefined): PhaseCompletion {
  if (stored === undefined) return 'unknown';
  return stored.completed ? 'complete' : 'incomplete';
}

function buildPhaseNode(
  row: ConstructionRow,
  profilePhase: GeneratedPhase,
  stored: PhaseRow | undefined
): PhaseNode {
  const completedAt = stored?.completedAt;
  return {
    nodeId: `${row.activityId}::${profilePhase.phase}`,
    activityId: row.activityId,
    id: profilePhase.id,
    phase: profilePhase.phase,
    name: profilePhase.name,
    // From the PROFILE. A stored weight that disagrees with Table A-1 is stale
    // data, and honouring it would make one activity's 100% mean something
    // different from another's.
    weight: profilePhase.weight,
    exitCriterion: profilePhase.exitCriterion,
    completion: completionOf(stored),
    ...(completedAt !== undefined ? { completedAt } : {}),
    tasks: profilePhase.tasks
      // A conditional task is emitted ONLY once a real attempt exists for it.
      // Rendering a row for work that never happened is the exact failure this
      // rewrite exists to remove.
      .filter((t) => !t.conditional || row.attempts.some((a) => a.task === t.task))
      .map((t) => buildTaskNode(row.activityId, profilePhase.phase, t, row.attempts)),
  };
}

// ---------------------------------------------------------------------------
// The derivation
// ---------------------------------------------------------------------------

/**
 * Derive the three-tier tree for every construction row, in the order given.
 *
 * Input order is preserved: sorting tier 1 is the toolbar's job (network order
 * vs float ascending), and tiers 2 and 3 are a Figure A-1 SEQUENCE, which is
 * never sorted at all.
 */
export function buildActivityTree(
  rows: readonly ConstructionRow[],
  opts: BuildActivityTreeOptions = {}
): ActivityNode[] {
  return rows.map((row) => buildActivityNode(row, opts.meta?.[row.activityId]));
}

function buildActivityNode(row: ConstructionRow, meta: ActivityMeta | undefined): ActivityNode {
  const profile = profileFor(row.kind, row.variant);
  const stored = storedByPhase(row.phases);

  const phases: PhaseNode[] =
    profile === undefined ? [] : profile.map((p) => buildPhaseNode(row, p, stored.get(p.phase)));

  // A stored phase the profile does not carry is a data defect in the committed
  // project state (one real row classifies as a two-phase `uiDesign` while
  // carrying five Service phases). Counted, never rendered as an extra row — and
  // not counted at all when there is no profile to be off, because there the
  // defect is the missing classification, already reported by `unclassified`.
  const profilePhases = new Set(phases.map((p) => p.phase));
  const offProfilePhaseCount =
    profile === undefined ? 0 : row.phases.filter((p) => !profilePhases.has(p.phase)).length;

  const tasks = phases.flatMap((p) => p.tasks);

  return {
    nodeId: row.activityId,
    activityId: row.activityId,
    label: meta?.label ?? row.activityId,
    ...(row.kind !== undefined ? { kind: row.kind } : {}),
    ...(row.variant !== undefined ? { variant: row.variant } : {}),
    unclassified: !row.classified,
    hasBuildEvidence: row.hasBuildEvidence,
    ...(row.status !== undefined ? { status: row.status } : {}),
    // An EMPTY current phase is an ABSENT current phase.
    //
    // The real fix landed where it belongs — mapConstructionRow now drops
    // `CurrentPhase` at its zero value, the way its four siblings (kind, status,
    // worstOrigin, layer) already did, so `''` no longer reaches any consumer
    // from the wire. This guard stays because it is not a mirror of that rule
    // but a defence of THIS module's own contract: `ConstructionRow
    // .currentLifecyclePhase` is typed `string | undefined`, so `''` remains a
    // type-legal input (a hand-built fixture, a future second producer), and a
    // phase named nothing must never become an ActivityNode field.
    ...(row.currentLifecyclePhase !== undefined && row.currentLifecyclePhase.length > 0
      ? { currentLifecyclePhase: row.currentLifecyclePhase }
      : {}),
    ...(row.layer !== undefined ? { layer: row.layer } : {}),
    ...(row.layerBand !== undefined ? { layerBand: row.layerBand } : {}),
    ...(row.worstOrigin !== undefined ? { worstOrigin: row.worstOrigin } : {}),
    ...(meta?.componentId !== undefined ? { componentId: meta.componentId } : {}),
    phases,
    ...percentCompleteOf(profile, phases),
    offProfilePhaseCount,
    taskCount: tasks.length,
    retryCount: tasks.reduce((acc, t) => acc + Math.max(0, t.attemptCount - 1), 0),
    ...(meta?.effortDays !== undefined ? { effortDays: meta.effortDays } : {}),
    ...(meta?.float !== undefined ? { float: meta.float } : {}),
    ...(meta?.onCriticalPath !== undefined ? { onCriticalPath: meta.onCriticalPath } : {}),
    ...(meta?.band !== undefined ? { band: meta.band } : {}),
    row,
  };
}

/**
 * App A §1.3: progress is Σ the Table A-1 weights of the COMPLETE phases.
 *
 * Returned as a spreadable partial so the key is genuinely ABSENT (not present
 * holding `undefined`) whenever the figure is unknowable — which is whenever
 * the activity has no profile at all, or any one of its profile phases went
 * unreported. Reporting 0% for an activity we know nothing about is the same
 * lie in a different font.
 */
function percentCompleteOf(
  profile: readonly GeneratedPhase[] | undefined,
  phases: readonly PhaseNode[]
): { percentComplete?: number } {
  if (profile === undefined) return {};
  if (phases.some((p) => p.completion === 'unknown')) return {};
  return {
    percentComplete: phases
      .filter((p) => p.completion === 'complete')
      .reduce((acc, p) => acc + p.weight, 0),
  };
}
