/**
 * The three node bodies the plan GRAPH draws: one ACTIVITY tile, the M0
 * milestone, and the invisible spacer that reserves the row gutter's room at
 * fit (rowGutter.ts's rule — the canvas keeps the labels' column clear so cards
 * never slide under them).
 *
 * The tile carries the activity's own mini lifecycle, so a fork, a gate or a
 * failure is visible without opening anything. The CRITICAL PATH is a 3px left
 * edge on the tile's inner LANE — never a border round the card and never a
 * colour on an edge (the architect's Q2 ruling) — and it always says the word
 * "CP" beside it, because colour is never the sole carrier of a fact
 * (WCAG 1.4.1).
 *
 * Presentation only: props and useTokens. Where each node goes is
 * planGraphLayout.ts's answer; what each node IS is planTiles.ts's.
 */
import type { ReactNode } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { MUTED_OPACITY } from '../flow/flowLayout';
import { CRITICAL_PATH_TOKEN } from '../project/bandRamp';
import { taskDetailStateFill } from '../construction/detail/detailPaneState.ts';
import { LifecycleGraphMini } from './LifecycleGraphMini';
import { miniLifecycleLabel } from './miniLifecycleFromRow.ts';
import { CRITICAL_MARK } from './planCopy.ts';
import { PLAN_GUTTER_ROOM, PLAN_MILESTONE, PLAN_TILE } from './planGraphLayout.ts';
import {
  PLAN_STATE_DETAIL,
  PLAN_STATE_LABEL,
  planStateFor,
  type PlanActivity,
} from './planTiles.ts';

export interface PlanTileData extends Record<string, unknown> {
  activity: PlanActivity;
  dimmed: boolean;
  onOpen: (activityId: string) => void;
}

export interface PlanMilestoneData extends Record<string, unknown> {
  label: string;
  caption: string;
  dimmed: boolean;
}

/** Top/left targets and bottom/right sources: build order runs down, the
 *  front-end chain runs across. Invisible — the tile's own box is the mark. */
function Handles(): ReactNode {
  const hidden = { opacity: 0 };
  return (
    <>
      <Handle id="t" position={Position.Top} style={hidden} type="target" />
      <Handle id="tl" position={Position.Left} style={hidden} type="target" />
      <Handle id="b" position={Position.Bottom} style={hidden} type="source" />
      <Handle id="sr" position={Position.Right} style={hidden} type="source" />
    </>
  );
}

export function PlanTileNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const { activity: a, dimmed, onOpen } = data as PlanTileData;
  const state = planStateFor(a.lifecycle);
  const fill = taskDetailStateFill(t, PLAN_STATE_DETAIL[state]);
  const done = a.lifecycle.filter((n) => n.state === 'done').length;
  const open = (): void => {
    onOpen(a.id);
  };
  return (
    <Box
      aria-label={`${a.id} — ${a.title}, ${PLAN_STATE_LABEL[state]}${
        a.onCriticalPath ? ', on the critical path' : ''
      }`}
      data-critical={String(a.onCriticalPath)}
      data-state={state}
      data-testid={UI_IDENTIFIERS.Plan.tile(a.id)}
      role="button"
      sx={{
        width: PLAN_TILE.w,
        height: PLAN_TILE.h,
        boxSizing: 'border-box',
        display: 'flex',
        bgcolor: t.paper,
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        boxShadow: t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
        cursor: 'pointer',
        opacity: dimmed ? MUTED_OPACITY : 1,
        transition: 'opacity 120ms',
        '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
        outline: 'none',
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: '2px' },
      }}
      tabIndex={0}
      onClick={open}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          open();
        }
      }}
    >
      <Handles />
      {/* The LANE: the critical path's 3px edge lives here, inside the card. */}
      <Box
        sx={{
          flexGrow: 1,
          minWidth: 0,
          display: 'flex',
          flexDirection: 'column',
          px: 1,
          py: 0.75,
          borderLeft: `${a.onCriticalPath ? '3px' : '2px'} solid ${
            a.onCriticalPath ? t[CRITICAL_PATH_TOKEN] : 'transparent'
          }`,
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, minWidth: 0 }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 10.5,
              color: t.ink,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              flexGrow: 1,
            }}
          >
            {a.id}
          </Typography>
          {a.onCriticalPath ? (
            <Typography
              sx={{
                flexShrink: 0,
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 8.5,
                letterSpacing: '0.06em',
                color: t[CRITICAL_PATH_TOKEN],
              }}
            >
              {CRITICAL_MARK}
            </Typography>
          ) : null}
          <Box
            component="span"
            sx={{
              flexShrink: 0,
              px: '5px',
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 8.5,
              lineHeight: '14px',
              letterSpacing: '0.04em',
              whiteSpace: 'nowrap',
              textTransform: 'uppercase',
              color: fill.fg,
              bgcolor: fill.bg,
              border: `1px solid ${fill.border}`,
              borderRadius: '3px',
            }}
          >
            {PLAN_STATE_LABEL[state]}
          </Box>
        </Box>
        <Typography
          sx={{
            fontWeight: 600,
            fontSize: 13,
            lineHeight: '18px',
            color: t.ink,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
        >
          {a.title}
        </Typography>
        <Box
          sx={{
            flexGrow: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 0.5,
          }}
        >
          {a.lifecycle.length > 0 ? (
            <>
              <LifecycleGraphMini
                label={miniLifecycleLabel(a.lifecycle)}
                maxWidth={PLAN_TILE.w - 58}
                nodes={a.lifecycle}
              />
              <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, flexShrink: 0 }}>
                {`${String(done)}/${String(a.lifecycle.length)}`}
              </Typography>
            </>
          ) : (
            <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
              no lifecycle reported
            </Typography>
          )}
        </Box>
      </Box>
    </Box>
  );
}

export function PlanMilestoneNode({ id, data }: NodeProps): ReactNode {
  const t = useTokens();
  const { label, caption, dimmed } = data as PlanMilestoneData;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Plan.milestone(id)}
      sx={{
        width: PLAN_MILESTONE.w,
        height: PLAN_MILESTONE.h,
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        opacity: dimmed ? MUTED_OPACITY : 1,
        transition: 'opacity 120ms',
        '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
      }}
    >
      <Handles />
      <Box
        sx={{
          width: 22,
          height: 22,
          flexShrink: 0,
          transform: 'rotate(45deg)',
          bgcolor: t.paper,
          border: `2px solid ${t.accent}`,
          ml: '5px',
        }}
      />
      <Box sx={{ minWidth: 0 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            color: t.accent,
            whiteSpace: 'nowrap',
          }}
        >
          {`◆ ${label}`}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted, whiteSpace: 'nowrap' }}>
          {caption}
        </Typography>
      </Box>
    </Box>
  );
}

/** Reserves the gutter's column so no card ever slides under its labels. */
export function PlanSpacerNode(): ReactNode {
  return <Box sx={{ width: PLAN_GUTTER_ROOM - 22, height: PLAN_TILE.h, visibility: 'hidden' }} />;
}
