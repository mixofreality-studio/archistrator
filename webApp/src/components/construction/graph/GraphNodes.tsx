/**
 * The GRAPH lens's one node type: a CARD — a component of the architecture (or
 * a surface / project-wide activity) carrying one lane per activity, each lane
 * drawing its lifecycle as a spine (laneSpine.ts).
 *
 * Presentation only. Every rule it applies is decided in a pinned `.ts`
 * sibling: which card and lanes exist (activityGraphModel), how big it is
 * (activityGraphLayout), what the spine says (laneSpine), which token paints
 * which state (graphPresentation), when labels appear (graphViewport.lodFor).
 *
 * CHANNELS (spec §7.2, the channel tables this whole rewrite follows)
 * -------------------------------------------------------------------
 *  - the layer: a 3px top edge in the layer colour (layerColors, shared with
 *    every architecture diagram);
 *  - lifecycle progress: segment WIDTH (Table A-1 weight) and FILL (state);
 *  - state: at most ONE chip per lane (chipFor — nothing for unknown/notStarted);
 *  - provenance: TEXTURE only — the hatched rail on a reconstructed lane
 *    (ProvenanceRailMark) and ONE spelled-out `≈ RECONSTRUCTED` on the card,
 *    the group (ProvenanceGroupStamp) — never a colour, never per lane;
 *  - coverage: a component no activity builds is HOLLOW, labelled `no activity`.
 *
 * LOD (Decision D7): below zoom 0.8 a lane is its id and a bare spine; at 0.8
 * or on hover, segments gain phase names and per-task ticks. Hover at ANY zoom
 * also opens an unscaled card (NodeToolbar) naming each lane's phases, because
 * at fit zoom nothing on the canvas is legible.
 */
