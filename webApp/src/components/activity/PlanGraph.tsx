/**
 * The plan's GRAPH lens — one tile per ACTIVITY, read as a PROJECT NETWORK in
 * Table 11-1 BUILD order (FRONT END → RESOURCES → RESOURCE ACCESS → ENGINES →
 * MANAGERS → CLIENTS → SYSTEM TESTING, with N-STP in its own side lane), every
 * edge a dependency pointing DOWN, and M0 fanning into the build roots.
 *
 * FOUR behaviours are carried over from the retired construction graph lens,
 * and exactly four:
 *   - the pinned row GUTTER (rowGutter.ts), synced to the viewport's y so the
 *     labels stay readable at fit;
 *   - transitive HOVER FOCUS (planGraphLayout.planFocusFor), dimming the rest;
 *   - CRITICAL-PATH weight, on the tile's inner lane edge (PlanTile.tsx) and
 *     said out loud in a numeral below the canvas;
 *   - VIEWPORT MEMORY keyed by a content signature (graphViewport.ts), so the
 *     1.5s cascade poll never snaps the reader back to fit and ✕ from an
 *     activity returns to the canvas where they left it.
 *
 * Deliberately NOT carried over: the milestone RIBBON, the hover cards, the
 * lane schedule strip and the graph filter. All four belong to the per-activity
 * lane view, which no longer exists — the activity's own full-screen experience
 * replaced it, and re-drawing its furniture around a plan tile would promise a
 * detail this canvas does not show.
 *
 * Where each node goes is planGraphLayout.ts's answer; what each node IS is
 * planTiles.ts's; this file only binds the two to xyflow.
 */
import { useMemo, useState, type ReactNode } from 'react';
import { useStore, type Edge, type Node } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';

import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { FlowCanvas } from '../flow/flowShared';
import { flowEdge } from '../flow/flowLayout';
import { CRITICAL_PATH_TOKEN } from '../project/bandRamp';
import { graphSignatureOf, loadGraphViewport, saveGraphViewport } from './graphViewport.ts';
import {
  CONTROLS_OFFSET_PX,
  GUTTER_PX,
  GUTTER_SOLID_PX,
  gutterLabelFontPx,
  gutterWidthFor,
  rowGutterLabels,
} from './rowGutter.ts';
import {
  PLAN_GUTTER_ROOM,
  PLAN_MILESTONE,
  PLAN_TILE,
  layoutPlanGraph,
  planFocusFor,
  type PlanGraphRow,
} from './planGraphLayout.ts';
import {
  criticalPathNote,
  planEmptyState,
  unplacedTilesNote,
  M0_CAPTION,
  M0_LABEL,
} from './planCopy.ts';
import {
  PlanMilestoneNode,
  PlanSpacerNode,
  PlanTileNode,
  type PlanMilestoneData,
  type PlanTileData,
} from './PlanTile';
import { MILESTONE_ID, planTilesFrom, type PlanActivity } from './planTiles.ts';

/** Not exported: a `components`-only export beside a component trips fast refresh. */
const PLAN_NODE_TYPES = {
  planTile: PlanTileNode,
  planMilestone: PlanMilestoneNode,
  planSpacer: PlanSpacerNode,
};

export interface PlanGraphProps {
  activities: readonly PlanActivity[];
  /** Part of the viewport signature: another project starts from fit. */
  projectId: string;
  onOpen: (activityId: string) => void;
}

/** The pinned row gutter — the house rule (rowGutter.ts); this only paints it. */
function PlanRowGutter({ rows }: { rows: readonly PlanGraphRow[] }): ReactNode {
  const t = useTokens();
  const transform = useStore((s) => s.transform);
  const canvasHeight = useStore((s) => s.height);
  const labels = rowGutterLabels(rows, transform, canvasHeight);
  const fontPx = gutterLabelFontPx(transform[2]);
  const width = gutterWidthFor(transform[0]);
  const rail = width < GUTTER_PX;
  return (
    <Box
      data-gutter-mode={rail ? 'rail' : 'full'}
      data-testid={UI_IDENTIFIERS.Plan.GUTTER}
      sx={{
        position: 'absolute',
        left: 0,
        top: 0,
        bottom: 0,
        width,
        zIndex: 4,
        pointerEvents: 'none',
        overflow: 'hidden',
        background: rail
          ? alpha(t.bg, 0.95)
          : `linear-gradient(to right, ${alpha(t.bg, 0.95)} ${String(GUTTER_SOLID_PX)}px, ${alpha(t.bg, 0)})`,
      }}
    >
      {labels
        .filter((l) => l.visible)
        .map((l) => (
          <Box
            data-testid={UI_IDENTIFIERS.Plan.gutterRow(l.row)}
            key={l.row}
            sx={{
              position: 'absolute',
              left: 0,
              width: rail ? width : GUTTER_PX - 8,
              top: l.top,
              height: l.height,
              display: 'flex',
              alignItems: 'center',
              justifyContent: rail ? 'center' : 'flex-end',
            }}
          >
            <Box
              component="span"
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: fontPx,
                letterSpacing: '0.06em',
                textTransform: 'uppercase',
                color: t.muted,
                textAlign: 'right',
                whiteSpace: rail ? 'nowrap' : 'pre-line',
                lineHeight: 1.15,
                ...(rail ? { writingMode: 'vertical-rl', transform: 'rotate(180deg)' } : {}),
              }}
            >
              {rail ? l.label.replace('\n', ' ') : l.label}
            </Box>
          </Box>
        ))}
    </Box>
  );
}

