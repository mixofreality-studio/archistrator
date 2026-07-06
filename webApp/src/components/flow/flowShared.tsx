/**
 * Shared React-Flow chrome for the architecture flow family (static C4, dynamic
 * call-chain, component perspective, deployment): the layer legend, the bordered
 * fit-to-view canvas, and the empty-state placeholder. Pure layout primitives
 * (colours, node/edge factories, the layer vocabulary) live in ./flowLayout so
 * this module exports only components.
 */
import { useEffect, type ReactNode } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  Panel,
  useReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
  type NodeMouseHandler,
  type EdgeMouseHandler,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../utilities/theme/themes';
import { type Layer, LAYER_LABEL } from './flowLayout';
import { edgeTypes, nodeTypes } from './flowNodeTypes';

/** The shared layer-colour legend Panel (only the layers actually present). When
 *  `showAgentic` is set (the view contains an agent-driven component), a "✳
 *  agent-driven" row is appended. */
export function LayerLegend({
  usedLayers,
  colors,
  t,
  showAgentic = false,
}: {
  usedLayers: Layer[];
  colors: Record<Layer, string>;
  t: Tokens;
  showAgentic?: boolean;
}): ReactNode {
  return (
    <Panel position="top-left">
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          gap: 0.5,
          p: 1,
          bgcolor: t.paper,
          border: `1.5px solid ${t.line}`,
          borderRadius: t.radius / 8 + 0.5,
        }}
      >
        {usedLayers.map((l) => (
          <Box key={l} sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <Box sx={{ width: 12, height: 4, bgcolor: colors[l] }} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink }}>
              {LAYER_LABEL[l]}
            </Typography>
          </Box>
        ))}
        {showAgentic ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <Box
              sx={{ width: 12, textAlign: 'center', fontSize: 11, fontWeight: 700, color: t.ink }}
            >
              ✳
            </Box>
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink }}>
              agent-driven
            </Typography>
          </Box>
        ) : null}
      </Box>
    </Panel>
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
  children,
}: {
  nodes: Node[];
  edges: Edge[];
  height: number;
  t: Tokens;
  nodeTypes?: NodeTypes;
  onNodeMouseEnter?: NodeMouseHandler;
  onNodeMouseLeave?: NodeMouseHandler;
  /** Arm-on-click handlers. Used for comment anchoring — React Flow's built-in
   *  selection is inert here (nodes/edges are controlled with no change handler),
   *  so click callbacks, not `selected` state, drive the comment affordances. */
  onNodeClick?: NodeMouseHandler;
  onEdgeClick?: EdgeMouseHandler;
  children?: ReactNode;
}): ReactNode {
  return (
    <Box
      sx={{
        height,
        width: '100%',
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: t.bg,
      }}
    >
      <ReactFlow
        elementsSelectable
        fitView
        edgeTypes={edgeTypes}
        edges={edges}
        fitViewOptions={{ padding: 0.15 }}
        maxZoom={1.4}
        minZoom={0.3}
        nodeTypes={nodeTypesOverride ?? nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        proOptions={{ hideAttribution: true }}
        {...(onNodeMouseEnter ? { onNodeMouseEnter } : {})}
        {...(onNodeMouseLeave ? { onNodeMouseLeave } : {})}
        {...(onNodeClick ? { onNodeClick } : {})}
        {...(onEdgeClick ? { onEdgeClick } : {})}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls showInteractive={false} />
        {children}
      </ReactFlow>
    </Box>
  );
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
          duration: 400,
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

/** Shared "nothing to render" placeholder used by every flow. */
export function FlowEmpty({ label, t }: { label: string; t: Tokens }): ReactNode {
  return <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>{label}</Box>;
}