import type { KeyboardEvent, ReactElement } from 'react';
import { Handle, NodeToolbar, Position, useStore, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { prefersReducedMotion } from '../../../utilities/reducedMotion';
import { MUTED_OPACITY } from '../../flow/flowLayout';
import type { ActivityNode } from '../list/activityTree';
import { activityRowState, chipFor, type RowChip } from '../list/activityRowPresentation';
import { hoverCardStampFor, hoverLaneMarksFor } from './hoverCard';
import { taskDetailStateFill } from '../detail/detailPaneState';
import { ProvenanceGroupStamp, ProvenanceRailMark, readProvenance } from '../provenance';
import type { GraphCard } from './activityGraphModel';
import { CARD_HEAD_H, CARD_W, LANE_H } from './activityGraphLayout';
import type { LaneSpine, SpineSegment } from './laneSpine';
import { lodFor, type Lod } from './graphViewport';
import { SEGMENT_STATE_LABEL, segmentPaint, tickPaint } from './graphPresentation';
import { cardFrameFor } from './graphCardPresentation';
import {
  effortText,
  floatTooltip,
  scheduleLine,
  type LaneFloat,
  type LaneSchedule,
} from './laneSchedule';
import { bandTokens } from '../../project/bandTokens';

/** What the lens hands each card node through `data`. */
export interface GraphCardData {
  card: GraphCard<ActivityNode>;
  /** The spine per lane, keyed by activity id — computed once by the lens. */
  spines: Readonly<Record<string, LaneSpine>>;
  /** The schedule channels per lane (effort, float, critical path) — laneSchedule.ts. */
  schedules: Readonly<Record<string, LaneSchedule>>;
  height: number;
  layerColor: string;
  /** The activity the URL selects, when it rides on this card. */
  selectedActivityId?: string;
  /** Hover-focus: this card is outside the lit neighbourhood (cardFrameFor decides the mute). */
  outsideFocus: boolean;
  /** This card is the hovered one — LOD-1 and the hover card. */
  hovered: boolean;
  /** The card's top lies in the canvas's first row band, so its hover card
   *  opens BELOW it — opened above, the canvas edge clipped it. */
  topRow: boolean;
  /** Activities the toolbar's filters do NOT match (dimmed, never removed — D4). */
  unmatched: ReadonlySet<string>;
  onSelect: (activityId: string, lifecyclePhase?: string) => void;
  [key: string]: unknown;
}

export function GraphCardNode({ data }: NodeProps): ReactElement {
  const t = useTokens();
  const d = data as GraphCardData;
  const zoom = useStore((s) => s.transform[2]);
  const lod = lodFor(zoom, d.hovered);
  const { card } = d;
  const cardReading = readProvenance({ phases: card.lanes });
  // The card's frame reads the card alone — never a lane's schedule (Q2 ruling).
  const frame = cardFrameFor(card, { outsideFocus: d.outsideFocus });

  return (
    <Box
      data-hollow={String(card.hollow)}
      data-kind={card.kind}
      data-lanes={card.lanes.length}
      data-row={card.row}
      data-testid={UI_IDENTIFIERS.Construction.graphCard(card.id)}
      sx={{
        width: CARD_W,
        height: d.height,
        boxSizing: 'border-box',
        display: 'flex',
        flexDirection: 'column',
        bgcolor: frame.hollow ? 'transparent' : t.paper,
        border:
          frame.borderStyle === 'dashed'
            ? `1.5px dashed ${alpha(t.line, 0.8)}`
            : `1.5px solid ${t.line}`,
        borderTop: frame.layerEdge ? `3px solid ${d.layerColor}` : undefined,
        borderRadius: `${String(Math.min(t.radius, 8))}px`,
        opacity: frame.muted ? MUTED_OPACITY : 1,
        transition: 'opacity 120ms ease-out',
        overflow: 'hidden',
      }}
    >
      <Handle id="t" position={Position.Top} style={{ opacity: 0 }} type="target" />

      {/* The head is TWO lines: the title alone on the first, so the stamp can
          never cut a component's name down to ten characters, then the surface
          subtitle and the spelled-out stamp on the second. Fixed height (see
          CARD_HEAD_H) — never provenance-dependent, so nothing moves when
          "Observed only" is toggled. */}
      <Box
        sx={{
          height: CARD_HEAD_H - 3,
          flexShrink: 0,
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'center',
          gap: 0.25,
          px: 0.75,
          minWidth: 0,
        }}
      >
        <Typography
          noWrap
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            lineHeight: 1.15,
            color: card.hollow ? t.muted : t.ink,
          }}
          title={card.title}
        >
          {card.title}
        </Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, minWidth: 0, minHeight: 14 }}>
          {card.buildsComponent !== undefined ? (
            <Typography
              noWrap
              sx={{ fontFamily: t.mono, fontSize: 8.5, color: t.muted, minWidth: 0 }}
            >
              builds {card.buildsComponent}
            </Typography>
          ) : null}
          <ProvenanceGroupStamp reading={cardReading} t={t} />
        </Box>
      </Box>

      {card.hollow ? (
        <Typography
          sx={{
            px: 0.75,
            fontFamily: t.mono,
            fontSize: 9.5,
            fontStyle: 'italic',
            color: t.muted,
          }}
        >
          no activity
        </Typography>
      ) : (
        card.lanes.map((lane) => {
          const spine = d.spines[lane.activityId];
          return spine === undefined ? null : (
            <Lane
              dim={d.unmatched.has(lane.activityId)}
              key={lane.activityId}
              lane={lane}
              lod={lod}
              schedule={d.schedules[lane.activityId]}
              selected={d.selectedActivityId === lane.activityId}
              spine={spine}
              t={t}
              onSelect={d.onSelect}
            />
          );
        })
      )}

      <Handle id="b" position={Position.Bottom} style={{ opacity: 0 }} type="source" />

      <NodeToolbar isVisible={d.hovered} position={d.topRow ? Position.Bottom : Position.Top}>
        <HoverCard card={card} schedules={d.schedules} spines={d.spines} t={t} />
      </NodeToolbar>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// One lane: provenance rail · id · one chip · the spine
// ---------------------------------------------------------------------------

