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
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Tooltip from '@mui/material/Tooltip';
import WarningAmberIcon from '@mui/icons-material/WarningAmber';
import EditNoteIcon from '@mui/icons-material/EditNote';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

const STALE_LABEL = 'basis changed — reconcile';

/**
 * F45/F64 stale banner for a committed artifact pane: a prominent, non-blocking surface (not
 * just the stepper ⚠) with the two ways out — reconcile via an amendment, or mark the artifact
 * "reviewed — unaffected" (a two-step confirm-strip with an optional note that clears
 * StaleBasis WITHOUT a redraft). Shared by the committed-with-session view and the
 * committed-no-session CommittedArtifactPanel.
 */
export function StaleBasisBanner({
  onReconcile,
  onAcknowledge,
  acknowledgePending = false,
}: {
  /** Launch the reconcile amendment (opens the amend composer, or fires it directly). */
  onReconcile: () => void;
  /** Clear StaleBasis with an audit note, no redraft. Omitted ⇒ the action is hidden. */
  onAcknowledge?: (note: string) => void;
  /** An AcknowledgeStaleBasis mutation is in flight — disable the confirm. */
  acknowledgePending?: boolean;
}): ReactNode {
  const t = useTokens();
  const [confirming, setConfirming] = useState(false);
  const [note, setNote] = useState('');
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.DesignExperience.STALE_BANNER}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        px: 2,
        py: 1.5,
        bgcolor: t.paperAlt,
        border: `1.5px solid ${t.bandYellow}`,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <WarningAmberIcon sx={{ fontSize: 18, color: t.bandYellow }} />
        <Typography sx={{ fontWeight: 700, fontSize: 13.5, color: t.ink }}>
          Basis changed since this was committed
        </Typography>
      </Box>
      <Typography sx={{ fontSize: 12.5, color: t.muted, lineHeight: 1.5 }}>
        An upstream artifact this one was built on has been amended. Reconcile it with an
        amendment, or — if the change does not affect this artifact — mark it reviewed.
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
                setConfirming(false);
                setNote('');
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
          <Button
            data-testid={UI_IDENTIFIERS.DesignExperience.STALE_RECONCILE}
            size="small"
            startIcon={<EditNoteIcon sx={{ fontSize: 16 }} />}
            sx={{ color: t.ink, borderColor: t.line, textTransform: 'none' }}
            variant="outlined"
            onClick={onReconcile}
          >
            Reconcile via amendment
          </Button>
          {onAcknowledge !== undefined ? (
            <Button
              data-testid={UI_IDENTIFIERS.DesignExperience.STALE_MARK_REVIEWED}
              size="small"
              sx={{ color: t.muted, textTransform: 'none' }}
              variant="text"
              onClick={() => {
                setConfirming(true);
              }}
            >
              Mark reviewed — unaffected
            </Button>
          ) : null}
        </Box>
      )}
    </Paper>
  );
}

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
