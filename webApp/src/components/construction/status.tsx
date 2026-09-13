/* eslint-disable react-refresh/only-export-components -- token-driven status helpers colocated with their chips */
/**
 * The Phase-3 build-status palette + chips — derived from the token bag so they
 * recolor across all five themes (no hardcoded color). Ported from the frozen UX
 * mock (ux-mock/src/components/construction/status.tsx), bound to the real
 * BuildStatus lens (api/constructionAdapters).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import type { Tokens } from '../../utilities/theme/themes';
import { BUILD_STATUS_META, type BuildStatus } from '../../contracts/constructionAdapters';
import { STATUS_TOKEN } from './statusRamp';

/** One token-driven status colour (the chip's dot, the network node's fill).
 *  Which token is statusRamp.ts's STATUS_TOKEN (designer palette ruling). */
export function statusColor(t: Tokens, s: BuildStatus): string {
  return t[STATUS_TOKEN[s]];
}

/** soft fill behind a status chip. */
export function statusFill(t: Tokens, s: BuildStatus): { fg: string; bg: string } {
  switch (s) {
    case 'integrated':
      return { fg: t.committedFg, bg: t.committedBg };
    case 'in-review':
      return { fg: t.chatPmFg, bg: t.chatPmBg };
    case 'in-construction':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg };
    case 'in-detailed-design':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg };
    case 'eligible':
      return { fg: t.chatPmFg, bg: t.chatPmBg };
    case 'blocked':
      return { fg: t.awaitingFg, bg: t.awaitingBg };
    case 'not-started':
      return { fg: t.muted, bg: 'transparent' };
    case 'failed':
      // Same pairing episodes/EpisodesPanel outcomeFill uses for a failed episode.
      return { fg: t.dangerFg, bg: t.awaitingBg };
    case 'unclassified':
      return { fg: t.muted, bg: 'transparent' }; // same fill as not-started; label differs
  }
}

export function StatusChip({
  t,
  status,
  size = 'sm',
}: {
  t: Tokens;
  status: BuildStatus;
  size?: 'sm' | 'xs';
}): ReactNode {
  const f = statusFill(t, status);
  const dot = statusColor(t, status);
  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.5,
        px: size === 'xs' ? 0.55 : 0.75,
        py: size === 'xs' ? 0.1 : 0.2,
        borderRadius: 99,
        bgcolor: f.bg,
        color: f.fg,
        border: `1px solid ${status === 'not-started' || status === 'unclassified' ? t.line : dot}`,
        fontFamily: t.mono,
        fontSize: size === 'xs' ? 9 : 9.5,
        fontWeight: 700,
        letterSpacing: '0.06em',
        whiteSpace: 'nowrap',
      }}
    >
      <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: dot, flexShrink: 0 }} />
      {BUILD_STATUS_META[status][size === 'xs' ? 'short' : 'label'].toUpperCase()}
    </Box>
  );
}
