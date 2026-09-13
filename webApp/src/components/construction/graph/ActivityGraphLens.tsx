/**
 * The GRAPH lens — the committed architecture, layer by layer, each component
 * carrying its activity's lifecycle (spec §7.6), in the spirit of Löwy's
 * Table 11-1: one coding activity per component, its dependencies the
 * architecture's call chains.
 *
 * This file composes; it decides nothing it could get wrong silently. Every
 * rule lives in a pinned `.ts` sibling:
 *
 *   activityGraphModel  which cards and lanes exist, hollow coverage, edge alarms
 *   graphEdges          hover-focus, and how each edge is drawn (alarms never hide)
 *   activityGraphLayout where every card goes — the same state, the same place
 *   laneSpine           what each lane's lifecycle says
 *   gateRibbon          what each milestone may and may not claim
 *   graphViewport       the viewport's memory across remounts, and the LOD rule
 *   graphPresentation   which token paints which state
 *
 * WHAT IT READS — the same values the LIST lens reads, so the lenses and the
 * detail pane can never disagree about one activity: the evidence-view tree
 * (`activities` — "Observed only" already applied), the toolbar-filtered tree
 * (`visible`), and the committed `system` and `network` slots.
 *
 * EDGES are the architecture's call chains, never labelled (labels live only
 * in a Dynamic step-through, founder convention). An ALARM edge (an upward
 * call, or a sideways one App C does not sanction) is red and is never hidden,
 * not even while hover-focus hides every other non-incident wire (R5: "never
 * hide it or route around it").
 *
 * FILTERS DIM, NEVER MOVE (Decision D4). The shared toolbar's scope / kind /
 * layer / search apply here as a mute on lanes they do not match. Positions are
 * a function of state alone, so nothing re-lays-out under the operator's eye.
 *
 * SELECTION lives in the URL (Stage B); clicking a lane selects its activity,
 * clicking a spine segment (at LOD-1) its lifecycle phase. The VIEWPORT is
 * restored from a signature-keyed module store on every remount (graphViewport,
 * the NetworkView.tsx pattern spec §7.6 makes mandatory); a deep-linked
 * selection with nothing stored frames its card once.
 */
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactElement,
} from 'react';
import type { Edge, Node } from '@xyflow/react';
import Box from '@mui/material/Box';

import type { ArtifactModelEnvelope, NetworkModel } from '../../../contracts/types';
import { toC4View } from '../../../contracts/adapters';
import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { FlowCanvas, FlowEmpty, FocusNodes } from '../../flow/flowShared';
import { GUTTER_W, NODE_H, flowEdge, layerColors } from '../../flow/flowLayout';
import type { ActivityNode } from '../list/activityTree';
import type { LensSelection } from '../lens/useLensSelection';
import {
  buildActivityGraphModel,
  type ActivityGraphModel,
  type GraphCard,
  type GraphRow,
} from './activityGraphModel';
import { cardSetFocusFor, edgePresentationFor, hoverFocusFor, type GraphFocus } from './graphEdges';
import { CARD_W, UTIL_PAD, layoutActivityGraph, type GraphLayout } from './activityGraphLayout';
import { laneSpineFor, type LaneSpine } from './laneSpine';
import { laneScheduleFor, maxEffortOf, type LaneSchedule } from './laneSchedule';
import type { M0Facts } from './m0Gate';
import { gateRibbonFor } from './gateRibbon';
import {
  CANVAS_MIN_PX,
  canvasHeightPx,
  graphMountFor,
  graphSignatureOf,
  saveGraphViewport,
} from './graphViewport';
import { graphNodeTypes } from './graphNodeTypes';
import type { GraphCardData } from './GraphNodes';
import { GateRibbon, GraphKey } from './GraphStrips';

/** Far enough out to fit the widest real row (ten ResourceAccess cards) at 1280. */
const MIN_ZOOM = 0.15;
/** Hover-leave debounce: sweeping between cards never flashes an un-muted frame. */
const LEAVE_MS = 90;
/** The alarm stroke — wider than any other edge, so it survives a crowded corridor. */
const ALARM_STROKE_WIDTH = 2.5;

