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
import Alert from '@mui/material/Alert';
import AlertTitle from '@mui/material/AlertTitle';
import { toC4View, type C4Component, type C4Relationship } from '../../contracts/adapters';
import type { ArtifactModelEnvelope } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import {
  type Layer,
  LAYER_ORDER,
  LAYER_LABEL,
  layerColors,
  computeLayout,
  decorativeNodes,
  c4Node,
  flowEdge,
  sortByLayoutPosition,
  type Layout,
} from './flowLayout';
import { LayerLegend, FlowCanvas, FlowEmpty, FocusNodes } from './flowShared';
import { relationshipAnchor, useComments, type Anchor } from '../comments/CommentContext';

interface Model {
  components: C4Component[];
  relationships: C4Relationship[];
  layout: Layout;
  layerOf: Map<string, Layer>;
  colors: Record<Layer, string>;
  usedLayers: Layer[];
  /**
   * The single layer every component claims when the layer data is DEGENERATE (F81):
   * multiple components but only one distinct layer. This is the fingerprint of a
   * drafting agent that omitted the per-component layer — the strict codec silently
   * defaults an absent layer to "client", collapsing the whole system onto one row.
   * null when the layer data is healthy (>1 distinct layer, or a single component).
   */
  degenerateLayer: Layer | null;
}

function buildModel(envelope: ArtifactModelEnvelope | undefined, t: Tokens): Model {
  const view = toC4View(envelope);
  const layout = computeLayout(view.components, view.relationships);
  const layerOf = new Map(view.components.map((c) => [c.id, c.layer]));
  const colors = layerColors(t);
  const present = new Set(view.components.map((c) => c.layer));
  const usedLayers = LAYER_ORDER.filter((l) => present.has(l));
  const degenerateLayer =
    view.components.length > 1 && usedLayers.length === 1 ? (usedLayers[0] ?? null) : null;
  return {
    components: view.components,
    relationships: view.relationships,
    layout,
    layerOf,
    colors,
    usedLayers,
    degenerateLayer,
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
  selectedId: string | null,
  t: Tokens
): { nodes: Node[]; edges: Edge[] } {
  const { components, relationships, layout, layerOf, colors } = model;
  const near = hoveredId !== null ? neighbourhood(hoveredId, relationships) : null;
  const nameOf = new Map(components.map((c) => [c.id, c.name]));

  // Emit nodes in the layout's visual reading order (row top→down, then x) so
  // DOM/tab order matches what the eye sees, not the model's drafted order.
  const nodes: Node[] = sortByLayoutPosition(components, layout).map((c) => {
    const pos = layout.pos.get(c.id) ?? { x: 0, y: 0 };
    // Utilities are shared infrastructure (the side bar just exists) — never dimmed.
    const dimmed = near !== null && !near.has(c.id) && c.layer !== 'utility';
    // Controlled selection (no onNodesChange): mark the pinned node via data so its
    // NodeToolbar Comment button shows — the explicit comment affordance now that a
    // plain click only selects/highlights and no longer silently arms an anchor.
    return c4Node(c, pos, colors, { dimmed, selected: c.id === selectedId });
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
  const { nodes, edges } = useMemo(
    () => derive(model, activeId, selectedId, t),
    [model, activeId, selectedId, t]
  );

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

  // Per-layer component counts, surfaced as cardinality chips in the legend.
  const layerCounts = useMemo(() => {
    const counts = Object.fromEntries(LAYER_ORDER.map((l) => [l, 0])) as Record<Layer, number>;
    for (const c of model.components) counts[c.layer] += 1;
    return counts;
  }, [model.components]);

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
      {model.degenerateLayer !== null && (
        <Alert severity="warning" sx={{ mb: 1.5, alignItems: 'flex-start' }}>
          <AlertTitle>Layer data looks degenerate</AlertTitle>
          All {model.components.length} components claim layer &ldquo;
          {LAYER_LABEL[model.degenerateLayer]}&rdquo; — a healthy Method system spans Managers,
          Engines, ResourceAccess and Resources. This usually means the draft omitted each
          component&rsquo;s layer (which silently defaults to &ldquo;client&rdquo;); send it back
          rather than committing a flat architecture.
        </Alert>
      )}
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
          // Click SELECTS/highlights the component (its call-chain neighbourhood lights
          // up and its Comment toolbar appears) — it no longer arms a comment directly.
          // Commenting is an explicit action: the toolbar button (mouse) or Enter/'c'
          // on the focused node (keyboard). Toggle the pin off on re-click.
          setSelectedId((s) => (s === n.id ? null : n.id));
        }}
        onNodeMouseEnter={(_e, n) => {
          enterNode(n);
        }}
        onNodeMouseLeave={() => {
          leaveNode();
        }}
      >
        <LayerLegend
          colors={model.colors}
          counts={layerCounts}
          t={t}
          usedLayers={model.usedLayers}
        />
        {selectedId !== null && <FocusNodes dep={selectedId} nodeIds={[selectedId]} />}
      </FlowCanvas>
    </Box>
  );
}
