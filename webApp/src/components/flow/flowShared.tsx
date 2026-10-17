/**
 * Shared React-Flow chrome for the architecture flow family (static C4, dynamic
 * call-chain, component perspective, deployment): the layer legend, the bordered
 * fit-to-view canvas, and the empty-state placeholder. Pure layout primitives
 * (colours, node/edge factories, the layer vocabulary) live in ./flowLayout so
 * this module exports only components.
 */
import { useEffect, useId, useState, type CSSProperties, type ReactNode } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  useReactFlow,
  useStore,
  type Edge,
  type Node,
  type NodeTypes,
  type NodeMouseHandler,
  type EdgeMouseHandler,
  type OnMove,
  type Viewport,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../utilities/theme/themes';
import { prefersReducedMotion } from '../../utilities/reducedMotion';
import { type Layer, LAYER_LABEL } from './flowLayout';
import { edgeTypes, nodeTypes } from './flowNodeTypes';
import { flowInstanceId } from './flowInstanceId.ts';
import { fitToWidth } from './fitContent.ts';
import { useMeasuredSizes } from './measuredSizes.ts';

/**
 * The shared diagram KEY: one wrapped strip ABOVE the canvas (never a panel over
 * it — an overlay hid the top rows of nodes behind itself, and grew with every
 * row a lens needed to explain). It keys the layer colours actually present
 * (with each layer's component count when given), the arrow styles actually
 * drawn, and any lens-specific entries a caller passes as `footer`.
 */
