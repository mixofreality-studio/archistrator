/**
 * WHICH (activity, task) a design artifact kind lives on.
 *
 * Every delivery write is addressed `(projectId, activityId)` + a `taskID`; the
 * server resolves the artifact kind from the activity's lifecycle. The Activity
 * Experience already knows both, because its route carries them — but the screens
 * that are keyed by ARTIFACT KIND (the MCP system-design widget, and the home
 * page's Phase-1 spine) know only the kind, so they need the inverse.
 *
 * It is DERIVED from `lifecycles.gen.ts`, not hand-listed: that file is generated
 * from the same method-assets lifecycles the server reads through
 * `artifactKindForTask`, so a lifecycle that renames a task or moves an artifact to
 * another activity moves this with it. A hand table would be a second source of
 * truth for a mapping the server owns.
 *
 * Only the three DESIGN activities are searched. A construction artifact ('SRS',
 * 'DetailedDesign', …) names no slot and is never addressed by kind — its screens
 * always have the activityId — so it resolves to `undefined` here rather than to
 * something plausible.
 */
import type { ArtifactKindFull } from '../../contracts/types.ts';
import { lifecycleFor, type LifecycleTaskDef } from './lifecycles.gen.ts';
import { SLOT_KIND } from './taskArtifactFor.ts';

/** The (activity, task) pair a delivery write is addressed to. */
export interface DesignTaskRef {
  activityId: string;
  taskId: string;
}

/**
 * The three design activities, in plan order. The activity id IS the lifecycle type
 * key for these three — `ARCHITECTURE_ACTIVITY_ID` is the same string
 * (taskArtifactFor.ts), and the router addresses them by it.
 */
const DESIGN_ACTIVITY_IDS = ['requirements', 'architecture', 'projectDesign'] as const;

function tasksOf(activityId: string): readonly LifecycleTaskDef[] {
  return lifecycleFor(activityId)?.tasks ?? [];
}

/** Whether a lifecycle task produces this app-level artifact kind. */
function produces(task: LifecycleTaskDef, kind: ArtifactKindFull): boolean {
  const raw = task.artifactKind;
  return raw !== undefined && SLOT_KIND[raw] === kind;
}

/**
 * The DISPATCH task that produces this artifact — what a draft, a redraft or a
 * re-derivation is addressed to.
 *
 * `projectDesign`'s single task is a `review`, not a `dispatch`: the M0 plan is
 * computed, and `DispatchActivityTask` maps its id onto `RequestSDPCommit`. So this
 * takes the task that CARRIES the kind whatever its own kind is, which is exactly
 * what the server's `artifactKindForTask` does.
 */
export function dispatchRefFor(kind: ArtifactKindFull): DesignTaskRef | undefined {
  for (const activityId of DESIGN_ACTIVITY_IDS) {
    const task = tasksOf(activityId).find((t) => produces(t, kind));
    if (task !== undefined) return { activityId, taskId: task.id };
  }
  return undefined;
}

/**
 * The REVIEW task that judges this artifact — what a decision, a question, a
 * comment-status flip or a stale acknowledgement is addressed to.
 *
 * A review task carries no `artifactKind` of its own; it carries `reviews`, naming
 * the dispatch it judges. Where a lifecycle's one task is already the review (the
 * M0 gate), that task is its own answer.
 */
export function reviewRefFor(kind: ArtifactKindFull): DesignTaskRef | undefined {
  for (const activityId of DESIGN_ACTIVITY_IDS) {
    const tasks = tasksOf(activityId);
    const produced = tasks.find((t) => produces(t, kind));
    if (produced === undefined) continue;
    if (produced.kind === 'review') return { activityId, taskId: produced.id };
    const review = tasks.find((t) => t.reviews === produced.id);
    if (review !== undefined) return { activityId, taskId: review.id };
    // A dispatch with no reviewing task cannot be decided on; say so by absence
    // rather than by addressing the dispatch and letting the server refuse.
    return undefined;
  }
  return undefined;
}
