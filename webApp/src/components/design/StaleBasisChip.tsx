/**
 * The 'basis changed — reconcile' surface for a committed slot whose upstream
 * basis has since drifted (ArtifactSlotView.staleBasis). Advisory and
 * non-blocking — it never gates any action.
 *
 * Two renders, one signal:
 *   • StaleBasisHeaderChip — a COMPACT "⚠ basis changed" chip that sits beside the
 *     COMMITTED status chip in the experience header. Clicking it opens a popover
 *     carrying the explanation + BOTH ways out (reconcile via amendment / mark
 *     reviewed — unaffected). Replaces the old full-width amber banner so the first
 *     paint of a committed step is content, not a stack of banners (UX-P1-4/P2-10).
 *   • StaleBasisMarker — a compact icon-only badge for the tight SlimSpine pip and
 *     the HomeBase artifact rows, carrying the same copy in its tooltip.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Popover from '@mui/material/Popover';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Tooltip from '@mui/material/Tooltip';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import EditNoteIcon from '@mui/icons-material/EditNote';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

const STALE_LABEL = 'basis changed — reconcile';

const GENERIC_STALE_EXPLANATION =
  'An upstream artifact this one was built on has been amended. Reconcile it with an amendment, or — if the change does not affect this artifact — mark it reviewed.';

/**
 * Compose the stale explanation. When a specific upstream CAUSE is known (PM-P1-2:
 * a server `staleCause` field naming the drifted slot) name it; otherwise fall back
 * to the generic copy. Coded defensively so an absent/blank cause never breaks it.
 */
function staleExplanation(cause: string | undefined): string {
  if (cause !== undefined && cause.trim().length > 0) {
    return `${cause.trim()} was amended after this was committed. Reconcile it with an amendment, or — if the change does not affect this artifact — mark it reviewed.`;
  }
  return GENERIC_STALE_EXPLANATION;
}

/**
 * The compact header treatment (UX-P1-4/P2-10 + P3-13): a small "⚠ basis changed"
 * chip that opens a popover with the explanation and the two non-blocking ways out,
 * given EQUAL-FOOTING button treatment. Reconcile launches an amendment; "mark
 * reviewed — unaffected" clears StaleBasis with an optional audit note, no redraft.
 */
export function StaleBasisHeaderChip({
  onReconcile,
  onAcknowledge,
  acknowledgePending = false,
  cause,
}: {
  /** Launch the reconcile amendment (fires the amend flow directly). */
  onReconcile: () => void;
  /** Clear StaleBasis with an audit note, no redraft. Omitted ⇒ the action is hidden. */
  onAcknowledge?: (note: string) => void;
  /** An AcknowledgeStaleBasis mutation is in flight — disable the confirm. */
  acknowledgePending?: boolean;
  /** The upstream slot that drifted, when the read model exposes it (PM-P1-2). */
  cause?: string | undefined;
}): ReactNode {
  const t = useTokens();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [note, setNote] = useState('');
  const open = anchorEl !== null;

  const close = (): void => {
    setAnchorEl(null);
    setConfirming(false);
    setNote('');
  };

  return (
    <>
      <Chip
        clickable
        data-testid={UI_IDENTIFIERS.DesignExperience.STALE_CHIP}
        icon={<WarningAmberIcon sx={{ fontSize: 15 }} />}
        label="basis changed"
        size="small"
        sx={{
          bgcolor: t.paperAlt,
          color: t.ink,
          fontWeight: 700,
          border: `1.5px solid ${t.bandYellow}`,
          '& .MuiChip-icon': { color: t.bandYellow, ml: 0.75 },
        }}
        onClick={(e) => {
          setAnchorEl(e.currentTarget);
        }}
      />
      <Popover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        open={open}
        transformOrigin={{ vertical: 'top', horizontal: 'left' }}
        onClose={close}
      >
        <Box
          data-testid={UI_IDENTIFIERS.DesignExperience.STALE_BANNER}
          sx={{
            display: 'flex',
            flexDirection: 'column',
            gap: 1,
            p: 2,
            maxWidth: 360,
            bgcolor: t.paper,
          }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <WarningAmberIcon sx={{ fontSize: 18, color: t.bandYellow }} />
            <Typography sx={{ fontWeight: 700, fontSize: 13.5, color: t.ink }}>
              Basis changed since this was committed
            </Typography>
          </Box>
          <Typography sx={{ fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}>
            {staleExplanation(cause)}
          </Typography>
          {confirming ? (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1, mt: 0.5 }}>
              <TextField
                fullWidth
                data-testid={UI_IDENTIFIERS.DesignExperience.STALE_ACK_NOTE}
                label="Why is it unaffected? (optional)"
                placeholder="e.g. the upstream change was diagrams only"
                size="small"
                value={note}
                onChange={(e) => {
                  setNote(e.target.value);
                }}
              />
              <Box sx={{ display: 'flex', gap: 1 }}>
                <Button
                  color="primary"
                  data-testid={UI_IDENTIFIERS.DesignExperience.STALE_ACK_CONFIRM}
                  disabled={acknowledgePending}
                  size="small"
                  variant="contained"
                  onClick={() => {
                    onAcknowledge?.(note.trim());
                    close();
                  }}
                >
                  Confirm — reviewed, unaffected
                </Button>
                <Button
                  data-testid={UI_IDENTIFIERS.DesignExperience.STALE_ACK_CANCEL}
                  size="small"
                  sx={{ color: t.muted, textTransform: 'none' }}
                  onClick={() => {
                    setConfirming(false);
                    setNote('');
                  }}
                >
                  Cancel
                </Button>
              </Box>
            </Box>
          ) : (
            <Box sx={{ display: 'flex', gap: 1, mt: 0.5, flexWrap: 'wrap' }}>
              {/* UX-P3-13: both exits get equal-footing (outlined) buttons rather
                  than one outlined + one text (which read as disabled). */}
              <Button
                data-testid={UI_IDENTIFIERS.DesignExperience.STALE_RECONCILE}
                size="small"
                startIcon={<EditNoteIcon sx={{ fontSize: 16 }} />}
                sx={{ color: t.ink, borderColor: t.line, textTransform: 'none' }}
                variant="outlined"
                onClick={() => {
                  onReconcile();
                  close();
                }}
              >
                Reconcile via amendment
              </Button>
              {onAcknowledge !== undefined ? (
                <Button
                  data-testid={UI_IDENTIFIERS.DesignExperience.STALE_MARK_REVIEWED}
                  size="small"
                  sx={{ color: t.ink, borderColor: t.line, textTransform: 'none' }}
                  variant="outlined"
                  onClick={() => {
                    setConfirming(true);
                  }}
                >
                  Mark reviewed — unaffected
                </Button>
              ) : null}
            </Box>
          )}
        </Box>
      </Popover>
    </>
  );
}

/** Compact icon-only stale badge for the SlimSpine pip / HomeBase rows; copy lives in the tooltip. */
export function StaleBasisMarker({ kind }: { kind: string }): ReactNode {
  const t = useTokens();
  return (
    <Tooltip title={STALE_LABEL}>
      <Box
        aria-label={STALE_LABEL}
        data-testid={UI_IDENTIFIERS.DesignExperience.spineStale(kind)}
        sx={{ display: 'inline-flex', alignItems: 'center', color: t.bandYellow }}
      >
        <WarningAmberIcon sx={{ fontSize: 14 }} />
      </Box>
    </Tooltip>
  );
}
