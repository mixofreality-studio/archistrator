/**
 * The ONE translation between the `QueryActivityView` wire shape and the
 * lifecycle graph's props vocabulary — and the ONE place the four facts the
 * wire does not carry (worker class, command, exit criterion, artifact kind)
 * are joined in from the generated lifecycle table.
 *
 * The two vocabularies differ on purpose. The wire says a task `passed` and a
 * lifecycle phase is `completed`; the graph says a node is `done` and a phase
 * has `passed`. The wire's revision outcome is a closed enum; the graph shows
 * the outcome VERBATIM, so it needs a display string. Translating inline at
 * each call site is how two screens end up disagreeing about one task — the
 * defect this module exists to make impossible.
 *
 * Pure and React-free: `node --test` loads it directly.
 */
import type { components } from '../../contracts/schema.ts';
import { lifecycleFor, type LifecycleDef, type LifecycleTaskDef } from './lifecycles.gen.ts';
import type {
  LifecycleNode,
  LifecycleNodeState,
  LifecyclePhase,
  LifecycleRevision,
} from './lifecycleGraphTypes.ts';

export type ActivityViewWire = components['schemas']['DeliveryActivityView'];
type TaskWire = components['schemas']['DeliveryActivityTaskView'];
type RevisionWire = components['schemas']['DeliveryTaskRevisionView'];
type TaskStateWire = components['schemas']['DeliveryActivityTaskState'];
type OutcomeWire = components['schemas']['DeliveryTaskRevisionOutcome'];

/**
 * The wire's task state → the graph's node state. Only `passed`/`done` differ;
 * the rest are the same word on both sides, and are listed anyway so a new
 * wire value fails to compile here rather than rendering as `pending`.
 */
const NODE_STATE: Readonly<Record<TaskStateWire, LifecycleNodeState>> = {
  pending: 'pending',
  locked: 'locked',
  running: 'running',
  awaitingHuman: 'awaitingHuman',
  passed: 'done',
  sentBack: 'sentBack',
  failed: 'failed',
};

/** The revision outcome, said the way a reader says it. Shown verbatim. */
export const OUTCOME_TEXT: Readonly<Record<OutcomeWire, string>> = {
  running: 'running',
  awaitingHuman: 'awaiting you',
  passed: 'approved',
  sentBack: 'sent back',
  failed: 'failed',
  skipped: 'skipped',
};

/**
 * The lifecycle key for a wire (type, variant) pair — the same rule
 * `projectstate.LifecycleKeyFor` applies server-side: `testing:<variant>` for
 * a testing activity, the bare type for everything else. A testing row that
 * arrived without a variant reads as the plan variant, matching the server's
 * zero `TestingVariant`.
 */
export function lifecycleKeyFor(type: string, variant: string | undefined): string {
  if (type !== 'testing') return type;
  return `testing:${variant !== undefined && variant.length > 0 ? variant : 'plan'}`;
}

/** The dispatch header's facts for one task — every one a join, none on the wire. */
export interface TaskFacts {
  workerClass?: string | undefined;
  command?: string | undefined;
  /** Resolved, never raw — see {@link artifactKindOf}. */
  artifactKind?: string | undefined;
  /** The binary exit criterion of the task's lifecycle phase. */
  exitCriterion?: string | undefined;
}

/**
 * WHICH ARTIFACT A TASK IS ABOUT.
 *
 * A REVIEW task carries no `artifactKind` of its own — it carries `reviews`,
 * naming the dispatch it judges, and that dispatch is what names the artifact.
 * Verified across all fourteen lifecycles: `missionReview`, `glossaryReview`,
 * `volatilitiesReview`, `coreUseCasesReview`, `architectureReview`, `srsReview`,
 * `designReview`, `codeReview`, `stpReview` and `testing` all have `reviews` and
 * no `artifactKind`. The ONE exception is `projectDesign`'s `sdpReview`, which
 * carries `artifactKind: 'SdpReview'` and NO `reviews`, because the SDP is
 * computed and there is no dispatch to judge (spec §6/R7).
 *
 * Reading `task.artifactKind` alone therefore returns `undefined` for every
 * design review in the product — and the review body's whole verb table keys on
 * it. The chain is: the task's own kind, then its `reviews` target's, then its
 * `revisionGroup` sibling's (the group a draft and its review share, which is
 * the same answer by another road and survives a lifecycle that ever omits
 * `reviews`).
 */
export function artifactKindOf(def: LifecycleDef, task: LifecycleTaskDef): string | undefined {
  if (task.artifactKind !== undefined) return task.artifactKind;
  if (task.reviews !== undefined) {
    const judged = def.tasks.find((t) => t.id === task.reviews);
    if (judged?.artifactKind !== undefined) return judged.artifactKind;
  }
  const sibling = def.tasks.find(
    (t) =>
      t.id !== task.id && t.revisionGroup === task.revisionGroup && t.artifactKind !== undefined
  );
  return sibling?.artifactKind;
}

export function taskFactsFor(view: ActivityViewWire, taskId: string): TaskFacts {
  const def: LifecycleDef | undefined = lifecycleFor(lifecycleKeyFor(view.type, view.variant));
  if (def === undefined) return {};
  const task: LifecycleTaskDef | undefined = def.tasks.find((t) => t.id === taskId);
  if (task === undefined) return {};
  const phase = def.phases.find((p) => p.id === task.phase);
  const artifactKind = artifactKindOf(def, task);
  return {
    ...(task.workerClass !== undefined ? { workerClass: task.workerClass } : {}),
    ...(task.command !== undefined ? { command: task.command } : {}),
    ...(artifactKind !== undefined ? { artifactKind } : {}),
    ...(phase !== undefined ? { exitCriterion: phase.exitCriterion } : {}),
  };
}

function revisionOf(r: RevisionWire): LifecycleRevision {
  return {
    n: r.n,
    outcome: OUTCOME_TEXT[r.outcome],
    ...(r.commentCount > 0 ? { commentCount: r.commentCount } : {}),
    ...(r.decidedAt !== undefined ? { at: shortDate(r.decidedAt) } : {}),
  };
}

/** "2026-09-12T…" → "Sep 12". An unparseable stamp is shown verbatim, never as "Invalid Date". */
export function shortDate(at: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return `${MONTHS[d.getUTCMonth()] ?? ''} ${String(d.getUTCDate())}`.trim();
}

const MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
] as const;

function nodeOf(
  _view: ActivityViewWire,
  t: TaskWire,
  def: LifecycleDef | undefined
): LifecycleNode {
  const gen = def?.tasks.find((d) => d.id === t.id);
  return {
    id: t.id,
    kind: t.kind,
    title: t.title,
    phase: t.phase,
    state: NODE_STATE[t.state],
    dependsOn: t.dependsOn,
    revisions: t.revisions.map(revisionOf),
    ...(gen?.revisionGroup !== undefined ? { revisionGroup: gen.revisionGroup } : {}),
  };
}

export interface ActivityGraph {
  nodes: LifecycleNode[];
  phases: LifecyclePhase[];
}

/**
 * The whole activity as the graph wants it. Task order is the wire's (the
 * server emits them in lifecycle order, and the first-authored chain is what
 * the layout treats as the trunk — lifecycleGraphLayout.ts).
 */
export function activityViewToGraph(view: ActivityViewWire): ActivityGraph {
  const def = lifecycleFor(lifecycleKeyFor(view.type, view.variant));
  return {
    nodes: view.tasks.map((t) => nodeOf(view, t, def)),
    phases: view.phases.map((p) => ({
      id: p.id,
      label: p.label,
      weight: p.weight,
      passed: p.completed,
    })),
  };
}
