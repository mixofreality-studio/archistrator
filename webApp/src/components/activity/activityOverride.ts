/**
 * WHICH OF THE TWO OVERRIDES AN ACTIVITY CAN TAKE — steer, re-open, or neither.
 *
 * `OverrideActivity` means TWO things now, keyed by liveness (stage 4b1 Task 12,
 * fix round 1):
 *
 *   - STEER a live escalation. The walk's dispatch failed, the child is holding at
 *     a takeover gate, and the operator's override is fed through the same
 *     decide→execute machinery the automatic variance path uses. The façade refuses
 *     anything else, and since stage 4b2 Task 2 it refuses on the LEDGER: *"activity
 *     X is in flight but no task on its ledger holds a failed attempt, so it is not
 *     escalated and there is nothing an override could name"*.
 *   - RE-OPEN a finished activity. There is no child at all — a failed walk, a
 *     spent variance budget, an operator's own Skip, or a Completed activity whose
 *     slot commit failed after its exit. The Manager re-arms the row
 *     (`RecordOperatorNote{requeue}`), the pump selects it on its next tick, and the
 *     re-run seeds every task that PASSED from the ledger and re-dispatches only
 *     what did not.
 *
 * The SPA must label them as TWO ACTIONS, never one button whose meaning the server
 * infers from state the reader cannot see — the two consequences are different
 * enough that a reader owed one and given the other has been lied to.
 *
 * ── HOW LIVENESS IS READ, and the one approximation in it ───────────────────
 * `DeliveryActivityView.state` has five members — notStarted, running,
 * awaitingHuman, done, failed — and NONE of them is "escalated": the server folds a
 * takeover gate and an approval gate into the same `awaitingHuman`. TERMINAL is
 * therefore exact (done | failed, the two the store's own `CoarsePhaseFor` answers
 * and the two `RecordOperatorNote{requeue}` accepts), and ESCALATED is DERIVED, by
 * the same rule the Manager recovers the escalated task with (`escalatedTaskOf`:
 * the last task whose highest-numbered attempt FAILED) on an activity that has not
 * exited.
 *
 * AND THERE IS NO LONGER A BACKSTOP UNDER IT. This paragraph used to end "the façade's
 * own precheck refuses the steer and says which stage the activity is actually at".
 * Stage 4b2 Task 2 removed that stage precheck — it read a single-valued session stage
 * that a fork made wrong — so the server now applies the SAME ledger rule this file
 * does. Where they agree, the override lands on the right task; where the derivation is
 * stale (a failure the walk already re-dispatched inside the same poll window) the
 * server does not catch it, because it is reading the same ledger and reaching the same
 * answer. The refusal that remains is the honest one — an empty ledger names nothing —
 * not a second opinion about liveness. The exact answer is still the live session's
 * `awaitingTakeover` stage, one more read (`QueryProjectView{session}`) than this screen
 * makes today.
 *
 * Pure and React-free so `node --test` loads it directly.
 */
import type { ActivityViewWire } from './activityViewToGraph.ts';

/** The one override this activity can take right now. */
export type ActivityOverrideAction = 'steer' | 'reopen' | 'none';

/**
 * THE ONLY override kind either action sends. `retry` is what both mean — re-run the
 * work — and the reopen arm ignores the kind entirely (it is reached by the activity
 * having no live child, not by the word on the button). `skip` and `reassign` belong
 * to a variance surface this screen does not yet have.
 */
export const OVERRIDE_KIND = 'retry' as const;

/**
 * Which override an activity can take, from the activity view alone.
 *
 * TERMINAL IS CHECKED FIRST and that ordering is load-bearing: a failed activity has
 * a failed attempt too, so asking "is anything failed?" before "has it exited?" would
 * offer a steer with no child to reach.
 */
export function overrideActionFor(view: ActivityViewWire | undefined): ActivityOverrideAction {
  if (view === undefined) return 'none';
  if (view.state === 'done' || view.state === 'failed') return 'reopen';
  if (view.state === 'notStarted') return 'none';
  return hasFailedLatestAttempt(view) ? 'steer' : 'none';
}

/**
 * `escalatedTaskOf`'s rule, client-side: a task whose LATEST revision failed. The
 * latest is the last in `revisions` — the wire orders them oldest first — so a task
 * that failed and was then re-dispatched is not escalated any more.
 */
function hasFailedLatestAttempt(view: ActivityViewWire): boolean {
  return view.tasks.some((task) => {
    return task.revisions[task.revisions.length - 1]?.outcome === 'failed';
  });
}
