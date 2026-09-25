/**
 * The plan row's mini lifecycle, derived from ONE project read.
 *
 * `QueryActivityView` answers for one activity; a plan screen showing 32 of
 * them would make 32 HTTP reads, each on its own 2s/8s poll. The project read
 * already carries, per activity, the stored lifecycle-phase completions, the
 * coarse phase and the coarse build status — which is exactly what
 * `components/construction/graph/laneSpine.ts` derives its spine from today
 * (and has done since the graph lens shipped). This module reads only those
 * five members (`kind`, `variant`, `phases`, `currentLifecyclePhase`,
 * `status` — see {@link MiniLifecycleInput}), never the per-task attempt
 * ledger: that ledger is per-task history the project read does not carry at
 * this granularity, which is exactly why `revisions` below is always empty.
 * The batched read (`QueryProjectView`) is stage 4's op, not stage 5's.
 *
 * The nodes this produces carry STATES ONLY. `revisions` is deliberately
 * empty: a revision is per-task history the project read does not carry, and
 * a fabricated one would let a mini graph claim a round that never happened.
 * Opening the activity is what fetches real revisions.
 */
import { lifecycleFor } from './lifecycles.gen.ts';
import { lifecycleKeyFor } from './activityViewToGraph.ts';
import type { ConstructionRow } from '../../contracts/types.ts';
import type { LifecycleNode, LifecycleNodeState } from './lifecycleGraphTypes.ts';

/**
 * Exactly the five members of `ConstructionRow` this rule reads. Taking a
 * `Pick` rather than the whole row keeps the test fixtures four lines long and
 * makes it impossible for this module to start depending on evidence flags it
 * has no business branching on.
 */
export type MiniLifecycleInput = Pick<
  ConstructionRow,
  'kind' | 'variant' | 'phases' | 'currentLifecyclePhase' | 'status'
>;

const COMPLETE: LifecycleNodeState = 'done';

/**
 * A phase's tasks all take that phase's state; the graph's job here is to say
 * how far the activity has got, not which task inside a phase is moving —
 * the project read cannot answer that, and pretending otherwise is the lie
 * this module is shaped to avoid.
 */
export function miniLifecycleFromRow(input: MiniLifecycleInput): LifecycleNode[] {
  // An UNCLASSIFIED row has no `kind`, so it has no lifecycle and gets no
  // spine at all (§9 AC4). A default profile would launder "we do not know
  // what this is" into a plausible-but-wrong lifecycle.
  if (input.kind === undefined) return [];
  const def = lifecycleFor(lifecycleKeyFor(input.kind, input.variant));
  if (def === undefined) return [];
  const completed = new Set(input.phases.filter((p) => p.completed).map((p) => p.phase));
  return def.tasks.map((t) => ({
    id: t.id,
    kind: t.kind,
    title: t.title,
    phase: t.phase,
    state: stateFor(t.phase, completed, input),
    dependsOn: t.dependsOn,
    revisions: [],
    // `LifecycleTaskDef.revisionGroup` is a required member — every task has
    // one — so this is a plain assignment, not a guarded spread; the guard the
    // brief carried over from `LifecycleNode`'s own OPTIONAL `revisionGroup`
    // is dead code on this source type (`@typescript-eslint/no-unnecessary-condition`).
    revisionGroup: t.revisionGroup,
  }));
}

function stateFor(
  phase: string,
  completed: ReadonlySet<string>,
  input: MiniLifecycleInput
): LifecycleNodeState {
  if (completed.has(phase)) return COMPLETE;
  if (phase !== input.currentLifecyclePhase) return 'pending';
  // `status` is the row's coarse build state (ConstructionRow.status —
  // 'integrated' | 'in-review' | 'in-construction' | 'failed'). 'in-review' IS
  // the awaiting-a-human state on the plan screen: the pump has suspended at a
  // gate. Absent status (unclassified, or no build evidence) reads as running
  // only because the phase is the current one — nothing else claims progress.
  if (input.status === 'failed') return 'failed';
  if (input.status === 'in-review') return 'awaitingHuman';
  return 'running';
}

/** The mini graph's accessible name — a count, never a colour (WCAG 1.4.1). */
export function miniLifecycleLabel(nodes: readonly LifecycleNode[]): string {
  const done = nodes.filter((n) => n.state === 'done').length;
  return `Lifecycle: ${String(done)} of ${String(nodes.length)} tasks done`;
}
