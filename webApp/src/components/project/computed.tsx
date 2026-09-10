/**
 * The defining Phase-2 UX distinction: much of Project Design is COMPUTED (CPM
 * derivations over the one network) rather than AUTHORED. Computed values read
 * read-only/badged; authored inputs read editable. These badges express it
 * consistently everywhere a value is surfaced. Ported from the frozen UX mock
 * (ux-mock/src/components/project/computed.tsx).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';
import FunctionsIcon from '@mui/icons-material/Functions';
import EditOutlinedIcon from '@mui/icons-material/EditOutlined';
import type { Tokens } from '../../utilities/theme/themes';
import { scanlines } from '../../utilities/theme/textures';

export function ComputedBadge({ t, label = 'computed' }: { t: Tokens; label?: string }): ReactNode {
  return (
    <Tooltip title="Computed by CPM over the network — read-only. Change the inputs (activities, dependencies) to move it.">
      <Box
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.4,
          px: 0.6,
          py: 0.1,
          borderRadius: 99,
          bgcolor: t.committedBg,
          color: t.committedFg,
          border: `1px solid ${t.committedDot}`,
          fontFamily: t.mono,
          fontSize: 9,
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          whiteSpace: 'nowrap',
        }}
      >
        <FunctionsIcon sx={{ fontSize: 11 }} />
        {label}
      </Box>
    </Tooltip>
  );
}

export function AuthoredBadge({ t, label = 'authored' }: { t: Tokens; label?: string }): ReactNode {
  return (
    <Tooltip title="Authored input — editable. The architect drafts it; the founder gates it.">
      <Box
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.4,
          px: 0.6,
          py: 0.1,
          borderRadius: 99,
          bgcolor: 'transparent',
          color: t.muted,
          border: `1px dashed ${t.line}`,
          fontFamily: t.mono,
          fontSize: 9,
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          whiteSpace: 'nowrap',
        }}
      >
        <EditOutlinedIcon sx={{ fontSize: 11 }} />
        {label}
      </Box>
    </Tooltip>
  );
}

/**
 * The third member of the family: a record that was RECONSTRUCTED rather than
 * observed.
 *
 * `ComputedBadge` and `AuthoredBadge` answer "who produced this value?".
 * This one answers a question neither can — "how do we know it is true?" — and
 * it joins them here, rather than starting a fourth vocabulary somewhere else,
 * because a reader who has learnt two badges should not have to learn a
 * language to read the third.
 *
 * The mark is TEXTURE, never colour. Colour on the construction surface is
 * already committed to status and float, and a coloured provenance badge would
 * read as a status change; the hatch it carries is the same `scanlines`
 * geometry as the rail on the rows beneath it, so the group stamp and the row
 * mark are visibly ONE material. Everything else is `muted` — the badge is a
 * qualification of the row, not an alarm on it.
 *
 * Density rule: this belongs on GROUP headers (tier 1 and tier 2) only. Task
 * rows inherit the rail alone — see components/construction/provenanceAxis.ts.
 */
export function ReconstructedBadge({
  t,
  label = 'reconstructed',
  title,
}: {
  t: Tokens;
  label?: string;
  /** The sub-grade and basis prose — provenanceTooltipFor() supplies it. */
  title: string;
}): ReactNode {
  return (
    <Tooltip title={<span style={{ whiteSpace: 'pre-line' }}>{title}</span>}>
      <Box
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.4,
          px: 0.6,
          py: 0.1,
          borderRadius: 99,
          color: t.ink,
          // The hatch runs BEHIND the word at a fraction of the ink. At full
          // strength (the rail's own weight) a 9px uppercase label on a 2px-on/
          // 2px-off scanline stops being a word and becomes a redaction bar —
          // measured on the first at-rest screenshot, not assumed.
          backgroundImage: scanlines(alpha(t.ink, 0.22)),
          border: `1px solid ${t.muted}`,
          fontFamily: t.mono,
          fontSize: 9,
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          whiteSpace: 'nowrap',
        }}
      >
        <Box component="span" sx={{ fontSize: 11, lineHeight: 1 }}>
          ≈
        </Box>
        {label}
      </Box>
    </Tooltip>
  );
}

/** A one-line legend used at the top of computed-heavy artifacts. */
export function ComputedLegend({ t }: { t: Tokens }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
        This artifact is mostly
      </Typography>
      <ComputedBadge t={t} />
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
        — floats, the critical path &amp; risk are derived, not typed. Inputs are
      </Typography>
      <AuthoredBadge t={t} />
    </Box>
  );
}
