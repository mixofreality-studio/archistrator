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
import type { Edge, Node, NodeMouseHandler } from '@xyflow/react';
import { toPerspective, type C4Component, type C4View } from '../../contracts/adapters';
import type { Finding } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import {
  computeLayout,
  decorativeNodes,
  c4Node,
  flowEdge,
  layerColors,
  sortByLayoutPosition,
} from './flowLayout';
import {
  EMPTY_STRUCTURE_OVERLAYS,
  computeStructureOverlays,
  edgeOverlayKey,
  type StructureOverlays,
} from './findingOverlays';
import { FlowCanvas, FlowEmpty } from './flowShared';
import type { C4NodeData } from './C4Node';

function build(
  view: C4View,
  componentId: string,
  t: Tokens,
  overlays: StructureOverlays
): { nodes: Node[]; edges: Edge[] } {
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

  // Emit nodes in the layout's visual reading order (row top→down, then x) so
  // DOM/tab order matches what the eye sees, not focus-then-neighbours order.
  const nodes: Node[] = sortByLayoutPosition(subset, layout).map((c) => {
    const isFocus = c.id === focus.id;
    const findings = overlays.nodes.get(c.id);
    // Only the focused node carries its (2-line clamped) volatility preview; the
    // neighbours render names + layer tag ONLY, so their prose can never overlap or
    // hide the focus node's title — the focus keeps its detail in-node + on hover,
    // matching the Static/Dynamic lens treatment.
    const base = c4Node(c, layout.pos.get(c.id) ?? { x: 0, y: 0 }, colors, {
      showEncapsulates: isFocus,
      ...(findings !== undefined ? { findings } : {}),
    });
    if (isFocus) {
      return {
        ...base,
        data: { ...base.data, color: t.accent },
        style: { filter: `drop-shadow(0 0 6px ${t.accent})` },
      };
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
    const slug = r.label
      .toLowerCase()
      .replace(/\s+/g, '-')
      .replace(/[^a-z0-9-]/g, '');
    const id = slug ? `${r.from}-${r.to}-${slug}` : `${r.from}-${r.to}-${String(i)}`;
    const findings = overlays.edges.get(edgeOverlayKey(r.from, r.to));
    edges.push(
      flowEdge(id, r.from, r.to, r.label, t, {
        dashed: r.mode !== 'sync',
        ...(findings !== undefined ? { findings } : {}),
      })
    );
  }

  return { nodes, edges };
}

export function PerspectiveFlow({
  view,
  componentId,
  height = 600,
  onFocusComponent,
  findings,
}: {
  view: C4View;
  /** The id of the component to focus on. */
  componentId: string;
  height?: number;
  /** Re-point the perspective onto another component. When set, clicking a neighbour
   *  node re-focuses the view onto it. */
  onFocusComponent?: (componentId: string) => void;
  /** Design-Health findings to join onto the focused neighbourhood (same overlay
   *  treatment as the Static lens). Absent/empty → no overlays (graceful). */
  findings?: Finding[];
}): ReactNode {
  const t = useTokens();
  const overlays = useMemo(
    () =>
      findings === undefined || findings.length === 0
        ? EMPTY_STRUCTURE_OVERLAYS
        : computeStructureOverlays(findings, view.components, view.relationships),
    [findings, view]
  );
  const { nodes, edges } = useMemo(
    () => build(view, componentId, t, overlays),
    [view, componentId, t, overlays]
  );

  if (nodes.length === 0) {
    return <FlowEmpty label="Select a component to focus on." t={t} />;
  }

  // Clicking a NEIGHBOUR re-points the perspective onto it (more useful than arming a
  // comment). Commenting stays on the always-available node toolbar / Enter-'c' path
  // (per ArchitectureFlow's select-not-comment rule), so a bare click never steals it.
  // Clicking the already-focused centre node is a no-op — the view is already there.
  // Only wired when a re-focus callback exists; conditionally SPREAD (not passed as
  // explicit `undefined`) so it satisfies FlowCanvas's `onNodeClick?: NodeMouseHandler`
  // under exactOptionalPropertyTypes — the same shape FlowCanvas forwards to ReactFlow.
  const onNodeClick: NodeMouseHandler | undefined =
    onFocusComponent !== undefined
      ? (_e, n): void => {
          if (n.type !== 'c4') return;
          const d = n.data as C4NodeData;
          if (d.componentId !== componentId) onFocusComponent(d.componentId);
        }
      : undefined;

  return (
    <FlowCanvas
      edges={edges}
      height={height}
      nodes={nodes}
      t={t}
      {...(onNodeClick !== undefined ? { onNodeClick } : {})}
    />
  );
}
