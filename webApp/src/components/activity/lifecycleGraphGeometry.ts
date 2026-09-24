/**
 * Pure pixel geometry for the activity lifecycle graph: turns the grid layout
 * (lifecycleGraphLayout.ts) into column x-centres, lane y-centres, directed rail
 * paths, return arcs, phase-label and lane-label boxes. Types-only import, so
 * node:test loads it directly; the component (LifecycleGraph.tsx) only paints
 * what this returns.
 *
 * ── Columns are elastic ─────────────────────────────────────────────────────
 * A column is a pip plus the gap to the next one, EXCEPT:
 *
 *   • the ACTIVE column is as wide as the active pill, whose title is inline — so
 *     the pill never collides with its right-hand neighbour, on any lane;
 *   • a phase whose eyebrow label is wider than its pips stretches the gaps
 *     BETWEEN its columns (a one-column phase stretches that column), so two
 *     adjacent labels never overlap;
 *   • a gap a rail BENDS in widens until the bend has its full run and still
 *     finishes ahead of the arrowhead (and of any lane-label zone).
 *
 * ── Rails are directed ──────────────────────────────────────────────────────
 * Every connection points FORWARD: it stops short of its target and ends in a
 * small arrowhead that never touches the pip (or the pill). A run between two
 * waypoints on one lane is a straight line; a lane change is a cubic S-bend,
 * horizontal at both ends, confined to the last `bend` px it is allowed (longer
 * when it crosses several lanes) — so a stretched gap reads as "the trunk carries
 * on, the branch peels off", like a git graph.
 *
 * ── Return arcs ─────────────────────────────────────────────────────────────
 * The only BACKWARD edge: a review that has sent its work back at least once gets
 * a slim arc from its pip back over the rail to the dispatch task it judges, the
 * arrowhead pointing down at that task and the `↻N` revision count riding the
 * arc's crown. The caller decides which pairs have one (backEdgesOf in
 * lifecycleGraphTypes.ts); no send-back, no arc. An arc lives strictly between
 * its own pips' tops and the lane above — lane 0's gets a band of its own under
 * the phase labels — so it can touch neither a rail nor a label.
 *
 * ── Lane labels ─────────────────────────────────────────────────────────────
 * A branch is named ON its own rail, in a zone reserved just ahead of the
 * branch's first pip and its arrowhead: every bend into the column finishes
 * BEFORE the zone, so the labels of one fork line up as a left-aligned legend
 * over straight, horizontal rail. A label whose phase is already named beneath
 * the rails is dropped: that phase label is the branch's name.
 *
 * ── Phase labels ────────────────────────────────────────────────────────────
 * A mono eyebrow over the phase's FIRST column — text only. (It used to be a
 * bracket spanning the phase; the bar read as a relationship between the tasks
 * under it, which the return arc now says properly.) A phase that owns a trunk
 * (lane 0) node is labelled ABOVE the rails; one that lives wholly on a branch
 * (Figure A-1's Test Plan) BELOW them. Labels that would still overlap on one
 * side stack into further rows.
 */
import type { LifecycleLayout } from './lifecycleGraphLayout.ts';

export interface LifecycleMetrics {
  /** Pip diameter. */
  pip: number;
  /** Rail length between two adjacent pips. */
  gap: number;
  laneHeight: number;
  /** Horizontal run of an S-bend between two ADJACENT lanes. */
  bend: number;
  /** Extra run per further lane a bend crosses. */
  bendPerLane: number;
  /** Height of one phase-label row. */
  labelRow: number;
  /** Clear space between two phase labels on one row. */
  labelGap: number;
  /** Rail left showing either side of a lane label. */
  labelPad: number;
  labelHeight: number;
  /** Arrowhead length, half-width, and the clear space between its tip and the pip. */
  arrowLen: number;
  arrowHalf: number;
  arrowGap: number;
  /** A return arc: how far above the pip tops it starts / lands, and how hard it bows. */
  arcStart: number;
  arcLand: number;
  arcBow: number;
  /** The `↻N` badge riding a return arc. */
  arcBadge: { w: number; h: number };
}

export const LIFECYCLE_METRICS: LifecycleMetrics = {
  pip: 24,
  gap: 20,
  laneHeight: 44,
  bend: 40,
  bendPerLane: 16,
  labelRow: 14,
  labelGap: 14,
  labelPad: 6,
  labelHeight: 11,
  arrowLen: 5,
  arrowHalf: 3,
  arrowGap: 3,
  arcStart: 2,
  arcLand: 6,
  arcBow: 10,
  arcBadge: { w: 24, h: 10 },
};

