/**
 * The four-tile CPM summary strip for the Project Network — TOTAL DURATION ·
 * CRITICAL PATH (activity count) · NEAR-CRITICAL (≤5d float) · LONGEST FLOAT. All
 * four are distinct, server-COMPUTED facts (read from NetworkView.summary), each
 * carrying the COMPUTED badge. Extracted so the design wizard, the construction
 * tracker, and the home base all render the same strip from the same model.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import type { NetworkView } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { ComputedBadge } from './computed';

function Metric({
  t,
  value,
  unit,
  label,
  accent,
}: {
  t: Tokens;
  value: string;
  unit: string;
  label: string;
  accent?: boolean;
}): ReactNode {
  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.12em', color: t.muted }}
        >
          {label}
        </Typography>
        <ComputedBadge t={t} />
      </Box>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.6 }}>
        <Typography
          sx={{
            fontFamily: t.display,
            fontWeight: 800,
            fontSize: 28,
            lineHeight: 1.1,
            color: accent === true ? t.accent : t.ink,
          }}
        >
          {value}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>{unit}</Typography>
      </Box>
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
        alignItems: 'center',
        gap: { xs: 2, md: 3.5 },
      }}
    >
      <Metric label="TOTAL DURATION" t={t} unit="days" value={String(view.totalDurationDays)} />
      <Metric
        accent
        label="CRITICAL PATH"
        t={t}
        unit="activities"
        value={String(view.criticalPathActivityCount)}
      />
      <Metric label="NEAR-CRITICAL" t={t} unit="≤5d float" value={String(view.nearCriticalCount)} />
      <Metric label="LONGEST FLOAT" t={t} unit="days" value={String(view.maxFloat)} />
    </Paper>
  );
}
