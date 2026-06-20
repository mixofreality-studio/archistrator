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
import FunctionsIcon from '@mui/icons-material/Functions';
import EditOutlinedIcon from '@mui/icons-material/EditOutlined';
import type { Tokens } from '../../theme/themes';

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

/** A one-line legend used at the top of computed-heavy artifacts. */
export function ComputedLegend({ t }: { t: Tokens }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>This artifact is mostly</Typography>
      <ComputedBadge t={t} />
      <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
        — floats, the critical path &amp; risk are derived, not typed. Inputs are
      </Typography>
      <AuthoredBadge t={t} />
    </Box>
  );
}
