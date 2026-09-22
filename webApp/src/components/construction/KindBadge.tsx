/* eslint-disable react-refresh/only-export-components -- token-driven kind helpers colocated with KindBadge */
/**
 * The three activity-KIND palette + badge chip — token-driven so it recolors
 * across all themes. Ported from ux-mock KindBadge.tsx, bound to the real
 * ConstructionRow.kind (service | frontend | testing).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import MemoryOutlinedIcon from '@mui/icons-material/MemoryOutlined';
import DesignServicesOutlinedIcon from '@mui/icons-material/DesignServicesOutlined';
import FactCheckOutlinedIcon from '@mui/icons-material/FactCheckOutlined';
import RocketLaunchOutlinedIcon from '@mui/icons-material/RocketLaunchOutlined';
import MenuBookOutlinedIcon from '@mui/icons-material/MenuBookOutlined';
import PaletteOutlinedIcon from '@mui/icons-material/PaletteOutlined';
import HubOutlinedIcon from '@mui/icons-material/HubOutlined';
import type { Tokens } from '../../utilities/theme/themes';

export type ActivityKind =
  | 'service'
  | 'frontend'
  | 'testing'
  | 'deployment'
  | 'documentation'
  | 'uiDesign'
  | 'integration'
  // The three design types at the head of the plan (spec 2026-09-20 §5.1).
  | 'requirements'
  | 'architecture'
  | 'projectDesign';

export const KIND_META: Record<ActivityKind, { label: string }> = {
  service: { label: 'Service' },
  frontend: { label: 'Frontend' },
  testing: { label: 'Testing' },
  deployment: { label: 'Deployment' },
  documentation: { label: 'Docs' },
  uiDesign: { label: 'UI design' },
  integration: { label: 'Integration' },
  requirements: { label: 'Requirements' },
  architecture: { label: 'Architecture' },
  projectDesign: { label: 'Project design' },
};

/** The activity-kind palette — token-driven; no hardcoded colour. */
export function kindColor(t: Tokens, k: ActivityKind): { fg: string; bg: string } {
  switch (k) {
    case 'service':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg };
    case 'frontend':
      return { fg: t.chatPmFg, bg: t.chatPmBg };
    case 'testing':
      return { fg: t.committedFg, bg: t.committedBg };
    case 'deployment':
      return { fg: t.awaitingFg, bg: t.awaitingBg };
    case 'documentation':
      return { fg: t.muted, bg: t.paperAlt };
    case 'uiDesign':
      return { fg: t.chatPmFg, bg: t.chatPmBg };
    case 'integration':
      return { fg: t.committedFg, bg: t.committedBg };
    // The three design kinds are architect-owned, so they wear the architect's colour.
    case 'requirements':
    case 'architecture':
    case 'projectDesign':
      return { fg: t.chatArchitectFg, bg: t.chatArchitectBg };
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
    case 'deployment':
      return <RocketLaunchOutlinedIcon sx={sx} />;
    case 'documentation':
      return <MenuBookOutlinedIcon sx={sx} />;
    case 'uiDesign':
      return <PaletteOutlinedIcon sx={sx} />;
    case 'integration':
      return <HubOutlinedIcon sx={sx} />;
    case 'requirements':
      return <MenuBookOutlinedIcon sx={sx} />;
    case 'architecture':
      return <HubOutlinedIcon sx={sx} />;
    case 'projectDesign':
      // The SDP review is a gate; FactCheck is the gate-flavoured icon already here.
      return <FactCheckOutlinedIcon sx={sx} />;
  }
}

/** The small KIND chip used by Artifacts tab list + detail. `iconOnly` keeps the
 *  chip and its colour but drops the word (the kind is then its accessible name
 *  and tooltip) — the list's narrow-width form (designer P1-8). */
export function KindBadge({
  kind,
  size = 'sm',
  iconOnly = false,
  t,
}: {
  kind: ActivityKind;
  size?: 'sm' | 'xs';
  iconOnly?: boolean;
  t: Tokens;
}): ReactNode {
  const c = kindColor(t, kind);
  if (iconOnly) {
    return (
      <Tooltip title={KIND_META[kind].label}>
        <Box
          aria-label={KIND_META[kind].label}
          role="img"
          sx={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 20,
            height: 16,
            borderRadius: 99,
            bgcolor: c.bg,
            color: c.fg,
            border: `1px solid ${c.fg}`,
          }}
        >
          {kindIcon(kind, 11)}
        </Box>
      </Tooltip>
    );
  }
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
