/**
 * ONE vocabulary for "a human is owed something here", shared by every lens and
 * the pane (designer P1-6 / review I4, the Stage C plan's Q4).
 *
 * The owed set (owedWork.ts) is the only source of it: the live workflow stage and
 * the pump's own failure record. Head-state `in-review` is NOT — the server derives
 * it from "some phases are complete, not all", which is true of every activity
 * mid-lifecycle, gated or not (spec §1). So the list's "Awaiting me" scope, its row
 * chips and the pane's state chip all read the marks built here, and one reason
 * says one word everywhere: a failure is "Failed" in the row, the list and the pane
 * (designer P0-2), never "Stopped" in one and "Failed" in another.
 */
import type { OwedItem, OwedReason } from './owedWork.ts';

export interface OwedChip {
  /** Sentence case; the views uppercase it. */
  label: string;
  /** The fill it takes (detailPaneState.taskDetailStateFill). */
  state: 'awaitingHuman' | 'failed';
}

export const OWED_CHIP: Readonly<Record<OwedReason, OwedChip>> = {
  gate: { label: 'Awaiting you', state: 'awaitingHuman' },
  takeover: { label: 'Steer needed', state: 'awaitingHuman' },
  failed: { label: 'Failed', state: 'failed' },
};

/** What an activity is owed for, as the list and the pane need it. */
export interface OwedMark {
  reason: OwedReason;
  /** `gate` marks: the gate task the decision is on, where the profile names one. */
  gateTask?: string;
  /** `gate` marks: the lifecycle phase the gate is on. */
  lifecyclePhase?: string;
}

export type OwedMarks = ReadonlyMap<string, OwedMark>;

/** One mark per activity (owedWork yields at most one owed item per activity). */
export function owedMarksFor(
  items: readonly Pick<OwedItem, 'activityId' | 'reason' | 'gate'>[]
): Map<string, OwedMark> {
  const out = new Map<string, OwedMark>();
  for (const i of items) {
    out.set(i.activityId, {
      reason: i.reason,
      ...(i.gate?.task !== undefined ? { gateTask: i.gate.task } : {}),
      ...(i.gate?.lifecyclePhase !== undefined ? { lifecyclePhase: i.gate.lifecyclePhase } : {}),
    });
  }
  return out;
}

/**
 * Steer-needed and failed activities are REVIEW-ONLY until follow-up B1 persists
 * the operator's note and delivers it to the next attempt (the PM's must-hold): no
 * Retry, Re-queue or Skip. Run stays in the bar, because Run is always present, but
 * disabled with this line as its reason (tasks merge review I2 ruling;
 * detailPaneState.reviewOnlyActionsFor). The pane also says so in this muted line
 * (designer P0-2, orchestrator ruling).
 */
export const REVIEW_ONLY_NOTE =
  'Retry and re-queue arrive once your note reaches the agent. Until then, steer from GitHub or the MCP override_activity tool.';

/**
 * The owed chip the pane's selection carries, or none — the pane's AWAITING YOU
 * (and STEER NEEDED, FAILED) comes from the owed set and nowhere else (review I4).
 * A gate applies to the activity, its gated phase and its gate task; a steer or a
 * failure to the activity itself (reviewOnlyFor).
 */
export function owedStateFor(
  mark: OwedMark | undefined,
  selection: { lifecyclePhase?: string | undefined; task?: string | undefined }
): OwedChip | undefined {
  if (mark === undefined) return undefined;
  if (mark.reason !== 'gate')
    return reviewOnlyFor(mark, selection) ? OWED_CHIP[mark.reason] : undefined;
  if (selection.lifecyclePhase !== undefined && selection.lifecyclePhase !== mark.lifecyclePhase) {
    return undefined;
  }
  if (selection.task !== undefined && selection.task !== mark.gateTask) return undefined;
  return OWED_CHIP.gate;
}

/** Whether the pane's selection is a steer-needed or failed activity itself (the
 *  activity, not one of its phases or tasks — a passed task inside a failed
 *  activity is still a passed task). */
export function reviewOnlyFor(
  mark: OwedMark | undefined,
  selection: { lifecyclePhase?: string | undefined; task?: string | undefined }
): boolean {
  if (mark === undefined || mark.reason === 'gate') return false;
  return selection.lifecyclePhase === undefined && selection.task === undefined;
}
