/**
 * Pure root-detection for the use-case walkthrough: which nodes are legal entry
 * points into an activity diagram. A UML activity conventionally begins at its
 * single `start` pseudostate, but an accept/time-event node with no incoming
 * edges is also a legal entry — the activity can begin when that event arrives,
 * not only when the reader clicks through from a literal start node. Roots are
 * the union of `start`-kind nodes (always) and in-degree-0 `timeEvent`/
 * `acceptEvent` nodes, deduped, in diagram (node-array) order — the SAME entry
 * vocabulary the platform gates use (CC-TRIGGER-EVENT / UC-ACT-PRESENT):
 * `{start, timeEvent, acceptEvent}`. An in-degree-0 node of any OTHER kind
 * (notably `note` — the Task-7 design amendment's edge-less documentation
 * nodes, e.g. `customer-charged`, `in-flight`, `argo-reconcile`) is not a legal
 * entry and must not become a walkthrough root just because nothing points to
 * it (fix-round-1 FINDING 1: this previously offered dead-end notes as
 * beginnings in the entry chooser). Extracted from UseCaseWalkthrough so
 * multi-root diagrams are unit-testable without mounting the component.
 *
 * `walkthroughPathTo` is the same vocabulary read the other way round: given a
 * node, the route a reader would have taken from a root to reach it — what a
 * deep link into the middle of a diagram needs to seed the stepper.
 */
import type { ActivityNodeKind } from '../../contracts/types';

const EVENT_ENTRY_KINDS = new Set<ActivityNodeKind>(['timeEvent', 'acceptEvent']);

export function walkthroughRoots(
  nodes: { id: string; kind: ActivityNodeKind }[],
  edges: { from: string; to: string }[]
): string[] {
  const inDegree = new Map<string, number>();
  for (const n of nodes) inDegree.set(n.id, 0);
  for (const e of edges) {
    if (!inDegree.has(e.to)) continue;
    inDegree.set(e.to, (inDegree.get(e.to) ?? 0) + 1);
  }

  const isRoot = new Set<string>();
  for (const n of nodes) {
    if (n.kind === 'start' || (EVENT_ENTRY_KINDS.has(n.kind) && (inDegree.get(n.id) ?? 0) === 0)) {
      isRoot.add(n.id);
    }
  }

  return nodes.filter((n) => isRoot.has(n.id)).map((n) => n.id);
}

/**
 * The shortest walked path from a legal entry point to `targetId` — the node ids
 * the walkthrough would have visited to arrive there. Seeds a walkthrough that
 * must OPEN mid-diagram: the Architecture lens' `?step=` deep link resolves a
 * call to its owning activity node and needs the stepper standing on it, not at
 * the start.
 *
 * Breadth-first from the roots (walkthroughRoots order), following each node's
 * outgoing edges in authored edge order — so the answer is deterministic: among
 * the shortest routes, the first one found wins. Returns undefined when the
 * target is not a node of this diagram or no root reaches it; the caller then
 * opens at the natural beginning rather than inventing a route.
 */
export function walkthroughPathTo(
  nodes: { id: string; kind: ActivityNodeKind }[],
  edges: { from: string; to: string }[],
  targetId: string
): string[] | undefined {
  const known = new Set(nodes.map((n) => n.id));
  if (!known.has(targetId)) return undefined;

  // Mirror UseCaseWalkthrough's own degenerate fallback (a cyclic diagram with
  // neither a start nor an in-degree-0 node still has to begin somewhere) so a
  // seeded path is always one the walkthrough itself could have walked.
  const detected = walkthroughRoots(nodes, edges);
  const first = nodes[0];
  const roots = detected.length > 0 ? detected : first !== undefined ? [first.id] : [];

  const outgoing = new Map<string, string[]>();
  for (const e of edges) {
    const list = outgoing.get(e.from);
    if (list === undefined) outgoing.set(e.from, [e.to]);
    else list.push(e.to);
  }

  const parent = new Map<string, string>();
  const seen = new Set<string>(roots);
  // The array iterator is live: nodes pushed while walking are visited in the
  // same pass, which is exactly the BFS frontier (no separate head index).
  const queue = [...roots];
  for (const id of queue) {
    if (id === targetId) {
      const path = [id];
      for (let p = parent.get(id); p !== undefined; p = parent.get(p)) path.unshift(p);
      return path;
    }
    for (const to of outgoing.get(id) ?? []) {
      if (seen.has(to) || !known.has(to)) continue;
      seen.add(to);
      parent.set(to, id);
      queue.push(to);
    }
  }
  return undefined;
}

/**
 * The walked-path length below which Back/Restart have nothing left to rewind
 * to. A single-root diagram floors at 1 — the start node is always on the
 * path, so there is no "before the start". A multi-root diagram floors at 0 —
 * the entry chooser (no step picked yet) is itself a legal, revisitable state,
 * since more than one beginning exists and the reader may want to reconsider
 * which one they took.
 */
export function walkthroughNavFloor(rootCount: number): number {
  return rootCount > 1 ? 0 : 1;
}
