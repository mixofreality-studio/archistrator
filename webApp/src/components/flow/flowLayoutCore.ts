/**
 * The layered layout ENGINE, dependency-free: the row vocabulary, the base
 * geometry and `computeLayout`'s barycenter row ordering.
 *
 * Extracted from flowLayout.ts (which re-exports everything here, so no import
 * site changed) so that a pure consumer can run it under Node's test runner:
 * flowLayout.ts value-imports @xyflow/react and uses extensionless relative
 * imports, neither of which that runner loads. This module imports types only
 * — erased before Node ever sees them — which is what lets the construction
 * GRAPH lens reuse the house ordering algorithm and pin its own layout with a
 * golden test (construction/graph/activityGraphLayout.test.ts) instead of
 * re-implementing the sweep.
 */
import type { Layer } from '../../contracts/types';

/**
 * A lane in the layered layout: a Method layer, or the synthetic `person` lane.
 * People (use-case actors) are NOT System components — they hold no volatility
 * and sit outside the architecture — but they DO participate in a realized call
 * chain, so the dynamic lens places them in their own row above the Clients.
 */
export type FlowLayer = Layer | 'person';

/** The lanes that occupy horizontal rows (top→down). Utility is excluded — it is
 *  rendered as a vertical bar on the right that spans all rows (Righting Software
 *  Fig 3-4). A row with no members is skipped, so the person lane costs nothing
 *  in the views (static / perspective) that place no people. */
export const LAYER_ROWS: readonly FlowLayer[] = [
  'person',
  'client',
  'manager',
  'engine',
  'resourceAccess',
  'resource',
];

// --- geometry -------------------------------------------------------------
export const COL_W = 220;
export const ROW_H = 150;
export const NODE_W = 188;
/** Gap between the right-most row node and the Utilities bar. */
const BAR_GAP = 96;

// --- layered layout engine ------------------------------------------------

/** Minimal shape the layout needs from a placed participant (component or person). */
export interface LayoutComponent {
  id: string;
  layer: FlowLayer;
}
/** Minimal shape the layout needs from a relationship (direction: from → to). */
export interface LayoutEdge {
  from: string;
  to: string;
}

export interface Layout {
  /** Absolute position per component id. */
  pos: Map<string, { x: number; y: number }>;
  /** The present non-utility rows, in top→down order, with their y. */
  rows: { layer: FlowLayer; y: number }[];
  /** Utility component ids, stacked top→down in the side bar. */
  utilityIds: string[];
  /** X of the Utilities bar column. */
  barX: number;
  barTop: number;
  barBottom: number;
}

/**
 * Places components into fixed horizontal layer rows (top→down) and the Utilities
 * into a right-hand vertical bar. Within each row, nodes are ordered by the mean x
 * of their already-placed callers (a single top-down barycenter sweep) so a node
 * tends to sit under whatever calls it — which is what kills the edge crossings.
 */
export function computeLayout(components: LayoutComponent[], relationships: LayoutEdge[]): Layout {
  const pos = new Map<string, { x: number; y: number }>();
  const utility = components.filter((c) => c.layer === 'utility');
  const rowLayers = LAYER_ROWS.filter((l) => components.some((c) => c.layer === l));

  // who calls each node (its upstream sources)
  const sourcesOf = new Map<string, string[]>();
  for (const r of relationships) {
    const arr = sourcesOf.get(r.to);
    if (arr === undefined) sourcesOf.set(r.to, [r.from]);
    else arr.push(r.from);
  }

  let colsMax = 1;
  rowLayers.forEach((layer, ri) => {
    const y = ri * ROW_H;
    let row = components.filter((c) => c.layer === layer);
    if (ri > 0) {
      const keyed = row.map((c, idx) => {
        const xs = (sourcesOf.get(c.id) ?? [])
          .map((s) => pos.get(s)?.x)
          .filter((x): x is number => x !== undefined);
        const key =
          xs.length > 0 ? xs.reduce((a, b) => a + b, 0) / xs.length : Number.POSITIVE_INFINITY;
        return { c, key, idx };
      });
      keyed.sort((a, b) => (a.key === b.key ? a.idx - b.idx : a.key - b.key));
      row = keyed.map((k) => k.c);
    }
    row.forEach((c, i) => pos.set(c.id, { x: i * COL_W, y }));
    colsMax = Math.max(colsMax, row.length);
  });

  const rows = rowLayers.map((layer, ri) => ({ layer, y: ri * ROW_H }));
  const barTop = 0;
  const barBottom = Math.max((rowLayers.length - 1) * ROW_H, 0);
  const barX = (colsMax - 1) * COL_W + NODE_W + BAR_GAP;
  const span = barBottom - barTop;
  utility.forEach((u, i) => {
    const y = utility.length > 1 ? barTop + (span * i) / (utility.length - 1) : barTop;
    pos.set(u.id, { x: barX, y });
  });

  return { pos, rows, utilityIds: utility.map((u) => u.id), barX, barTop, barBottom };
}

/** Row-gutter label text: the Method component-layer name for each row (matches
 *  the legend). Utilities are labeled by their own side-bar frame, not a gutter row. */
export function rowLabelText(layer: FlowLayer): string | null {
  switch (layer) {
    case 'person':
      return 'People';
    case 'client':
      return 'Clients';
    case 'manager':
      return 'Managers';
    case 'engine':
      return 'Engines';
    case 'resourceAccess':
      return 'Resource\nAccess';
    case 'resource':
      return 'Resources';
    case 'utility':
      return null; // utility → side bar frame
  }
}
