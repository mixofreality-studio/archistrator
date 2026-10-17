/**
 * The Project Network artifact — a CPM summary strip (four distinct computed
 * facts), a filter/focus toolbar, the React-Flow centerpiece (critical path bold +
 * animated; non-CP edges tinted by their target's float band), a minimap, a
 * collapsible float-band legend whose swatches double as per-band filters, and the
 * spelled-out critical path. Bound to the CPM-derived NetworkView
 * (api/projectAdapters.toNetworkView over ActivityList × Network). Float-criticality
 * colour-coding follows Löwy ch.8 §2: the band channel is each node's left border,
 * mirrored on edges and the legend.
 *
 * Filter/focus selection is held in a module-level store keyed by the network's
 * CONTENT signature, not the envelope object identity — so the 2s session-poll
 * (which hands us a fresh-but-content-identical envelope, and can briefly remount
 * this subtree) never wipes the operator's selection mid-glance.
 */
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  MarkerType,
  useReactFlow,
  useNodesState,
  useEdgesState,
  type Edge,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { alpha } from '@mui/material/styles';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import type { ProjectArtifactModelEnvelope } from '../../contracts/types';
import {
  toNetworkView,
  type FloatBand,
  type NetworkView as NetworkViewModel,
} from '../../contracts/projectAdapters';
import type { BuildStatus } from '../../contracts/constructionAdapters';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { ComputedBadge, ComputedLegend } from './computed';
import { SectionHeading } from './SectionHeading';
import { NetworkSummaryStrip } from './NetworkSummaryStrip';
import { NetworkNode, type NetworkNodeData } from './NetworkNode';
import { bandTokens, BAND_LABEL } from './bandTokens';

const nodeTypes = { network: NetworkNode };

const COL_W = 284;
// Rows are packed close (a two-line node plus a ~24px gutter): this network's widest
// layer stacks ~20 activities in one column, and at the old 124px pitch fitView
// had to zoom to ~0.25 to frame it, which left every label unreadable.
const ROW_H = 84;
const LEFT_PAD = 28;
const TOP_PAD = 28;
// Explicit node box dimensions (match NetworkNode's rendered card) so the MiniMap
// can draw rects — it skips custom nodes whose width/height aren't in the store.
const NODE_W = 232;
const NODE_H = 60;

const NETWORK_SOURCE = 'Project Network · CPM';

// Band sort order for within-column stacking and legend rows (critical loudest).
const BAND_ORDER: Record<FloatBand, number> = { critical: 0, red: 1, yellow: 2, green: 3 };

/**
 * The active filter. The toolbar offers All · Critical path · Near-critical; the
 * legend swatches add the four per-band filters (a FloatBand value). "near" and
 * the "red" band select the same population by the absolute bands, so the toolbar
 * carries "near" and the legend's red swatch is the discoverable equivalent.
 */
type FilterMode = 'all' | 'critical' | 'near' | FloatBand;

const TOOLBAR_MODES: readonly Extract<FilterMode, 'all' | 'critical' | 'near'>[] = [
  'all',
  'critical',
  'near',
];

const TOOLBAR_LABELS: Record<'all' | 'critical' | 'near', string> = {
  all: 'All',
  critical: 'Critical path',
  near: 'Near-critical',
};

// ---------------------------------------------------------------------------
// Selection persistence — survives the 2s poll's envelope-identity churn.
// ---------------------------------------------------------------------------

interface Selection {
  /** The content signature this selection belongs to (see signatureOf). */
  signature: string;
  filter: FilterMode;
  focusId: string | null;
  /** The selected node id — persisted here (not just the RF store) so it survives
   *  the 2s poll's remount, which otherwise wipes xyflow's internal selection. */
  selectedId: string | null;
}

/**
 * A content signature for the network: stable across a fresh-but-identical
 * envelope, changing only when the actual activity set / dependencies change. We
 * key the persisted selection on this so switching to a genuinely different
 * network resets, but a poll-driven re-fetch of the SAME network does not.
 */
function signatureOf(view: NetworkViewModel): string {
  return `${String(view.nodes.length)}:${String(view.edges.length)}:${view.criticalPath.join(',')}`;
}

interface StoredSelection {
  filter: FilterMode;
  focusId: string | null;
  selectedId: string | null;
}

