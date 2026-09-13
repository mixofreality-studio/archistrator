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

import {
  LAYER_ROWS,
  COL_W,
  ROW_H,
  NODE_W,
  computeLayout,
  rowLabelText,
  type FlowLayer,
  type Layout,
  type LayoutComponent,
  type LayoutEdge,
} from './flowLayoutCore.ts';

export type { Layer };

// The layered layout ENGINE (the row vocabulary, the base geometry and the
// barycenter sweep) lives in ./flowLayoutCore — dependency-free, so a pure
// consumer can run it under node:test. Re-exported here unchanged, so every
// existing import of these names from ./flowLayout still resolves.
export { LAYER_ROWS, COL_W, ROW_H, NODE_W, computeLayout };
export type { FlowLayer, Layout, LayoutComponent, LayoutEdge };

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

/**
 * The opacity a MUTED (out-of-focus) participant fades to. One token, shared by
 * every flow that mutes — nodes and edges alike: the static graph's hover
 * neighbourhood (ArchitectureFlow), the dynamic lens' focused call/fragment
 * (DynamicViewFlow), and flowEdge's muted variant must all read as the SAME
 * treatment — that identity is the point (founder QA round 2).
 */
export const MUTED_OPACITY = 0.12;

/**
 * The opacity of the VISITED tier — the calls (and their endpoints) the reader
 * has already walked past in the Dynamic lens' fragment mode (founder QA round
 * 4's trail accretion). Deliberately a mid tint: it must read as "already
 * walked" at a glance, sitting clearly ABOVE the never-walked ghosts at
 * MUTED_OPACITY and clearly BELOW the current fragment at full strength.
 * One token for nodes and edges alike, so the trail reads as one thing.
 */
export const VISITED_OPACITY = 0.55;

/**
 * The opacity of the TRAIL — the handful of calls the reader has already walked
 * past, drawn behind the current one in the Dynamic lens (DynamicViewFlow's
 * TRAIL_LIMIT bounds how many).
 *
 * Deliberately LOWER than the VISITED_OPACITY the walked NODES keep. A node
 * occupies its own box and never overlaps another, so 0.55 reads as exactly one
 * quiet box. Wires do overlap, and opacity COMPOSITES: N strokes stacked in the
 * same corridor render as 1-(1-a)^N, so the same tint on a bundle of trail edges
 * paints a solid band precisely where the chain is densest. Bounding the trail
 * and tinting it down here keeps even its worst-case overlap a hairline.
 */
export const TRAIL_OPACITY = 0.35;

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
// COL_W / ROW_H / NODE_W and the engine itself (computeLayout, rowLabelText)
// live in ./flowLayoutCore — see the import at the top of this file.
export const NODE_H = 74;
/** Left gutter reserved for the per-row labels (Client / Business Logic / …). */
export const GUTTER_W = 150;
/** Head-room above the first Utility node for the "Utilities" title. */
const UTIL_HEAD = 34;
/** Padding of the Utilities frame around its nodes. */
const UTIL_PAD = 16;

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
    ...(opts.dimmed === true ? { style: { opacity: MUTED_OPACITY } } : {}),
  };
}

/** Builds a `person`-type React-Flow node for one use-case actor participating in
 *  a realized call chain (PersonNode renders the stick-figure glyph + role). Not a
 *  component: no layer tag, no volatility, no comment anchor — an actor is outside
 *  the system boundary. */
export function personNode(
  person: { id: string; role: string },
  position: { x: number; y: number },
  color: string,
  opts: {
    /** Fade this actor out — the same mute a non-neighbour component takes when
     *  the diagram has a focus (c4Node's `dimmed`; MUTED_OPACITY). */
    dimmed?: boolean;
  } = {}
): Node {
  return {
    id: person.id,
    type: 'person',
    position,
    data: { personId: person.id, role: person.role, color },
    draggable: false,
    ...(opts.dimmed === true ? { style: { opacity: MUTED_OPACITY } } : {}),
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
  /** Explicit source/target handle ids, overriding the layered default.
   *
   *  The layered views all flow one way (top → down), so a fixed bottom→top pair
   *  is right for them. A deployment view has no single direction — a browser
   *  reaches right into a gateway, a server reaches down into its store — so it
   *  picks the pair per edge from where the two boxes actually sit, and passes
   *  it here rather than forking the edge factory and losing the shared stroke,
   *  marker, label and focus/muted treatment. */
  handles?: { source: string; target: string };
  /** Don't render this edge at all. */
  hidden?: boolean;
  /** Explicit stroke colour, overriding the variant default (e.g. test target/pass). */
  stroke?: string;
  /** Explicit opacity, overriding the variant default. */
  opacity?: number;
  /** Explicit stroke width, overriding the variant default (2 for a focused or
   *  finding-bearing edge, 1.5 otherwise) — the Dynamic lens' trail tier draws a
   *  1px hairline so an already-walked call cannot compete with the current one. */
  strokeWidth?: number;
  /** Render dashed — used for queued / pub-sub (async) calls vs solid sync calls. */
  dashed?: boolean;
  /** When set, the edge is commentable: selecting it reveals a Comment affordance
   *  that arms this anchor (only the static architecture graph passes this). */
  comment?: Anchor;
  /** Design-Health structure findings anchored to this relationship: the stroke
   *  takes the severity colour and LayeredStepEdge renders a midpoint badge
   *  carrying "ruleId — message" (tooltip + aria). */
  findings?: Finding[];
  /** Founder QA round 4 (canvas↔caption correspondence): the call's GLOBAL
   *  sequence number, drawn as a small high-contrast chip at the edge's
   *  midpoint. Set ONLY on the current fragment's calls — the chip is what ties
   *  a numbered caption line to a specific wire on the canvas, so numbering
   *  everything would defeat it. A string when the call carries an
   *  alt-group display label ("1a", "1b", … — call-chain rollout Task 5)
   *  instead of the plain global number. */
  seqChip?: number | string;
  /** Founder QA round 4 (parallel-strand separation): this edge's slot among the
   *  strands sharing its (source, target) pair — see parallelEdges.ts. Absent
   *  (or count 1) leaves the path exactly where it has always been drawn. */
  parallel?: { index: number; count: number };
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
  const opacity = opts.opacity ?? (variant === 'muted' ? MUTED_OPACITY : 1);
  // Only spread a `parallel` slot the renderer would act on: a lone strand must
  // leave the drawn path byte-identical to what every other view already emits.
  const parallel =
    opts.parallel !== undefined && opts.parallel.count > 1 ? opts.parallel : undefined;
  const hasData =
    opts.comment !== undefined ||
    findings !== undefined ||
    opts.seqChip !== undefined ||
    parallel !== undefined;
  return {
    id,
    source,
    target,
    sourceHandle: opts.handles?.source ?? (opts.toUtility === true ? 'sr' : 'b'),
    targetHandle: opts.handles?.target ?? (opts.toUtility === true ? 'tl' : 't'),
    label: opts.showLabel === true ? label : undefined,
    hidden: opts.hidden === true,
    selectable: opts.comment !== undefined,
    ...(hasData
      ? {
          data: {
            ...(opts.comment !== undefined ? { comment: opts.comment } : {}),
            ...(findings !== undefined ? { findings } : {}),
            ...(opts.seqChip !== undefined ? { seqChip: opts.seqChip } : {}),
            ...(parallel !== undefined ? { parallel } : {}),
          },
        }
      : {}),
    type: 'layeredStep',
    style: {
      stroke,
      strokeWidth: opts.strokeWidth ?? (variant === 'focus' || findings !== undefined ? 2 : 1.5),
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
