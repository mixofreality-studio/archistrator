/**
 * Pure (JSX-free) call-chain -> activity-diagram "you-are-here" join for the
 * Architecture dynamic lens, kept in a leaf module so it is unit-testable under
 * `node --test` (the callStatus.ts / findingOverlays.ts pattern).
 *
 * The dynamic lens walks a linearized call chain; every call carries the
 * activity node (`stepNodeId`) whose CallStep authored it. Rendering the owning
 * use case's activity diagram beside the chain therefore needs only a
 * projection: which node is the reader standing on, and which nodes/edges has
 * the walk already covered. That projection is an ActivityHighlight — the very
 * shape UseCaseWalkthrough already feeds ActivityFlow — so the two surfaces
 * light up identically.
 *
 * Visited EDGES take the both-endpoints rule: an activity edge is walked only
 * when both of its nodes have been reached by the chain so far. The chain is a
 * linearization of the graph, not a single path, so a step's successors are not
 * knowable from the call list alone; requiring both endpoints keeps the trace
 * honest (it never draws an arm of a decision the reader has not reached).
 */
import type { ActivityHighlight } from '../usecase/ActivityFlow';

/**
 * Projects the chain's position onto the activity diagram: the step owning the
 * call at `currentSeq` is "you are here", every step owning a call at or before
 * it is visited, and an activity edge counts as walked when BOTH endpoints are
 * visited.
 *
 * Returns undefined — meaning "render the plain, undimmed diagram" — when there
 * is no current call, when no call carries that seq, or when the current call
 * has no owning step (a synthetic view with no linked activity diagram: blank
 * `stepNodeId`). An empty highlight would dim every node to 25% with nothing
 * ringed, which is worse than no trace at all.
 */
export function traceHighlight(
  edges: readonly { seq: number; stepNodeId: string }[],
  currentSeq: number | undefined,
  activityEdges: readonly { from: string; to: string }[]
): ActivityHighlight | undefined {
  if (currentSeq === undefined) return undefined;
  const current = edges.find((e) => e.seq === currentSeq);
  if (current === undefined || current.stepNodeId.length === 0) return undefined;

  const visitedNodes = new Set<string>();
  for (const call of edges) {
    if (call.seq <= currentSeq && call.stepNodeId.length > 0) visitedNodes.add(call.stepNodeId);
  }

  const visitedEdges = new Set<string>();
  for (const e of activityEdges) {
    if (visitedNodes.has(e.from) && visitedNodes.has(e.to)) visitedEdges.add(`${e.from}-${e.to}`);
  }

  return { current: current.stepNodeId, visitedNodes, visitedEdges };
}