const selectionStore = new Map<string, StoredSelection>();

function loadSelection(signature: string): Selection {
  const stored: StoredSelection = selectionStore.get(signature) ?? {
    filter: 'all',
    focusId: null,
    selectedId: null,
  };
  return { signature, ...stored };
}

function build(
  view: NetworkViewModel,
  t: Tokens,
  onFocus: (id: string) => void,
  statusFor?: (id: string) => { status: BuildStatus; active: boolean }
): { nodes: Node[]; edges: Edge[] } {
  // Band now comes straight from the server (per node); never re-derived here.
  const bandById = new Map<string, FloatBand>();
  for (const n of view.nodes) bandById.set(n.id, n.band);

  // Lay nodes out column-by-column (topological depth). Within each column, sort by
  // band (critical → red → yellow → green) so the slack gradient reads top-down.
  const byCol = new Map<number, NetworkViewModel['nodes']>();
  for (const n of view.nodes) {
    const col = byCol.get(n.col) ?? [];
    col.push(n);
    byCol.set(n.col, col);
  }
  const yByNode = new Map<string, number>();
  for (const [col, colNodes] of byCol) {
    const sorted = [...colNodes].sort((a, b) => {
      const ba = BAND_ORDER[a.band];
      const bb = BAND_ORDER[b.band];
      return ba !== bb ? ba - bb : a.id.localeCompare(b.id);
    });
    sorted.forEach((n, row) => {
      yByNode.set(n.id, TOP_PAD + row * ROW_H);
    });
    byCol.set(col, sorted);
  }

  const nodes: Node[] = view.nodes.map((n) => {
    const s = statusFor?.(n.id);
    return {
      id: n.id,
      type: 'network',
      // The focusable node wrapper's accessible name. Without it a keyboard or
      // screen-reader user heard the card's concatenated text
      // ("C-artifact-access10d10djunior-developer") — two bare "10d"s with no
      // way to tell duration from float.
      ariaLabel: nodeAriaLabel(n),
      position: { x: LEFT_PAD + n.col * COL_W, y: yByNode.get(n.id) ?? TOP_PAD },
      width: NODE_W,
      height: NODE_H,
      data: {
        activityId: n.id,
        kind: n.kind,
        label: n.label,
        days: n.days,
        workerClass: n.workerClass,
        float: n.float,
        onCriticalPath: n.onCriticalPath,
        coding: n.coding,
        band: n.band,
        source: NETWORK_SOURCE,
        jsonPath: `$.dependencies[activity=${n.id}]`,
        onFocus,
        ...(n.isPublic !== undefined ? { isPublic: n.isPublic } : {}),
        ...(s !== undefined ? { nodeStatus: s.status, nodeActive: s.active } : {}),
      } satisfies NetworkNodeData,
      draggable: false,
      zIndex: 2,
    };
  });

  const edges: Edge[] = view.edges.map((e, i) => {
    const onCp = e.onCriticalPath;
    // Non-CP edges adopt their TARGET node's band colour at reduced saturation, so
    // the graph reads its slack gradient; CP edges stay loudest (criticalFg, animated).
    const targetBand = bandById.get(e.to) ?? 'green';
    const stroke = onCp ? t.criticalFg : alpha(bandTokens(t, targetBand).fg, 0.5);
    return {
      id: `${e.from}-${e.to}-${String(i)}`,
      source: e.from,
      target: e.to,
      type: 'smoothstep',
      animated: onCp,
      style: { stroke, strokeWidth: onCp ? 3 : 1.25 },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: stroke,
        width: onCp ? 22 : 14,
        height: onCp ? 22 : 14,
      },
      zIndex: onCp ? 3 : 1,
    };
  });

  return { nodes, edges };
}

/** The spoken description of one network node (activity or milestone). */
function nodeAriaLabel(n: NetworkViewModel['nodes'][number]): string {
  if (n.kind === 'milestone') {
    return `Milestone ${n.id}: ${n.label}, ${n.isPublic === true ? 'public' : 'internal'}${n.onCriticalPath ? ', on the critical path' : ''}`;
  }
  const who = n.workerClass.length > 0 ? `, ${n.workerClass}` : '';
  return n.onCriticalPath
    ? `${n.id}: ${String(n.days)} days of work, zero float, on the critical path${who}`
    : `${n.id}: ${String(n.days)} days of work, ${String(n.float)} days of float${n.band === 'red' ? ', near-critical' : ''}${who}`;
}

