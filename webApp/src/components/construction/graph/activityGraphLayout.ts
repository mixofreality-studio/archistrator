/**
 * The GRAPH lens's layout — deterministic by construction.
 *
 * THE ONE PROPERTY
 * ----------------
 * The same state gives the same positions, and "state" means the ARCHITECTURE
 * (which components, in the committed `system` slot's component order, and
 * which calls) and the ACTIVITY SET (which card carries how many lanes).
 *
 * The component ARRAY ORDER is a layout input, on purpose: component cards enter
 * the barycenter sweep in that order and its ties break by it, and the Utilities
 * bar stacks in it. It is committed data, stable across reads, so it is state —
 * but reordering the system slot's components can reorder a row. (The viewport
 * signature sorts ids, so such a reorder keeps the remembered viewport.)
 *
 * Nothing else is an input: not a status, not an attempt, not the order
 * ACTIVITY rows arrived in from the wire (the model sorts them by id), not a
 * schedule figure (float, effort, criticality — lane channels, never geometry),
 * not a toolbar filter. The console polls
 * every 1.5s while the pump cascades; a card that moved when an activity
 * completed would throw the operator's eye off on every tick. Pinned by
 * activityGraphLayout.test.ts, including a golden layout with every coordinate
 * written out.
 *
 * WHAT IS REUSED, AND WHAT IS NOT
 * -------------------------------
 * The within-row ORDER is the house layout's own barycenter sweep
 * (flowLayoutCore.computeLayout — the same one ArchitectureFlow uses, founder
 * convention: layered top-down, each node under what calls it). The GEOMETRY is
 * this module's own, because a card here grows with its lane count while the
 * C4 node is fixed-size: a row is as tall as its tallest card, and rows stack
 * with a fixed gap. The row labels are the house gutter's (rowLabelText), plus
 * the System-wide band this lens adds under the Resources.
 *
 * The utilities go in a side bar to the right of the widest row, stacked in
 * architecture order. No edge is ever drawn to them (activityGraphModel drops
 * those edges); the bar just exists.
 *
 * Pure — no React — so it runs under node:test. Its only value import is the
 * dependency-free flowLayoutCore.
 */
import {
  COL_W as CORE_COL_W,
  computeLayout,
  rowLabelText,
  type FlowLayer,
} from '../../flow/flowLayoutCore.ts';
import {
  LAYERED_ROWS,
  type ActivityGraphModel,
  type GraphActivityLike,
  type GraphCard,
  type GraphRow,
} from './activityGraphModel.ts';

// ---------------------------------------------------------------------------
// Geometry — exported so the renderer draws exactly the boxes laid out here.
// ---------------------------------------------------------------------------

export const CARD_W = 196;
/** Horizontal gap between two cards in a row. */
export const COL_GAP = 24;
/** One column: a card plus its gap. */
export const COL = CARD_W + COL_GAP;
/** The card's head: the title on its own line (never cut by the stamp), then
 *  the provenance stamp / surface subtitle on a second. Fixed, never
 *  provenance-dependent — a height that followed provenance would move cards
 *  when "Observed only" is toggled. */
export const CARD_HEAD_H = 44;
/** One activity lane: its id line and its lifecycle spine. */
export const LANE_H = 34;
/** Padding under the last lane. */
export const CARD_PAD = 8;
/** Vertical gap between two rows — the corridor edges route through. */
export const ROW_GAP = 72;
/** Gap between the widest row and the Utilities bar. */
const BAR_GAP = 96;
/** Head-room above the first utility for the bar's "Utilities" title. */
export const UTIL_HEAD = 34;
/** Gap between two stacked utilities. */
const UTIL_GAP = 16;
/** Padding under the last utility, inside the bar's frame. */
export const UTIL_PAD = 16;

