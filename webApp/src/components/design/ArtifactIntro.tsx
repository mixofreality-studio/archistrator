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
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import FlagOutlinedIcon from '@mui/icons-material/FlagOutlined';
import CheckIcon from '@mui/icons-material/Check';
import type { ArtifactKind } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

/** The framing copy per rich-canvas kind, by stage. Pure presentation data. */
const INTRO: Partial<Record<ArtifactKind, { draft: string; committed: string }>> = {
  volatilities: {
    draft: 'DRAFT — the two-axis decomposition. Up = evolves for one customer over time; right = differs across customers at one moment. Click a chip to inspect or comment.',
    committed: 'COMMITTED — the two-axis decomposition is sealed and in context for Core Use Cases.',
  },
  coreUseCases: {
    draft: 'DRAFT — flip through each use case’s activity diagram, then gate below. Tip: click a step or highlight text to comment.',
    committed: 'COMMITTED — the core use cases are sealed and drive the architecture decomposition.',
  },
  system: {
    draft: 'DRAFT — a navigable C4 family. Switch lenses: the Static decomposition, a Dynamic call chain per use case, or a single Component’s perspective. Pan / zoom; click any node to comment.',
    committed: 'COMMITTED — the layered architecture is sealed, with one dynamic view per core use case.',
  },
};

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