/** Builds predecessor/successor adjacency from the view edges for subgraph BFS. */
function adjacency(view: NetworkViewModel): {
  preds: Map<string, string[]>;
  succs: Map<string, string[]>;
} {
  const preds = new Map<string, string[]>();
  const succs = new Map<string, string[]>();
  for (const n of view.nodes) {
    preds.set(n.id, []);
    succs.set(n.id, []);
  }
  for (const e of view.edges) {
    succs.get(e.from)?.push(e.to);
    preds.get(e.to)?.push(e.from);
  }
  return { preds, succs };
}

/** The ancestors+descendants of `id` (inclusive) — the focusable subgraph. */
function subgraphOf(id: string, view: NetworkViewModel): Set<string> {
  const { preds, succs } = adjacency(view);
  const keep = new Set<string>([id]);
  const walk = (start: string, adj: Map<string, string[]>): void => {
    const stack = [start];
    while (stack.length > 0) {
      const cur = stack.pop();
      if (cur === undefined) break;
      for (const next of adj.get(cur) ?? []) {
        if (!keep.has(next)) {
          keep.add(next);
          stack.push(next);
        }
      }
    }
  };
  walk(id, preds);
  walk(id, succs);
  return keep;
}

/** Whether a node passes the active toolbar/legend filter (band is server-sent). */
function passesFilter(n: NetworkViewModel['nodes'][number], mode: FilterMode): boolean {
  if (mode === 'all') return true;
  if (mode === 'critical') return n.onCriticalPath;
  if (mode === 'near') return n.band === 'red';
  return n.band === mode;
}

