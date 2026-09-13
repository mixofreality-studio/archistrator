/**
 * The lane spine — one activity's lifecycle drawn as GEOMETRY rather than as a
 * graph (spec §7.6: "Each lane body is the 5-segment lifecycle spine: segment
 * width = Table A-1 weight, fill = state. That is the mini-lifecycle").
 *
 * FIVE CANONICAL SLOTS (Decision D5)
 * ---------------------------------
 * Every spine has the same five slots in the same order, so two lanes read
 * against each other at a glance. The order is the Service profile's — the one
 * profile that is Figure A-1 verbatim and carries all five phases — read from
 * the generated vocabulary, never typed out here. A phase the activity's
 * profile carries is as wide as its Table A-1 weight; a phase it does not carry
 * is a narrow fixed gap (§7.2 `absent`: "gap in the spine, 40% opacity"), so a
 * Deployment lane visibly HAS no Requirements or Test Plan rather than seeming
 * to be missing data. An unclassified activity has no spine at all (§9 AC4:
 * zero lifecycle sub-rows) — a default profile would launder "we do not know
 * what this is" into a plausible lifecycle.
 *
 * STATE IS THE LIST'S, NEVER A SECOND DERIVATION (Decision D6)
 * -----------------------------------------------------------
 * The segments read the list lens's own tree (activityTree.buildActivityTree):
 * a phase is `complete` only because the SERVER reported it complete (the tree
 * never re-derives completion, and neither does this). Ticks — one per Figure
 * A-1 task the profile emits — take the list's own task-row rule
 * (taskRowState, with noAttemptStateFor), so the graph and the list can never
 * disagree about one task. A segment that is not complete takes the loudest of
 * its ticks (awaitingHuman › failed › running), else `incomplete` when the
 * server reported the gate not passed or any task in it ran, else the list's
 * no-attempt state: `notStarted` (classified, nothing recorded) or `unknown`
 * (recorded work that predates per-task history).
 *
 * Pure — no React — pinned by laneSpine.test.ts.
 */
import type { ActivityNode, PhaseNode } from '../list/activityTree.ts';
import { GENERATED_TEMPLATES, type LifecyclePhase } from '../lifecycleTemplates.gen.ts';
import { taskRowState, type RowState } from '../list/activityRowPresentation.ts';
import { noAttemptStateFor, type NoAttemptState } from '../detail/detailPaneState.ts';

/** The five lifecycle phases in Method order — the Service profile's order. */
export const CANONICAL_LIFECYCLE: readonly LifecyclePhase[] = GENERATED_TEMPLATES.service.map(
  (p) => p.phase
);

/** The width of a phase the profile does not carry, as a fraction of the spine. */
export const ABSENT_GAP_FRACTION = 0.04;

export type SegmentState =
  | 'complete'
  | 'awaitingHuman'
  | 'failed'
  | 'running'
  | 'incomplete'
  | 'notStarted'
  | 'unknown'
  | 'absent';

/** One Figure A-1 task on the spine (drawn at LOD-1). */
export interface SpineTick {
  /** The tree's node id — `<activityId>::<phase>::<task>`. */
  nodeId: string;
  task: string;
  label: string;
  state: RowState;
  /** Attempts beyond the first — drawn as a doubled stroke. */
  retries: number;
  gate: boolean;
}

export interface SpineSegment {
  phase: LifecyclePhase;
  /** The profile's display name. Absent on an `absent` gap. */
  name?: string;
  /** Table A-1 weight. Absent on an `absent` gap. */
  weight?: number;
  /** Share of the spine's width, 0..1; the segments sum to 1. */
  fraction: number;
  state: SegmentState;
  ticks: SpineTick[];
}

export interface LaneSpine {
  unclassified: boolean;
  /** Five canonical slots, or none for an unclassified activity. */
  segments: SpineSegment[];
}

function segmentState(
  p: PhaseNode,
  ticks: readonly SpineTick[],
  noAttempt: NoAttemptState
): SegmentState {
  if (p.completion === 'complete') return 'complete';
  const states = new Set<RowState>(ticks.map((t) => t.state));
  if (states.has('awaitingHuman')) return 'awaitingHuman';
  if (states.has('failed')) return 'failed';
  if (states.has('running')) return 'running';
  if (p.completion === 'incomplete') return 'incomplete';
  // The server reported nothing for this phase, but a task in it ran: work has
  // happened and the gate has not been recorded as passed.
  if (states.has('passed') || states.has('skipped')) return 'incomplete';
  return noAttempt;
}

export function laneSpineFor(node: ActivityNode): LaneSpine {
  if (node.unclassified) return { unclassified: true, segments: [] };

  const byPhase = new Map<LifecyclePhase, PhaseNode>(node.phases.map((p) => [p.phase, p]));
  const absentCount = CANONICAL_LIFECYCLE.filter((ph) => !byPhase.has(ph)).length;
  const totalWeight = node.phases.reduce((acc, p) => acc + p.weight, 0);
  const usable = Math.max(0, 1 - absentCount * ABSENT_GAP_FRACTION);
  const noAttempt = noAttemptStateFor(node.row);

  const segments = CANONICAL_LIFECYCLE.map((phase): SpineSegment => {
    const p = byPhase.get(phase);
    if (p === undefined) {
      return { phase, fraction: ABSENT_GAP_FRACTION, state: 'absent', ticks: [] };
    }
    const ticks = p.tasks.map(
      (t): SpineTick => ({
        nodeId: t.nodeId,
        task: t.task,
        label: t.label,
        state: taskRowState(t, node.row.status, noAttempt),
        retries: Math.max(0, t.attemptCount - 1),
        gate: t.gate,
      })
    );
    return {
      phase,
      name: p.name,
      weight: p.weight,
      // A profile's weights sum to 100; the equal split only guards a
      // malformed all-zero profile against a division by zero.
      fraction: totalWeight > 0 ? (usable * p.weight) / totalWeight : usable / node.phases.length,
      state: segmentState(p, ticks, noAttempt),
      ticks,
    };
  });

  return { unclassified: false, segments };
}
