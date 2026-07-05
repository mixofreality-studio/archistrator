/**
 * The 'basis changed — reconcile' warning surface for a committed slot whose
 * upstream basis has since drifted (ArtifactSlotView.staleBasis). Advisory and
 * non-blocking — it never gates any action.
 *
 * Two renders, one signal:
 *   • StaleBasisChip — a labelled amber chip for roomy surfaces (the committed
 *     panel header, the HomeBase artifact rows). An optional Reconcile button
 *     (committed panel only) hangs off it to launch the amend flow pre-filled.
 *   • StaleBasisMarker — a compact icon-only badge for the tight SlimSpine pip,
 *     carrying the same copy in its tooltip.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Tooltip from '@mui/material/Tooltip';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

const STALE_LABEL = 'basis changed — reconcile';

export function StaleBasisChip({ onReconcile }: { onReconcile?: () => void }): ReactNode {
  const t = useTokens();
  return (
    <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.75 }}>
      <Chip
        data-testid={UI_IDENTIFIERS.DesignExperience.STALE_CHIP}
        icon={<WarningAmberIcon sx={{ fontSize: 15 }} />}
        label={STALE_LABEL}
        size="small"
        sx={{
          // A true warning AMBER (the theme's bandYellow) rather than the awaiting
          // BROWN, which blended into the app's brown accent family and read as
          // passive. Text stays on `ink` for AA contrast against the paper chip; the
          // amber carries the "needs-attention" signal via the icon + outline.
          bgcolor: t.paperAlt,
          color: t.ink,
          fontWeight: 700,
          border: `1.5px solid ${t.bandYellow}`,
          '& .MuiChip-icon': { color: t.bandYellow, ml: 0.75 },
        }}
      />
      {onReconcile !== undefined ? (
        <Button
          data-testid={UI_IDENTIFIERS.DesignExperience.RECONCILE}
          size="small"
          sx={{ color: t.ink, fontSize: 11.5, minWidth: 0, textTransform: 'none' }}
          variant="text"
          onClick={onReconcile}
        >
          Reconcile
        </Button>
      ) : null}
    </Box>
  );
}

/** Compact icon-only stale badge for the SlimSpine pip; copy lives in the tooltip. */
export function StaleBasisMarker({ kind }: { kind: string }): ReactNode {
  const t = useTokens();
  return (
    <Tooltip title={STALE_LABEL}>
      <Box
        aria-label={STALE_LABEL}
        data-testid={UI_IDENTIFIERS.DesignExperience.spineStale(kind)}
        sx={{ display: 'inline-flex', alignItems: 'center', color: t.bandYellow, ml: 0.25 }}
      >
        <WarningAmberIcon sx={{ fontSize: 14 }} />
      </Box>
    </Tooltip>
  );
}