export interface ActivityGraphLensProps {
  projectId: string;
  /** Every activity, as the evidence view reads it ("Observed only" applied). */
  activities: readonly ActivityNode[];
  /** The activities the shared toolbar's filters match. */
  visible: readonly ActivityNode[];
  /** The committed Phase-1 `system` slot. */
  systemEnvelope: ArtifactModelEnvelope | undefined;
  /** The committed `network` slot — its milestones and dependencies. */
  network: NetworkModel | undefined;
  selection: LensSelection;
  onSelect: (selection: LensSelection) => void;
  /** M0's facts from the project read (phase + SDP review staleness) — m0Gate.ts. */
  m0: M0Facts;
  /** Navigation only: the stale M0 approval's way back to the SDP review. */
  onOpenSdpReview?: () => void;
}

export function ActivityGraphLens({
  projectId,
  activities,
  visible,
  systemEnvelope,
  network,
  selection,
  onSelect,
  m0,
  onOpenSdpReview,
}: ActivityGraphLensProps): ReactElement {
  const t = useTokens();
  const c4 = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const model = useMemo(
    () =>
      buildActivityGraphModel({
        components: c4.components,
        relationships: c4.relationships,
        activities,
      }),
    [c4, activities]
  );
  const layout = useMemo(() => layoutActivityGraph(model), [model]);
  const spines = useMemo(
    (): Record<string, LaneSpine> =>
      Object.fromEntries(activities.map((a) => [a.activityId, laneSpineFor(a)])),
    [activities]
  );
  // Effort, float and critical path — from the ONE join both lenses read (the
  // tree's node fields). The spine scale is the widest effort in the WHOLE plan,
  // never the filtered set: filters dim, they never resize anything.
  const schedules = useMemo((): Record<string, LaneSchedule> => {
    const max = maxEffortOf(activities);
    return Object.fromEntries(activities.map((a) => [a.activityId, laneScheduleFor(a, max)]));
  }, [activities]);
  const ribbon = useMemo(
    () => gateRibbonFor(network?.milestones ?? [], network?.dependencies ?? [], activities),
    [network, activities]
  );
  const unmatched = useMemo((): ReadonlySet<string> => {
    const shown = new Set(visible.map((n) => n.activityId));
    return new Set(activities.filter((a) => !shown.has(a.activityId)).map((a) => a.activityId));
  }, [activities, visible]);
  const signature = useMemo(
    () =>
      graphSignatureOf(
        projectId,
        c4.components.map((c) => c.id),
        activities.map((a) => a.activityId)
      ),
    [projectId, c4, activities]
  );

  // A milestone hovered on the ribbon focuses its feeders (or, for M0, what it
  // gates) on the canvas — as card ids.
  const [ribbonHover, setRibbonHover] = useState<string | null>(null);
  const ribbonFocus = useMemo((): ReadonlySet<string> | null => {
    if (ribbonHover === null) return null;
    const m = ribbon.find((x) => x.id === ribbonHover);
    if (m === undefined) return null;
    const ids = m.feeders.length > 0 ? m.feeders : m.gates;
    return new Set(
      ids.map((id) => model.cardOfActivity[id]).filter((c): c is string => c !== undefined)
    );
  }, [ribbonHover, ribbon, model]);

  const onSelectLane = useCallback(
    (activityId: string, lifecyclePhase?: string): void => {
      onSelect({ activityId, ...(lifecyclePhase !== undefined ? { lifecyclePhase } : {}) });
    },
    [onSelect]
  );

  if (c4.components.length === 0) {
    return <FlowEmpty label="No committed architecture to draw yet." t={t} />;
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
      <GateRibbon
        m0={m0}
        nodes={activities}
        ribbon={ribbon}
        t={t}
        onHover={setRibbonHover}
        {...(onOpenSdpReview !== undefined ? { onOpenSdpReview } : {})}
      />
      <GraphKey model={model} t={t} />
      <GraphCanvas
        // A genuinely different architecture or plan starts from its own
        // remembered viewport (or a fit); the same one keeps where it was left.
        key={signature}
        layout={layout}
        model={model}
        ribbonFocus={ribbonFocus}
        schedules={schedules}
        selectedActivityId={selection.activityId}
        signature={signature}
        spines={spines}
        t={t}
        unmatched={unmatched}
        onSelectLane={onSelectLane}
      />
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The canvas
// ---------------------------------------------------------------------------

function GraphCanvas({
  signature,
  model,
  layout,
  spines,
  schedules,
  unmatched,
  selectedActivityId,
  ribbonFocus,
  t,
  onSelectLane,
}: {
  signature: string;
  model: ActivityGraphModel<ActivityNode>;
  layout: GraphLayout;
  spines: Readonly<Record<string, LaneSpine>>;
  schedules: Readonly<Record<string, LaneSchedule>>;
  unmatched: ReadonlySet<string>;
  selectedActivityId: string | undefined;
  ribbonFocus: ReadonlySet<string> | null;
  t: Tokens;
  onSelectLane: (activityId: string, lifecyclePhase?: string) => void;
}): ReactElement {
  // Read ONCE per signature: the viewport left on it, and — only when there is
  // none — the deep-linked card to frame. Neither re-fires on a later click, so
  // a selection never pans the canvas out from under the operator. Held in
  // state and re-read during render when the signature changes (the
  // useLensToolbar pattern); the parent also keys this component by signature,
  // so in practice it is read at mount.
  const [mount, setMount] = useState(() =>
    graphMountFor(signature, selectedActivityId, model.cardOfActivity)
  );
  if (mount.signature !== signature) {
    setMount(graphMountFor(signature, selectedActivityId, model.cardOfActivity));
  }
  const { stored, initialFocus } = mount;
  const initialFocusIds = useMemo(
    () => (initialFocus !== undefined ? [initialFocus] : []),
    [initialFocus]
  );

  // Hover-focus, debounced exactly as ArchitectureFlow's: moving between two
  // cards crosses empty canvas, and clearing at once would flash the whole
  // un-muted graph in between.
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelPendingLeave = (): void => {
    if (leaveTimer.current !== null) {
      clearTimeout(leaveTimer.current);
      leaveTimer.current = null;
    }
  };
  const enter = useCallback((n: { id: string; type?: string | undefined }): void => {
    cancelPendingLeave();
    if (n.type === 'graphCard') setHoveredId(n.id);
  }, []);
  const leave = useCallback((): void => {
    cancelPendingLeave();
    leaveTimer.current = setTimeout(() => {
      setHoveredId(null);
      leaveTimer.current = null;
    }, LEAVE_MS);
  }, []);
  useEffect(() => cancelPendingLeave, []);

  const focus = useMemo((): GraphFocus | null => {
    if (hoveredId !== null) return hoverFocusFor(hoveredId, model.edges);
    if (ribbonFocus !== null) return cardSetFocusFor(ribbonFocus);
    return null;
  }, [hoveredId, ribbonFocus, model]);

  const nodes = useMemo(
    () =>
      buildNodes({
        model,
        layout,
        spines,
        schedules,
        unmatched,
        selectedActivityId,
        focus,
        hoveredId,
        t,
        onSelectLane,
      }),
    [
      model,
      layout,
      spines,
      schedules,
      unmatched,
      selectedActivityId,
      focus,
      hoveredId,
      t,
      onSelectLane,
    ]
  );
  const edges = useMemo(() => buildEdges(model, focus, t), [model, focus, t]);

  // The canvas's HEIGHT is measured, not guessed (graphViewport.canvasHeightPx):
  // it fills the scroller from where it sits at rest down to the scroller's
  // bottom, whatever the header, toolbar, ribbon and key above it wrapped to at
  // this width. Written straight to the DOM from a layout effect, never through
  // React state, and only when the value changes — the shell's own geometry
  // rule (ConstructionShell / lensGeometry). The parent is observed too: the
  // ribbon and key above re-wrap as the width changes, moving this box's top.
  const boxRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (box === null) return undefined;
    let scroller: HTMLElement | null = box.parentElement;
    while (scroller !== null && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) {
      scroller = scroller.parentElement;
    }
    let written = '';
    const measure = (): void => {
      const bottom = scroller?.getBoundingClientRect().bottom ?? window.innerHeight;
      const topAtRest = box.getBoundingClientRect().top + (scroller?.scrollTop ?? window.scrollY);
      const next = `${String(canvasHeightPx(bottom, topAtRest))}px`;
      if (next !== written) {
        box.style.height = next;
        written = next;
      }
    };
    measure();
    const observer = new ResizeObserver(measure);
    if (scroller !== null) observer.observe(scroller);
    if (box.parentElement !== null) observer.observe(box.parentElement);
    return (): void => {
      observer.disconnect();
    };
  }, []);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_CANVAS}
      ref={boxRef}
      sx={{ height: CANVAS_MIN_PX }}
    >
      <FlowCanvas
        edges={edges}
        height="100%"
        minZoom={MIN_ZOOM}
        nodeTypes={graphNodeTypes}
        nodes={nodes}
        t={t}
        {...(stored !== undefined ? { defaultViewport: stored } : {})}
        onMoveEnd={(_event, viewport) => {
          saveGraphViewport(signature, viewport);
        }}
        onNodeMouseEnter={(_event, n) => {
          enter(n);
        }}
        onNodeMouseLeave={() => {
          leave();
        }}
      >
        {initialFocus !== undefined ? (
          <FocusNodes dep={initialFocus} nodeIds={initialFocusIds} />
        ) : null}
      </FlowCanvas>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Nodes and edges