function Lane({
  lane,
  spine,
  schedule,
  lod,
  selected,
  dim,
  t,
  onSelect,
}: {
  lane: ActivityNode;
  spine: LaneSpine;
  schedule: LaneSchedule | undefined;
  lod: Lod;
  selected: boolean;
  dim: boolean;
  t: Tokens;
  onSelect: (activityId: string, lifecyclePhase?: string) => void;
}): ReactElement {
  const critical = schedule?.critical === true;
  const reading = readProvenance(lane);
  const state = activityRowState(lane.row);
  const chip = chipFor(state);
  const select = (): void => {
    onSelect(lane.activityId);
  };
  const onKeyDown = (e: KeyboardEvent): void => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      select();
    }
  };

  return (
    <Box
      aria-label={`${lane.activityId} — ${lane.label}`}
      aria-pressed={selected}
      className="nodrag nopan"
      data-critical={String(critical)}
      data-effort={schedule?.effortDays ?? ''}
      data-provenance={reading.origin}
      data-selected={String(selected)}
      data-state={state}
      data-testid={UI_IDENTIFIERS.Construction.graphLane(lane.activityId)}
      role="button"
      sx={{
        height: LANE_H,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'stretch',
        gap: 0.5,
        px: 0.5,
        boxSizing: 'border-box',
        // The critical path is the LANE's left edge, full-bleed: 3px in full ink
        // on the path, a receding 2px off it (laneSchedule, the list's
        // criticalBorderPx). Never on the card, never on an edge.
        borderLeft: `${String(schedule?.borderPx ?? 2)}px solid ${
          critical ? t.ink : alpha(t.line, 0.3)
        }`,
        cursor: 'pointer',
        opacity: dim ? MUTED_OPACITY : 1,
        outline: selected ? `2px solid ${t.accent}` : 'none',
        outlineOffset: -2,
        borderRadius: 0.5,
        '&:hover': { bgcolor: t.paperAlt },
        '&:focus-visible': { outline: `2px solid ${t.accent}` },
      }}
      tabIndex={0}
      onClick={(e) => {
        e.stopPropagation();
        select();
      }}
      onKeyDown={onKeyDown}
    >
      <ProvenanceRailMark reading={reading} t={t} />
      <Box sx={{ minWidth: 0, flexGrow: 1, display: 'flex', flexDirection: 'column', py: 0.25 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, minHeight: 13 }}>
          <Typography
            noWrap
            sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.ink, flexGrow: 1, minWidth: 0 }}
            title={`${lane.activityId} — ${lane.label}`}
          >
            {lane.activityId}
          </Typography>
          {schedule?.float !== undefined ? (
            <FloatMark activityId={lane.activityId} float={schedule.float} t={t} />
          ) : null}
          {chip !== undefined ? <StateChip chip={chip} size={7.5} t={t} /> : null}
        </Box>
        {spine.unclassified ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 8.5, color: t.muted, mt: 0.25 }}>
            Unclassified — no lifecycle
          </Typography>
        ) : (
          // Effort sets the spine's LENGTH; the segments inside keep their
          // Table A-1 proportions. No effort on record: the full track, titled so.
          <Box
            data-spine-fraction={schedule?.spineFraction ?? ''}
            sx={{ width: `${String((schedule?.spineFraction ?? 1) * 100)}%` }}
            title={schedule !== undefined ? effortText(schedule) : undefined}
          >
            <SpineBar
              activityId={lane.activityId}
              lod={lod}
              spine={spine}
              t={t}
              onSegment={(phase) => {
                onSelect(lane.activityId, phase);
              }}
            />
          </Box>
        )}
      </Box>
    </Box>
  );
}

/** A lane's one state chip (chipFor) — the lane and the hover card share it. */
function StateChip({ chip, size, t }: { chip: RowChip; size: number; t: Tokens }): ReactElement {
  const fill = taskDetailStateFill(t, chip.state);
  return (
    <Box
      component="span"
      data-chip-state={chip.state}
      sx={{
        flexShrink: 0,
        px: 0.4,
        fontFamily: t.mono,
        fontSize: size,
        fontWeight: 800,
        letterSpacing: '0.04em',
        textTransform: 'uppercase',
        lineHeight: `${String(Math.round(size * 1.45))}px`,
        color: fill.fg,
        bgcolor: fill.bg,
        border: `1px solid ${fill.border}`,
        borderRadius: 0.5,
      }}
    >
      {chip.label}
    </Box>
  );
}

