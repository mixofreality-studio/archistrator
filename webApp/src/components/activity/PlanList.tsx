/**
 * The plan's LIST lens — one row per activity in Table 11-1 order, with the M0
 * divider drawn where the three front-end design activities end and the build
 * stack begins (planTiles.LIST_ROWS, planCopy.M0_DIVIDER).
 *
 * Every row opens the SAME full-screen Activity Experience — there is no side
 * panel on this screen and no second way in. The row is a real `<button>`, so
 * the keyboard reaches it without this file re-implementing Enter and Space.
 *
 * Presentation only (the pure `components` layer): props and useTokens. Which
 * activities exist, what order they are in and how far each has got are all
 * decided by planTiles.ts; which colour a state paints is the ONE shared fill
 * every other construction surface already reads (taskDetailStateFill), so the
 * plan invents no palette of its own.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import FlagOutlinedIcon from '@mui/icons-material/FlagOutlined';

import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { CRITICAL_PATH_TOKEN } from '../project/bandRamp';
import { taskDetailStateFill } from '../construction/detail/detailPaneState.ts';
import { LifecycleGraphMini } from './LifecycleGraphMini';
import { miniLifecycleLabel } from './miniLifecycleFromRow.ts';
import { CRITICAL_MARK, M0_DIVIDER, planEmptyState } from './planCopy.ts';
import {
  PLAN_STATE_DETAIL,
  PLAN_STATE_LABEL,
  planStateFor,
  type PlanActivity,
} from './planTiles.ts';

export interface PlanListProps {
  activities: readonly PlanActivity[];
  /** Why the list is empty, when it is — never a bare blank surface. */
  emptyReason?: 'noPlan' | 'noRows' | undefined;
  onOpen: (activityId: string) => void;
}

function PlanListRow({
  activity,
  t,
  onOpen,
}: {
  activity: PlanActivity;
  t: Tokens;
  onOpen: () => void;
}): ReactNode {
  const state = planStateFor(activity.lifecycle);
  const fill = taskDetailStateFill(t, PLAN_STATE_DETAIL[state]);
  const done = activity.lifecycle.filter((n) => n.state === 'done').length;
  return (
    <Box
      component="button"
      data-critical={String(activity.onCriticalPath)}
      data-state={state}
      data-testid={UI_IDENTIFIERS.Plan.row(activity.id)}
      sx={{
        width: '100%',
        textAlign: 'left',
        font: 'inherit',
        color: 'inherit',
        bgcolor: 'transparent',
        display: 'grid',
        gridTemplateColumns: {
          xs: '1fr auto',
          md: 'minmax(150px, 190px) minmax(180px, 1fr) 132px 220px 44px',
        },
        alignItems: 'center',
        columnGap: 2,
        px: 2,
        py: 1.25,
        cursor: 'pointer',
        // The critical path is a 3px LEFT EDGE on the row's own lane (the
        // CRITICAL_PATH_TOKEN idiom) plus the word "CP" below — never a border
        // round the card, never a colour on its own.
        borderLeft: `${activity.onCriticalPath ? '3px' : '2px'} solid ${
          activity.onCriticalPath ? t[CRITICAL_PATH_TOKEN] : 'transparent'
        }`,
        borderTop: 'none',
        borderRight: 'none',
        borderBottom: `1px solid ${t.line}`,
        '&:hover, &:focus-visible': { bgcolor: t.paperAlt },
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: '-2px' },
      }}
      type="button"
      onClick={onOpen}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12.5,
          color: t.ink,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {activity.id}
      </Typography>
      <Typography sx={{ fontWeight: 600, fontSize: 15, color: t.ink, minWidth: 0 }}>
        {activity.title}
      </Typography>
      <Box
        component="span"
        sx={{
          display: { xs: 'none', md: 'inline-flex' },
          justifySelf: 'start',
          alignItems: 'center',
          height: 20,
          px: 0.75,
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 10,
          letterSpacing: '0.04em',
          textTransform: 'uppercase',
          color: fill.fg,
          bgcolor: fill.bg,
          border: `1px solid ${fill.border}`,
          borderRadius: '3px',
        }}
      >
        {PLAN_STATE_LABEL[state]}
      </Box>
      <Box sx={{ display: { xs: 'none', md: 'flex' }, alignItems: 'center', gap: 1.5 }}>
        {activity.lifecycle.length > 0 ? (
          <>
            <LifecycleGraphMini
              label={miniLifecycleLabel(activity.lifecycle)}
              nodes={activity.lifecycle}
            />
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
              {`${String(done)}/${String(activity.lifecycle.length)}`}
            </Typography>
          </>
        ) : (
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>—</Typography>
        )}
      </Box>
      <Typography
        sx={{
          display: { xs: 'none', md: 'block' },
          justifySelf: 'end',
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 10.5,
          letterSpacing: '0.06em',
          color: activity.onCriticalPath ? t[CRITICAL_PATH_TOKEN] : 'transparent',
        }}
      >
        {activity.onCriticalPath ? CRITICAL_MARK : ''}
      </Typography>
    </Box>
  );
}

export function PlanList({ activities, emptyReason, onOpen }: PlanListProps): ReactNode {
  const t = useTokens();
  if (activities.length === 0) {
    return (
      <Paper data-testid={UI_IDENTIFIERS.Plan.LIST} sx={{ p: 3 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {planEmptyState(emptyReason ?? 'noPlan')}
        </Typography>
      </Paper>
    );
  }
  // The divider sits after the LAST front-end activity — the point where the
  // committed order stops being design and starts being construction. An
  // absent front end (a plan with no design activities) draws no divider
  // rather than a divider over nothing.
  const lastFrontEnd = activities.reduce((at, a, i) => (a.row === 'frontEnd' ? i : at), -1);
  return (
    <Paper data-testid={UI_IDENTIFIERS.Plan.LIST} sx={{ overflow: 'hidden' }}>
      {activities.map((a, i) => (
        <Box key={a.id}>
          <PlanListRow
            activity={a}
            t={t}
            onOpen={() => {
              onOpen(a.id);
            }}
          />
          {i === lastFrontEnd ? (
            <Box
              data-testid={UI_IDENTIFIERS.Plan.M0_DIVIDER}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                px: 2,
                py: 0.75,
                bgcolor: t.paperAlt,
                borderBottom: `1px solid ${t.line}`,
              }}
            >
              <FlagOutlinedIcon sx={{ fontSize: 15, color: t.accent }} />
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.12em',
                  color: t.accent,
                }}
              >
                {M0_DIVIDER}
              </Typography>
              <Box sx={{ flexGrow: 1, height: '1.5px', bgcolor: t.line }} />
            </Box>
          ) : null}
        </Box>
      ))}
    </Paper>
  );
}