// ---------------------------------------------------------------------------

/** The row a card's layer colour comes from; System-wide is not a Method layer. */
function colourOf(t: Tokens, row: GraphRow): string {
  return row === 'systemWide' ? t.ink : layerColors(t)[row];
}

function buildNodes(args: {
  model: ActivityGraphModel<ActivityNode>;
  layout: GraphLayout;
  spines: Readonly<Record<string, LaneSpine>>;
  schedules: Readonly<Record<string, LaneSchedule>>;
  unmatched: ReadonlySet<string>;
  selectedActivityId: string | undefined;
  focus: GraphFocus | null;
  hoveredId: string | null;
  t: Tokens;
  onSelectLane: (activityId: string, lifecyclePhase?: string) => void;
}): Node[] {
  const { model, layout, spines, schedules, unmatched, selectedActivityId, focus, hoveredId, t } =
    args;
  const rowIndex = new Map<GraphRow, number>(layout.rows.map((r, i) => [r.row, i]));

  // The visual reading order — rows top→down, then left→right, the utility bar
  // last — so DOM and tab order follow what the eye sees.
  const cards = [...model.cards].sort((a, b) => {
    const ra = rowIndex.get(a.row) ?? Number.MAX_SAFE_INTEGER;
    const rb = rowIndex.get(b.row) ?? Number.MAX_SAFE_INTEGER;
    if (ra !== rb) return ra - rb;
    const pa = layout.pos.get(a.id) ?? { x: 0, y: 0 };
    const pb = layout.pos.get(b.id) ?? { x: 0, y: 0 };
    return pa.y - pb.y || pa.x - pb.x;
  });

  const nodes: Node[] = cards.map((card: GraphCard<ActivityNode>): Node => {
    const holdsSelection =
      selectedActivityId !== undefined &&
      card.lanes.some((l) => l.activityId === selectedActivityId);
    const data: GraphCardData = {
      card,
      spines,
      schedules,
      height: layout.size.get(card.id)?.h ?? 0,
      layerColor: colourOf(t, card.row),
      ...(holdsSelection ? { selectedActivityId } : {}),
      outsideFocus: focus !== null && !focus.cards.has(card.id),
      hovered: hoveredId === card.id,
      topRow: (layout.pos.get(card.id)?.y ?? 0) < (layout.rows[0]?.height ?? 0),
      unmatched,
      onSelect: args.onSelectLane,
    };
    return {
      id: card.id,
      type: 'graphCard',
      position: layout.pos.get(card.id) ?? { x: 0, y: 0 },
      data,
      draggable: false,
      selectable: false,
      ...(hoveredId === card.id ? { zIndex: 20 } : {}),
    };
  });

  // The house decor: the left gutter's row labels, and the Utilities frame.
  for (const r of layout.rows) {
    nodes.push({
      id: `__row-${r.row}`,
      type: 'rowLabel',
      position: { x: -GUTTER_W, y: r.y + (r.height - NODE_H) / 2 },
      data: { text: r.label },
      draggable: false,
      selectable: false,
      focusable: false,
    });
  }
  if (layout.bar !== undefined) {
    nodes.push({
      id: '__utility-frame',
      type: 'utilityFrame',
      position: { x: layout.bar.x - UTIL_PAD, y: layout.bar.top },
      data: { width: CARD_W + UTIL_PAD * 2, height: layout.bar.bottom - layout.bar.top },
      draggable: false,
      selectable: false,
      focusable: false,
      zIndex: -1,
    });
  }
  return nodes;
}

/** Tokens onto the pure edge presentation (graphEdges.ts decides every rule). */
function buildEdges(
  model: ActivityGraphModel<ActivityNode>,
  focus: GraphFocus | null,
  t: Tokens
): Edge[] {
  return model.edges.map((e) => {
    const p = edgePresentationFor(e, focus);
    const alarm = p.alarm ? { stroke: t.dangerFg, strokeWidth: ALARM_STROKE_WIDTH } : {};
    const edge = flowEdge(p.id, p.from, p.to, '', t, {
      dashed: p.dashed,
      ...alarm,
      ...(focus !== null ? { hidden: p.hidden, variant: p.variant } : {}),
    });
    return { ...edge, className: p.className };
  });
}
