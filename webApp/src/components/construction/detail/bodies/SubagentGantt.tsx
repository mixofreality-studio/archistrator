/**
 * The subagent-span gantt strip — one lane per span, laid out against the
 * episode's own wall clock.
 *
 * The geometry is decided in subagentGantt.ts (pure, tested); this file is only
 * how it is drawn. An UNTIMED span is drawn as a dashed full-width track rather
 * than dropped or placed plausibly: the span happened, the clock is what is
 * missing, and a bar at a guessed position would be a fabricated measurement in
 * the one view whose whole purpose is measurement.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';

import { useTokens } from '../../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { formatSpanDuration, ganttBarsFor, type GanttBar, type SpanLike } from './spanGeometry.ts';

const LANE_HEIGHT = 9;
/** Enough of a tool-use id to tell two lanes apart without eating the track. */
const ID_COLUMN = 92;

export function SubagentGantt({
  episode,
}: {
  episode: { startedAt: string; endedAt: string; subagentSpans?: readonly SpanLike[] };
}): ReactElement | null {
  const t = useTokens();
  const bars = ganttBarsFor(episode);
  if (bars.length === 0) return null;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_SUBAGENT_GANTT}
      sx={{ display: 'flex', flexDirection: 'column', gap: 0.4 }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9.5,
          fontWeight: 700,
          letterSpacing: '0.08em',
          color: t.muted,
        }}
      >
        {`SUBAGENT SPANS · ${String(bars.length)} — across this episode's own window`}
      </Typography>
      {bars.map((bar) => (
        <Lane bar={bar} key={bar.toolUseId} t={t} />
      ))}
    </Box>
  );
}

function Lane({ bar, t }: { bar: GanttBar; t: Tokens }): ReactElement {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      <Typography
        sx={{
          flexShrink: 0,
          width: ID_COLUMN,
          fontFamily: t.mono,
          fontSize: 9,
          color: t.muted,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
        title={bar.toolUseId}
      >
        {bar.toolUseId}
      </Typography>
      <Tooltip
        title={
          bar.untimed
            ? 'This span carries no usable start/end — its position is unknown, so none is drawn.'
            : `${formatSpanDuration(bar.durationMs)} · starts ${bar.leftPct.toFixed(0)}% into the episode`
        }
      >
        <Box
          sx={{
            position: 'relative',
            flexGrow: 1,
            minWidth: 0,
            height: LANE_HEIGHT,
            bgcolor: alpha(t.line, 0.35),
          }}
        >
          <Box
            sx={{
              position: 'absolute',
              top: 0,
              bottom: 0,
              left: `${String(bar.leftPct)}%`,
              width: `${String(bar.widthPct)}%`,
              // Untimed: a dashed outline with no fill — present, unplaced. The
              // SAME "we do not know" mark the rest of this surface uses.
              ...(bar.untimed
                ? { border: `1px dashed ${alpha(t.ink, 0.45)}` }
                : { bgcolor: t.chatArchitectFg }),
            }}
          />
        </Box>
      </Tooltip>
      <Typography
        sx={{
          flexShrink: 0,
          width: 52,
          textAlign: 'right',
          fontFamily: t.mono,
          fontSize: 9,
          color: t.muted,
        }}
      >
        {bar.untimed ? 'untimed' : formatSpanDuration(bar.durationMs)}
      </Typography>
    </Box>
  );
}
