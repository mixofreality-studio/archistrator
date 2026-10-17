/* eslint-disable react-refresh/only-export-components -- token-driven phase helpers colocated with their chips */
/**
 * The operated-runtime Phase palette — derived from the theme token bag so it
 * recolors across all five themes (no hardcoded color). One mapping, used by the
 * SLO listview, the deployment cards, and the health timeline. Ported from the
 * ux-mock operations/phase.tsx, retyped against operationsAdapters.RuntimePhase.
 *
 * Phase==Unknown renders honestly as the NORMAL just-published transient
 * (converging) — visually distinct from Degraded. (operatedRuntimeAccess §3.)
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import type { Tokens } from '../../utilities/theme/themes';
import { type RuntimePhase, phaseIsConverging } from '../../contracts/operationsAdapters';

export function phaseColor(t: Tokens, p: RuntimePhase): string {
  switch (p) {
    case 'Running':
      return t.committedDot;
    case 'Degraded':
      return t.accent;
    case 'Pending':
    case 'Unknown':
      return t.chatPmFg;
    case 'Paused':
    case 'Withdrawn':
      return t.muted;
    default:
      return t.muted;
  }
}

export function phaseFill(t: Tokens, p: RuntimePhase): { fg: string; bg: string } {
  switch (p) {
    case 'Running':
      return { fg: t.committedFg, bg: t.committedBg };
    case 'Degraded':
      return { fg: t.awaitingFg, bg: t.awaitingBg };
    case 'Pending':
    case 'Unknown':
      return { fg: t.chatPmFg, bg: t.chatPmBg };
    case 'Paused':
      return { fg: t.muted, bg: t.paperAlt };
    case 'Withdrawn':
      return { fg: t.muted, bg: 'transparent' };
    default:
      return { fg: t.muted, bg: 'transparent' };
  }
}

const PHASE_NOTE: Record<RuntimePhase, string> = {
  Unknown: 'Just published and not reporting yet — normal for a few moments; not a fault.',
  Pending: 'The platform is rolling out the version you published.',
  Running: 'Rolled out and serving.',
  Degraded: 'Running, but unhealthy or missing a service-level objective.',
  Paused: 'Paused while idle (scaled to zero); the next request wakes it up.',
  Withdrawn: 'Withdrawn; the platform has removed (or is removing) its resources.',
};

export function PhaseChip({
  t,
  phase,
  size = 'sm',
}: {
  t: Tokens;
  phase: RuntimePhase;
  size?: 'sm' | 'xs';
}): ReactNode {
  const f = phaseFill(t, phase);
  const dot = phaseColor(t, phase);
  const pulse = phaseIsConverging(phase);
  return (
    <Tooltip describeChild title={PHASE_NOTE[phase]}>
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
          border: `1px solid ${phase === 'Withdrawn' ? t.line : dot}`,
          fontFamily: t.mono,
          fontSize: size === 'xs' ? 9 : 9.5,
          fontWeight: 700,
          letterSpacing: '0.06em',
          whiteSpace: 'nowrap',
        }}
        tabIndex={0}
      >
        <Box
          sx={{
            width: 7,
            height: 7,
            borderRadius: '50%',
            bgcolor: dot,
            flexShrink: 0,
            ...(pulse && {
              animation: 'opPulse 1.1s ease-in-out infinite',
              '@keyframes opPulse': { '0%,100%': { opacity: 1 }, '50%': { opacity: 0.3 } },
            }),
          }}
        />
        {phase.toUpperCase()}
      </Box>
    </Tooltip>
  );
}

/** A boolean SLO posture pill (SloMet true/false), themeable. */
export function SloPill({ t, met }: { t: Tokens; met: boolean }): ReactNode {
  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.5,
        px: 0.75,
        py: 0.2,
        borderRadius: 99,
        bgcolor: met ? t.committedBg : t.awaitingBg,
        color: met ? t.committedFg : t.awaitingFg,
        border: `1px solid ${met ? t.committedDot : t.accent}`,
        fontFamily: t.mono,
        fontSize: 9.5,
        fontWeight: 700,
        letterSpacing: '0.06em',
      }}
    >
      {met ? '✓ SLO MET' : '✕ BREACHING'}
    </Box>
  );
}
