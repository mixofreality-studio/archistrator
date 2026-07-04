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
    <Box
      aria-label="Near-critical chain float"
      component="table"
      sx={{ width: '100%', borderCollapse: 'collapse', tableLayout: 'fixed' }}
    >
      <Box component="thead">
        <Box component="tr">
          <Box
            component="th"
            scope="col"
            sx={{
              textAlign: 'left',
              borderBottom: `1.5px solid ${t.line}`,
              px: 2,
              py: 0.75,
              color: t.muted,
              fontFamily: t.mono,
              fontSize: 9,
              fontWeight: 400,
              letterSpacing: '0.08em',
            }}
          >
            CHAIN (activity)
          </Box>
          <Box
            component="th"
            scope="col"
            sx={{
              width: 110,
              textAlign: 'left',
              borderBottom: `1.5px solid ${t.line}`,
              px: 2,
              py: 0.75,
              color: t.muted,
              fontFamily: t.mono,
              fontSize: 9,
              fontWeight: 400,
              letterSpacing: '0.08em',
            }}
          >
            TOTAL FLOAT
          </Box>
        </Box>
      </Box>
      <Box component="tbody">
        {chains.map((r) => (
          <Box component="tr" key={r.chain}>
            <Box
              component="td"
              sx={{
                borderBottom: `1px solid ${t.line}`,
                px: 2,
                py: 0.9,
                color: t.ink,
                fontFamily: t.mono,
                fontSize: 12,
              }}
            >
              {r.chain}
            </Box>
            <Box
              component="td"
              sx={{
                borderBottom: `1px solid ${t.line}`,
                px: 2,
                py: 0.9,
                color: r.floatDays <= 5 ? t.awaitingFg : t.ink,
                fontFamily: t.mono,
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              {`${r.floatDays.toString()}d`}
            </Box>
          </Box>
        ))}
      </Box>
    </Box>
  );
}
