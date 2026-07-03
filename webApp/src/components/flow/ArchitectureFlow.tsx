/**
 * The System (architecture) artifact as a top-down *layered* C4 view, rendered with
 * @xyflow/react. Bound to adapters.toC4View (C4Component[] + C4Relationship[]).
 *
 * Layout (shared computeLayout): each Method layer is a fixed horizontal row
 * (Client → Managers → Engines → ResourceAccess → Resources), nodes within a row
 * are ordered under their callers to minimise crossings, and Utilities form a
 * vertical bar on the right spanning all rows (Righting Software Fig 3-4). A left
 * gutter labels the layers.
 *
 * Edges are directed calls. Labels are hidden by default (they collapse into an
 * unreadable band otherwise) and utility edges aren't drawn at all — until you
 * hover a component: then it + its direct callers/callees stay lit while everything
 * else fades, and the incident edges (utilities included) highlight with labels.
 *
 * Layout primitives, decoration, the legend, the canvas chrome, and the colour map
 * are shared with the dynamic / perspective / deployment flows via ./flowShared.
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import { toC4View, type C4Component, type C4Relationship } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import {
  type Layer,
  LAYER_ORDER,
  layerColors,
  computeLayout,
  decorativeNodes,
  c4Node,
  flowEdge,
  type Layout,
} from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty } from './flowShared';

interface Model {
  components: C4Component[];
  relationships: C4Relationship[];
  layout: Layout;
  layerOf: Map<string, Layer>;
  colors: Record<Layer, string>;
  usedLayers: Layer[];
}

function buildModel(envelope: ArtifactModelEnvelope | undefined, t: Tokens): Model {
  const view = toC4View(envelope);
  const layout = computeLayout(view.components, view.relationships);
  const layerOf = new Map(view.components.map((c) => [c.id, c.layer]));
  const colors = layerColors(t);
  const present = new Set(view.components.map((c) => c.layer));
  const usedLayers = LAYER_ORDER.filter((l) => present.has(l));
  return { components: view.components, relationships: view.relationships, layout, layerOf, colors, usedLayers };
}

/** The hovered node's closed neighbourhood: itself + direct callers + direct callees. */
function neighbourhood(hoveredId: string, rels: C4Relationship[]): Set<string> {
  const set = new Set<string>([hoveredId]);
  for (const r of rels) {
    if (r.from === hoveredId) set.add(r.to);
    if (r.to === hoveredId) set.add(r.from);
  }
  return set;
}

function edgeId(r: C4Relationship, i: number): string {
  const slug = r.label.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
  return slug ? `${r.from}-${r.to}-${slug}` : `${r.from}-${r.to}-${String(i)}`;
}

function derive(model: Model, hoveredId: string | null, t: Tokens): { nodes: Node[]; edges: Edge[] } {
  const { components, relationships, layout, layerOf, colors } = model;
  const near = hoveredId !== null ? neighbourhood(hoveredId, relationships) : null;

  const nodes: Node[] = components.map((c) => {
    const pos = layout.pos.get(c.id) ?? { x: 0, y: 0 };
    // Utilities are shared infrastructure (the side bar just exists) — never dimmed.
    const dimmed = near !== null && !near.has(c.id) && c.layer !== 'utility';
    return c4Node(c, pos, colors, { dimmed });
  });
  nodes.push(...decorativeNodes(layout));

  // Utility calls are never drawn as lines (the bar just exists, Fig 3-4). No edge
  // ever shows a label here — labels live only in the Dynamic step-through.
  const edges: Edge[] = relationships
    .filter((r) => layerOf.get(r.to) !== 'utility')
    .map((r, i) => {
      const incident = hoveredId !== null && (r.from === hoveredId || r.to === hoveredId);
      const dashed = r.mode !== 'sync'; // queued / pub-sub calls render dashed
      if (hoveredId === null) return flowEdge(edgeId(r, i), r.from, r.to, r.label, t, { dashed });
      // Hover: only the hovered node's own edges stay; the rest fade out.
      return flowEdge(edgeId(r, i), r.from, r.to, r.label, t, {
        hidden: !incident,
        variant: incident ? 'focus' : 'muted',
        dashed,
      });
    });

  return { nodes, edges };
}

export function ArchitectureFlow({
  envelope,
  height = 600,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const model = useMemo(() => buildModel(envelope, t), [envelope, t]);
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const { nodes, edges } = useMemo(() => derive(model, hoveredId, t), [model, hoveredId, t]);

  // Hover focus, debounced: moving the cursor between two nodes briefly crosses
  // empty canvas (firing mouse-leave then mouse-enter). Clearing immediately would
  // flash the whole un-muted diagram in between — reads as flicker. Instead the
  // clear is deferred and cancelled if another node is entered within the window,
  // so sweeping across the graph stays steady and only truly leaving it un-mutes.
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelPendingLeave = (): void => {
    if (leaveTimer.current !== null) {
      clearTimeout(leaveTimer.current);
      leaveTimer.current = null;
    }
  };
  const enterNode = useCallback((n: { id: string; type?: string | undefined }): void => {
    cancelPendingLeave();
    if (n.type === 'c4') setHoveredId(n.id);
  }, []);
  const leaveNode = useCallback((): void => {
    cancelPendingLeave();
    leaveTimer.current = setTimeout(() => {
      setHoveredId(null);
      leaveTimer.current = null;
    }, 90);
  }, []);
  useEffect(() => cancelPendingLeave, []);

  if (model.components.length === 0) {
    return <FlowEmpty label="No architecture drafted yet." t={t} />;
  }

  return (
    <FlowCanvas
      edges={edges}
      height={height}
      nodes={nodes}
      t={t}
      onNodeMouseEnter={(_e, n) => { enterNode(n); }}
      onNodeMouseLeave={() => { leaveNode(); }}
    >
      <LayerLegend colors={model.colors} t={t} usedLayers={model.usedLayers} />
    </FlowCanvas>
  );
}
