/**
 * Near-critical-chain float table (App C §5.6). Ported from the UX mock's
 * NEAR-CRITICAL CHAIN FLOAT section
 * (ux-mock/src/components/construction/ConstructionTracker.tsx ~lines 154–183),
 * adapted to real per-activity float data derived from the NetworkView.
 *
 * Delta column is OMITTED because per-week float delta is not available from the
 * current head-state (the server does not store prior-week snapshots). Only the
 * chain name and total float are rendered — no fabricated spend.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../theme/ThemeContext';

export interface FloatChain {
  chain: string;
  floatDays: number;
}

export function NearCriticalFloat({ chains }: { chains: FloatChain[] }): ReactNode {
  const t = useTokens();

  if (chains.length === 0) {
    return null;
  }

  return (
    <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 110px' }}>
      {(['CHAIN (activity)', 'TOTAL FLOAT'] as const).map((h) => (
        <Box key={h} sx={{ borderBottom: `1.5px solid ${t.line}`, px: 2, py: 0.75 }}>
          <Typography sx={{ color: t.muted, fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em' }}>
            {h}
          </Typography>
        </Box>
      ))}
      {chains.map((r) => (
        <Box key={r.chain} sx={{ display: 'contents' }}>
          <Box sx={{ borderBottom: `1px solid ${t.line}`, px: 2, py: 0.9 }}>
            <Typography sx={{ color: t.ink, fontFamily: t.mono, fontSize: 12 }}>{r.chain}</Typography>
          </Box>
          <Box sx={{ borderBottom: `1px solid ${t.line}`, px: 2, py: 0.9 }}>
            <Typography
              sx={{
                color: r.floatDays <= 5 ? t.awaitingFg : t.ink,
                fontFamily: t.mono,
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              {`${r.floatDays.toString()}d`}
            </Typography>
          </Box>
        </Box>
      ))}
    </Box>
  );
}
