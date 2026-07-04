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
import { useEffect, useMemo, type ReactNode } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  useReactFlow,
  type Edge,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import Box from '@mui/material/Box';
import type { UseCaseView } from '../../api/adapters';
import { ActivityNode } from './ActivityNode';
import { SwimlaneBackground } from './SwimlaneBackground';
import { activityNodeAnchor } from '../comments/CommentContext';
import { laneColors, laneBand } from './laneColors';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { layoutActivity, isBackEdge, HEADER_HEIGHT } from './activityLayout';

const nodeTypes = { activity: ActivityNode, swimlane: SwimlaneBackground };

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
  hl: ActivityHighlight | undefined
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

  const edges: Edge[] = uc.edges.map((e) => {
    const dashed = e.kind === 'guardedFlow' || isBackEdge(layout, e.from, e.to);
    const onPath = hl?.visitedEdges.has(`${e.from}-${e.to}`) ?? false;
    const dim = hl !== undefined && !onPath;
    const stroke = onPath ? t.accent : dashed ? t.accent2 : t.muted;
    return {
      id: `${e.from}-${e.to}`,
      source: e.from,
      target: e.to,
      ...(e.guard.length > 0 ? { label: e.guard } : {}),
      type: 'smoothstep',
      zIndex: onPath ? 3 : 2,
      style: {
        stroke,
        strokeWidth: onPath ? 2.75 : 1.5,
        strokeDasharray: dashed && !onPath ? '5 4' : undefined,
        opacity: dim ? 0.2 : 1,
      },
      labelStyle: { fontFamily: t.mono, fontSize: 10, fontWeight: 700, fill: t.ink },
      labelBgStyle: { fill: t.paper, fillOpacity: 0.95 },
      labelBgPadding: [5, 3] as [number, number],
      markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
    };
  });

  return { nodes: [...laneNodes, ...activityNodes], edges };
}

/**
 * Pans + zooms the canvas to center the current walkthrough node whenever it
 * changes (mirrors the contracts view centering on a clicked method). Lives as a
 * child of <ReactFlow> so it can use the flow hooks; a double-rAF waits for the
 * node to be measured before centering, else its dimensions read stale.
 */
function FocusNode({ nodeId }: { nodeId: string }): null {
  const { setCenter, getNode } = useReactFlow();
  useEffect(() => {
    if (nodeId === '') return undefined;
    let raf2 = 0;
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        const n = getNode(nodeId);
        if (n === undefined) return;
        const w = n.measured?.width ?? 0;
        const h = n.measured?.height ?? 0;
        void setCenter(n.position.x + w / 2, n.position.y + h / 2, {
          zoom: 1,
          duration: 400,
        });
      });
    });
    return (): void => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, [nodeId, setCenter, getNode]);
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
  const { nodes, edges } = useMemo(
    () => build(uc, useCaseIndex, t, highlight),
    [uc, useCaseIndex, t, highlight]
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
        edges={edges}
        fitView={highlight === undefined}
        fitViewOptions={{ padding: 0.18 }}
        key={uc.id}
        maxZoom={1.4}
        minZoom={0.3}
        nodeTypes={nodeTypes}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background color={t.line} gap={22} size={1} />
        <Controls showInteractive={false} />
        {highlight !== undefined && <FocusNode nodeId={highlight.current} />}
      </ReactFlow>
    </Box>
  );
}