export function NetworkView({
  networkEnvelope,
  activityEnvelope,
  height = 640,
  showSummary = true,
  showComputedLegend = true,
  statusFor,
  statusSig,
  onSelect,
}: {
  networkEnvelope: ProjectArtifactModelEnvelope | undefined;
  activityEnvelope: ProjectArtifactModelEnvelope | undefined;
  height?: number;
  /** Render the 4-tile CPM summary strip above the graph (default true). */
  showSummary?: boolean;
  /** Render the computed-vs-authored legend above the graph (default true). */
  showComputedLegend?: boolean;
  /** Construction lens: per-node build status (colours the node on top of band). */
  statusFor?: (id: string) => { status: BuildStatus; active: boolean };
  /** A signature of the status map so the graph rebuilds when build state changes. */
  statusSig?: string;
  /** Extra callback when a node is selected (construction surfaces a detail panel). */
  onSelect?: (id: string) => void;
}): ReactNode {
  const t = useTokens();
  const view = useMemo(
    () => toNetworkView(networkEnvelope, activityEnvelope),
    [networkEnvelope, activityEnvelope]
  );

  // Persist selection by content signature so a poll-driven refetch (new envelope
  // identity, even a brief remount) restores the operator's filter/focus instead of
  // snapping back to "All". A genuinely different network gets a fresh signature.
  const signature = useMemo(() => signatureOf(view), [view]);
  const [selection, setSelection] = useState<Selection>(() => loadSelection(signature));

  // Adjust state during render when the network identity (signature) changes — the
  // React-sanctioned alternative to a setState-in-effect (the previous signature is
  // tracked in state itself, not a ref). A fresh-but-identical envelope from the 2s
  // poll keeps the SAME signature, so the operator's selection survives untouched.
  if (selection.signature !== signature) {
    setSelection(loadSelection(signature));
  }

  const applySelection = useCallback(
    (next: StoredSelection) => {
      selectionStore.set(signature, next);
      setSelection({ signature, ...next });
    },
    [signature]
  );

  // A filter/focus change deselects any node (next pass loads selectedId: null).
  const applyFilterFocus = useCallback(
    (next: { filter: FilterMode; focusId: string | null }) => {
      applySelection({ ...next, selectedId: null });
    },
    [applySelection]
  );

  const filter = selection.signature === signature ? selection.filter : 'all';
  const focusId = selection.signature === signature ? selection.focusId : null;
  const selectedId = selection.signature === signature ? selection.selectedId : null;

  // Node click → persist selectedId, preserving the CURRENT filter/focus. We read
  // them from the store at call time (not from a closure), because a select-change
  // can fire synchronously right after onFocus — before the focus state has
  // committed — and a stale closure would clobber focusId back to null.
  const onSelectNode = useCallback(
    (id: string | null) => {
      const cur = loadSelection(signature);
      applySelection({ filter: cur.filter, focusId: cur.focusId, selectedId: id });
      if (id !== null) onSelect?.(id);
    },
    [applySelection, signature, onSelect]
  );

  const onFocus = useCallback(
    (id: string) => {
      // Keep the focused node SELECTED (selectedId: id). Clearing it here caused a
      // deselect→reselect churn: the reconcile deselected the node, xyflow re-emitted
      // a select for the still-focused DOM node, and that select-change overwrote
      // focusId right back to null. Keeping it selected makes focus atomic.
      applySelection({ filter: 'all', focusId: id, selectedId: id });
    },
    [applySelection]
  );

  // Key the built graph on the content SIGNATURE, not the `view` object identity:
  // the 2s poll hands us a fresh-but-identical `view` every cycle, and rebuilding
  // the nodes array each time would re-fire the canvas reconcile and clobber
  // xyflow's live selection. Same signature → same array identity → selection (and
  // the NodeToolbar / Focus-subgraph / Comment) survive the poll untouched.
  // Keyed on signature + statusSig: signature is the stable proxy for `view`
  // content (so identical polls don't churn selection), and statusSig rebuilds the
  // nodes when the construction build-status changes. A rebuild still preserves the
  // controlled selection (carried by selectedId), so neither breaks the other.

  const { nodes, edges } = useMemo(
    () => build(view, t, onFocus, statusFor),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on signature (the stable proxy for view content) instead of view itself, so identical polls don't rebuild and clobber xyflow's live selection (see the comment block above)
    [signature, statusSig, t, onFocus, statusFor]
  );

  // The visible set: node-focus (a subgraph) wins; else the toolbar/legend filter.
  // Also keyed on signature (not `view`) so it is stable across content-equal polls.
  const visibleIds = useMemo(() => {
    if (focusId !== null) return subgraphOf(focusId, view);
    const set = new Set<string>();
    for (const n of view.nodes) if (passesFilter(n, filter)) set.add(n.id);
    return set;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- signature is the stable proxy for `view` content
  }, [signature, filter, focusId]);

  const shownCount = visibleIds.size;
  const totalCount = view.nodes.length;

  // The critical path in EXECUTION order. The server lists it end-first
  // (C-billing-manager → … → requirements), which read as the project running
  // backwards; earliest start puts it the way the work actually flows.
  const startOf = new Map(view.nodes.map((n) => [n.id, n.earlyStart]));
  const criticalPath = [...view.criticalPath].sort(
    (a, b) => (startOf.get(a) ?? 0) - (startOf.get(b) ?? 0)
  );
  const activityCount = view.nodes.filter((n) => n.kind === 'activity').length;

  if (view.nodes.length === 0) {
    return (
      <Paper sx={{ p: 3 }}>
        <SectionHeading
          hint="The project network appears once the activities and their dependencies are drafted: it orders the work, finds the critical path, and shows how much each activity can slip (its float)."
          t={t}
        >
          Project network — not drafted yet
        </SectionHeading>
      </Paper>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <SectionHeading
        adornment={<ComputedBadge t={t} />}
        hint="The order the work must happen in. Each box is an activity, each arrow a dependency, each diamond a milestone. Float is how many days an activity can slip without delaying the project; zero float means it is on the critical path."
        t={t}
      >
        Project network
      </SectionHeading>
      {showComputedLegend ? <ComputedLegend t={t} /> : null}

      {showSummary ? <NetworkSummaryStrip view={view} /> : null}

      <Paper sx={{ p: 1.25, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 1.5 }}>
        <ToggleButtonGroup
          exclusive
          aria-label="Filter activities by float criticality"
          size="small"
          // A per-band legend filter shows here as "none selected" so the band-state
          // is owned by the legend, not duplicated in the toolbar.
          value={filter === 'all' || filter === 'critical' || filter === 'near' ? filter : null}
          onChange={(_e, next: 'all' | 'critical' | 'near' | null) => {
            if (next === null) return;
            applyFilterFocus({ filter: next, focusId: null });
          }}
        >
          {TOOLBAR_MODES.map((m) => (
            <ToggleButton
              key={m}
              sx={{ fontFamily: t.mono, fontSize: 11, textTransform: 'none', py: 0.4 }}
              value={m}
            >
              {TOOLBAR_LABELS[m]}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>

        {/* Milestone filters: each focuses the milestone's bounded subnetwork
            (reusing the node-focus subgraph machinery). Diamond = milestone. The
            chip says the milestone's NAME, not just its id: "M2" alone told a
            reader nothing. */}
        <Typography
          id="network-milestones-label"
          sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted, ml: 0.5 }}
        >
          Show work up to a milestone:
        </Typography>
        {view.milestones.map((m) => (
          <Chip
            clickable
            aria-label={`Focus milestone ${m.id}, ${m.name}`}
            aria-pressed={focusId === m.id}
            icon={
              <Box
                aria-hidden
                sx={{
                  width: 9,
                  height: 9,
                  transform: 'rotate(45deg)',
                  ml: 0.5,
                  bgcolor: m.isPublic ? (m.onCriticalPath ? t.criticalFg : t.muted) : 'transparent',
                  border: `1.5px solid ${m.onCriticalPath ? t.criticalFg : t.muted}`,
                }}
              />
            }
            key={m.id}
            label={`${m.id} · ${m.name}`}
            size="small"
            sx={{
              fontFamily: t.mono,
              fontSize: 11,
              ...(focusId === m.id ? { borderColor: t.accent, color: t.accentInk } : {}),
            }}
            variant="outlined"
            onClick={() => {
              applyFilterFocus({ filter: 'all', focusId: m.id });
            }}
          />
        ))}

        {focusId !== null && (
          <Chip
            aria-label={`Clear focus on ${focusId}`}
            label={`Showing only ${focusId} and its dependencies`}
            size="small"
            sx={{ fontFamily: t.mono, fontSize: 11 }}
            onDelete={() => {
              applyFilterFocus({ filter: 'all', focusId: null });
            }}
          />
        )}

        {(filter === 'red' || filter === 'yellow' || filter === 'green' || filter === 'critical') &&
          focusId === null && (
            <Chip
              aria-label={`Clear ${filter} band filter`}
              label={`Float band: ${BAND_LABEL[filter]}`}
              size="small"
              sx={{ fontFamily: t.mono, fontSize: 11 }}
              onDelete={() => {
                applyFilterFocus({ filter: 'all', focusId: null });
              }}
            />
          )}

        <Box sx={{ flexGrow: 1 }} />
        <Typography aria-live="polite" sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          {`${String(shownCount)} of ${String(totalCount)} nodes shown (${String(activityCount)} activities, ${String(view.milestones.length)} milestones)`}
        </Typography>
      </Paper>

      <Paper sx={{ p: 1.25, display: 'flex', flexDirection: 'column', gap: 1.25 }}>
        <Box
          aria-label={`Project network diagram: ${String(activityCount)} activities, ${String(view.edges.length)} dependencies, ${String(view.milestones.length)} milestones. Critical path: ${criticalPath.join(', ')}. Tab to each node to hear its duration and float; the activity list above has the same activities as a list.`}
          role="region"
          sx={{
            height,
            width: '100%',
            position: 'relative',
            border: `1.5px solid ${t.line}`,
            borderRadius: t.radius / 8 + 0.5,
            bgcolor: t.bg,
            // Keyboard users focus graph nodes via react-flow's own tab order;
            // give them a visible focus ring (the default UA outline is suppressed
            // by react-flow's node styling).
            '& .react-flow__node:focus-visible': {
              outline: `2px solid ${t.accent}`,
              outlineOffset: 2,
            },
            // React Flow's zoom controls ship light-only (white buttons, dark
            // glyphs): on the dark themes they were white squares with invisible
            // icons. Theme them, and give them a usable 36px target.
            // At phone width the minimap covered a third of a 358px canvas.
            '& .react-flow__minimap': { display: { xs: 'none', sm: 'block' } },
            '& .react-flow__controls': { boxShadow: 'none', border: `1.5px solid ${t.line}` },
            '& .react-flow__controls-button': {
              width: 36,
              height: 36,
              bgcolor: t.paper,
              color: t.ink,
              borderBottom: `1px solid ${t.line}`,
              '&:hover': { bgcolor: t.paperAlt },
              '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: -2 },
              '& svg': { fill: t.ink, maxWidth: 14, maxHeight: 14 },
            },
          }}
        >
          <ReactFlowProvider>
            <NetworkCanvas
              edges={edges}
              nodes={nodes}
              selectedId={selectedId}
              t={t}
              visibleIds={visibleIds}
              onSelectNode={onSelectNode}
            />
          </ReactFlowProvider>
        </Box>
        {/* The key lives OUTSIDE the canvas and is always open: collapsed inside
            the graph as a 9px "KEY" toggle it was the one thing that explains the
            colours, and almost nobody found it. */}
        <NetworkKey
          activeBand={
            filter === 'red' || filter === 'yellow' || filter === 'green' || filter === 'critical'
              ? filter
              : null
          }
          t={t}
          onBandFilter={(band) => {
            applyFilterFocus({ filter: band, focusId: null });
          }}
        />
      </Paper>

      {criticalPath.length > 0 && (
        <Paper sx={{ p: 2, bgcolor: t.awaitingBg, border: `1.5px solid ${t.line}` }}>
          <Box sx={{ mb: 1 }}>
            <SectionHeading color={t.awaitingFg} level={4} t={t}>
              Critical path, in order
            </SectionHeading>
          </Box>
          <Box
            component="ol"
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: 0.5,
              listStyle: 'none',
              m: 0,
              p: 0,
            }}
          >
            {criticalPath.map((id, i, arr) => (
              <Box component="li" key={id} sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                <Box
                  sx={{
                    fontFamily: t.mono,
                    fontWeight: 700,
                    fontSize: 11,
                    // A critical-path mark: the ONE critical colour (designer
                    // palette ruling), never the accent.
                    color: t.criticalText,
                    bgcolor: t.criticalFg,
                    border: `1.5px solid ${t.criticalFg}`,
                    borderRadius: t.radius / 8 + 0.5,
                    px: 0.75,
                    py: 0.25,
                  }}
                >
                  {id}
                </Box>
                {i < arr.length - 1 && (
                  <Typography aria-hidden sx={{ color: t.criticalFg, fontWeight: 700 }}>
                    →
                  </Typography>
                )}
              </Box>
            ))}
          </Box>
          <Typography
            sx={{ fontFamily: t.body, fontSize: 12.5, color: t.awaitingFg, opacity: 0.9, mt: 1.25 }}
          >
            The chain of activities with zero float, drawn as thick animated arrows in the diagram —
            any slip here slips the whole project.
          </Typography>
        </Paper>
      )}
    </Box>
  );
}

