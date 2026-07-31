/**
 * Pure (non-component) layout primitives for the architecture flow family: the
 * layer vocabulary, colour map, the shared top-down *layered* layout engine
 * (`computeLayout` + `decorativeNodes`), and the C4-node / edge factories. Kept
 * JSX-free so it can be shared without tripping the react-refresh "only export
 * components" rule (the JSX chrome + node-type registry live in ./flowShared and
 * the decorative node components in ./flowDecor).
 */
import { MarkerType, type Edge, type Node } from '@xyflow/react';
import type { Finding, Layer, Severity } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import type { C4Component } from '../../contracts/adapters';
import type { Anchor } from '../comments/CommentContext';
import { componentLacksVolatility } from './architectureCues';
import { maxSeverity } from './findingOverlays';

export type { Layer };

/**
 * A lane in the layered layout: a Method layer, or the synthetic `person` lane.
 * People (use-case actors) are NOT System components — they hold no volatility
 * and sit outside the architecture — but they DO participate in a realized call
 * chain, so the dynamic lens places them in their own row above the Clients.
 */
export type FlowLayer = Layer | 'person';

/** Full Method layer stack, top-to-bottom (utility is drawn as a side bar, not a row). */
export const LAYER_ORDER: readonly Layer[] = [
  'client',
  'manager',
  'engine',
  'resourceAccess',
  'resource',
  'utility',
];

/** Every lane the layout can place, in render order: the people who drive the
 *  system sit above the Method stack. Drives the visual reading (tab) order. */
export const FLOW_LAYER_ORDER: readonly FlowLayer[] = ['person', ...LAYER_ORDER];

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

export const LAYER_LABEL: Record<Layer, string> = {
  client: 'Clients',
  manager: 'Managers',
  engine: 'Engines',
  resourceAccess: 'ResourceAccess',
  resource: 'Resources',
  utility: 'Utility',
};

export function layerColors(t: Tokens): Record<FlowLayer, string> {
  return {
    // People are not a Method layer, so the person lane deliberately takes the
    // neutral ink tone rather than a sixth accent: it must never read as one of
    // the five layer colours (and t.muted is already Resource / Utility).
    person: t.ink,
    client: t.accent,
    manager: t.accent2,
    engine: t.committedDot,
    resourceAccess: t.awaitingFg,
    resource: t.muted,
    utility: t.muted,
  };
}

/** Theme colour for a Design-Health finding severity (edge strokes + badges). */
export function severityColor(t: Tokens, severity: Severity): string {
  switch (severity) {
    case 'error':
      return t.dangerFg;
    case 'warning':
      return t.awaitingFg;
    case 'info':
      return t.muted;
  }
}

