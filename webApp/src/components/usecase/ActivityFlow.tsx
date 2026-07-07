/**
 * One core use case's activity diagram, rendered with @xyflow/react as a real UML
 * activity diagram (PlantUML activity-new style). Bound to a UseCaseView
 * (adapters.toCoreUseCasesView) — typed ActivityNodeView[] (start / action /
 * decision / merge / fork / join / note / end …, each in a swim-lane) and
 * ActivityEdgeView[] (control / guarded flow, including branch + loop-back edges).
 * Nodes are laid out in swim-lane columns (one column per lane), with Y by graph
 * depth (longest-path rank) and parallel branches sharing a (lane, rank) fanned out
 * side-by-side within the lane — so a decision visibly forks into its guarded arms
 * and they reconverge at the merge. Layout math lives in activityLayout.ts; each
 * lane gets a faint background band + a role header. Guarded edges (and any
 * detected back-edge / loop) render dashed in the accent color. Selecting a node
 * arms a comment anchor. Derivation is memoized on (use case, theme).
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  useReactFlow,
  useNodesInitialized,
  type Edge,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import type { UseCaseView } from '../../contracts/adapters';
import { ActivityNode } from './ActivityNode';
import { ActivityEdge } from './ActivityEdge';
import { SwimlaneBackground } from './SwimlaneBackground';
import { activityNodeAnchor } from '../comments/CommentContext';
import { laneColors, laneBand } from './laneColors';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import {
  layoutActivity,
  isBackEdge,
  edgeHandles,
  nodeCenter,
  HEADER_HEIGHT,
} from './activityLayout';
import { NODE_DIMS } from './nodeDims';
import type { ActivityNodeKind } from '../../contracts/models';

const nodeTypes = { activity: ActivityNode, swimlane: SwimlaneBackground };
const edgeTypes = { activity: ActivityEdge };

const FIT_PADDING = 0.14;

// Per-step walkthrough focus: how tightly the camera zooms onto the current node
// and how long the glide takes. FOCUS_ZOOM sits below ReactFlow's maxZoom (1.4) so
// the ringed node reads large while its immediate neighborhood (the steps it flows
// from / to, and the adjacent lanes) stays on-canvas and legible — a "zoom to this
// step" move, not a claustrophobic crop.
const FOCUS_ZOOM = 0.95;
const FOCUS_DURATION = 500;

/**
 * Walkthrough highlight state: the current node, the set of visited node ids, and
 * the set of traversed edge keys (`${from}-${to}`). When present, the diagram
 * behaves as a "you-are-here" map — current node ringed, visited path emphasized,
 * everything else dimmed. Absent → the plain static diagram.
 */
export interface ActivityHighlight {
  current: string;
  visitedNodes: Set<string>;
  visitedEdges: Set<string>;
}

