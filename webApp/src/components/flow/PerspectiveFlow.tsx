/**
 * A component-focused ("perspective") view: the selected component plus its direct
 * callers and callees, drawn in the SAME top-down layered layout as the static and
 * dynamic views (shared computeLayout) — a "pinned" version of the static hover
 * state. Only the focus + its immediate neighbours are placed, each in its real
 * Method-layer row, with the row-label gutter and Utilities side bar; all edges show
 * their call label. The focus node is accent-highlighted. A component with no
 * relationships renders as the lone focus node. Reuses the shared flow chrome and
 * preserves comment anchoring through C4Node.
 */
import { useMemo, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import { toPerspective, type C4Component, type C4View } from '../../api/adapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { computeLayout, decorativeNodes, c4Node, flowEdge, layerColors } from './flowLayout';
import { FlowCanvas, FlowEmpty } from './flowShared';

function build(view: C4View, componentId: string, t: Tokens): { nodes: Node[]; edges: Edge[] } {
  const { focus, inbound, outbound } = toPerspective(view, componentId);
  if (focus === undefined) return { nodes: [], edges: [] };

  const colors = layerColors(t);
  const byId = new Map(view.components.map((c) => [c.id, c]));
  const rels = [...inbound, ...outbound];

  // The placed subset: focus + every direct caller/callee, deduped, in their rows.
  const subset: C4Component[] = [];
  const seen = new Set<string>();
  const add = (id: string): void => {
    if (seen.has(id)) return;
    const c = byId.get(id);
    if (c !== undefined) {
      seen.add(id);
      subset.push(c);
    }
  };
  add(focus.id);
  for (const r of rels) {
    add(r.from);
    add(r.to);
  }

  const layout = computeLayout(subset, rels);
  const layerOf = new Map(subset.map((c) => [c.id, c.layer]));

  const nodes: Node[] = subset.map((c) => {
    const base = c4Node(c, layout.pos.get(c.id) ?? { x: 0, y: 0 }, colors);
    if (c.id === focus.id) {
      return { ...base, data: { ...base.data, color: t.accent }, style: { filter: `drop-shadow(0 0 6px ${t.accent})` } };
    }
    return base;
  });
  nodes.push(...decorativeNodes(layout));

  // Plain directed arrows: no labels, and no lines to the Utilities bar (it just
  // exists) — consistent with the static view.
  const edges: Edge[] = [];
  for (const [i, r] of rels.entries()) {
    if (!seen.has(r.from) || !seen.has(r.to)) continue;
    if (layerOf.get(r.to) === 'utility') continue;
    const slug = r.label.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
    const id = slug ? `${r.from}-${r.to}-${slug}` : `${r.from}-${r.to}-${String(i)}`;
    edges.push(flowEdge(id, r.from, r.to, r.label, t, { dashed: r.mode !== 'sync' }));
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