// --- geometry -------------------------------------------------------------
export const COL_W = 220;
export const ROW_H = 150;
export const NODE_W = 188;
export const NODE_H = 74;
/** Left gutter reserved for the per-row labels (Client / Business Logic / …). */
export const GUTTER_W = 150;
/** Gap between the right-most row node and the Utilities bar. */
const BAR_GAP = 96;
/** Head-room above the first Utility node for the "Utilities" title. */
const UTIL_HEAD = 34;
/** Padding of the Utilities frame around its nodes. */
const UTIL_PAD = 16;

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
function rowLabelText(layer: FlowLayer): string | null {
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

/**
 * The non-interactive decoration for a layered layout: the left row-label gutter
 * (one Method layer name per row: Clients / Managers / Engines / Resource Access /
 * Resources) and the Utilities frame. Rendered by the `rowLabel` / `utilityFrame`
 * node types.
 */
export function decorativeNodes(layout: Layout): Node[] {
  const nodes: Node[] = [];
  const decor = (id: string, y: number, text: string): Node => ({
    id: `__row-${id}`,
    type: 'rowLabel',
    position: { x: -GUTTER_W, y },
    data: { text },
    draggable: false,
    selectable: false,
    focusable: false,
  });

  for (const r of layout.rows) {
    const text = rowLabelText(r.layer);
    if (text !== null) nodes.push(decor(r.layer, r.y, text));
  }

  if (layout.utilityIds.length > 0) {
    nodes.push({
      id: '__utility-frame',
      type: 'utilityFrame',
      position: { x: layout.barX - UTIL_PAD, y: layout.barTop - UTIL_HEAD },
      data: {
        width: NODE_W + UTIL_PAD * 2,
        height: layout.barBottom - layout.barTop + NODE_H + UTIL_HEAD + UTIL_PAD,
      },
      draggable: false,
      selectable: false,
      focusable: false,
      zIndex: -1,
    });
  }
  return nodes;
}

/**
 * Returns a COPY of `components` ordered by the computed layout's visual reading
 * order — lane row top→down (People first, Utilities side bar last), then
 * left→right within a row (top→down within the bar). React-Flow renders nodes in array order, so the
 * DOM/tab order of the focusable C4 nodes follows the visual top-down layout
 * instead of the model's drafted order (F-QA2-51). Ids/keys are untouched — only
 * the emission order changes, so React-Flow keys stay stable.
 */
export function sortByLayoutPosition<T extends LayoutComponent>(
  components: readonly T[],
  layout: Layout
): T[] {
  return [...components].sort((a, b) => {
    const rowDelta = FLOW_LAYER_ORDER.indexOf(a.layer) - FLOW_LAYER_ORDER.indexOf(b.layer);
    if (rowDelta !== 0) return rowDelta;
    const pa = layout.pos.get(a.id) ?? { x: 0, y: 0 };
    const pb = layout.pos.get(b.id) ?? { x: 0, y: 0 };
    return pa.y - pb.y || pa.x - pb.x;
  });
}

// --- node / edge factories ------------------------------------------------

/** Builds a `c4`-type React-Flow node for one component at an explicit position.
 *  `showEncapsulates` (default true) governs whether the node body renders the
 *  clamped volatility preview: on for the Static / Component-focus lenses, off for
 *  the Dynamic step-through (where the caption rail already carries the detail), per
 *  the house diagram convention — names + layer tags on nodes, prose off the canvas. */
export function c4Node(
  c: C4Component,
  position: { x: number; y: number },
  colors: Record<Layer, string>,
  opts: {
    dimmed?: boolean;
    showEncapsulates?: boolean;
    selected?: boolean;
    /** Design-Health structure findings anchored to this component (a quiet
     *  severity badge beside the layer tag — the no-volatility cue idiom). */
    findings?: Finding[];
  } = {}
): Node {
  return {
    id: c.id,
    type: 'c4',
    position,
    data: {
      componentId: c.id,
      name: c.name,
      layer: LAYER_LABEL[c.layer],
      encapsulates: c.encapsulates,
      showEncapsulates: opts.showEncapsulates !== false,
      // Anti-functional-decomposition cue: a volatility-bearing layer with no
      // identified volatility gets a quiet warning badge (architectureCues).
      // Rides the same lens gate as the volatility preview — lenses that hide
      // volatility detail (Dynamic step-through, synthetic test participants)
      // hide the cue too.
      volatilityWarning: opts.showEncapsulates !== false && componentLacksVolatility(c),
      color: colors[c.layer],
      // Selection travels through `data` (not the Node.selected field): with the
      // controlled-node flow having no onNodesChange, xyflow's built-in selection is
      // inert, so a data flag is the reliable way to drive the Comment toolbar + ring.
      isSelected: opts.selected === true,
      ...(opts.findings !== undefined && opts.findings.length > 0
        ? { structureFindings: opts.findings }
        : {}),
    },
    draggable: false,
    ...(opts.dimmed === true ? { style: { opacity: 0.12 } } : {}),
  };
}

/** Builds a `person`-type React-Flow node for one use-case actor participating in
 *  a realized call chain (PersonNode renders the stick-figure glyph + role). Not a
 *  component: no layer tag, no volatility, no comment anchor — an actor is outside
 *  the system boundary. */
export function personNode(
  person: { id: string; role: string },
  position: { x: number; y: number },
  color: string
): Node {
  return {
    id: person.id,
    type: 'person',
    position,
    data: { personId: person.id, role: person.role, color },
    draggable: false,
  };
}

/** Visual weight + label/handle behaviour for a shared-language edge. */
export interface EdgeOpts {
  /** Show the call label. Off by default — labels only surface on hover/focus. */
  showLabel?: boolean;
  /** normal = present but quiet; focus = highlighted; muted = faded into the bg. */
  variant?: 'normal' | 'focus' | 'muted';
  /** Route via the side handles (source Right → target Left) into the Utilities bar. */
  toUtility?: boolean;
  /** Don't render this edge at all. */
  hidden?: boolean;
  /** Explicit stroke colour, overriding the variant default (e.g. test target/pass). */
  stroke?: string;
  /** Explicit opacity, overriding the variant default. */
  opacity?: number;
  /** Render dashed — used for queued / pub-sub (async) calls vs solid sync calls. */
  dashed?: boolean;
  /** When set, the edge is commentable: selecting it reveals a Comment affordance
   *  that arms this anchor (only the static architecture graph passes this). */
  comment?: Anchor;
  /** Design-Health structure findings anchored to this relationship: the stroke
   *  takes the severity colour and LayeredStepEdge renders a midpoint badge
   *  carrying "ruleId — message" (tooltip + aria). */
  findings?: Finding[];
}

/** A directed, arrow-headed smoothstep edge in the shared visual language. */
export function flowEdge(
  id: string,
  source: string,
  target: string,
  label: string,
  t: Tokens,
  opts: EdgeOpts = {}
): Edge {
  const variant = opts.variant ?? 'normal';
  const findings =
    opts.findings !== undefined && opts.findings.length > 0 ? opts.findings : undefined;
  // A finding edge keeps its severity stroke in every variant (hover-focus
  // included) so the violation stays visible while the neighbourhood is lit.
  const stroke =
    opts.stroke ??
    (findings !== undefined
      ? severityColor(t, maxSeverity(findings))
      : variant === 'focus'
        ? t.ink
        : t.muted);
  const opacity = opts.opacity ?? (variant === 'muted' ? 0.12 : 1);
  return {
    id,
    source,
    target,
    sourceHandle: opts.toUtility === true ? 'sr' : 'b',
    targetHandle: opts.toUtility === true ? 'tl' : 't',
    label: opts.showLabel === true ? label : undefined,
    hidden: opts.hidden === true,
    selectable: opts.comment !== undefined,
    ...(opts.comment !== undefined || findings !== undefined
      ? {
          data: {
            ...(opts.comment !== undefined ? { comment: opts.comment } : {}),
            ...(findings !== undefined ? { findings } : {}),
          },
        }
      : {}),
    type: 'layeredStep',
    style: {
      stroke,
      strokeWidth: variant === 'focus' || findings !== undefined ? 2 : 1.5,
      opacity,
      ...(opts.dashed === true ? { strokeDasharray: '6 4' } : {}),
      ...(opts.comment !== undefined ? { cursor: 'pointer' } : {}),
    },
    labelStyle: { fontFamily: t.mono, fontSize: 10, fontWeight: 700, fill: t.ink },
    labelBgStyle: { fill: t.paper, fillOpacity: 0.95 },
    labelBgPadding: [5, 3] as [number, number],
    markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
    zIndex: variant === 'focus' ? 10 : 0,
  };
}
