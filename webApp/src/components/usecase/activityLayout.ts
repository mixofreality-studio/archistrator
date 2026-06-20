/**
 * Pure layout math for the activity-diagram renderer. Reads a UseCaseView's
 * nodes/edges and produces (x,y) placement so the diagram reads as a real UML
 * activity diagram: lanes are columns, Y is graph depth (longest-path rank), and
 * parallel branches that share a (lane, rank) fan out side-by-side within the lane
 * — so a decision visibly forks into its guarded arms and they reconverge at the
 * merge below. Knows nothing about React-Flow, theming, or node shapes; ActivityFlow
 * consumes these positions and the back-edge set to build the actual nodes/edges.
 */
import type { UseCaseView } from '../../api/adapters';
import { NODE_DIMS } from './nodeDims';

export const SLOT_WIDTH = 280;
export const ROW_HEIGHT = 168;
export const HEADER_HEIGHT = 36;
export const TOP_PAD = 20;

export interface PlacedNode {
  /** Top-left x of the node box. */
  x: number;
  /** Top-left y of the node box. */
  y: number;
}

export interface ActivityLayout {
  /** Top-left position keyed by node id. */
  positions: Map<string, PlacedNode>;
  /** `${from}->${to}` for every back-edge (loop) detected via DFS. */
  backEdges: Set<string>;
  /** Per-lane horizontal extents, in `uc.lanes` order. */
  laneX: Map<string, number>;
  laneWidth: Map<string, number>;
  /** Full canvas height the lane backgrounds should span. */
  canvasHeight: number;
  /** Largest rank assigned (0-based). */
  maxRank: number;
}

const backEdgeKey = (from: string, to: string): string => `${from}->${to}`;

