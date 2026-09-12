/**
 * COVERAGE — the persistent strip above the LIST lens tree (Stage B Task 12).
 *
 * The committed activity list derives 40 activities from the architecture; the
 * construction head-state carries 69 records; they intersect in 9. Sixty of
 * those 69 name activities the architecture no longer derives — a superseded
 * hand-authored inventory, not a fabrication. Nothing else on this surface said
 * so before this strip: the tree just rendered 69 rows as though they were one
 * coherent population. This makes the seam visible instead.
 *
 * Every number comes from `coverageCounts.ts`'s `computeCoverageCounts` — this
 * file only lays the six out with the exact punctuation the brief specifies
 * (`·` inside each half, `‖` between the derived half and the legacy half),
 * and never computes or hardcodes a count of its own.
 *
 * `derived`/`mapped`/`cross-cutting` read in the surface's normal ink; the
 * `⚠ orphaned` figure is the one number this strip treats as a standing
 * caution — not an alarm about fabricated data (the tooltip is explicit that
 * these are SUPERSEDED, and some carry real recorded evidence), but a count
 * that should shrink as the reconciliation the founder ordered proceeds.
 */
import type { ReactElement, ReactNode } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import WarningAmberRoundedIcon from '@mui/icons-material/WarningAmberRounded';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { LEGACY_GROUP_COPY, type CoverageCounts } from './coverageCounts.ts';

export function CoverageStrip({ counts }: { counts: CoverageCounts }): ReactElement {
  const t = useTokens();
  return (
    <Tooltip
      title={
        <span style={{ whiteSpace: 'pre-line' }}>
          {'The activity list slot is authoritative for what EXISTS; .activityConstruction is ' +
            `head-state that has outlived part of its plan.\n${LEGACY_GROUP_COPY}`}
        </span>
      }
    >
      <Box
        data-testid={UI_IDENTIFIERS.Construction.COVERAGE_STRIP}
        sx={{
          display: 'flex',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 0.85,
          px: 1.25,
          py: 0.85,
          border: `1.5px solid ${t.line}`,
          borderRadius: `${String(t.radius)}px`,
          bgcolor: t.paperAlt,
        }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10.5,
            fontWeight: 700,
            letterSpacing: '0.1em',
            color: t.muted,
            mr: 0.35,
          }}
        >
          COVERAGE
        </Typography>

        <Figure label="derived" t={t} value={counts.derivedTotal} />
        <Sep t={t} />
        <Figure muted label="mapped" t={t} value={counts.mapped} />
        <Sep t={t} />
        <Figure muted label="cross-cutting" t={t} value={counts.crossCutting} />

        <Typography
          sx={{ fontFamily: t.mono, fontSize: 12, color: t.line, mx: 0.35, fontWeight: 700 }}
        >
          ‖
        </Typography>

        <Figure label="legacy" t={t} value={counts.legacyTotal} />
        <Sep t={t} />
        <Figure muted label="reconcile" t={t} value={counts.reconcile} />
        <Sep t={t} />
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.3 }}>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 11.5, fontWeight: 700, color: t.awaitingFg }}
          >
            {`${String(counts.orphaned)} orphaned`}
          </Typography>
          <WarningAmberRoundedIcon sx={{ fontSize: 13, color: t.awaitingFg }} />
        </Box>
      </Box>
    </Tooltip>
  );
}

function Sep({ t }: { t: Tokens }): ReactElement {
  return (
    <Typography aria-hidden sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
      ·
    </Typography>
  );
}

function Figure({
  value,
  label,
  t,
  muted = false,
}: {
  value: number;
  label: string;
  t: Tokens;
  muted?: boolean;
}): ReactNode {
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontSize: 11.5,
        fontWeight: muted ? 400 : 700,
        color: muted ? t.muted : t.ink,
        whiteSpace: 'nowrap',
      }}
    >
      {`${String(value)} ${label}`}
    </Typography>
  );
}
