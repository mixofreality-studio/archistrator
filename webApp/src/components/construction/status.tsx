/* eslint-disable react-refresh/only-export-components -- token-driven status helpers colocated with their chips */
/**
 * The Phase-3 build-status palette + chips — derived from the token bag so they
 * recolor across all five themes (no hardcoded color). Ported from the frozen UX
 * mock (ux-mock/src/components/construction/status.tsx), bound to the real
 * BuildStatus lens (api/constructionAdapters).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { Tokens } from '../../theme/themes';
import { BUILD_STATUS_META, type BuildStatus } from '../../api/constructionAdapters';

/** One token-driven status colour, used by the tracker node + the status legend. */
export function statusColor(t: Tokens, s: BuildStatus): string {
  switch (s) {
    case 'integrated':
      return t.committedDot;
    case 'in-review':
      return t.chatPmFg;
    case 'in-construction':
      return t.accent2;
    case 'in-detailed-design':
      return t.chatArchitectFg;
    case 'eligible':
      return t.chatPmFg; // teal/green: ready to start, distinct from in-construction
    case 'blocked':
      return t.accent; // the accent draws the eye to the thing needing intervention
    case 'not-started':
      return t.muted;
    default:
      return t.muted;
  }
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
    default:
      return { fg: t.muted, bg: 'transparent' };
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
        border: `1px solid ${status === 'not-started' ? t.line : dot}`,
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

/** the shared status legend, used at the top of the Tracker. */
export function StatusLegend({ t }: { t: Tokens }): ReactNode {
  const order: BuildStatus[] = [
    'integrated',
    'in-review',
    'in-construction',
    'in-detailed-design',
    'eligible',
    'blocked',
    'not-started',
  ];
  return (
    <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 1.25 }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted, letterSpacing: '0.08em' }}>
        BUILD STATUS
      </Typography>
      {order.map((s) => (
        <Box key={s} sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box
            sx={{
              width: 9,
              height: 9,
              borderRadius: '50%',
              bgcolor: statusColor(t, s),
              border: `1px solid ${t.line}`,
            }}
          />
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.ink }}>
            {BUILD_STATUS_META[s].label}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}
