/**
 * The System (architecture) artifact as a layered C4 component view, rendered with
 * @xyflow/react. Bound to adapters.toC4View (C4Component[] + C4Relationship[]).
 * Components are laid out top-to-bottom by Method layer (Clients → Managers →
 * Engines → ResourceAccess → Resources → Utility), relationships become labeled
 * edges, and a legend keys the layer colors. Fit-to-view; nodes selectable (a
 * selection arms a comment anchor via C4Node). Node/edge derivation is memoized.
 *
 * Layout primitives, the legend, the canvas chrome, and the colour map are shared
 * with the dynamic / perspective / deployment flows via ./flowShared.
 */
import { useMemo, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import { toC4View } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { type Layer, LAYER_ORDER, layerColors, COL_W, ROW_H, c4Node, flowEdge } from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty } from './flowShared';

function build(
  envelope: ArtifactModelEnvelope | undefined,
  t: Tokens
): { nodes: Node[]; edges: Edge[]; colors: Record<Layer, string>; usedLayers: Layer[] } {
  const view = toC4View(envelope);
  const colors = layerColors(t);
  const byLayer: Partial<Record<Layer, number>> = {};
  const nodes: Node[] = view.components.map((c) => {
    const col = byLayer[c.layer] ?? 0;
    byLayer[c.layer] = col + 1;
    const row = Math.max(LAYER_ORDER.indexOf(c.layer), 0);
    return c4Node(c, { x: col * COL_W, y: row * ROW_H }, colors);
  });
  const edges: Edge[] = view.relationships.map((r, i) => {
    const labelSlug = r.label.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
    const id = labelSlug ? `${r.from}-${r.to}-${labelSlug}` : `${r.from}-${r.to}-${String(i)}`;
    return flowEdge(id, r.from, r.to, r.label, t);
  });
  const present = new Set(view.components.map((c) => c.layer));
  const usedLayers = LAYER_ORDER.filter((l) => present.has(l));
  return { nodes, edges, colors, usedLayers };
}

export function ArchitectureFlow({
  envelope,
  height = 600,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const { nodes, edges, colors, usedLayers } = useMemo(() => build(envelope, t), [envelope, t]);

  if (nodes.length === 0) {
    return <FlowEmpty label="No architecture drafted yet." t={t} />;
  }

  return (
    <FlowCanvas edges={edges} height={height} nodes={nodes} t={t}>
      <LayerLegend colors={colors} t={t} usedLayers={usedLayers} />
    </FlowCanvas>
  );
}