function build(
  uc: UseCaseView,
  useCaseIndex: number,
  t: Tokens,
  hl: ActivityHighlight | undefined,
  selectedId: string | null
): { nodes: Node[]; edges: Edge[] } {
  const colors = laneColors(t, uc.lanes);
  const layout = layoutActivity(uc);

  // Background band + header per lane, painted behind everything. Each lane is sized
  // to its own (variable) width and the shared canvas height.
  const laneNodes: Node[] = uc.lanes.map((lane, i) => ({
    id: `__lane__${lane}`,
    type: 'swimlane',
    position: { x: layout.laneX.get(lane) ?? 0, y: 0 },
    data: {
      lane,
      color: colors[lane] ?? t.muted,
      band: laneBand(t, i),
      width: layout.laneWidth.get(lane) ?? 0,
      height: layout.canvasHeight,
      headerHeight: HEADER_HEIGHT,
    },
    draggable: false,
    selectable: false,
    connectable: false,
    zIndex: 0,
    style: { zIndex: 0 },
  }));

  const activityNodes: Node[] = uc.nodes.map((n) => {
    const pos = layout.positions.get(n.id) ?? { x: 0, y: 0 };
    const isCurrent = hl?.current === n.id;
    const onPath = hl?.visitedNodes.has(n.id) ?? false;
    const dim = hl !== undefined && !isCurrent && !onPath;
    return {
      id: n.id,
      type: 'activity',
      position: { x: pos.x, y: pos.y },
      data: {
        label: n.label,
        lane: n.lane,
        kind: n.kind,
        color: colors[n.lane] ?? t.muted,
        source: `${uc.name} · activity diagram`,
        jsonPath: activityNodeAnchor(useCaseIndex, n.id),
        // Selection travels through data (controlled flow → xyflow selection is inert):
        // drives the NodeToolbar Comment button + accent ring. Click arms nothing now.
        isSelected: n.id === selectedId,
        // Walkthrough "you-are-here" current step — carries the ring AND a black-box
        // testid so the per-step camera move is assertable. Exactly one node has it.
        isCurrent,
      },
      draggable: false,
      zIndex: isCurrent ? 3 : 1,
      style: {
        zIndex: isCurrent ? 3 : 1,
        opacity: dim ? 0.25 : 1,
        ...(isCurrent ? { boxShadow: `0 0 0 3px ${t.accent}`, borderRadius: 6 } : {}),
        transition: 'opacity 120ms',
      },
    };
  });

  const kindOf = new Map(uc.nodes.map((n) => [n.id, n.kind]));

  const edges: Edge[] = uc.edges.map((e) => {
    const back = isBackEdge(layout, e.from, e.to);
    const dashed = e.kind === 'guardedFlow' || back;
    const onPath = hl?.visitedEdges.has(`${e.from}-${e.to}`) ?? false;
    const dim = hl !== undefined && !onPath;
    const stroke = onPath ? t.accent : dashed ? t.accent2 : t.muted;
    const s = nodeCenter(layout, e.from, kindOf.get(e.from) ?? 'action');
    const tgt = nodeCenter(layout, e.to, kindOf.get(e.to) ?? 'action');
    const { sourceHandle, targetHandle } = edgeHandles(s, tgt, back);
    return {
      id: `${e.from}-${e.to}`,
      source: e.from,
      target: e.to,
      sourceHandle,
      targetHandle,
      type: 'activity',
      data: { label: e.guard, dim, onPath },
      zIndex: onPath ? 3 : 2,
      style: {
        stroke,
        strokeWidth: onPath ? 2.75 : 1.5,
        strokeDasharray: dashed && !onPath ? '5 4' : undefined,
        opacity: dim ? 0.2 : 1,
      },
      markerEnd: { type: MarkerType.ArrowClosed, color: stroke, width: 16, height: 16 },
    };
  });

  return { nodes: [...laneNodes, ...activityNodes], edges };
}

/**
 * Camera controller for the diagram. Two behaviors, keyed off whether the diagram
 * is a walkthrough "you-are-here" map (`focusId` set) or the plain static view:
 *
 *  • Whole-graph fit — on mount and whenever the use case (`fitToken`) changes, once
 *    the nodes have actually been measured (React-Flow's own `fitView` prop fires on
 *    first paint, before the async-measured node sizes are known — which clipped the
 *    start node/first lane at the left edge). Waiting on `useNodesInitialized`
 *    guarantees the entire graph — every lane, start, terminal — is framed. This is
 *    the initial frame in BOTH modes and the ONLY camera move in the static view.
 *
 *  • Per-step focus (walkthrough only) — when the current step's node (`focusId`)
 *    changes, glide the camera to center on it at FOCUS_ZOOM (the founder-loved
 *    "zoom to next item"). The first step of a freshly-mounted graph is skipped so
 *    the initial whole-graph fit stands; every advance/back/rewind after that
 *    re-centers on the new current node. The ring + dim still carry "you are here";
 *    the camera move makes the current step readable without hunting for the ring.
 */