export function PlanGraph({ activities, projectId, onOpen }: PlanGraphProps): ReactNode {
  const t = useTokens();
  const [hovered, setHovered] = useState<string | null>(null);

  const { tiles, unplaced } = useMemo(() => planTilesFrom(activities), [activities]);
  const layout = useMemo(() => layoutPlanGraph(tiles, MILESTONE_ID), [tiles]);
  const byId = useMemo(() => new Map(activities.map((a) => [a.id, a])), [activities]);
  // Keyed by the CONTENT — the project and the drawn ids — never by any status,
  // so a poll that completes an activity keeps the reader's pan and zoom.
  const signature = useMemo(
    () => graphSignatureOf(projectId, [], [...tiles.map((x) => x.id), MILESTONE_ID]),
    [projectId, tiles]
  );

  const focus = hovered === null ? null : planFocusFor(hovered, tiles, layout.edges, MILESTONE_ID);
  const dimmed = (id: string): boolean => focus !== null && !focus.tiles.has(id);
  const fixed = { draggable: false, selectable: false } as const;
  const criticalCount = activities.filter((a) => a.onCriticalPath).length;

  if (tiles.length === 0) {
    return (
      <Box data-testid={UI_IDENTIFIERS.Plan.GRAPH} sx={{ p: 3 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {planEmptyState(activities.length === 0 ? 'noPlan' : 'noRows')}
        </Typography>
        {unplaced.length > 0 ? (
          <Typography
            data-testid={UI_IDENTIFIERS.Plan.UNPLACED_NOTE}
            sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, mt: 1 }}
          >
            {unplacedTilesNote(unplaced)}
          </Typography>
        ) : null}
      </Box>
    );
  }

  const nodes: Node[] = [
    ...tiles.flatMap((tile): Node[] => {
      const activity = byId.get(tile.id);
      if (activity === undefined) return [];
      return [
        {
          id: tile.id,
          type: 'planTile',
          position: layout.pos.get(tile.id) ?? { x: 0, y: 0 },
          width: PLAN_TILE.w,
          height: PLAN_TILE.h,
          data: { activity, dimmed: dimmed(tile.id), onOpen } satisfies PlanTileData,
          ...fixed,
        },
      ];
    }),
    ...(layout.pos.has(MILESTONE_ID)
      ? [
          {
            id: MILESTONE_ID,
            type: 'planMilestone',
            position: layout.pos.get(MILESTONE_ID) ?? { x: 0, y: 0 },
            width: PLAN_MILESTONE.w,
            height: PLAN_MILESTONE.h,
            data: {
              label: M0_LABEL,
              caption: M0_CAPTION,
              dimmed: dimmed(MILESTONE_ID),
            } satisfies PlanMilestoneData,
            ...fixed,
          } satisfies Node,
        ]
      : []),
    ...layout.rows.map(
      (r): Node => ({
        id: `__row-${r.row}`,
        type: 'planSpacer',
        position: { x: -PLAN_GUTTER_ROOM, y: r.y },
        width: PLAN_GUTTER_ROOM - 22,
        height: PLAN_TILE.h,
        data: {},
        focusable: false,
        ...fixed,
      })
    ),
  ];

  const edges: Edge[] = layout.edges.map((e) => {
    const incident = focus?.incident(e) === true;
    return flowEdge(e.id, e.from, e.to, '', t, {
      // The front-end chain runs left→right; everything else top→down.
      ...(e.kind === 'sequence' ? { handles: { source: 'sr', target: 'tl' } } : {}),
      // The forced dependency reads differently from a call chain: dashed, accent.
      ...(e.kind === 'milestone' ? { dashed: true, stroke: t.accent } : {}),
      // `muted` IS MUTED_OPACITY (flowLayout's own variant table) — the dimmed
      // set fades to the same tint the tiles do, rather than vanishing.
      ...(focus !== null ? { variant: incident ? ('focus' as const) : ('muted' as const) } : {}),
    });
  });

  const stored = loadGraphViewport(signature);
  return (
    <Box data-testid={UI_IDENTIFIERS.Plan.GRAPH}>
      <Box sx={{ position: 'relative' }}>
        <FlowCanvas
          controlsStyle={{ left: CONTROLS_OFFSET_PX }}
          edges={edges}
          edgesFocusable={false}
          height="clamp(420px, calc(100vh - 300px), 1400px)"
          minZoom={0.25}
          nodeTypes={PLAN_NODE_TYPES}
          nodes={nodes}
          t={t}
          {...(stored !== undefined ? { defaultViewport: stored } : {})}
          onMoveEnd={(_e, viewport) => {
            saveGraphViewport(signature, viewport);
          }}
          onNodeMouseEnter={(_e, n) => {
            if (n.type === 'planTile' || n.type === 'planMilestone') setHovered(n.id);
          }}
          onNodeMouseLeave={() => {
            setHovered(null);
          }}
        >
          <PlanRowGutter rows={layout.rows} />
        </FlowCanvas>
      </Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap', mt: 1 }}>
        <Box
          aria-hidden
          sx={{ width: 14, height: 3, bgcolor: t[CRITICAL_PATH_TOKEN], flexShrink: 0 }}
        />
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.ink }}>
          {criticalPathNote(criticalCount)}
        </Typography>
      </Box>
      {unplaced.length > 0 ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Plan.UNPLACED_NOTE}
          sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, mt: 1 }}
        >
          {unplacedTilesNote(unplaced)}
        </Typography>
      ) : null}
    </Box>
  );
}
