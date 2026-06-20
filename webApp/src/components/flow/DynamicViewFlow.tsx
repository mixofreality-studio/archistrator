/**
 * One DynamicView (call chain for a single use case) as a layered C4 view: only
 * the participating components are placed (by Method layer, like the static view),
 * and the ordered call edges carry their sequence number as a label prefix
 * (e.g. "1. acceptRawItem"). Reuses the shared C4 node, colours, legend and
 * canvas chrome; comment anchoring is preserved through C4Node.
 */
import { useMemo, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import { toDynamicView } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { type Layer, LAYER_ORDER, layerColors, COL_W, ROW_H, c4Node, flowEdge } from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty } from './flowShared';

function build(
  envelope: ArtifactModelEnvelope | undefined,
  viewKey: string,
  t: Tokens,
  focalComponentId: string | undefined
): { nodes: Node[]; edges: Edge[]; colors: Record<Layer, string>; usedLayers: Layer[] } {
  const view = toDynamicView(envelope, viewKey);
  const colors = layerColors(t);
  const byLayer: Partial<Record<Layer, number>> = {};
  const nodes: Node[] = view.participants.map((c) => {
    const col = byLayer[c.layer] ?? 0;
    byLayer[c.layer] = col + 1;
    const row = Math.max(LAYER_ORDER.indexOf(c.layer), 0);
    const base = c4Node(c, { x: col * COL_W, y: row * ROW_H }, colors);
    // Apply focal highlight: override the color to the accent tone so the node's
    // top border and layer label read in the accent colour.
    if (focalComponentId !== undefined && c.id === focalComponentId) {
      return {
        ...base,
        data: { ...base.data, color: t.accent },
        style: { filter: `drop-shadow(0 0 6px ${t.accent})` },
      };
    }
    return base;
  });
  const edges: Edge[] = view.edges.map((r) =>
    flowEdge(`${String(r.seq)}-${r.from}-${r.to}`, r.from, r.to, `${String(r.seq)}. ${r.label}`, t)
  );
  const present = new Set(view.participants.map((c) => c.layer));
  const usedLayers = LAYER_ORDER.filter((l) => present.has(l));
  return { nodes, edges, colors, usedLayers };
}

export function DynamicViewFlow({
  envelope,
  viewKey,
  height = 600,
  focalComponentId,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  /** The DynamicView.key to render. */
  viewKey: string;
  height?: number;
  /** Optional component id (kebab-case) to visually emphasize in the diagram. */
  focalComponentId?: string;
}): ReactNode {
  const t = useTokens();
  const { nodes, edges, colors, usedLayers } = useMemo(
    () => build(envelope, viewKey, t, focalComponentId),
    [envelope, viewKey, t, focalComponentId]
  );

  if (nodes.length === 0) {
    return <FlowEmpty label="No call chain for this use case yet." t={t} />;
  }

  return (
    <FlowCanvas edges={edges} height={height} nodes={nodes} t={t}>
      <LayerLegend colors={colors} t={t} usedLayers={usedLayers} />
    </FlowCanvas>
  );
}
