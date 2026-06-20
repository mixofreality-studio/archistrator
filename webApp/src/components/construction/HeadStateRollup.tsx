/**
 * Stacked status bar + legend showing the distribution of activity build statuses
 * across the committed network. Ported from the UX mock's HEAD-STATE ROLL-UP section
 * (ux-mock/src/components/construction/ConstructionTracker.tsx ~lines 111–138),
 * adapted to real counts derived from constructionRows + computeActivityStatuses.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { statusColor } from './status';
import { BUILD_STATUS_META, type BuildStatus } from '../../api/constructionAdapters';
import { useTokens } from '../../theme/ThemeContext';

export interface StatusCount {
  status: BuildStatus;
  count: number;
}

export function HeadStateRollup({ counts }: { counts: StatusCount[] }): ReactNode {
  const t = useTokens();
  const total = counts.reduce((a, b) => a + b.count, 0);
  const nonZero = counts.filter((r) => r.count > 0);

  if (total === 0) {
    return null;
  }

  return (
    <Box>
      <Box
        sx={{
          border: `1.5px solid ${t.line}`,
          borderRadius: 99,
          display: 'flex',
          height: 16,
          overflow: 'hidden',
        }}
      >
        {nonZero.map((r) => (
          <Box
            key={r.status}
            sx={{
              bgcolor: statusColor(t, r.status),
              borderRight: `1px solid ${t.bg}`,
              width: `${((r.count / total) * 100).toFixed(2)}%`,
            }}
            title={`${BUILD_STATUS_META[r.status].label}: ${r.count.toString()}`}
          />
        ))}
      </Box>
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1.5, mt: 1 }}>
        {nonZero.map((r) => (
          <Box key={r.status} sx={{ alignItems: 'center', display: 'flex', gap: 0.5 }}>
            <Box
              sx={{
                bgcolor: statusColor(t, r.status),
                border: `1px solid ${t.line}`,
                borderRadius: '50%',
                height: 9,
                width: 9,
              }}
            />
            <Typography sx={{ color: t.ink, fontFamily: t.mono, fontSize: 10.5 }}>
              {BUILD_STATUS_META[r.status].label} <b>{r.count}</b>
            </Typography>
          </Box>
        ))}
      </Box>
    </Box>
  );
}