/** Computes the full activity-diagram layout for one use case. */
export function layoutActivity(uc: UseCaseView): ActivityLayout {
  const nodeIds = new Set(uc.nodes.map((n) => n.id));

  // 1. Forward adjacency over real edges only, preserving source order.
  const adjacency = new Map<string, string[]>();
  for (const n of uc.nodes) adjacency.set(n.id, []);
  const realEdges = uc.edges.filter((e) => nodeIds.has(e.from) && nodeIds.has(e.to));
  for (const e of realEdges) {
    const list = adjacency.get(e.from);
    if (list !== undefined) list.push(e.to);
  }

  // 2. Back-edge detection via DFS, marking edges that point at a node currently
  //    on the recursion stack.
  const backEdges = new Set<string>();
  const visited = new Set<string>();
  const onStack = new Set<string>();

  const dfs = (start: string): void => {
    const stack: { id: string; childIndex: number }[] = [{ id: start, childIndex: 0 }];
    visited.add(start);
    onStack.add(start);
    while (stack.length > 0) {
      const frame = stack[stack.length - 1];
      if (frame === undefined) break;
      const children = adjacency.get(frame.id) ?? [];
      if (frame.childIndex < children.length) {
        const child = children[frame.childIndex];
        frame.childIndex += 1;
        if (child === undefined) continue;
        if (onStack.has(child)) {
          backEdges.add(backEdgeKey(frame.id, child));
        } else if (!visited.has(child)) {
          visited.add(child);
          onStack.add(child);
          stack.push({ id: child, childIndex: 0 });
        }
      } else {
        onStack.delete(frame.id);
        stack.pop();
      }
    }
  };

  // Roots = in-degree 0 over all real edges (back-edges aren't known until DFS runs).
  const rawInDegree = new Map<string, number>();
  for (const n of uc.nodes) rawInDegree.set(n.id, 0);
  for (const e of realEdges) rawInDegree.set(e.to, (rawInDegree.get(e.to) ?? 0) + 1);
  const roots = uc.nodes.filter((n) => (rawInDegree.get(n.id) ?? 0) === 0).map((n) => n.id);

  if (roots.length > 0) {
    for (const r of roots) if (!visited.has(r)) dfs(r);
  } else if (uc.nodes.length > 0) {
    const first = uc.nodes[0];
    if (first !== undefined) dfs(first.id);
  }
  // Classify any remaining unvisited nodes so every edge is reachable/classified.
  for (const n of uc.nodes) if (!visited.has(n.id)) dfs(n.id);

  // 3. Rank (Y) = longest path over the DAG (edges minus back-edges) via Kahn.
  const dagEdges = realEdges.filter((e) => !backEdges.has(backEdgeKey(e.from, e.to)));
  const inDegree = new Map<string, number>();
  for (const n of uc.nodes) inDegree.set(n.id, 0);
  for (const e of dagEdges) inDegree.set(e.to, (inDegree.get(e.to) ?? 0) + 1);

  const dagAdjacency = new Map<string, string[]>();
  for (const n of uc.nodes) dagAdjacency.set(n.id, []);
  for (const e of dagEdges) {
    const list = dagAdjacency.get(e.from);
    if (list !== undefined) list.push(e.to);
  }

  const rank = new Map<string, number>();
  for (const n of uc.nodes) rank.set(n.id, 0);

  const queue: string[] = [];
  const pendingInDegree = new Map(inDegree);
  for (const n of uc.nodes) if ((pendingInDegree.get(n.id) ?? 0) === 0) queue.push(n.id);

  let head = 0;
  while (head < queue.length) {
    const u = queue[head];
    head += 1;
    if (u === undefined) continue;
    const ru = rank.get(u) ?? 0;
    for (const v of dagAdjacency.get(u) ?? []) {
      if ((rank.get(v) ?? 0) < ru + 1) rank.set(v, ru + 1);
      const remaining = (pendingInDegree.get(v) ?? 0) - 1;
      pendingInDegree.set(v, remaining);
      if (remaining === 0) queue.push(v);
    }
  }

  let maxRank = 0;
  for (const n of uc.nodes) maxRank = Math.max(maxRank, rank.get(n.id) ?? 0);

  // 4. Per-(lane, rank) grouping for X, preserving source order within a group.
  interface Group {
    lane: string;
    rank: number;
    ids: string[];
  }
  const groups: Group[] = [];
  const groupByKey = new Map<string, Group>();
  for (const n of uc.nodes) {
    const r = rank.get(n.id) ?? 0;
    const key = `${n.lane}@@${String(r)}`;
    const existing = groupByKey.get(key);
    if (existing === undefined) {
      const group: Group = { lane: n.lane, rank: r, ids: [n.id] };
      groupByKey.set(key, group);
      groups.push(group);
    } else {
      existing.ids.push(n.id);
    }
  }

  const laneSlots = new Map<string, number>();
  for (const lane of uc.lanes) laneSlots.set(lane, 1);
  for (const group of groups) {
    laneSlots.set(group.lane, Math.max(laneSlots.get(group.lane) ?? 1, group.ids.length));
  }

  const laneWidth = new Map<string, number>();
  const laneX = new Map<string, number>();
  let cumulative = 0;
  for (const lane of uc.lanes) {
    const w = (laneSlots.get(lane) ?? 1) * SLOT_WIDTH;
    laneWidth.set(lane, w);
    laneX.set(lane, cumulative);
    cumulative += w;
  }

  // 5. Place each node within its (lane, rank) group.
  const kindOf = new Map(uc.nodes.map((n) => [n.id, n.kind]));
  const positions = new Map<string, PlacedNode>();
  for (const group of groups) {
    const k = group.ids.length;
    const width = laneWidth.get(group.lane) ?? SLOT_WIDTH;
    const baseX = laneX.get(group.lane) ?? 0;
    group.ids.forEach((id, j) => {
      const kind = kindOf.get(id);
      const dim = kind !== undefined ? NODE_DIMS[kind] : { w: 0, h: 0 };
      const slotCenterX = baseX + ((j + 0.5) * width) / k;
      const x = slotCenterX - dim.w / 2;
      const y = HEADER_HEIGHT + TOP_PAD + group.rank * ROW_HEIGHT + (ROW_HEIGHT - dim.h) / 2;
      positions.set(id, { x, y });
    });
  }

  const canvasHeight = HEADER_HEIGHT + TOP_PAD + (maxRank + 1) * ROW_HEIGHT;

  return { positions, backEdges, laneX, laneWidth, canvasHeight, maxRank };
}

/** True if `from->to` was classified a back-edge (loop). */
export function isBackEdge(layout: ActivityLayout, from: string, to: string): boolean {
  return layout.backEdges.has(backEdgeKey(from, to));
}
