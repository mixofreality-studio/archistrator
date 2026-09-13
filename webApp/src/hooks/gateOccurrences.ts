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
 *
 * WHEN A READ WAS ASKED FOR (tasks round 2)
 * -----------------------------------------
 * Each occurrence also carries when its latest read was REQUESTED. A read that was
 * already in flight when a decision failed arrives after the failure but describes
 * the gate from before it; the decision's evidence rule (decisionFlow.ts) counts a
 * read from its request, so such a read cannot re-enable the buttons.
 */
import type { ConstructionStage } from '../contracts/types.ts';

export interface GateOccurrence {
  /** How many times the session has been observed entering `awaitingApproval`. */
  epoch: number;
  /** The latest observed stage; `null` where the session no longer exists. */
  stage: ConstructionStage | null;
  /** When the latest observation ARRIVED (the query's dataUpdatedAt) — orders reads. */
  seenAt: number;
  /** When the latest observation was REQUESTED (its fetch began); 0 where unknown,
   *  which never counts as newer than anything. */
  requestedAt: number;
  /** When it was first seen away from the gate since last at it; absent at the gate. */
  leftAt?: number;
}

/** Fold one session read into the activity's occurrence. A read no newer than the
 *  last one folded changes nothing (several observers report the same fetch). */
export function observeGate(
  prev: GateOccurrence | undefined,
  stage: ConstructionStage | null,
  seenAt: number,
  requestedAt: number
): GateOccurrence {
  if (prev !== undefined && seenAt <= prev.seenAt) return prev;
  const atGate = stage === 'awaitingApproval';
  const wasAtGate = prev?.stage === 'awaitingApproval';
  const epoch = (prev?.epoch ?? 0) + (atGate && !wasAtGate ? 1 : 0);
  const leftAt = atGate ? undefined : wasAtGate ? seenAt : prev?.leftAt;
  return { epoch, stage, seenAt, requestedAt, ...(leftAt !== undefined ? { leftAt } : {}) };
}

export function occurrenceKey(projectId: string, activityId: string): string {
  return `${projectId} ${activityId}`;
}

/** How many activities' occurrences the store keeps (review minor: it grew for the
 *  QueryClient's lifetime). Far above the supervision cap, so no activity a
 *  decision is live on is ever dropped: every read moves its entry to the end. */
export const OCCURRENCE_LIMIT = 256;

/**
 * The store with one occurrence set, most recently observed LAST, and only the
 * newest `limit` kept — the oldest-observed activities fall off the front.
 */
export function withOccurrence(
  store: ReadonlyMap<string, GateOccurrence>,
  key: string,
  occurrence: GateOccurrence,
  limit = OCCURRENCE_LIMIT
): Map<string, GateOccurrence> {
  const out = new Map(store);
  out.delete(key);
  out.set(key, occurrence);
  for (const oldest of out.keys()) {
    if (out.size <= limit) break;
    out.delete(oldest);
  }
  return out;
}
