/**
 * Pure layout for the GRAPH view of the project plan: one tile per ACTIVITY, read
 * as a PROJECT NETWORK in Table 11-1 BUILD order — top→down is the order things
 * get built, not the architecture's own top→down layering:
 *
 *   FRONT END   1 Requirements → 2 Architecture → 3 Project Design → ◆ M0
 *   RESOURCES → RESOURCE ACCESS → ENGINES → MANAGERS → DEPLOYMENT → CLIENTS
 *   SYSTEM TESTING (N-IT, terminal)
 *
 * plus N-STP in its own side lane, clear of the widest row, running from M0
 * straight down to N-IT — Table 11-1's 4→21 chain.
 *
 * Every edge is a DEPENDENCY edge pointing DOWN in build order: predecessor →
 * dependent, arrowhead at the dependent (what a tile CALLS is what it depends
 * on, so it is built after). M0 is a FORCED dependency of all construction
 * (Righting Software ch. 11, "About Milestones"): it fans into the graph's
 * BUILD ROOTS — the tiles with no known predecessor (every Resource, any
 * dependency-free ResourceAccess, and N-STP) — and the rest inherit the gate
 * transitively through their own dependency edges ({@link planFocusFor} lights
 * the lot on an M0 hover).
 *
 * Within a row, a tile's column is its dependencies' mean x — a barycenter
 * sweep run row-by-row in BUILD order (the row above is always what the row
 * below depends on), so a tile still sits under what it needs. No import
 * beyond types, so node:test loads this directly.
 */
import { COL_W, NODE_W, ROW_H } from '../flow/flowLayoutCore.ts';

export type PlanRow =
  | 'frontEnd'
  | 'resource'
  | 'resourceAccess'
  | 'engine'
  | 'manager'
  | 'deployment'
  | 'client'
  | 'systemTesting'
  /** N-STP: no horizontal band — a vertical lane beside every row. */
  | 'sideLane';

export interface PlanTileInput {
  id: string;
  row: PlanRow;
  /** Activities whose components this one CALLS — what it depends on, so the
   *  callee is built first (Front-end and side-lane tiles carry none). */
  calls: readonly string[];
}

export type PlanEdgeKind = 'sequence' | 'milestone' | 'call';

export interface PlanEdge {
  id: string;
  from: string;
  to: string;
  kind: PlanEdgeKind;
}

export interface PlanGraphRow {
  row: PlanRow;
  y: number;
  height: number;
  /** The gutter label (house wording; `\n` breaks a two-word row name). */
  label: string;
}

export interface PlanGraphLayout {
  pos: ReadonlyMap<string, { x: number; y: number }>;
  rows: PlanGraphRow[];
  edges: PlanEdge[];
  width: number;
  height: number;
}

export const PLAN_TILE = { w: NODE_W, h: 96 } as const;
export const PLAN_MILESTONE = { w: 132, h: 44 } as const;
export const PLAN_COL_W = COL_W;
export const PLAN_ROW_H = ROW_H;
/** Clear space between the widest row and N-STP's side lane. */
const LANE_GAP = 96;

const ROW_LABEL: Record<Exclude<PlanRow, 'sideLane'>, string> = {
  frontEnd: 'Front end',
  resource: 'Resources',
  resourceAccess: 'Resource\nAccess',
  engine: 'Engines',
  manager: 'Managers',
  deployment: 'Deployment',
  client: 'Clients',
  systemTesting: 'System\nTesting',
};

/** The stack, in BUILD order top→down: what nothing depends on comes first,
 *  what depends on everything else (the clients) comes last, System Testing
 *  closes it out. */
const STACK_ROWS: readonly Exclude<PlanRow, 'frontEnd' | 'sideLane'>[] = [
  'resource',
  'resourceAccess',
  'engine',
  'manager',
  'deployment',
  'client',
  'systemTesting',
];