export interface GeometryNode {
  id: string;
  phase: string;
  /** Rendered width of this node's lane label, estimated by the caller; 0 / absent ⇒ none. */
  laneLabelWidth?: number | undefined;
}

export interface GeometryPhase {
  id: string;
  /** Rendered width of the eyebrow label, estimated by the caller (mono font). */
  labelWidth: number;
}

/** The active pill: how far it reaches either side of its pip's CENTRE. */
export interface GeometryActive {
  nodeId: string;
  left: number;
  right: number;
}

/** A review → dispatch return edge the caller wants drawn. */
export interface GeometryBackEdge {
  from: string;
  to: string;
}

/** A phase label's box: over (or under) the phase's first column. */
export interface PhaseBox {
  id: string;
  x: number;
  width: number;
  side: 'top' | 'bottom';
  row: number;
}

/** A box in the rails' coordinate space (y from the first lane's top; may be negative). */
export interface RailBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface LaneLabelBox extends RailBox {
  nodeId: string;
}

/** An arrowhead: its tip, and the way it points. */
export interface ArrowHead {
  x: number;
  y: number;
  dir: 'right' | 'down';
}

export interface RailPath {
  from: string;
  to: string;
  d: string;
  arrow: ArrowHead;
}

export interface BackArc {
  from: string;
  to: string;
  d: string;
  arrow: ArrowHead;
  /** Centre of the `↻N` badge, on the arc's crown. */
  badge: { x: number; y: number };
  /** Everything the arc draws, badge included. */
  box: RailBox;
}

export interface LifecycleGeometry {
  width: number;
  height: number;
  /** y of the first lane's top edge (below the top label rows and lane 0's arc band). */
  railsTop: number;
  railsHeight: number;
  /** Pip-centre x per node id / y per lane. */
  x: ReadonlyMap<string, number>;
  laneY: (lane: number) => number;
  rails: RailPath[];
  backArcs: BackArc[];
  phases: PhaseBox[];
  laneLabels: LaneLabelBox[];
  topRows: number;
  bottomRows: number;
}

/** First row (from 0) on which [x, x+width] clears everything already placed. */
function firstClearRow(
  placed: PhaseBox[],
  side: PhaseBox['side'],
  x: number,
  width: number,
  gap: number
): number {
  for (let row = 0; ; row += 1) {
    const clash = placed.some(
      (b) => b.side === side && b.row === row && x < b.x + b.width + gap && b.x < x + width + gap
    );
    if (!clash) return row;
  }
}

