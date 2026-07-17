/**
 * Themed chip mapping a head-state slot stage to a labelled, coloured pill.
 * Ported from the frozen UX mock and bound to the real SlotStage union
 * (adapters.ts) — 'empty' renders as a muted "NOT DRAFTED", and reject/withdraw
 * surface their terminal state so the architect sees the gate history. Beyond
 * SlotStage, the chip also accepts the live session stages 'drafting' and
 * 'redrafting' so a generation in flight reads as "DRAFTING…"/"REDRAFTING…",
 * never as "NOT DRAFTED". A visually-hidden polite live region mirrors the
 * label so stage transitions (e.g. drafting → awaitingReview after a
 * multi-minute wait) are announced to screen readers; the mirror node is keyed
 * by label, so poll re-renders with an unchanged stage announce nothing.
 */
import type { ReactElement, ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import CheckIcon from '@mui/icons-material/Check';
import RateReviewOutlinedIcon from '@mui/icons-material/RateReviewOutlined';
import CircleOutlinedIcon from '@mui/icons-material/CircleOutlined';
import CloseIcon from '@mui/icons-material/Close';
import UndoIcon from '@mui/icons-material/Undo';
import AutorenewIcon from '@mui/icons-material/Autorenew';
import type { SlotStage } from '../contracts/adapters';
import { useTokens } from '../utilities/theme/ThemeContext';

/** SlotStage plus the two in-flight generation stages the header chip can show. */
export type StageChipStage = SlotStage | 'drafting' | 'redrafting';

export function StageChip({
  stage,
  size = 'small',
}: {
  stage: StageChipStage;
  size?: 'small' | 'medium';
}): ReactNode {
  const t = useTokens();
  const map: Record<StageChipStage, { label: string; fg: string; bg: string; icon: ReactElement }> =
    {
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
      drafting: {
        label: 'DRAFTING…',
        fg: t.accent,
        bg: 'transparent',
        icon: <AutorenewIcon sx={{ fontSize: 15 }} />,
      },
      redrafting: {
        label: 'REDRAFTING…',
        fg: t.accent,
        bg: 'transparent',
        icon: <AutorenewIcon sx={{ fontSize: 15 }} />,
      },
    };
  const s = map[stage];
  return (
    <>
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
      {/* Visually-hidden polite announcer: the keyed child only remounts (and so
          only announces) when the LABEL changes, not on every poll re-render. */}
      <Box
        aria-live="polite"
        component="span"
        role="status"
        sx={{
          position: 'absolute',
          width: '1px',
          height: '1px',
          margin: '-1px',
          padding: 0,
          overflow: 'hidden',
          clipPath: 'inset(50%)',
          whiteSpace: 'nowrap',
        }}
      >
        <span key={s.label}>{`Stage: ${s.label}`}</span>
      </Box>
    </>
  );
}
