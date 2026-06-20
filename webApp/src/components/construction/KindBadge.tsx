/* eslint-disable react-refresh/only-export-components -- token-driven kind helpers colocated with KindBadge */
/**
 * The three activity-KIND palette + badge chip — token-driven so it recolors
 * across all themes. Ported from ux-mock KindBadge.tsx, bound to the real
 * ConstructionRow.kind (service | frontend | testing).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import MemoryOutlinedIcon from '@mui/icons-material/MemoryOutlined';
import DesignServicesOutlinedIcon from '@mui/icons-material/DesignServicesOutlined';
import FactCheckOutlinedIcon from '@mui/icons-material/FactCheckOutlined';
import type { Tokens } from '../../theme/themes';

export type ActivityKind = 'service' | 'frontend' | 'testing';

export const KIND_META: Record<ActivityKind, { label: string }> = {
  service: { label: 'Service' },
  frontend: { label: 'Frontend' },
  testing: { label: 'Testing' },
};

/** The three activity-kind palette — token-driven; no hardcoded colour. */
export function kindColor(t: Tokens, k: ActivityKind): { fg: string; bg: string } {
  switch (k) {
    case 'service':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg };
    case 'frontend':
      return { fg: t.chatPmFg, bg: t.chatPmBg };
    case 'testing':
      return { fg: t.committedFg, bg: t.committedBg };
  }
}

function kindIcon(k: ActivityKind, size = 13): ReactNode {
  const sx = { fontSize: size };
  switch (k) {
    case 'service':
      return <MemoryOutlinedIcon sx={sx} />;
    case 'frontend':
      return <DesignServicesOutlinedIcon sx={sx} />;
    case 'testing':
      return <FactCheckOutlinedIcon sx={sx} />;
  }
}

/** The small KIND chip used by Artifacts tab list + detail. */
export function KindBadge({
  kind,
  size = 'sm',
  t,
}: {
  kind: ActivityKind;
  size?: 'sm' | 'xs';
  t: Tokens;
}): ReactNode {
  const c = kindColor(t, kind);
  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 0.35,
        px: size === 'xs' ? 0.5 : 0.7,
        py: size === 'xs' ? 0.05 : 0.15,
        borderRadius: 99,
        bgcolor: c.bg,
        color: c.fg,
        border: `1px solid ${c.fg}`,
        fontFamily: t.mono,
        fontSize: size === 'xs' ? 8.5 : 9.5,
        fontWeight: 700,
        letterSpacing: '0.04em',
        whiteSpace: 'nowrap',
      }}
    >
      {kindIcon(kind, size === 'xs' ? 11 : 12)}
      {KIND_META[kind].label.toUpperCase()}
    </Box>
  );
}