export function layoutPlanGraph(
  tiles: readonly PlanTileInput[],
  milestoneId: string | undefined
): PlanGraphLayout {
  const known = new Set(tiles.map((t) => t.id));
  const pos = new Map<string, { x: number; y: number }>();
  const rows: PlanGraphRow[] = [];
  const edges: PlanEdge[] = [];
  let cursor = 0;
  let right = 0;

  const dependsOn = (t: PlanTileInput): string[] =>
    t.calls.filter((c) => known.has(c) && c !== t.id);

  // FRONT END: the authored chain, then the milestone at its end.
  const front = tiles.filter((t) => t.row === 'frontEnd');
  if (front.length > 0) {
    front.forEach((t, i) => {
      pos.set(t.id, { x: i * COL_W, y: cursor });
      const next = front[i + 1];
      if (next !== undefined) {
        edges.push({ id: `${t.id}>${next.id}`, from: t.id, to: next.id, kind: 'sequence' });
      }
    });
    right = (front.length - 1) * COL_W + PLAN_TILE.w;
    const last = front[front.length - 1];
    if (milestoneId !== undefined && last !== undefined) {
      pos.set(milestoneId, {
        x: front.length * COL_W,
        y: cursor + (PLAN_TILE.h - PLAN_MILESTONE.h) / 2,
      });
      edges.push({
        id: `${last.id}>${milestoneId}`,
        from: last.id,
        to: milestoneId,
        kind: 'sequence',
      });
      right = Math.max(right, front.length * COL_W + PLAN_MILESTONE.w);
    }
    rows.push({ row: 'frontEnd', y: cursor, height: PLAN_TILE.h, label: ROW_LABEL.frontEnd });
    cursor += ROW_H;
  }

  // The build-order stack: each row's x order is the mean x of its OWN
  // dependencies, already placed one or more rows up — the house barycenter
  // sweep, run top→down in BUILD order instead of call order.
  for (const row of STACK_ROWS) {
    const members = tiles.filter((t) => t.row === row);
    if (members.length === 0) continue;
    const keyed = members.map((t, idx) => {
      const xs = dependsOn(t)
        .map((c) => pos.get(c)?.x)
        .filter((x): x is number => x !== undefined);
      const key =
        xs.length > 0 ? xs.reduce((a, b) => a + b, 0) / xs.length : Number.POSITIVE_INFINITY;
      return { t, key, idx };
    });
    keyed.sort((a, b) => (a.key === b.key ? a.idx - b.idx : a.key - b.key));
    keyed.forEach(({ t }, i) => pos.set(t.id, { x: i * COL_W, y: cursor }));
    right = Math.max(right, (members.length - 1) * COL_W + PLAN_TILE.w);
    rows.push({ row, y: cursor, height: PLAN_TILE.h, label: ROW_LABEL[row] });
    cursor += ROW_H;
  }

  const height = Math.max(0, cursor - ROW_H + PLAN_TILE.h);

  // N-STP's side lane: clear of the widest row, spanning the full height —
  // M0 straight down to N-IT, like Table 11-1's 4→21 chain.
  const lane = tiles.filter((t) => t.row === 'sideLane');
  const laneX = right + LANE_GAP;
  if (lane.length > 0) {
    lane.forEach((t, i) => {
      const step = lane.length > 1 ? (height * i) / (lane.length - 1) : height / 2;
      pos.set(t.id, { x: laneX, y: step - PLAN_TILE.h / 2 });
    });
    right = laneX + PLAN_TILE.w;
  }

  // Dependency edges: predecessor → dependent, pointing DOWN in build order.
  for (const t of tiles) {
    if (t.row === 'frontEnd') continue;
    for (const c of dependsOn(t)) {
      edges.push({ id: `${c}>${t.id}`, from: c, to: t.id, kind: 'call' });
    }
  }

  // M0 gates every BUILD ROOT — a tile with no known predecessor: every
  // Resource, any dependency-free ResourceAccess, and N-STP. Everything else
  // inherits the gate transitively through its own dependency edges above.
  if (milestoneId !== undefined && pos.has(milestoneId)) {
    for (const t of tiles) {
      if (t.row === 'frontEnd' || dependsOn(t).length > 0) continue;
      edges.push({ id: `${milestoneId}>${t.id}`, from: milestoneId, to: t.id, kind: 'milestone' });
    }
  }

  return { pos, rows, edges, width: right, height };
}

/** What a hover lights: the tiles, and which edges stay drawn. */
export interface PlanFocus {
  tiles: ReadonlySet<string>;
  incident: (e: PlanEdge) => boolean;
}

/**
 * Hovering a tile lights it, its neighbours and the edges between them; every
 * other tile dims and every other edge hides (the house static hover-focus).
 * Hovering the MILESTONE lights everything it gates — all of construction and
 * the side lane and system testing, not just the build roots its edges reach
 * — because that is what a forced dependency means.
 */
export function planFocusFor(
  hoveredId: string,
  tiles: readonly PlanTileInput[],
  edges: readonly PlanEdge[],
  milestoneId: string | undefined
): PlanFocus {
  const lit = new Set<string>([hoveredId]);
  for (const e of edges) {
    if (e.from === hoveredId) lit.add(e.to);
    if (e.to === hoveredId) lit.add(e.from);
  }
  if (hoveredId === milestoneId) {
    for (const t of tiles) {
      if (t.row !== 'frontEnd') lit.add(t.id);
    }
  }
  return { tiles: lit, incident: (e) => e.from === hoveredId || e.to === hoveredId };
}
