/**
 * The ONE translation between the `QueryActivityView` wire shape and the
 * lifecycle graph's props vocabulary. (The task's facts — worker class,
 * command, exit criterion, artifact kind — are the server's AgentTaskSpec now,
 * read by `useTaskSpec`; nothing here joins them in.)
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
import { lifecycleFor, type LifecycleDef } from './lifecycles.gen.ts';
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
  withdrawn: 'withdrawn',
};

/**
 * The lifecycle key for a wire (type, variant) pair — the same rule
 * `projectstate.LifecycleKeyFor` applies server-side: `testing:<variant>` for
 * a testing activity, the bare type for everything else. A testing row that
 * arrived without a variant keys as `testing:none`, which names no lifecycle:
 * the server's zero `TestingVariant` is None since the system test plan
 * (`plan`) and system testing (`systemTest`) variants were retired (spec §7), so there is nothing to
 * fall back to.
 */
export function lifecycleKeyFor(type: string, variant: string | undefined): string {
  if (type !== 'testing') return type;
  return `testing:${variant !== undefined && variant.length > 0 ? variant : 'none'}`;
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