export function LayerLegend({
  usedLayers,
  colors,
  counts,
  footer,
  edgeKinds,
  t,
}: {
  usedLayers: Layer[];
  colors: Record<Layer, string>;
  /** Optional per-layer component count — surfaces The Method's per-layer
   *  cardinality guidance where the eye already is (Static architecture view). */
  counts?: Record<Layer, number>;
  /** Optional extra entries after the layer + arrow keys (e.g. the structure-
   *  findings count chip, or a lens's own glow/trail vocabulary). */
  footer?: ReactNode;
  /** Which arrow styles the canvas actually draws, so the key can explain them:
   *  solid = a synchronous call, dashed = a queued / pub-sub call. Absent → no
   *  arrow key. */
  edgeKinds?: { sync: boolean; async: boolean };
  t: Tokens;
}): ReactNode {
  const showEdges = edgeKinds !== undefined && (edgeKinds.sync || edgeKinds.async);
  return (
    <Box
      aria-label="Diagram key"
      role="group"
      sx={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        columnGap: 2,
        rowGap: 0.75,
        mb: 1,
        px: 1.25,
        py: 0.75,
        bgcolor: t.paper,
        border: `1px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
      }}
    >
      <KeyGroup label={counts !== undefined ? 'Layers (components)' : 'Layers'} t={t}>
        {usedLayers.map((l) => (
          <Box key={l} sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
            <Box aria-hidden sx={{ width: 12, height: 4, bgcolor: colors[l], flexShrink: 0 }} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>
              {LAYER_LABEL[l]}
              {counts !== undefined ? ` · ${String(counts[l])}` : ''}
            </Typography>
          </Box>
        ))}
      </KeyGroup>
      {showEdges ? (
        <KeyGroup label="Arrows" t={t}>
          {edgeKinds.sync ? (
            <EdgeKeyRow dashed={false} label="call, waits for a reply" t={t} />
          ) : null}
          {edgeKinds.async ? <EdgeKeyRow dashed label="queued / published, no wait" t={t} /> : null}
        </KeyGroup>
      ) : null}
      {footer ?? null}
    </Box>
  );
}

/** One labelled cluster of the key strip ("Layers", "Arrows", …). */
export function KeyGroup({
  label,
  t,
  children,
}: {
  label: string;
  t: Tokens;
  children: ReactNode;
}): ReactNode {
  return (
    <Box
      sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', columnGap: 1.25, rowGap: 0.5 }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 10,
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          color: t.muted,
        }}
      >
        {label}
      </Typography>
      {children}
    </Box>
  );
}

/** One arrow-style entry of the key: a short line sample + what it means. */
function EdgeKeyRow({
  dashed,
  label,
  t,
}: {
  dashed: boolean;
  label: string;
  t: Tokens;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
      <svg aria-hidden="true" focusable="false" height="6" width="20">
        <line
          stroke={t.ink}
          strokeDasharray={dashed ? '4 3' : undefined}
          strokeWidth="1.5"
          x1="0"
          x2="20"
          y1="3"
          y2="3"
        />
      </svg>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>{label}</Typography>
    </Box>
  );
}

/**
 * The shared flow chrome: a bordered canvas + ReactFlow configured exactly like
 * ArchitectureFlow (selectable, fit-to-view, no drag/connect, hidden attribution)
 * with Background + Controls. `nodeTypes` defaults to the C4 node; pass an override
 * for non-C4 flows (e.g. deployment). Children render extra panels (e.g. legend).
 */
export function FlowCanvas({
  nodes,
  edges,
  height,
  t,
  nodeTypes: nodeTypesOverride,
  onNodeMouseEnter,
  onNodeMouseLeave,
  onNodeClick,
  onEdgeClick,
  defaultViewport,
  onMoveEnd,
  minZoom = 0.3,
  controlsStyle,
  edgesFocusable,
  fitMaxZoom,
  fitToContent,
  children,
}: {
  nodes: Node[];
  edges: Edge[];
  /** A fixed pixel height (the historical default) or a CSS length — the
   *  walkthrough-driven trace passes a viewport-relative `clamp(...)` so the
   *  two-up layout fits above the fold instead of a tall fixed canvas pushing
   *  the fold below both panes (founder QA round 5). */
  height: number | string;
  t: Tokens;
  nodeTypes?: NodeTypes;
  onNodeMouseEnter?: NodeMouseHandler;
  onNodeMouseLeave?: NodeMouseHandler;
  /** Arm-on-click handlers. Used for comment anchoring — React Flow's built-in
   *  selection is inert here (nodes/edges are controlled with no change handler),
   *  so click callbacks, not `selected` state, drive the comment affordances. */
  onNodeClick?: NodeMouseHandler;
  onEdgeClick?: EdgeMouseHandler;
  /** A viewport to open at INSTEAD of fitting the view — how a canvas that
   *  remounts (a lens switch, a poll) restores where the reader left it. Absent
   *  (every historical caller) keeps the fit-on-mount behaviour unchanged. */
  defaultViewport?: Viewport;
  /** Fired when a pan/zoom settles — pairs with `defaultViewport` to remember it. */
  onMoveEnd?: OnMove;
  /** The furthest zoom-out. Defaults to the historical 0.3. */
  minZoom?: number;
  /** Extra style for the zoom controls' panel (xyflow's bottom-left). Absent
   *  for every historical caller; the graph lens pushes them clear of its
   *  pinned row gutter. */
  controlsStyle?: CSSProperties;
  /** Whether edges are Tab stops. Absent keeps xyflow's default (every
   *  historical caller); a canvas whose edges carry no action passes false so
   *  the keyboard reaches its nodes without crossing every wire. */
  edgesFocusable?: boolean;
  /** Cap every fit's zoom, the Controls' fit button included. Absent: xyflow's own. */
  fitMaxZoom?: number;
  /**
   * Size the canvas to the drawing instead of fitting it into `height`
   * (fitContent.ts): the zoom by the width alone, capped at `fitMaxZoom` (1.0 by
   * default — never larger than drawn), the drawing top-aligned, and the canvas
   * as tall as the drawing within these bounds. `height` is then ignored. Absent
   * for every historical caller.
   */
  fitToContent?: { minHeight: number; maxHeight: number };
  children?: ReactNode;
}): ReactNode {
  // Its own React Flow id (flowInstanceId.ts): every instance defaulting to `1`
  // duplicated xyflow's DOM ids whenever two canvases shared the page.
  const rfId = flowInstanceId(useId());
  const [contentHeight, setContentHeight] = useState<number | undefined>(undefined);
  const fitOptions = {
    padding: 0.15,
    ...(fitMaxZoom !== undefined ? { maxZoom: fitMaxZoom } : {}),
  };
  return (
    <Box
      data-canvas-height={fitToContent !== undefined ? String(contentHeight ?? 0) : undefined}
      sx={{
        height: fitToContent !== undefined ? (contentHeight ?? fitToContent.minHeight) : height,
        width: '100%',
        ...(fitToContent !== undefined ? { boxSizing: 'border-box' } : {}),
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: t.bg,
        // xyflow's zoom controls ship light-only styling: on the dark themes they
        // were near-white squares with near-white glyphs. Theme them from tokens.
        '& .react-flow__controls-button': {
          bgcolor: t.paper,
          color: t.ink,
          fill: t.ink,
          borderColor: t.line,
          '&:hover': { bgcolor: t.paperAlt },
          '& svg': { fill: t.ink },
        },
      }}
    >
      <ReactFlow
        elementsSelectable
        edgeTypes={edgeTypes}
        edges={edges}
        // Fit on mount ONLY when no viewport is being restored — a restored one
        // is exactly the reader's own pan/zoom, which a fit would throw away —
        // and not when the canvas sizes itself to the drawing (FitToContent).
        fitView={defaultViewport === undefined && fitToContent === undefined}
        fitViewOptions={fitOptions}
        id={rfId}
        maxZoom={1.4}
        minZoom={minZoom}
        {...(defaultViewport !== undefined ? { defaultViewport } : {})}
        {...(onMoveEnd !== undefined ? { onMoveEnd } : {})}
        nodeTypes={nodeTypesOverride ?? nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        // Each node owns its own focusable, labeled inner element (C4Node) that carries
        // the accessible name + keyboard comment shortcut — xyflow's wrapper focus is
        // off so there is a single, well-labeled tab stop per node.
        nodesFocusable={false}
        {...(edgesFocusable !== undefined ? { edgesFocusable } : {})}
        proOptions={{ hideAttribution: true }}
        {...(onNodeMouseEnter ? { onNodeMouseEnter } : {})}
        {...(onNodeMouseLeave ? { onNodeMouseLeave } : {})}
        {...(onNodeClick ? { onNodeClick } : {})}
        {...(onEdgeClick ? { onEdgeClick } : {})}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls
          fitViewOptions={fitOptions}
          showInteractive={false}
          {...(controlsStyle !== undefined ? { style: controlsStyle } : {})}
        />
        {fitToContent !== undefined ? (
          <FitToContent
            height={contentHeight}
            maxHeight={fitToContent.maxHeight}
            maxZoom={fitMaxZoom ?? 1}
            minHeight={fitToContent.minHeight}
            minZoom={minZoom}
            nodes={nodes}
            onHeight={setContentHeight}
          />
        ) : null}
        {children}
      </ReactFlow>
    </Box>
  );
}

/** Frames, at most, to wait for React Flow to measure the nodes before a fit. */
const MEASURE_FRAMES = 30;
/** Clear space around a drawing fit to its content, in screen pixels. */
const CONTENT_GUTTER = 24;
/** The canvas frame's two 1.5px borders. */
const CONTENT_FRAME = 3;

/**
 * FlowCanvas's `fitToContent`: once every node is measured, size the canvas to
 * the drawing and place it (fitContent.fitToWidth) — the zoom by the width,
 * capped, top-aligned. A height change re-runs it, and the next pass places.
 */
function FitToContent({
  nodes,
  height,
  minHeight,
  maxHeight,
  minZoom,
  maxZoom,
  onHeight,
}: {
  nodes: Node[];
  height: number | undefined;
  minHeight: number;
  maxHeight: number;
  minZoom: number;
  maxZoom: number;
  onHeight: (height: number) => void;
}): null {
  const { setViewport, getNodes, getNodesBounds, getInternalNode } = useReactFlow();
  const paneWidth = useStore((s) => s.width);
  const measured = useMeasuredSizes();
  useEffect(() => {
    let raf = 0;
    let frames = 0;
    const step = (): void => {
      const current = getNodes();
      const unmeasured = current.some((n) => {
        const m = getInternalNode(n.id)?.measured;
        return (m?.width ?? 0) === 0 || (m?.height ?? 0) === 0;
      });
      if ((unmeasured || paneWidth === 0) && frames < MEASURE_FRAMES) {
        frames += 1;
        raf = requestAnimationFrame(step);
        return;
      }
      const fit = fitToWidth({
        paneWidth,
        bounds: getNodesBounds(current),
        minZoom,
        maxZoom,
        gutter: CONTENT_GUTTER,
        frame: CONTENT_FRAME,
        minHeight,
        maxHeight,
      });
      if (fit.height !== height) {
        onHeight(fit.height);
        return;
      }
      void setViewport({ x: fit.x, y: fit.y, zoom: fit.zoom });
    };
    raf = requestAnimationFrame(step);
    return (): void => {
      cancelAnimationFrame(raf);
    };
  }, [
    nodes,
    measured,
    height,
    paneWidth,
    minHeight,
    maxHeight,
    minZoom,
    maxZoom,
    onHeight,
    setViewport,
    getNodes,
    getNodesBounds,
    getInternalNode,
  ]);
  return null;
}

/**
 * Pans + zooms the canvas to frame a set of nodes whenever `dep` changes — used to
 * recenter the Dynamic call-chain view on the current step's endpoints (and to
 * focus a searched component in Static), mirroring the use-case walkthrough. Lives
 * as a child of <ReactFlow> for hook access; a double-rAF waits for measurement.
 */
export function FocusNodes({ nodeIds, dep }: { nodeIds: string[]; dep: string }): null {
  const { fitView } = useReactFlow();
  useEffect(() => {
    if (nodeIds.length === 0) return undefined;
    let raf2 = 0;
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        void fitView({
          nodes: nodeIds.map((id) => ({ id })),
          // a11y reduced motion (F-QA2-51): instant jump when reduction is
          // requested — read at animation time so a live settings change applies.
          duration: prefersReducedMotion() ? 0 : 400,
          padding: 0.4,
          maxZoom: 1.2,
        });
      });
    });
    return (): void => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, [dep, nodeIds, fitView]);
  return null;
}

/** Shared "nothing to render" placeholder used by every flow. `tone` defaults
 *  to the plain muted placeholder every existing caller keeps; 'pending' (Task
 *  6, call-chain rollout) recolors it amber for DynamicViewFlow's honest
 *  "not yet realized — part of the pending realization amendment" case, so
 *  that state reads distinctly from a genuinely broken/synthetic empty view. */
export function FlowEmpty({
  label,
  t,
  tone = 'muted',
}: {
  label: string;
  t: Tokens;
  tone?: 'muted' | 'pending';
}): ReactNode {
  const color = tone === 'pending' ? t.awaitingFg : t.muted;
  return <Box sx={{ py: 6, textAlign: 'center', color, fontFamily: t.mono }}>{label}</Box>;
}
