/**
 * A component-focused ("perspective") view of the static C4 graph: the selected
 * component sits centred, its inbound callers form a column to the LEFT (edges
 * pointing INTO the focus), and its outbound callees form a column to the RIGHT
 * (edges pointing OUT). Layer-coloured C4 nodes; labelled edges. A component with
 * no relationships renders as the lone focus node. Reuses the shared flow chrome
 * and preserves comment anchoring through C4Node.
 */
import { useMemo, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import { toPerspective, type C4View } from '../../api/adapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { COL_W, ROW_H, c4Node, flowEdge, layerColors } from './flowLayout';
import { FlowCanvas, FlowEmpty } from './flowShared';

function build(
  view: C4View,
  componentId: string,
  t: Tokens
): { nodes: Node[]; edges: Edge[] } {
  const { focus, inbound, outbound } = toPerspective(view, componentId);
  if (focus === undefined) return { nodes: [], edges: [] };

  const colors = layerColors(t);
  const byId = new Map(view.components.map((c) => [c.id, c]));

  const sourceIds = [...new Set(inbound.map((r) => r.from))];
  const targetIds = [...new Set(outbound.map((r) => r.to))];

  const maxSide = Math.max(sourceIds.length, targetIds.length, 1);
  const focusY = ((maxSide - 1) * ROW_H) / 2;

  const nodes: Node[] = [c4Node(focus, { x: COL_W, y: focusY }, colors)];
  for (const [i, id] of sourceIds.entries()) {
    const c = byId.get(id);
    if (c !== undefined) nodes.push(c4Node(c, { x: 0, y: i * ROW_H }, colors));
  }
  for (const [i, id] of targetIds.entries()) {
    const c = byId.get(id);
    if (c !== undefined) nodes.push(c4Node(c, { x: 2 * COL_W, y: i * ROW_H }, colors));
  }

  const placed = new Set(nodes.map((n) => n.id));
  const edges: Edge[] = [];
  for (const [i, r] of inbound.entries()) {
    if (placed.has(r.from)) {
      const labelSlug = r.label.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
      const id = labelSlug ? `in-${r.from}-${r.to}-${labelSlug}` : `in-${r.from}-${r.to}-${String(i)}`;
      edges.push(flowEdge(id, r.from, r.to, r.label, t));
    }
  }
  for (const [i, r] of outbound.entries()) {
    if (placed.has(r.to)) {
      const labelSlug = r.label.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
      const id = labelSlug ? `out-${r.from}-${r.to}-${labelSlug}` : `out-${r.from}-${r.to}-${String(i)}`;
      edges.push(flowEdge(id, r.from, r.to, r.label, t));
    }
  }

  return { nodes, edges };
}

export function PerspectiveFlow({
  view,
  componentId,
  height = 600,
}: {
  view: C4View;
  /** The id of the component to focus on. */
  componentId: string;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const { nodes, edges } = useMemo(() => build(view, componentId, t), [view, componentId, t]);

  if (nodes.length === 0) {
    return <FlowEmpty label="Select a component to focus on." t={t} />;
  }

  return <FlowCanvas edges={edges} height={height} nodes={nodes} t={t} />;
}
