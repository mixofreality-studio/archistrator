/**
 * The contextual framing banner shown above a rich-canvas artifact in the System
 * Design experience — ports the per-artifact "how to read this diagram" notes from
 * the frozen UX mock (DesignWizard). It explains what the diagram is and how to
 * interact with it (pick a C4 diagram, flip use cases, click a node to comment),
 * and signals the stage:
 *
 *   awaitingReview / drafted  → ⚑ a flagged DRAFT note in the awaiting palette
 *   committed                 → ✓ a sealed COMMITTED note in the committed palette
 *
 * Only the rich canvases get a banner (volatilities / system / coreUseCases); the
 * prose artifacts already read as documents and need no framing. Returns null for
 * any other kind so the dispatcher can call it unconditionally.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Popover from '@mui/material/Popover';
import Tooltip from '@mui/material/Tooltip';
import FlagOutlinedIcon from '@mui/icons-material/FlagOutlined';
import CheckIcon from '@mui/icons-material/Check';
import HelpOutlineIcon from '@mui/icons-material/HelpOutline';
import type { ArtifactKind } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/** The framing copy per rich-canvas kind, by stage. Pure presentation data. */
const INTRO: Partial<Record<ArtifactKind, { draft: string; committed: string }>> = {
  volatilities: {
    draft:
      'DRAFT — the two-axis decomposition. Up = evolves for one customer over time; right = differs across customers at one moment. Click a chip to inspect or comment.',
    committed:
      'COMMITTED — the two-axis decomposition is sealed and in context for Core Use Cases.',
  },
  coreUseCases: {
    draft:
      'DRAFT — flip through each use case’s activity diagram, then gate below. Tip: click a step or highlight text to comment.',
    committed:
      'COMMITTED — the core use cases are sealed and drive the architecture decomposition.',
  },
  system: {
    draft:
      'DRAFT — a navigable C4 family. Switch lenses: the Static decomposition, a Dynamic call chain per use case, or a single Component’s perspective. Pan / zoom; click any node to comment.',
    committed:
      'COMMITTED — the layered architecture is sealed, with a dynamic view for every use case.',
  },
};

/**
 * The committed-state "how to read this" copy, moved off the full-width banner into
 * a small (?) info icon-button + popover next to the header title (UX-P1-4/P2-10).
 * Renders nothing for kinds without framing copy, so the header can call it
 * unconditionally.
 */
export function ArtifactInfoButton({ kind }: { kind: ArtifactKind | undefined }): ReactNode {
  const t = useTokens();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  if (kind === undefined) return null;
  const copy = INTRO[kind];
  if (copy === undefined) return null;
  const open = anchorEl !== null;
  return (
    <>
      <Tooltip title="What is this artifact?">
        <IconButton
          aria-label="about this artifact"
          data-testid={UI_IDENTIFIERS.DesignExperience.ARTIFACT_INFO}
          size="small"
          sx={{ color: t.muted }}
          onClick={(e) => {
            setAnchorEl(e.currentTarget);
          }}
        >
          <HelpOutlineIcon sx={{ fontSize: 18 }} />
        </IconButton>
      </Tooltip>
      <Popover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        open={open}
        transformOrigin={{ vertical: 'top', horizontal: 'left' }}
        onClose={() => {
          setAnchorEl(null);
        }}
      >
        <Box sx={{ p: 2, maxWidth: 360, bgcolor: t.paper }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
            {copy.committed}
          </Typography>
        </Box>
      </Popover>
    </>
  );
}

export function ArtifactIntro({
  kind,
  committed,
}: {
  kind: ArtifactKind | undefined;
  /** The active spine step is committed — show the sealed treatment. */
  committed: boolean;
}): ReactNode {
  const t = useTokens();
  if (kind === undefined) return null;
  const copy = INTRO[kind];
  if (copy === undefined) return null;

  const text = committed ? copy.committed : copy.draft;
  const fg = committed ? t.committedFg : t.awaitingFg;
  const bg = committed ? t.committedBg : t.awaitingBg;

  return (
    <Box
      data-testid={UI_IDENTIFIERS.DesignExperience.ARTIFACT_INTRO}
      sx={{
        mb: 2,
        p: 1.25,
        bgcolor: bg,
        border: `1.5px solid ${t.line}`,
        borderRadius: 1,
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 12,
          color: fg,
          display: 'flex',
          alignItems: 'flex-start',
          gap: 0.75,
          lineHeight: 1.45,
        }}
      >
        {committed ? (
          <CheckIcon sx={{ fontSize: 15, mt: 0.1, flexShrink: 0 }} />
        ) : (
          <FlagOutlinedIcon sx={{ fontSize: 15, mt: 0.1, flexShrink: 0 }} />
        )}
        {text}
      </Typography>
    </Box>
  );
}
