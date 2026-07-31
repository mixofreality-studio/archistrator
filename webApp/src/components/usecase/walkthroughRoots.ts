/**
 * Pure root-detection for the use-case walkthrough: which nodes are legal entry
 * points into an activity diagram. A UML activity conventionally begins at its
 * single `start` pseudostate, but an accept/time-event node with no incoming
 * edges is also a legal entry — the activity can begin when that event arrives,
 * not only when the reader clicks through from a literal start node. Roots are
 * the union of `start`-kind nodes and in-degree-0 nodes, deduped, in diagram
 * (node-array) order. Extracted from UseCaseWalkthrough so multi-root diagrams
 * are unit-testable without mounting the component.
 */
import type { ActivityNodeKind } from '../../contracts/types';

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
    if (n.kind === 'start' || (inDegree.get(n.id) ?? 0) === 0) isRoot.add(n.id);
  }

  return nodes.filter((n) => isRoot.has(n.id)).map((n) => n.id);
}
