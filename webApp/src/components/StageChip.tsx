/**
 * Themed chip mapping a head-state slot stage to a labelled, coloured pill.
 * Ported from the frozen UX mock and bound to the real SlotStage union
 * (adapters.ts) — 'empty' renders as a muted "NOT DRAFTED", and reject/withdraw
 * surface their terminal state so the architect sees the gate history.
 */
import type { ReactElement, ReactNode } from 'react';
import Chip from '@mui/material/Chip';
import CheckIcon from '@mui/icons-material/Check';
import RateReviewOutlinedIcon from '@mui/icons-material/RateReviewOutlined';
import CircleOutlinedIcon from '@mui/icons-material/CircleOutlined';
import CloseIcon from '@mui/icons-material/Close';
import UndoIcon from '@mui/icons-material/Undo';
import type { SlotStage } from '../api/adapters';
import { useTokens } from '../theme/ThemeContext';

export function StageChip({
  stage,
  size = 'small',
}: {
  stage: SlotStage;
  size?: 'small' | 'medium';
}): ReactNode {
  const t = useTokens();
  const map: Record<SlotStage, { label: string; fg: string; bg: string; icon: ReactElement }> = {
    committed: {
      label: 'COMMITTED',
      fg: t.committedFg,
      bg: t.committedBg,
      icon: <CheckIcon sx={{ fontSize: 15 }} />,
    },
    awaitingReview: {
      label: 'AWAITING YOU',
      fg: t.awaitingFg,
      bg: t.awaitingBg,
      icon: <RateReviewOutlinedIcon sx={{ fontSize: 15 }} />,
    },
    rejected: {
      label: 'REJECTED',
      fg: t.awaitingFg,
      bg: t.awaitingBg,
      icon: <CloseIcon sx={{ fontSize: 15 }} />,
    },
    withdrawn: {
      label: 'WITHDRAWN',
      fg: t.muted,
      bg: 'transparent',
      icon: <UndoIcon sx={{ fontSize: 15 }} />,
    },
    empty: {
      label: 'NOT DRAFTED',
      fg: t.muted,
      bg: 'transparent',
      icon: <CircleOutlinedIcon sx={{ fontSize: 14 }} />,
    },
  };
  const s = map[stage];
  return (
    <Chip
      icon={s.icon}
      label={s.label}
      size={size}
      sx={{
        color: s.fg,
        bgcolor: s.bg,
        opacity: stage === 'empty' || stage === 'withdrawn' ? 0.65 : 1,
        '& .MuiChip-icon': { color: s.fg, ml: 0.75 },
      }}
    />
  );
}
