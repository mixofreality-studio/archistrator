/**
 * The four-tile CPM summary strip for the Project Network — TOTAL DURATION ·
 * CRITICAL PATH (activity count) · NEAR-CRITICAL (≤5d float) · LONGEST FLOAT. All
 * four are distinct, server-COMPUTED facts (read from NetworkView.summary); the
 * section heading above the strip carries the ONE COMPUTED badge, and each tile
 * says in words what its number means. Extracted so the design wizard, the construction
 * tracker, and the home base all render the same strip from the same model.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { NetworkView } from '../../contracts/projectAdapters';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';

function Metric({
  t,
  value,
  unit,
  label,
  meaning,
  critical,
}: {
  t: Tokens;
  value: string;
  unit: string;
  label: string;
  /** What the figure means, in a phrase a first-time reader can act on. */
  meaning: string;
  /** The critical-path figure: the ONE critical colour (criticalFg), never the
   *  accent, which means selection (palette ruling). */
  critical?: boolean;
}): ReactNode {
  return (
    <Box sx={{ minWidth: 0, maxWidth: 220 }}>
      <Typography
        sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.12em', color: t.muted }}
      >
        {label}
      </Typography>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.6 }}>
        <Typography
          sx={{
            fontFamily: t.display,
            fontWeight: 800,
            fontSize: 28,
            lineHeight: 1.1,
            color: critical === true ? t.criticalFg : t.ink,
          }}
        >
          {value}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>{unit}</Typography>
      </Box>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.4 }}>
        {meaning}
      </Typography>
    </Box>
  );
}

export function NetworkSummaryStrip({ view }: { view: NetworkView }): ReactNode {
  const t = useTokens();
  return (
    <Paper
      sx={{
        p: 2,
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'flex-start',
        gap: { xs: 2, md: 3.5 },
      }}
    >
      <Metric
        label="TOTAL DURATION"
        meaning="Working days start to finish if every activity starts as soon as its dependencies allow — before staffing limits and the calendar."
        t={t}
        unit="working days"
        value={String(view.totalDurationDays)}
      />
      <Metric
        critical
        label="CRITICAL PATH"
        meaning="Activities with zero float: any slip in one of them delays the whole project."
        t={t}
        unit="activities"
        value={String(view.criticalPathActivityCount)}
      />
      <Metric
        label="NEAR-CRITICAL"
        meaning="Activities with 1–5 days of float — one small slip from becoming critical."
        t={t}
        unit="activities"
        value={String(view.nearCriticalCount)}
      />
      <Metric
        label="LONGEST FLOAT"
        meaning="The most slack any activity has: how far it can slip without delaying the project."
        t={t}
        unit="days"
        value={String(view.maxFloat)}
      />
    </Paper>
  );
}
