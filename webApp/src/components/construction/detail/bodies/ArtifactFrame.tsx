/**
 * THE ARTIFACT FRAME — one frame for every committed artifact in the pane
 * (designer renderers-placement §1).
 *
 * Its heading row states a ROLE — UNDER REVIEW (the thing Approve / Send back
 * decides), COMMITTED NOW (the artifact as it stands in project state today) or
 * REFERENCE (another phase's artifact, shown to help judge this one) — beside
 * what the artifact is, and a SOURCE line in mono under it saying where it was
 * read from. A Focus button opens it full-viewport.
 *
 * The frame NEVER carries the provenance hatch. The hatch belongs to the attempt
 * (ProvenanceNote, above): the artifact is genuinely committed state, and
 * hatching it would call real data reconstructed.
 *
 * Tokens: the role label is t.muted mono 10/700 like the old ArtifactHeading.
 * UNDER REVIEW alone takes awaitingFg with a 3px awaitingBg left edge — the
 * awaiting channel. No new colours.
 */
import type { ReactElement, ReactNode } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import OpenInFullRoundedIcon from '@mui/icons-material/OpenInFullRounded';

import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { ROLE_LABEL, type ArtifactRole } from './artifactPlacement.ts';

export interface ArtifactFrameProps {
  artifactRole: ArtifactRole;
  /** What the artifact is: SERVICE CONTRACT, WHO REACHES IT, … */
  title: string;
  source: string;
  /** Open the focus view. Absent: no Focus button (e.g. inside the focus view). */
  onFocus?: (() => void) | undefined;
  children?: ReactNode;
  /** A second id for the frame's own content kind (summary, reference, …). */
  kindTestId?: string | undefined;
}

export function ArtifactFrame({
  artifactRole: role,
  title,
  source,
  onFocus,
  children,
  kindTestId,
}: ArtifactFrameProps): ReactElement {
  const t = useTokens();
  const underReview = role === 'underReview';
  return (
    <Box
      data-kind={kindTestId}
      data-role={role}
      data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_FRAME}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        minWidth: 0,
        pl: underReview ? 1.25 : 0,
        borderLeft: underReview ? `3px solid ${t.awaitingBg}` : 'none',
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, minWidth: 0 }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 10,
              letterSpacing: '0.08em',
              color: underReview ? t.awaitingFg : t.muted,
            }}
          >
            <Box component="span" data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_ROLE}>
              {ROLE_LABEL[role]}
            </Box>
            {` · ${title}`}
          </Typography>
          <Typography
            data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_SOURCE}
            sx={{
              fontFamily: t.mono,
              fontSize: 10,
              color: t.muted,
              lineHeight: 1.5,
              mt: 0.25,
              wordBreak: 'break-word',
            }}
          >
            {source}
          </Typography>
        </Box>
        {onFocus !== undefined ? (
          <Tooltip title="Focus view (F)">
            <IconButton
              aria-label="Focus view"
              data-testid={UI_IDENTIFIERS.Construction.ARTIFACT_FOCUS}
              size="small"
              sx={{ color: t.ink, flexShrink: 0 }}
              onClick={onFocus}
            >
              <OpenInFullRoundedIcon sx={{ fontSize: 16 }} />
            </IconButton>
          </Tooltip>
        ) : null}
      </Box>
      {children}
    </Box>
  );
}

/**
 * A statement about an ABSENCE (designer §5). Calm: no red, no icon. The one
 * distinction is `tone`: `gap` (a real gap a person should fix) takes the
 * awaiting ink on its mono label; `byDesign` stays muted.
 */
export function AbsenceStatement({
  label,
  sentence,
  tone,
  testId,
  children,
}: {
  label: string;
  sentence: string;
  tone: 'gap' | 'byDesign';
  testId: string;
  children?: ReactNode;
}): ReactElement {
  const t = useTokens();
  return (
    <Box
      data-testid={testId}
      data-tone={tone}
      sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, minWidth: 0 }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 10,
          letterSpacing: '0.08em',
          color: tone === 'gap' ? t.awaitingFg : t.muted,
        }}
      >
        {label}
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
        {sentence}
      </Typography>
      {children}
    </Box>
  );
}
