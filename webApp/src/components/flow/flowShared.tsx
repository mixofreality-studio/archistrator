/**
 * Shared React-Flow chrome for the architecture flow family (static C4, dynamic
 * call-chain, component perspective, deployment): the layer legend, the bordered
 * fit-to-view canvas, and the empty-state placeholder. Pure layout primitives
 * (colours, node/edge factories, the layer vocabulary) live in ./flowLayout so
 * this module exports only components.
 */
import type { ReactNode } from 'react';
import { ReactFlow, Background, Controls, Panel, type Edge, type Node, type NodeTypes } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../theme/themes';
import { type Layer, LAYER_LABEL, nodeTypes } from './flowLayout';

/** The shared layer-colour legend Panel (only the layers actually present). */
export function LayerLegend({
  usedLayers,
  colors,
  t,
}: {
  usedLayers: Layer[];
  colors: Record<Layer, string>;
  t: Tokens;
}): ReactNode {
  return (
    <Panel position="top-left">
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, p: 1, bgcolor: t.paper, border: `1.5px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5 }}>
        {usedLayers.map((l) => (
          <Box key={l} sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <Box sx={{ width: 12, height: 4, bgcolor: colors[l] }} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink }}>{LAYER_LABEL[l]}</Typography>
          </Box>
        ))}
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
  children,
}: {
  nodes: Node[];
  edges: Edge[];
  height: number;
  t: Tokens;
  nodeTypes?: NodeTypes;
  children?: ReactNode;
}): ReactNode {
  return (
    <Box sx={{ height, width: '100%', border: `1.5px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5, bgcolor: t.bg }}>
      <ReactFlow
        elementsSelectable
        fitView
        edges={edges}
        fitViewOptions={{ padding: 0.15 }}
        maxZoom={1.4}
        minZoom={0.3}
        nodeTypes={nodeTypesOverride ?? nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls showInteractive={false} />
        {children}
      </ReactFlow>
    </Box>
  );
}

/** Shared "nothing to render" placeholder used by every flow. */
export function FlowEmpty({ label, t }: { label: string; t: Tokens }): ReactNode {
  return (
    <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>{label}</Box>
  );
}