function AutoFit({ fitToken, focusId }: { fitToken: string; focusId: string | undefined }): null {
  const initialized = useNodesInitialized();
  const { fitView, getNode, setCenter } = useReactFlow();
  // The fitToken whose whole-graph fit has run — re-fit once per use case.
  const fittedToken = useRef<string | null>(null);
  // The focusId that was current when this use case first mounted. Compared BY VALUE
  // (not a consume-once flag) so the initial step is skipped robustly across effect
  // re-runs, while every later step still focuses.
  const initialFocus = useRef<{ token: string; id: string | undefined } | null>(null);

  // Whole-graph fit, once per use case, after the nodes are measured. React-Flow's
  // built-in `fitView` prop frames the graph on first paint (before async
  // measurement); this re-fit — gated on `useNodesInitialized` when it resolves —
  // corrects the start-node/first-lane clipping. It is the ONLY camera move in the
  // static (non-walkthrough) view.
  useEffect(() => {
    if (!initialized) return undefined;
    if (fittedToken.current === fitToken) return undefined;
    fittedToken.current = fitToken;
    const raf = requestAnimationFrame(() => {
      void fitView({ padding: FIT_PADDING, duration: 220 });
    });
    return (): void => {
      cancelAnimationFrame(raf);
    };
  }, [initialized, fitToken, fitView]);

  // Per-step focus in walkthrough mode: glide the camera onto the current step's
  // node. Driven purely off `focusId` (NOT `useNodesInitialized`, which does not
  // reliably resolve for the you-are-here map) — the retry loop waits for the target
  // node's measured rect instead.
  useEffect(() => {
    if (focusId === undefined || focusId.length === 0) return undefined;
    // Record and skip the initial step (already framed by the whole-graph fit) so the
    // overview stands on mount; re-runs on the same step are skipped by value too.
    if (initialFocus.current?.token !== fitToken) {
      initialFocus.current = { token: fitToken, id: focusId };
      return undefined;
    }
    if (initialFocus.current.id === focusId) return undefined;
    // The nodes array is rebuilt every step; the target node may not be in the store
    // in the first frame after the click, so retry across a few frames. The you-are-
    // here map's nodes are not always MEASURED (useNodesInitialized never resolves),
    // so the rect comes from the known per-kind NODE_DIMS at the node's laid-out
    // position rather than an (often-absent) measured rect.
    let raf = 0;
    let tries = 0;
    const focus = (): void => {
      const node = getNode(focusId);
      if (node === undefined) {
        if (tries++ < 20) raf = requestAnimationFrame(focus);
        return;
      }
      const dim = NODE_DIMS[(node.data as { kind?: ActivityNodeKind }).kind ?? 'action'];
      const w = node.measured?.width ?? node.width ?? dim.w;
      const h = node.measured?.height ?? node.height ?? dim.h;
      void setCenter(node.position.x + w / 2, node.position.y + h / 2, {
        zoom: FOCUS_ZOOM,
        duration: FOCUS_DURATION,
      });
    };
    raf = requestAnimationFrame(focus);
    return (): void => {
      cancelAnimationFrame(raf);
    };
  }, [fitToken, focusId, getNode, setCenter]);

  return null;
}

export function ActivityFlow({
  uc,
  useCaseIndex,
  height = 560,
  highlight,
}: {
  uc: UseCaseView;
  useCaseIndex: number;
  height?: number;
  /** When set, the diagram renders as a walkthrough "you-are-here" map. */
  highlight?: ActivityHighlight;
}): ReactNode {
  const t = useTokens();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const { nodes, edges } = useMemo(
    () => build(uc, useCaseIndex, t, highlight, selectedId),
    [uc, useCaseIndex, t, highlight, selectedId]
  );

  if (uc.nodes.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        This use case has no activity diagram.
      </Box>
    );
  }

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
        elementsSelectable
        fitView
        edgeTypes={edgeTypes}
        edges={edges}
        fitViewOptions={{ padding: FIT_PADDING }}
        key={uc.id}
        maxZoom={1.4}
        minZoom={0.2}
        nodeTypes={nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        // Nodes carry their own focusable, labeled inner element (ActivityNode);
        // xyflow wrapper focus is off so there is a single well-labeled tab stop.
        nodesFocusable={false}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_e, n) => {
          // Click SELECTS the step (reveals its Comment toolbar) — it no longer arms a
          // comment directly. Commenting is an explicit action: the toolbar button
          // (mouse) or Enter/'c' on the focused node (keyboard). Toggle off on re-click.
          setSelectedId((s) => (s === n.id ? null : n.id));
        }}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls showInteractive={false} />
        <AutoFit fitToken={uc.id} focusId={highlight?.current} />
      </ReactFlow>
    </Box>
  );
}