/**
 * The graph canvas — lives inside ReactFlowProvider so it can call fitView() after
 * the visible set changes. Hides (not dims) filtered-out nodes+edges, then refits.
 * minZoom is intentionally tiny so fitView() can actually frame the full column-
 * stacked network (126 nodes need ~0.05 to fit a small pane).
 */
function NetworkCanvas({
  nodes,
  edges,
  visibleIds,
  selectedId,
  t,
  onSelectNode,
}: {
  nodes: Node[];
  edges: Edge[];
  visibleIds: Set<string>;
  selectedId: string | null;
  t: Tokens;
  onSelectNode: (id: string | null) => void;
}): ReactNode {
  const { fitView } = useReactFlow();

  // Controlled node/edge state. Node SELECTION is driven by `selectedId`, which the
  // parent persists in its signature-keyed store — so it survives the 2s poll's
  // remount that wipes xyflow's internal selection. The reconcile sets `selected`
  // from `selectedId` on every rebuild; clicks are reported up via onSelectionChange.
  const [rfNodes, setRfNodes, onNodesChange] = useNodesState([] as Node[]);
  const [rfEdges, setRfEdges, onEdgesChange] = useEdgesState([] as Edge[]);

  // Reconcile when the rebuilt nodes, the visible set, or the persisted selection
  // change: fresh position/data/dimensions, hidden from visibleIds, selected from
  // the persisted selectedId (NOT from the RF store, which the poll/remount wipes).
  useEffect(() => {
    setRfNodes(
      nodes.map((n) => ({
        ...n,
        hidden: !visibleIds.has(n.id),
        selected: n.id === selectedId,
      }))
    );
  }, [nodes, visibleIds, selectedId, setRfNodes]);

  useEffect(() => {
    setRfEdges(
      edges.map((e) => ({ ...e, hidden: !(visibleIds.has(e.source) && visibleIds.has(e.target)) }))
    );
  }, [edges, visibleIds, setRfEdges]);

  // Refit whenever the visible set changes so the focused subgraph fills the frame.
  useEffect(() => {
    if (visibleIds.size === 0) return;
    const id = window.setTimeout((): void => {
      void fitView({ padding: 0.12, maxZoom: 1 });
    }, 0);
    return (): void => {
      window.clearTimeout(id);
    };
  }, [visibleIds, fitView]);

  // Selection is CONTROLLED by the persisted selectedId (the reconcile sets each
  // node's `selected`). We capture user INTENT one-way: onNodeClick (mouse) and a
  // wrapped onNodesChange that watches for xyflow `select` changes (keyboard
  // Enter/Space). Both just report the id UP to the persisted store; the reconcile
  // is the single writer of `selected`, so there is no onSelectionChange feedback
  // loop. onPaneClick clears.
  const handleNodeClick = useCallback(
    (_e: unknown, node: Node) => {
      onSelectNode(node.id);
    },
    [onSelectNode]
  );
  const handlePaneClick = useCallback(() => {
    onSelectNode(null);
  }, [onSelectNode]);
  const handleNodesChange = useCallback(
    (changes: Parameters<typeof onNodesChange>[0]) => {
      const sel = changes.find((c) => c.type === 'select' && c.selected);
      if (sel !== undefined && 'id' in sel) onSelectNode(sel.id);
      onNodesChange(changes);
    },
    [onNodesChange, onSelectNode]
  );

  // Minimap fill: solid band colour per node so the heat-map actually draws.
  const bandColor = useCallback(
    (n: Node): string => bandTokens(t, (n.data as NetworkNodeData).band).fg,
    [t]
  );

  if (visibleIds.size === 0) {
    return (
      <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 13, color: t.muted }}>
          No activities match this focus.
        </Typography>
      </Box>
    );
  }

  return (
    <ReactFlow
      elementsSelectable
      fitView
      edges={rfEdges}
      // Edges are drawn, not operated: as tab stops they put ~60 "Edge from X to
      // Y" stops between the toolbar and the nodes. Each node's label already
      // carries its float; the dependency list is the activity list's job.
      edgesFocusable={false}
      fitViewOptions={{ padding: 0.12, maxZoom: 1 }}
      maxZoom={1.5}
      minZoom={0.05}
      nodeTypes={nodeTypes}
      nodes={rfNodes}
      nodesConnectable={false}
      nodesDraggable={false}
      proOptions={{ hideAttribution: true }}
      onEdgesChange={onEdgesChange}
      onNodeClick={handleNodeClick}
      onNodesChange={handleNodesChange}
      onPaneClick={handlePaneClick}
    >
      <Background color={t.line} gap={22} size={1} />
      <Controls showInteractive={false} />
      <MiniMap
        pannable
        zoomable
        bgColor={t.paper}
        maskColor={alpha(t.bg, 0.6)}
        nodeColor={bandColor}
        nodeStrokeWidth={3}
        position="bottom-right"
        style={{ border: `1px solid ${t.line}` }}
      />
    </ReactFlow>
  );
}

