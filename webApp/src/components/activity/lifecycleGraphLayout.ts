/**
 * Pure grid layout for an activity's internal task graph (Righting Software
 * Appendix A, Figure A-1) — the branching counterpart of SlimSpine's single lane.
 * Zero imports on purpose: node:test loads it directly, and the component that
 * draws it (LifecycleGraph.tsx) owns every pixel decision. This module only
 * answers "which column, which lane, and which way does each rail bend".
 *
 * ── Columns ─────────────────────────────────────────────────────────────────
 * A node's column is its LONGEST-path depth from a root, so a join sits after the
 * slowest of its parents and a short branch (STP → STP Review) simply ends early
 * and rides its lane until the join (Testing).
 *
 * ── Lanes, git-style ────────────────────────────────────────────────────────
 * Nodes are swept in (column, authored order). Every edge RESERVES a lane the
 * moment its source is placed:
 *
 *   • the source's first-authored child inherits the source's own lane, which is
 *     what keeps the first-authored chain on lane 0 — the trunk;
 *   • every further child opens a branch on the lowest lane nobody holds.
 *
 * A node lands on the LOWEST lane reserved by its incoming edges and releases the
 * rest, so a branch returns to the join's lane and its own lane is free for the
 * next fork. Because a lane is held by exactly one node-or-edge at any column, no
 * rail ever runs through a pip it does not belong to — including a redundant
 * transitive edge (A→C beside A→B→C), which detours on a lane of its own.
 *
 * ── Rails ───────────────────────────────────────────────────────────────────
 * An edge is a list of grid waypoints. It leaves its source's lane in the first
 * gap, travels along its reserved lane, and enters the target's lane in the last
 * gap; consecutive waypoints on the same lane are a straight run, on different
 * lanes a bend. How a bend is rounded is the renderer's business.
 */

/** One task of the graph, in authored order. Unknown / self dependencies are ignored. */
export interface LifecycleLayoutInput {
  id: string;
  dependsOn: readonly string[];
}

/** A grid position: column = longest-path depth, lane = git-style row (0 = trunk). */
export interface LifecycleGridPoint {
  col: number;
  lane: number;
}

/** One rail, as the waypoints it passes through (first = source, last = target). */
export interface LifecycleLayoutEdge {
  from: string;
  to: string;
  points: LifecycleGridPoint[];
}

export interface LifecycleLayout {
  /** Grid position per node id. */
  positions: ReadonlyMap<string, LifecycleGridPoint>;
  edges: LifecycleLayoutEdge[];
  /** Column / lane counts (max + 1); both 0 for an empty graph. */
  cols: number;
  lanes: number;
}

function edgeKey(from: string, to: string): string {
  return `${from}\u0000${to}`;
}

/** The lowest lane nobody holds; grows the lane table when all are taken. */
function lowestFreeLane(held: boolean[]): number {
  const free = held.indexOf(false);
  if (free !== -1) return free;
  held.push(false);
  return held.length - 1;
}

export function layoutLifecycleGraph(nodes: readonly LifecycleLayoutInput[]): LifecycleLayout {
  const order = new Map<string, number>();
  nodes.forEach((n, i) => {
    if (!order.has(n.id)) order.set(n.id, i);
  });

  // Known, de-duplicated, non-self parents — the only edges the layout honours.
  const parents = new Map<string, string[]>();
  for (const n of nodes) {
    if (parents.has(n.id)) continue;
    const seen = new Set<string>();
    parents.set(
      n.id,
      n.dependsOn.filter((d) => {
        if (d === n.id || !order.has(d) || seen.has(d)) return false;
        seen.add(d);
        return true;
      })
    );
  }

  // Longest-path depth. A dependency cycle is an authoring error, not a reason to
  // hang: an edge that closes one is dropped where the walk meets it.
  const depth = new Map<string, number>();
  const visiting = new Set<string>();
  const depthOf = (id: string): number => {
    const known = depth.get(id);
    if (known !== undefined) return known;
    visiting.add(id);
    const kept: string[] = [];
    let d = 0;
    for (const p of parents.get(id) ?? []) {
      if (visiting.has(p)) continue;
      kept.push(p);
      d = Math.max(d, depthOf(p) + 1);
    }
    parents.set(id, kept);
    visiting.delete(id);
    depth.set(id, d);
    return d;
  };
  for (const id of order.keys()) depthOf(id);

  const byAuthored = (a: string, b: string): number => (order.get(a) ?? 0) - (order.get(b) ?? 0);
  const children = new Map<string, string[]>();
  for (const [id, ps] of parents) {
    for (const p of ps) children.set(p, [...(children.get(p) ?? []), id]);
  }
  for (const kids of children.values()) kids.sort(byAuthored);

  const sweep = [...order.keys()].sort(
    (a, b) => (depth.get(a) ?? 0) - (depth.get(b) ?? 0) || byAuthored(a, b)
  );

  const held: boolean[] = [];
  const reserved = new Map<string, number>();
  const positions = new Map<string, LifecycleGridPoint>();
  for (const id of sweep) {
    const incoming = (parents.get(id) ?? []).map((p) => reserved.get(edgeKey(p, id)) ?? 0);
    const lane = incoming.length > 0 ? Math.min(...incoming) : lowestFreeLane(held);
    for (const l of incoming) held[l] = false;
    held[lane] = true;
    positions.set(id, { col: depth.get(id) ?? 0, lane });

    const kids = children.get(id) ?? [];
    kids.forEach((kid, i) => {
      const kidLane = i === 0 ? lane : lowestFreeLane(held);
      held[kidLane] = true;
      reserved.set(edgeKey(id, kid), kidLane);
    });
    if (kids.length === 0) held[lane] = false;
  }

  const edges: LifecycleLayoutEdge[] = [];
  for (const id of order.keys()) {
    const to = positions.get(id);
    if (to === undefined) continue;
    for (const p of parents.get(id) ?? []) {
      const from = positions.get(p);
      if (from === undefined) continue;
      const travel = reserved.get(edgeKey(p, id)) ?? from.lane;
      const points: LifecycleGridPoint[] = [from];
      if (to.col - from.col > 1) {
        if (travel !== from.lane) points.push({ col: from.col + 1, lane: travel });
        if (travel !== to.lane && to.col - 1 > from.col + (travel !== from.lane ? 1 : 0)) {
          points.push({ col: to.col - 1, lane: travel });
        }
      }
      points.push(to);
      edges.push({ from: p, to: id, points });
    }
  }

  let cols = 0;
  let lanes = 0;
  for (const p of positions.values()) {
    cols = Math.max(cols, p.col + 1);
    lanes = Math.max(lanes, p.lane + 1);
  }
  return { positions, edges, cols, lanes };
}
