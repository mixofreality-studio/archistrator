/**
 * ContractComponentFlow — the Component view tab of ServiceContractView.
 *
 * Renders a focused C4 component diagram built from the contract's own
 * inbound[] / outbound[] fields:
 *   - Inbound callers: top row
 *   - Focal component: center
 *   - Outbound callees: bottom row
 *
 * Edges: inbound → focal (inbound direction), focal → outbound (how as label).
 * Nodes labeled name + layer. Self-contained — does NOT cross-reference the
 * system-design slot.
 */
import type { ReactNode } from 'react';
import { useMemo } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  Position,
  MarkerType,
  type Node,
  type Edge,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { ContractParty } from '../../api/types';
import type { Tokens } from '../../theme/themes';
import { useTokens } from '../../theme/ThemeContext';

// ---------------------------------------------------------------------------
// Custom node
// ---------------------------------------------------------------------------

interface ComponentNodeData {
  label: string;
  layer: string;
  isFocal?: boolean;
  [key: string]: unknown;
}

const LAYER_ACCENT = (t: Tokens, layer: string): string => {
  switch (layer) {
    case 'Client': return t.chatPmFg;
    case 'Manager': return t.accent;
    case 'Engine': return t.chatArchitectFg;
    case 'ResourceAccess': return t.accent2;
    case 'Utility': return t.muted;
    default: return t.muted;
  }
};

function ComponentNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as ComponentNodeData;
  const accent = LAYER_ACCENT(t, d.layer);
  return (
    <>
      <Handle position={Position.Top} style={{ opacity: 0 }} type="target" />
      <Handle position={Position.Bottom} style={{ opacity: 0 }} type="source" />
      <Box
        sx={{
          minWidth: 140,
          maxWidth: 220,
          bgcolor: d.isFocal === true ? t.awaitingBg : t.paperAlt,
          border: `${d.isFocal === true ? '2.5px' : '1.5px'} solid ${d.isFocal === true ? t.accent : t.line}`,
          borderTop: `4px solid ${accent}`,
          borderRadius: '8px',
          overflow: 'hidden',
          boxShadow: d.isFocal === true ? `0 0 0 2px ${t.accent}` : 'none',
        }}
      >
        <Box sx={{ px: 1.25, py: 0.75 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 7.5, color: accent, letterSpacing: '0.06em', fontWeight: 700 }}>
            {d.layer.toUpperCase()}
          </Typography>
          <Typography sx={{ fontFamily: d.isFocal === true ? t.display : t.mono, fontWeight: 700, fontSize: d.isFocal === true ? 13 : 11, color: t.ink, lineHeight: 1.2 }}>
            {d.label}
          </Typography>
          {d.isFocal === true ? (
            <Typography sx={{ fontFamily: t.mono, fontSize: 8, color: t.accent, mt: 0.2 }}>focal component</Typography>
          ) : null}
        </Box>
      </Box>
    </>
  );
}

const nodeTypes = { component: ComponentNode };

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

const COL_W = 200;
const ROW_H = 120;

function rowX(count: number, i: number): number {
  return (i - (count - 1) / 2) * COL_W;
}

// ---------------------------------------------------------------------------
// ContractComponentFlow
// ---------------------------------------------------------------------------

export function ContractComponentFlow({
  component,
  layer,
  inbound,
  outbound,
  height = 420,
  t,
}: {
  component: string;
  layer: string;
  inbound: ContractParty[];
  outbound: ContractParty[];
  height?: number;
  t: Tokens;
}): ReactNode {
  const { nodes, edges } = useMemo(() => {
    const ns: Node[] = [];
    const es: Edge[] = [];

    // Focal node at center row (y=1).
    ns.push({
      id: 'focal',
      type: 'component',
      position: { x: 0, y: ROW_H },
      data: { label: component, layer, isFocal: true },
      draggable: false,
      selectable: false,
    });

    // Inbound callers — top row (y=0).
    inbound.forEach((p, i) => {
      const id = `in-${p.name}`;
      ns.push({
        id,
        type: 'component',
        position: { x: rowX(inbound.length, i), y: 0 },
        data: { label: p.name, layer: p.layer },
        draggable: false,
        selectable: false,
      });
      es.push({
        id: `${id}-focal`,
        source: id,
        target: 'focal',
        type: 'smoothstep',
        style: { stroke: t.line, strokeWidth: 1.5 },
        markerEnd: { type: MarkerType.ArrowClosed, color: t.line },
      });
    });

    // Outbound callees — bottom row (y=2).
    outbound.forEach((p, i) => {
      const id = `out-${p.name}`;
      ns.push({
        id,
        type: 'component',
        position: { x: rowX(outbound.length, i), y: ROW_H * 2 },
        data: { label: p.name, layer: p.layer },
        draggable: false,
        selectable: false,
      });
      const label = p.how !== undefined && p.how.length > 0 ? p.how : undefined;
      es.push({
        id: `focal-${id}`,
        source: 'focal',
        target: id,
        type: 'smoothstep',
        label,
        style: { stroke: t.accent2, strokeWidth: 1.5 },
        labelStyle: { fontFamily: t.mono, fontSize: 8.5, fill: t.accent2 },
        labelBgStyle: { fill: t.paper, fillOpacity: 0.9 },
        labelBgPadding: [3, 2] as [number, number],
        labelBgBorderRadius: 3,
        markerEnd: { type: MarkerType.ArrowClosed, color: t.accent2 },
      });
    });

    return { nodes: ns, edges: es };
  }, [component, layer, inbound, outbound, t]);

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
        fitView
        edges={edges}
        fitViewOptions={{ padding: 0.2 }}
        maxZoom={1.5}
        minZoom={0.25}
        nodeTypes={nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </Box>
  );
}