function BandSwatch({
  t,
  band,
  label,
  active,
  onClick,
}: {
  t: Tokens;
  band: FloatBand;
  label: string;
  active: boolean;
  onClick: () => void;
}): ReactNode {
  const { fg } = bandTokens(t, band);
  return (
    <Box
      aria-label={`Show only ${label}`}
      aria-pressed={active}
      component="button"
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 0.75,
        minHeight: 32,
        px: 1,
        py: 0.25,
        bgcolor: active ? alpha(fg, 0.16) : 'transparent',
        border: `1px solid ${active ? fg : t.line}`,
        borderRadius: '4px',
        cursor: 'pointer',
        textAlign: 'left',
        color: t.ink,
        '&:hover': { bgcolor: alpha(fg, 0.1) },
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
      onClick={onClick}
    >
      <Box
        sx={{
          width: 18,
          height: 12,
          flexShrink: 0,
          bgcolor: t.paperAlt,
          border: `1px solid ${t.line}`,
          borderLeft: `5px solid ${fg}`,
          borderRadius: '2px',
        }}
      />
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>{label}</Typography>
    </Box>
  );
}

function KeyRow({ t, swatch, label }: { t: Tokens; swatch: ReactNode; label: string }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      {swatch}
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>{label}</Typography>
    </Box>
  );
}

