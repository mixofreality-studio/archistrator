/**
 * ONE project read → the plan's activities, in Table 11-1 order.
 *
 * The three inputs are all carried by the single `useProject` read the plan
 * screen already makes: the committed `activityList` slot says what EXISTS (and
 * what each activity is called and costs), the committed `network` slot says
 * what each activity DEPENDS ON and which are on the critical path, and the
 * construction rows say how far each has GOT. Nothing here fetches, and nothing
 * here invents: an activity the model cannot place keeps its row `undefined`
 * and is reported as unplaced rather than dropped (planCopy.unplacedTilesNote).
 *
 * Pure — no React — so node:test loads it directly (relative VALUE imports
 * carry an explicit `.ts`).
 */
import type {
  ConstructionRow,
  ProjectArtifactModelEnvelope,
  NetworkDependency,
} from '../../contracts/types.ts';
import { narrowProject } from '../../contracts/projectAdapters.ts';
import type { TaskDetailState } from '../construction/detail/detailPaneState.ts';
import type { LifecycleNode } from './lifecycleGraphTypes.ts';
import { miniLifecycleFromRow } from './miniLifecycleFromRow.ts';
import { planRowFor } from './planRowFor.ts';
import type { PlanRow, PlanTileInput } from './planGraphLayout.ts';

/**
 * The SDP-review milestone. It is NOT an activity — it never appears in the
 * committed activity list — but it IS what every build root depends on, so the
 * network names it in `dependsOn` and the graph draws it as the gate
 * (layoutPlanGraph's milestone fan-out).
 */
export const MILESTONE_ID = 'M0';

export interface PlanActivity {
  id: string;
  title: string;
  /** planRowFor; `undefined` ⇒ listed, not drawn. */
  row: PlanRow | undefined;
  /** Its dependencies, from the committed network (slot 10) — `M0` included. */
  calls: readonly string[];
  /** miniLifecycleFromRow; `[]` for an activity with no row or no profile. */
  lifecycle: readonly LifecycleNode[];
  onCriticalPath: boolean;
  effortDays: number;
}

/**
 * How far an activity has got, read ONLY from its mini lifecycle — the same
 * nodes the row and the tile draw, so the word and the picture can never
 * disagree. `unknown` is the honest answer for an activity with no lifecycle at
 * all (unclassified, or a profile the generator does not carry): "not started"
 * would be a claim about an activity nothing is known about.
 */
export type PlanState = 'unknown' | 'notStarted' | 'running' | 'awaitingYou' | 'failed' | 'done';

/**
 * Which of the shared state fills a plan state paints with
 * (detailPaneState.taskDetailStateFill). The plan invents NO colour of its own:
 * the list row, the graph tile and the detail pane must never disagree about
 * what "awaiting you" looks like. Type-only import, so this module stays pure.
 */
export const PLAN_STATE_DETAIL: Record<PlanState, TaskDetailState> = {
  unknown: 'unknown',
  notStarted: 'notStarted',
  running: 'running',
  awaitingYou: 'awaitingHuman',
  failed: 'failed',
  done: 'passed',
};

/** The word each state says. Never a colour alone (WCAG 1.4.1). */
export const PLAN_STATE_LABEL: Record<PlanState, string> = {
  unknown: 'Unclassified',
  notStarted: 'Not started',
  running: 'Running',
  awaitingYou: 'Awaiting you',
  failed: 'Failed',
  done: 'Done',
};

/**
 * The worst thing the lifecycle says, in the order a supervisor cares about it:
 * a failure first, then a gate waiting on a human, then work in flight, then
 * fully done, then started-but-quiet, then untouched.
 */
export function planStateFor(lifecycle: readonly LifecycleNode[]): PlanState {
  if (lifecycle.length === 0) return 'unknown';
  if (lifecycle.some((n) => n.state === 'failed')) return 'failed';
  if (lifecycle.some((n) => n.state === 'awaitingHuman' || n.state === 'sentBack')) {
    return 'awaitingYou';
  }
  if (lifecycle.some((n) => n.state === 'running')) return 'running';
  if (lifecycle.every((n) => n.state === 'done')) return 'done';
  return lifecycle.some((n) => n.state === 'done') ? 'running' : 'notStarted';
}

/**
 * Table 11-1's order, top to bottom: the three front-end design activities (and
 * the M0 divider the LIST draws after them), then the build stack in BUILD
 * order, then the side lane, then system testing.
 *
 * The side lane sits SECOND-TO-LAST, not last: N-STP is written early and runs
 * beside the whole stack, but N-IT is the terminal activity of the project —
 * "system testing closes the list" is Table 11-1's own shape, and appending the
 * lane after it would put the plan's last row before its last activity.
 */
export const LIST_ROWS: readonly PlanRow[] = [
  'frontEnd',
  'resource',
  'resourceAccess',
  'engine',
  'manager',
  'client',
  'sideLane',
  'systemTesting',
];

function dependenciesOf(
  dependencies: readonly NetworkDependency[] | null | undefined
): ReadonlyMap<string, readonly string[]> {
  const byActivity = new Map<string, readonly string[]>();
  for (const d of dependencies ?? []) byActivity.set(d.activity, d.dependsOn ?? []);
  return byActivity;
}

export function planActivitiesFrom(input: {
  activityList: ProjectArtifactModelEnvelope | undefined;
  network: ProjectArtifactModelEnvelope | undefined;
  rows: Readonly<Record<string, ConstructionRow>>;
}): PlanActivity[] {
  const activities = narrowProject(input.activityList, 'activityList')?.activities ?? [];
  const network = narrowProject(input.network, 'network');
  const dependsOn = dependenciesOf(network?.dependencies);
  const critical = new Set(network?.criticalPath ?? []);

  const derived = activities.map((a): PlanActivity => {
    const row = input.rows[a.name];
    return {
      id: a.name,
      title: a.title !== undefined && a.title.length > 0 ? a.title : a.name,
      row: row === undefined ? undefined : planRowFor(row),
      calls: dependsOn.get(a.name) ?? [],
      lifecycle: row === undefined ? [] : miniLifecycleFromRow(row),
      onCriticalPath: critical.has(a.name),
      effortDays: a.effortDays,
    };
  });

  // The committed list's own order inside each row — the plan's rows are a
  // Table 11-1 grouping, not a re-sort of what the architect wrote.
  const ordered: PlanActivity[] = [];
  for (const row of LIST_ROWS) ordered.push(...derived.filter((a) => a.row === row));
  // An activity the model cannot place is still an activity: it is LISTED,
  // last, and named out loud under the graph (planCopy.unplacedTilesNote).
  ordered.push(...derived.filter((a) => a.row === undefined));
  return ordered;
}

/** The tiles the GRAPH draws (those with a row) and the ids it cannot place. */
export function planTilesFrom(activities: readonly PlanActivity[]): {
  tiles: PlanTileInput[];
  unplaced: string[];
} {
  const tiles: PlanTileInput[] = [];
  const unplaced: string[] = [];
  for (const a of activities) {
    if (a.row === undefined) unplaced.push(a.id);
    else tiles.push({ id: a.id, row: a.row, calls: a.calls });
  }
  return { tiles, unplaced };
}