/** Float: a rail PLUS its numeral — never colour alone (WCAG 1.4.1). Rendered
 *  only when the network has a computed entry; absence draws nothing. */
function FloatMark({
  activityId,
  float,
  t,
}: {
  activityId: string;
  float: LaneFloat;
  t: Tokens;
}): ReactElement {
  const colour = float.band !== undefined ? bandTokens(t, float.band).fg : t.line;
  return (
    <Box
      aria-label={floatTooltip(float)}
      component="span"
      data-band={float.band ?? ''}
      data-float={float.numeral}
      data-testid={UI_IDENTIFIERS.Construction.graphLaneFloat(activityId)}
      sx={{ display: 'inline-flex', alignItems: 'center', gap: '2px', flexShrink: 0 }}
      title={floatTooltip(float)}
    >
      <Box
        component="span"
        sx={{ width: 3, height: 10, bgcolor: colour, borderRadius: '1px', flexShrink: 0 }}
      />
      <Box
        component="span"
        sx={{ fontFamily: t.mono, fontSize: 9, fontWeight: 700, lineHeight: '11px', color: t.ink }}
      >
        {float.numeral}
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The spine
// ---------------------------------------------------------------------------

function SpineBar({
  activityId,
  spine,
  lod,
  t,
  onSegment,
}: {
  activityId: string;
  spine: LaneSpine;
  lod: Lod;
  t: Tokens;
  onSegment: (phase: string) => void;
}): ReactElement {
  return (
    <Box sx={{ display: 'flex', gap: '2px', mt: 0.35, height: lod === 1 ? 16 : 8 }}>
      {spine.segments.map((s) => (
        <Segment
          activityId={activityId}
          key={s.phase}
          lod={lod}
          segment={s}
          t={t}
          onSegment={onSegment}
        />
      ))}
    </Box>
  );
}

function Segment({
  activityId,
  segment,
  lod,
  t,
  onSegment,
}: {
  activityId: string;
  segment: SpineSegment;
  lod: Lod;
  t: Tokens;
  onSegment: (phase: string) => void;
}): ReactElement {
  const paint = segmentPaint(t, segment.state);
  const bar = lod === 1 ? 9 : 8;
  const label =
    segment.name !== undefined
      ? `${segment.name} · ${SEGMENT_STATE_LABEL[segment.state]}`
      : SEGMENT_STATE_LABEL.absent;
  const animate = paint.animated && !prefersReducedMotion();

  return (
    <Box
      aria-label={label}
      data-state={segment.state}
      data-testid={UI_IDENTIFIERS.Construction.graphSegment(activityId, segment.phase)}
      sx={{
        flex: `${String(segment.fraction)} 0 0`,
        minWidth: 0,
        display: 'flex',
        flexDirection: 'column',
        opacity: paint.opacity,
        cursor: segment.state === 'absent' ? 'default' : 'pointer',
      }}
      title={label}
      onClick={(e) => {
        if (segment.state === 'absent' || lod === 0) return;
        e.stopPropagation();
        onSegment(segment.phase);
      }}
    >
      <Box
        sx={{
          position: 'relative',
          height: bar,
          boxSizing: 'border-box',
          bgcolor: paint.fill,
          border:
            paint.borderStyle === 'none' ? 'none' : `1px ${paint.borderStyle} ${paint.border}`,
          borderLeft:
            paint.accentEdge !== undefined
              ? `3px solid ${paint.accentEdge}`
              : paint.borderStyle === 'none'
                ? 'none'
                : `1px ${paint.borderStyle} ${paint.border}`,
          borderRadius: '2px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-evenly',
          // The absent gap: one centred hairline, no box — "by design", not missing.
          ...(segment.state === 'absent'
            ? {
                backgroundImage: `linear-gradient(${t.line}, ${t.line})`,
                backgroundSize: '100% 1px',
                backgroundPosition: 'center',
                backgroundRepeat: 'no-repeat',
              }
            : {}),
          ...(animate
            ? {
                '@keyframes constructionGraphRunning': {
                  '0%,100%': { opacity: 1 },
                  '50%': { opacity: 0.35 },
                },
                animation: 'constructionGraphRunning 1.4s ease-in-out infinite',
              }
            : {}),
        }}
      >
        {lod === 1
          ? segment.ticks.map((tick) => {
              const tp = tickPaint(t, tick.state);
              return (
                <Box
                  data-retries={tick.retries}
                  data-state={tick.state}
                  key={tick.nodeId}
                  sx={{
                    width: 3,
                    height: 5,
                    boxSizing: 'border-box',
                    bgcolor: tp.hollow ? 'transparent' : tp.color,
                    border: `1px ${tp.dashed ? 'dashed' : 'solid'} ${tp.color}`,
                    // Retries: a doubled stroke (§7.2), never an extra tick.
                    boxShadow:
                      tick.retries > 0 ? `0 0 0 1px ${t.paper}, 0 0 0 2px ${tp.color}` : 'none',
                  }}
                  title={`${tick.label}${tick.retries > 0 ? ` · ↻${String(tick.retries)}` : ''}`}
                />
              );
            })
          : null}
      </Box>
      {lod === 1 && segment.name !== undefined ? (
        <Typography
          noWrap
          sx={{ fontFamily: t.mono, fontSize: 6.5, lineHeight: '7px', color: t.muted, mt: '1px' }}
        >
          {segment.name}
        </Typography>
      ) : null}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The hover card — unscaled, so it reads at any zoom
// ---------------------------------------------------------------------------

function HoverCard({
  card,
  spines,
  schedules,
  t,
}: {
  card: GraphCard<ActivityNode>;
  spines: Readonly<Record<string, LaneSpine>>;
  schedules: Readonly<Record<string, LaneSchedule>>;
  t: Tokens;
}): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_HOVER_CARD}
      sx={{
        maxWidth: 320,
        p: 1,
        bgcolor: t.paper,
        border: `1.5px solid ${t.line}`,
        borderRadius: 1,
        boxShadow: `0 4px 12px ${alpha(t.ink, 0.18)}`,
        pointerEvents: 'none',
      }}
    >
      {/* The header carries the card-level reading — the same stamp the card's
          own head shows — so the hover card never reads cleaner than the card. */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, fontWeight: 700, color: t.ink }}>
          {card.title}
        </Typography>
        {hoverCardStampFor(card.lanes) ? (
          <ProvenanceGroupStamp reading={readProvenance({ phases: card.lanes })} t={t} />
        ) : null}
      </Box>
      {card.hollow ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, mt: 0.5 }}>
          No activity in the plan builds this component.
        </Typography>
      ) : (
        card.lanes.map((lane) => {
          const spine = spines[lane.activityId];
          const schedule = schedules[lane.activityId];
          const marks = hoverLaneMarksFor(lane);
          return (
            <Box
              data-provenance={readProvenance(lane).origin}
              data-testid={UI_IDENTIFIERS.Construction.graphHoverLane(lane.activityId)}
              key={lane.activityId}
              sx={{ mt: 0.75 }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}>
                <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink }}>
                  {lane.activityId}
                  {lane.label !== lane.activityId ? ` — ${lane.label}` : ''}
                </Typography>
                {/* A reconstructed line: its stamp AND its state chip, so the
                    phases below never read "passed" unqualified (P0-1). An
                    unknown lane stays unmarked. */}
                {marks.stamp ? <ProvenanceGroupStamp reading={readProvenance(lane)} t={t} /> : null}
                {marks.chip !== undefined ? <StateChip chip={marks.chip} size={8.5} t={t} /> : null}
              </Box>
              {schedule !== undefined ? (
                <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, pl: 1 }}>
                  {scheduleLine(schedule)}
                </Typography>
              ) : null}
              {spine === undefined || spine.unclassified ? (
                <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
                  Unclassified — no lifecycle
                </Typography>
              ) : (
                spine.segments
                  .filter((s) => s.state !== 'absent')
                  .map((s) => (
                    <Typography
                      key={s.phase}
                      sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, pl: 1 }}
                    >
                      {s.name ?? s.phase} · {SEGMENT_STATE_LABEL[s.state]}
                    </Typography>
                  ))
              )}
            </Box>
          );
        })
      )}
    </Box>
  );
}