function KeyGroup({
  t,
  title,
  children,
}: {
  t: Tokens;
  title: string;
  children: ReactNode;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.6, minWidth: 0 }}>
      <Typography
        component="h4"
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 10.5,
          letterSpacing: '0.1em',
          textTransform: 'uppercase',
          color: t.muted,
          m: 0,
        }}
      >
        {title}
      </Typography>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 1 }}>{children}</Box>
    </Box>
  );
}

const DIAMOND_SX = { width: 10, height: 10, transform: 'rotate(45deg)', flexShrink: 0 } as const;

/**
 * The diagram's key: float bands (each doubling as a filter), edge styles,
 * milestone glyphs and the coding/noncoding node glyph. Always visible under
 * the canvas — see the call site.
 */
function NetworkKey({
  t,
  activeBand,
  onBandFilter,
}: {
  t: Tokens;
  activeBand: FloatBand | null;
  onBandFilter: (band: FloatBand) => void;
}): ReactNode {
  const bands: { band: FloatBand; label: string }[] = [
    { band: 'critical', label: 'Critical · 0 days float' },
    { band: 'red', label: 'Near-critical · 1–5 days' },
    { band: 'yellow', label: '6–25 days float' },
    { band: 'green', label: '26+ days float' },
  ];
  return (
    <Box
      aria-label="Diagram key"
      component="section"
      sx={{
        display: 'flex',
        flexWrap: 'wrap',
        columnGap: 3,
        rowGap: 1.5,
        px: 0.5,
      }}
    >
      <KeyGroup t={t} title="Float · left edge colour · click to filter">
        {bands.map((b) => (
          <BandSwatch
            active={activeBand === b.band}
            band={b.band}
            key={b.band}
            label={b.label}
            t={t}
            onClick={() => {
              onBandFilter(b.band);
            }}
          />
        ))}
      </KeyGroup>

      <KeyGroup t={t} title="Arrows">
        <KeyRow
          label="critical path"
          swatch={<Box sx={{ width: 18, flexShrink: 0, borderTop: `3px solid ${t.criticalFg}` }} />}
          t={t}
        />
        <KeyRow
          label="dependency"
          swatch={<Box sx={{ width: 18, flexShrink: 0, borderTop: `1.5px solid ${t.muted}` }} />}
          t={t}
        />
      </KeyGroup>

      <KeyGroup t={t} title="Nodes">
        <KeyRow
          label="public milestone (filled · shown to management)"
          swatch={
            <Box sx={{ ...DIAMOND_SX, bgcolor: t.muted, border: `1.5px solid ${t.muted}` }} />
          }
          t={t}
        />
        <KeyRow
          label="internal milestone (hollow)"
          swatch={<Box sx={{ ...DIAMOND_SX, border: `1.5px solid ${t.muted}` }} />}
          t={t}
        />
        <KeyRow
          label="coding activity"
          swatch={
            <Box
              sx={{
                width: 8,
                height: 8,
                flexShrink: 0,
                borderRadius: '2px',
                bgcolor: t.accent2,
                border: `1.5px solid ${t.accent2}`,
              }}
            />
          }
          t={t}
        />
        <KeyRow
          label="noncoding activity"
          swatch={
            <Box
              sx={{
                width: 8,
                height: 8,
                flexShrink: 0,
                borderRadius: '50%',
                border: `1.5px solid ${t.muted}`,
              }}
            />
          }
          t={t}
        />
      </KeyGroup>
    </Box>
  );
}