/** A card is as tall as its lanes; a hollow card keeps one lane's room for "no activity". */
export function cardHeight(laneCount: number): number {
  return CARD_HEAD_H + Math.max(1, laneCount) * LANE_H + CARD_PAD;
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

export interface GraphLayoutRow {
  row: GraphRow;
  y: number;
  height: number;
  /** The gutter label (house wording; `\n` breaks "Resource Access"). */
  label: string;
}

export interface GraphLayout {
  pos: ReadonlyMap<string, { x: number; y: number }>;
  size: ReadonlyMap<string, { w: number; h: number }>;
  /** The present rows, top→down: the Method layers, then System-wide. */
  rows: GraphLayoutRow[];
  /** The Utilities bar, when the architecture has any utility. */
  bar?: { x: number; top: number; bottom: number };
  width: number;
  height: number;
}

const SYSTEM_WIDE_LABEL = 'System-wide';

// ---------------------------------------------------------------------------
// The layout
// ---------------------------------------------------------------------------

export function layoutActivityGraph<A extends GraphActivityLike>(
  model: ActivityGraphModel<A>
): GraphLayout {
  const pos = new Map<string, { x: number; y: number }>();
  const size = new Map<string, { w: number; h: number }>();
  const layeredRowSet = new Set<GraphRow>(LAYERED_ROWS);

  // The house barycenter sweep decides each layered row's ORDER. Cards go in
  // in model order (architecture order, then surfaces by id) — itself a
  // function of state alone — and the sweep's tie-break is that input order.
  const layered = model.cards.filter((c) => layeredRowSet.has(c.row));
  const order = computeLayout(
    layered.map((c) => ({ id: c.id, layer: c.row as FlowLayer })),
    model.edges.map((e) => ({ from: e.from, to: e.to }))
  );
  const columnOf = (id: string): number => (order.pos.get(id)?.x ?? 0) / CORE_COL_W;

  const rowCards = (row: GraphRow): GraphCard<A>[] => {
    const cards = model.cards.filter((c) => c.row === row);
    return row === 'systemWide'
      ? cards
      : [...cards].sort((a, b) => columnOf(a.id) - columnOf(b.id));
  };

  const rows: GraphLayoutRow[] = [];
  let cursor = 0;
  let widestRight = 0;
  for (const row of [...LAYERED_ROWS, 'systemWide' as const]) {
    const cards = rowCards(row);
    if (cards.length === 0) continue;
    const height = Math.max(...cards.map((c) => cardHeight(c.lanes.length)));
    cards.forEach((c, i) => {
      pos.set(c.id, { x: i * COL, y: cursor });
      size.set(c.id, { w: CARD_W, h: cardHeight(c.lanes.length) });
    });
    widestRight = Math.max(widestRight, (cards.length - 1) * COL + CARD_W);
    rows.push({
      row,
      y: cursor,
      height,
      label: row === 'systemWide' ? SYSTEM_WIDE_LABEL : (rowLabelText(row) ?? row),
    });
    cursor += height + ROW_GAP;
  }
  const lastRow = rows.at(-1);
  const rowsBottom = lastRow === undefined ? 0 : lastRow.y + lastRow.height;

  // The Utilities bar: right of the widest row, utilities stacked top→down.
  const utilities = model.cards.filter((c) => c.row === 'utility');
  let bar: GraphLayout['bar'];
  if (utilities.length > 0) {
    const x = widestRight + BAR_GAP;
    const top = 0;
    let y = top + UTIL_HEAD;
    let lastBottom = y;
    for (const u of utilities) {
      const h = cardHeight(u.lanes.length);
      pos.set(u.id, { x, y });
      size.set(u.id, { w: CARD_W, h });
      lastBottom = y + h;
      y = lastBottom + UTIL_GAP;
    }
    bar = { x, top, bottom: lastBottom + UTIL_PAD };
  }

  return {
    pos,
    size,
    rows,
    ...(bar !== undefined ? { bar } : {}),
    width: bar !== undefined ? bar.x + CARD_W : widestRight,
    height: Math.max(rowsBottom, bar?.bottom ?? 0),
  };
}