export function lifecycleGeometry(
  layout: LifecycleLayout,
  nodes: readonly GeometryNode[],
  phases: readonly GeometryPhase[],
  active: GeometryActive | undefined,
  m: LifecycleMetrics = LIFECYCLE_METRICS,
  backEdges: readonly GeometryBackEdge[] = []
): LifecycleGeometry {
  const r = m.pip / 2;
  const activeCol = active === undefined ? undefined : layout.positions.get(active.nodeId)?.col;
  const padLeft = active !== undefined ? Math.max(0, active.left - r) : 0;
  const padRight = active !== undefined ? Math.max(0, active.right - r) : 0;
  /** What a rail must stay clear of ahead of its target: gap, arrowhead, a hair of straight. */
  const arrowReserve = m.arrowGap + m.arrowLen + 2;

  // Column cell = [lead-in][pip][lead-out + gap]. Only the active column has leads.
  const lead = (col: number): number => (col === activeCol ? padLeft : 0);
  const base = (col: number): number => (col === activeCol ? padRight : 0);
  const tail = Array.from({ length: layout.cols }, (_, col) => base(col));

  // Phase spans, in the caller's phase order (which is also label priority).
  const spans = phases.flatMap((p) => {
    const cols: number[] = [];
    let trunk = false;
    let ownsActive = false;
    for (const n of nodes) {
      const pos = n.phase === p.id ? layout.positions.get(n.id) : undefined;
      if (pos === undefined) continue;
      cols.push(pos.col);
      trunk = trunk || pos.lane === 0;
      ownsActive = ownsActive || n.id === active?.nodeId;
    }
    if (cols.length === 0) return [];
    const last = Math.max(...cols);
    // The pill only lengthens the span of the phase it belongs to — a branch phase
    // that merely shares the active COLUMN still ends at its own pip.
    const reach = ownsActive && last === activeCol ? padRight : 0;
    return [{ phase: p, first: Math.min(...cols), last, trunk, reach }];
  });

  // Lane-label zones: per column, the widest label of a branch starting there. A
  // label is dropped when a phase label beneath the rails already names its
  // branch, and in column 0, where there is no rail ahead of the pip to write on.
  const namedBelow = new Set(spans.filter((sp) => !sp.trunk).map((sp) => sp.phase.id));
  const labelled = nodes.filter((n) => {
    const col = layout.positions.get(n.id)?.col ?? 0;
    return (n.laneLabelWidth ?? 0) > 0 && col > 0 && !namedBelow.has(n.phase);
  });
  const zone = Array.from({ length: layout.cols }, () => 0);
  for (const n of labelled) {
    const col = layout.positions.get(n.id)?.col ?? 0;
    zone[col] = Math.max(zone[col] ?? 0, (n.laneLabelWidth ?? 0) + 2 * m.labelPad);
  }
  // A labelled column keeps its zone even where nothing bends into it.
  zone.forEach((z, col) => {
    if (z > 0)
      tail[col - 1] = Math.max(tail[col - 1] ?? 0, base(col - 1) + z + arrowReserve - m.gap);
  });

  // A bend needs its full run — longer across several lanes, or it turns into a
  // cliff — and must finish ahead of the arrowhead and of any label zone: widen
  // the gap it happens in until it has both.
  const bendRun = (lanes: number): number => m.bend + Math.max(0, lanes - 1) * m.bendPerLane;
  for (const e of layout.edges) {
    e.points.forEach((p, i) => {
      const prev = e.points[i - 1];
      if (prev === undefined || prev.lane === p.lane) return;
      const col = p.col - 1;
      const last = i === e.points.length - 1;
      const ahead = (zone[p.col] ?? 0) + (last || (zone[p.col] ?? 0) > 0 ? arrowReserve : 0);
      const need = bendRun(Math.abs(p.lane - prev.lane)) - r - m.gap + ahead;
      tail[col] = Math.max(tail[col] ?? 0, base(col) + Math.max(0, need));
    });
  }

  // Stretch a phase that is narrower than its label. Natural width runs from the
  // first column's cell start to the last column's pip (or pill) right edge.
  for (const s of spans) {
    let natural = 0;
    for (let col = s.first; col <= s.last; col += 1) {
      natural += lead(col) + m.pip + (col < s.last ? (tail[col] ?? 0) + m.gap : s.reach);
    }
    const deficit = s.phase.labelWidth - natural;
    if (deficit <= 0) continue;
    const from = s.first;
    const to = s.last > s.first ? s.last - 1 : s.last;
    const share = deficit / (to - from + 1);
    for (let col = from; col <= to; col += 1) tail[col] = (tail[col] ?? 0) + share;
  }

  const colStart: number[] = [];
  const colCentre: number[] = [];
  let cursor = 0;
  for (let col = 0; col < layout.cols; col += 1) {
    colStart.push(cursor);
    colCentre.push(cursor + lead(col) + r);
    cursor += lead(col) + m.pip + (tail[col] ?? 0) + m.gap;
  }
  const width = Math.max(0, cursor - m.gap);

  const placed: PhaseBox[] = [];
  for (const s of spans) {
    const x = colStart[s.first] ?? 0;
    const side = s.trunk ? 'top' : 'bottom';
    const w = s.phase.labelWidth;
    placed.push({
      id: s.phase.id,
      x,
      width: w,
      side,
      row: firstClearRow(placed, side, x, w, m.labelGap),
    });
  }
  const rowsOn = (side: PhaseBox['side']): number =>
    placed.reduce((n, b) => (b.side === side ? Math.max(n, b.row + 1) : n), 0);
  const topRows = rowsOn('top');
  const bottomRows = rowsOn('bottom');

  const laneY = (lane: number): number => lane * m.laneHeight + m.laneHeight / 2;
  const xOf = (id: string): number => colCentre[layout.positions.get(id)?.col ?? 0] ?? 0;

  // ── forward rails ─────────────────────────────────────────────────────────
  const rails: RailPath[] = layout.edges.map((e) => {
    let d = '';
    let arrow: ArrowHead = { x: 0, y: 0, dir: 'right' };
    e.points.forEach((p, i) => {
      const px = colCentre[p.col] ?? 0;
      const py = laneY(p.lane);
      const prev = e.points[i - 1];
      if (prev === undefined) {
        d = `M${fmt(px)} ${fmt(py)}`;
        return;
      }
      const last = i === e.points.length - 1;
      // The target's left edge — the pill's, when the target is the active task.
      const leftReach = last && e.to === active?.nodeId ? active.left : r;
      const tip = px - leftReach - m.arrowGap;
      const stop = last ? tip - m.arrowLen : px;
      if (last) arrow = { x: tip, y: py, dir: 'right' };
      if (prev.lane !== p.lane) {
        const z = zone[p.col] ?? 0;
        const bendEnd = z > 0 ? (colStart[p.col] ?? 0) - arrowReserve - z : last ? stop - 2 : px;
        const run = bendRun(Math.abs(p.lane - prev.lane));
        const bendStart = Math.max(colCentre[prev.col] ?? 0, bendEnd - run);
        const mid = (bendStart + bendEnd) / 2;
        const prevY = laneY(prev.lane);
        d += ` L${fmt(bendStart)} ${fmt(prevY)} C${fmt(mid)} ${fmt(prevY)} ${fmt(mid)} ${fmt(py)} ${fmt(bendEnd)} ${fmt(py)}`;
      }
      d += ` L${fmt(stop)} ${fmt(py)}`;
    });
    return { from: e.from, to: e.to, d, arrow };
  });

  // ── return arcs ───────────────────────────────────────────────────────────
  const backArcs: BackArc[] = backEdges.flatMap((b) => {
    const from = layout.positions.get(b.from);
    const to = layout.positions.get(b.to);
    if (from === undefined || to === undefined) return [];
    const [sx, ex] = [xOf(b.from), xOf(b.to)];
    const sy = laneY(from.lane) - r - m.arcStart;
    const tipY = laneY(to.lane) - r - m.arcLand;
    const ey = tipY - m.arrowLen;
    const [c1y, c2y] = [sy - m.arcBow, ey - m.arcBow];
    // The crown: the cubic at t = ½.
    const badge = { x: (sx + ex) / 2, y: (sy + 3 * c1y + 3 * c2y + ey) / 8 };
    // The curve's own crest (its control points overshoot it), sampled — x is
    // linear in the two x's only at the ends, but y is all the box needs.
    let crest = Math.min(sy, ey);
    for (let i = 1; i < 16; i += 1) {
      const u = i / 16;
      const v = 1 - u;
      crest = Math.min(
        crest,
        v * v * v * sy + 3 * v * v * u * c1y + 3 * v * u * u * c2y + u * u * u * ey
      );
    }
    const top = Math.min(badge.y - m.arcBadge.h / 2, crest);
    const left = Math.min(sx, ex, badge.x - m.arcBadge.w / 2);
    const right = Math.max(sx, ex, badge.x + m.arcBadge.w / 2);
    return [
      {
        from: b.from,
        to: b.to,
        d: `M${fmt(sx)} ${fmt(sy)} C${fmt(sx)} ${fmt(c1y)} ${fmt(ex)} ${fmt(c2y)} ${fmt(ex)} ${fmt(ey)}`,
        arrow: { x: ex, y: tipY, dir: 'down' },
        badge,
        box: { x: left, y: top, width: right - left, height: Math.max(sy, tipY) - top },
      },
    ];
  });
  // Lane 0 has no lane above to borrow room from: its arcs get a band of their own.
  const arcBand = Math.max(0, ...backArcs.map((a) => -a.box.y));

  const railsTop = topRows * m.labelRow + arcBand;
  const railsHeight = layout.lanes * m.laneHeight;

  const laneLabels: LaneLabelBox[] = labelled.flatMap((n) => {
    const pos = layout.positions.get(n.id);
    if (pos === undefined) return [];
    return [
      {
        nodeId: n.id,
        x: (colStart[pos.col] ?? 0) - arrowReserve - (zone[pos.col] ?? 0) + m.labelPad,
        y: laneY(pos.lane) - m.labelHeight / 2,
        width: n.laneLabelWidth ?? 0,
        height: m.labelHeight,
      },
    ];
  });

  const x = new Map<string, number>();
  for (const id of layout.positions.keys()) x.set(id, xOf(id));

  return {
    width,
    height: railsTop + railsHeight + bottomRows * m.labelRow,
    railsTop,
    railsHeight,
    x,
    laneY,
    rails,
    backArcs,
    phases: placed,
    laneLabels,
    topRows,
    bottomRows,
  };
}

function fmt(n: number): string {
  return String(Math.round(n * 10) / 10);
}
