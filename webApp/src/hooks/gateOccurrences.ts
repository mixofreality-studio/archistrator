/**
 * Which OCCURRENCE of an activity's gate the console is looking at (tasks-lens
 * review C2). Pure, so node:test pins it; useGateOccurrences feeds it from the
 * query cache.
 *
 * WHY AN OCCURRENCE, NOT JUST A STAGE
 * -----------------------------------
 * A decision answers ONE gate. The same activity reaches `awaitingApproval` again
 * after a send-back (the redraft's next round) and after an approve (the next
 * phase's gate), and until the server says which round a gate is (follow-up B1) the
 * client cannot tell them apart from the stage alone. Read against the stage
 * alone, the old decision saw "still at the gate" and said "did not land", with
 * Approve enabled, on a gate nobody had decided yet.
 *
 * So every observed session read is folded in here: `epoch` counts how many times
 * the session has been seen ENTERING `awaitingApproval`. A decision records the
 * epoch it answered; a larger one means its gate was left and a new one opened.
 * The limit is the poll: a gate left and re-entered between two reads (~3s) is
 * seen as one occurrence.
 */
import type { ConstructionStage } from '../contracts/types.ts';

export interface GateOccurrence {
  /** How many times the session has been observed entering `awaitingApproval`. */
  epoch: number;
  /** The latest observed stage; `null` where the session no longer exists. */
  stage: ConstructionStage | null;
  /** When the latest observation was fetched (the query's dataUpdatedAt). */
  seenAt: number;
  /** When it was first seen away from the gate since last at it; absent at the gate. */
  leftAt?: number;
}

/** Fold one session read into the activity's occurrence. A read no newer than the
 *  last one folded changes nothing (several observers report the same fetch). */
export function observeGate(
  prev: GateOccurrence | undefined,
  stage: ConstructionStage | null,
  seenAt: number
): GateOccurrence {
  if (prev !== undefined && seenAt <= prev.seenAt) return prev;
  const atGate = stage === 'awaitingApproval';
  const wasAtGate = prev?.stage === 'awaitingApproval';
  const epoch = (prev?.epoch ?? 0) + (atGate && !wasAtGate ? 1 : 0);
  const leftAt = atGate ? undefined : wasAtGate ? seenAt : prev?.leftAt;
  return { epoch, stage, seenAt, ...(leftAt !== undefined ? { leftAt } : {}) };
}

export function occurrenceKey(projectId: string, activityId: string): string {
  return `${projectId} ${activityId}`;
}
