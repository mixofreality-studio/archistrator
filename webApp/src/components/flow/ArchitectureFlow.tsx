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
import Box from '@mui/material/Box';
import Autocomplete from '@mui/material/Autocomplete';
import TextField from '@mui/material/TextField';
import { toC4View, type C4Component, type C4Relationship } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import {
  type Layer,
  LAYER_ORDER,
  LAYER_LABEL,
  layerColors,
  computeLayout,
  decorativeNodes,
  c4Node,
  flowEdge,
  type Layout,
} from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty, FocusNodes } from './flowShared';
import {
  componentAnchor,
  relationshipAnchor,
  useComments,
  type Anchor,
} from '../comments/CommentContext';
import type { C4NodeData } from './C4Node';

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
  return {
    components: view.components,
    relationships: view.relationships,
    layout,
    layerOf,
    colors,
    usedLayers,
  };
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
  const slug = r.label
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-]/g, '');
  return slug ? `${r.from}-${r.to}-${slug}` : `${r.from}-${r.to}-${String(i)}`;
}

/** Human-meaningful anchor for a call edge: "<from> → <to>" (+ the call label). */
function edgeAnchor(r: C4Relationship, nameOf: Map<string, string>): Anchor {
  const from = nameOf.get(r.from) ?? r.from;
  const to = nameOf.get(r.to) ?? r.to;
  const call = r.label.length > 0 ? `${r.label} · ` : '';
  return {
    kind: 'node',
    label: `${call}${from} → ${to}`,
    source: 'Architecture · C4',
    jsonPath: relationshipAnchor(r.from, r.to),
  };
}

function derive(
  model: Model,
  hoveredId: string | null,
  t: Tokens
): { nodes: Node[]; edges: Edge[] } {
  const { components, relationships, layout, layerOf, colors } = model;
  const near = hoveredId !== null ? neighbourhood(hoveredId, relationships) : null;
  const nameOf = new Map(components.map((c) => [c.id, c.name]));

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
      const comment = edgeAnchor(r, nameOf);
      if (hoveredId === null)
        return flowEdge(edgeId(r, i), r.from, r.to, r.label, t, { dashed, comment });
      // Hover: only the hovered node's own edges stay; the rest fade out.
      return flowEdge(edgeId(r, i), r.from, r.to, r.label, t, {
        hidden: !incident,
        variant: incident ? 'focus' : 'muted',
        dashed,
        comment,
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
  const { setAnchor } = useComments();
  const model = useMemo(() => buildModel(envelope, t), [envelope, t]);
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  // A component pinned via the finder / a click (keyboard + touch reach the
  // neighbourhood highlight this way, not only mouse hover). Hover wins while active.
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const activeId = hoveredId ?? selectedId;
  const { nodes, edges } = useMemo(() => derive(model, activeId, t), [model, activeId, t]);

  // Finder options: all components, grouped by Method layer, alpha within layer.
  const finderOptions = useMemo(
    () =>
      [...model.components].sort(
        (a, b) =>
          LAYER_ORDER.indexOf(a.layer) - LAYER_ORDER.indexOf(b.layer) ||
          a.name.localeCompare(b.name)
      ),
    [model.components]
  );
  const selectedOption = finderOptions.find((c) => c.id === selectedId) ?? null;

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
    <Box>
      <Autocomplete
        blurOnSelect
        clearOnEscape
        getOptionLabel={(c) => c.name}
        groupBy={(c) => LAYER_LABEL[c.layer]}
        isOptionEqualToValue={(a, b) => a.id === b.id}
        options={finderOptions}
        renderInput={(params) => (
          <TextField {...params} placeholder="Find a component…" sx={{ fontFamily: t.mono }} />
        )}
        size="small"
        sx={{ mb: 1.5, maxWidth: 360 }}
        value={selectedOption}
        onChange={(_e, c) => {
          setSelectedId(c?.id ?? null);
        }}
      />
      <FlowCanvas
        edges={edges}
        height={height}
        nodes={nodes}
        t={t}
        onEdgeClick={(_e, edge) => {
          const comment = (edge.data as { comment?: Anchor } | undefined)?.comment;
          if (comment !== undefined) setAnchor(comment);
        }}
        onNodeClick={(_e, n) => {
          if (n.type !== 'c4') return;
          // Highlight the call-chain neighbourhood AND arm a component comment
          // anchor (the rail auto-opens on arm, giving visible feedback) — clicking
          // a component is the primary way to comment on it in Static view.
          setSelectedId((s) => (s === n.id ? null : n.id));
          const d = n.data as C4NodeData;
          setAnchor({
            kind: 'node',
            label: d.name,
            source: `Architecture · ${d.name}`,
            jsonPath: componentAnchor(d.componentId),
          });
        }}
        onNodeMouseEnter={(_e, n) => {
          enterNode(n);
        }}
        onNodeMouseLeave={() => {
          leaveNode();
        }}
      >
        <LayerLegend colors={model.colors} t={t} usedLayers={model.usedLayers} />
        {selectedId !== null && <FocusNodes dep={selectedId} nodeIds={[selectedId]} />}
      </FlowCanvas>
    </Box>
  );
}
