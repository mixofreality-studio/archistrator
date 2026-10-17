/**
 * The code-level diagram's canvas — one frame both views draw in
 * (codeDiagramModel.ts has the language, CodeDiagramNodes.tsx the nodes).
 *
 * It sizes ITSELF to the drawing: the zoom by the column's width (never below
 * DIAGRAM_MIN_ZOOM nor past 1), the drawing top-aligned, the frame as tall as
 * the drawing — so nothing scrolls or zooms inside it at 1440, and a stimulus's
 * small diagram is a small frame. The legend is a caption above the canvas;
 * nothing overlays the classes. The frame is a named group carrying a text
 * summary (`summary`) for a screen reader, and the page scrolls straight over it
 * (no wheel zoom, no scroll capture).
 */
import { useEffect, useId, useState, type ReactNode } from 'react';
import { ReactFlow, Background, useReactFlow, useStore, type Edge, type Node } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import { flowInstanceId } from '../../flow/flowInstanceId.ts';
import { fitToWidth } from '../../flow/fitContent.ts';
import { useMeasuredSizes } from '../../flow/measuredSizes.ts';
import { prefersReducedMotion } from '../../../utilities/reducedMotion';
import {
  DIAGRAM_BORDERS,
  DIAGRAM_GUTTER,
  DIAGRAM_MIN_HEIGHT,
  DIAGRAM_MIN_ZOOM,
  ROLE_LEGEND,
  roleColor,
  roleDash,
  type DiagramRole,
} from './codeDiagramModel.ts';
import { InterfaceNode, StackNode } from './CodeDiagramNodes';

const nodeTypes = { iface: InterfaceNode, stack: StackNode };

/** Frames to wait, at most, for React Flow to measure newly added nodes. */
const MEASURE_FRAMES = 30;

/**
 * Re-frames the canvas whenever `dep` changes, its width does, or the drawing
 * RE-MEASURES (a webfont swapping in re-wraps the rows). Waits until every node
 * is measured, then sizes the frame to the drawing and places it.
 */
function FrameOnChange({
  dep,
  height,
  onHeight,
}: {
  dep: string;
  height: number;
  onHeight: (height: number) => void;
}): null {
  const { setViewport, getNodes, getNodesBounds, getInternalNode } = useReactFlow();
  const paneWidth = useStore((s) => s.width);
  const measured = useMeasuredSizes();
  useEffect(() => {
    let raf = 0;
    let frames = 0;
    const step = (): void => {
      const nodes = getNodes();
      const unmeasured = nodes.some((n) => {
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
        bounds: getNodesBounds(nodes),
        minZoom: DIAGRAM_MIN_ZOOM,
        maxZoom: 1,
        gutter: DIAGRAM_GUTTER,
        frame: DIAGRAM_BORDERS,
        minHeight: DIAGRAM_MIN_HEIGHT,
      });
      if (fit.height !== height) {
        onHeight(fit.height);
        return;
      }
      void setViewport(
        { x: fit.x, y: fit.y, zoom: fit.zoom },
        { duration: prefersReducedMotion() ? 0 : 200 }
      );
    };
    raf = requestAnimationFrame(() => {
      raf = requestAnimationFrame(step);
    });
    return (): void => {
      cancelAnimationFrame(raf);
    };
  }, [
    dep,
    measured,
    height,
    paneWidth,
    onHeight,
    setViewport,
    getNodes,
    getNodesBounds,
    getInternalNode,
  ]);
  return null;
}

/** A legend's line sample: a short stroke in the edge's colour and dash. */
function LegendSwatch({ role }: { role: DiagramRole }): ReactNode {
  const t = useTokens();
  const dash = roleDash(role);
  return (
    <Box
      aria-hidden
      component="span"
      sx={{
        display: 'inline-block',
        width: 18,
        height: 0,
        verticalAlign: 'middle',
        borderTop: `2px ${dash === '' ? 'solid' : role === 'proof' ? 'dotted' : 'dashed'} ${roleColor(t, role)}`,
        mr: 0.4,
      }}
    />
  );
}

/** The edge legend, one swatch per role the drawing uses. */
export function DiagramLegend({ roles }: { roles: readonly DiagramRole[] }): ReactNode {
  return (
    <>
      {roles.map((r) => (
        <Box component="span" key={r} sx={{ whiteSpace: 'nowrap', mr: 1.25 }}>
          <LegendSwatch role={r} />
          {ROLE_LEGEND[r]}
        </Box>
      ))}
    </>
  );
}

export function CodeDiagramCanvas({
  nodes,
  edges,
  label,
  summary,
  caption,
  dep,
  frameTestId,
  captionTestId,
}: {
  nodes: Node[];
  edges: Edge[];
  /** The group's accessible name ("Class diagram of the DeliveryManager interface"). */
  label: string;
  /** What the drawing shows, as text, for a screen reader. */
  summary: string;
  /** The caption above the canvas (legend included). */
  caption: ReactNode;
  /** Changes whenever the drawing does (a selection), so the frame re-fits. */
  dep: string;
  frameTestId?: string | undefined;
  captionTestId?: string | undefined;
}): ReactNode {
  const t = useTokens();
  const [height, setHeight] = useState(DIAGRAM_MIN_HEIGHT);
  const uid = useId();
  // Its own React Flow id: several canvases on one page (three stimuli, the
  // contract) would otherwise share xyflow's default `1` in every DOM id.
  const rfId = flowInstanceId(uid);
  const summaryId = `${uid}-summary`;
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75, minWidth: 0 }}>
      <Typography
        component="div"
        data-testid={captionTestId}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}
      >
        {caption}
      </Typography>
      <Box
        id={summaryId}
        sx={{
          position: 'absolute',
          width: 1,
          height: 1,
          overflow: 'hidden',
          clip: 'rect(0 0 0 0)',
          whiteSpace: 'nowrap',
        }}
      >
        {summary}
      </Box>
      <Box
        aria-describedby={summaryId}
        aria-label={label}
        data-canvas-height={String(height)}
        data-testid={frameTestId}
        role="group"
        sx={{
          height,
          width: '100%',
          boxSizing: 'border-box',
          border: `1.5px solid ${t.line}`,
          borderRadius: t.radius / 8 + 0.5,
          bgcolor: t.bg,
        }}
      >
        <ReactFlow
          edges={edges}
          edgesFocusable={false}
          elementsSelectable={false}
          id={rfId}
          maxZoom={1}
          minZoom={0.3}
          nodeTypes={nodeTypes}
          nodes={nodes}
          nodesConnectable={false}
          nodesDraggable={false}
          nodesFocusable={false}
          panOnDrag={false}
          panOnScroll={false}
          preventScrolling={false}
          proOptions={{ hideAttribution: true }}
          zoomOnDoubleClick={false}
          zoomOnPinch={false}
          zoomOnScroll={false}
          // Defining a node click keeps React Flow's pointer-events on the nodes
          // (a non-selectable, non-draggable node otherwise gets none, and the
          // contract's row buttons would never see a click).
          onNodeClick={noop}
        >
          <FrameOnChange dep={dep} height={height} onHeight={setHeight} />
          <Background color={t.line} gap={22} size={1} />
        </ReactFlow>
      </Box>
    </Box>
  );
}

function noop(): void {
  /* the rows handle their own clicks */
}
