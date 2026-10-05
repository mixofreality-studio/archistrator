/**
 * The confirm step in front of Pause (use case pause-construction; spec §12 B7).
 *
 * SetProjectRunState(paused) requires a non-empty reason — the Manager refuses an
 * empty one, because it is the durable record of WHY construction stopped — so the
 * dialog asks for it, and its confirm stays disabled until there is one. It also says
 * what a pause does and does not do before anything is sent: no further activity
 * starts, and nothing in flight is cancelled.
 *
 * Cancelling is always safe: nothing is sent until the confirm button is pressed.
 * Pure and props-only.
 */
import { useState, type ReactElement } from 'react';
import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { PAUSE_EXPLAINER } from './beginControl';

export interface PauseDialogProps {
  open: boolean;
  /** The pause request is in flight. */
  pending: boolean;
  onCancel: () => void;
  /** Called with the trimmed, non-empty reason. */
  onConfirm: (reason: string) => void;
}

export function PauseDialog({
  open,
  pending,
  onCancel,
  onConfirm,
}: PauseDialogProps): ReactElement {
  const t = useTokens();
  const [reason, setReason] = useState('');
  const trimmed = reason.trim();
  const cancel = (): void => {
    setReason('');
    onCancel();
  };
  return (
    <Dialog fullWidth maxWidth="sm" open={open} onClose={cancel}>
      <DialogTitle sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 15 }}>
        Pause construction?
      </DialogTitle>
      <DialogContent
        data-testid={UI_IDENTIFIERS.Construction.PAUSE_DIALOG}
        sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}
      >
        <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.5 }}>
          {PAUSE_EXPLAINER}
        </Typography>
        <TextField
          fullWidth
          multiline
          label="Why are you pausing?"
          minRows={2}
          placeholder="Required: the reason is recorded with the pause."
          slotProps={{
            htmlInput: { 'data-testid': UI_IDENTIFIERS.Construction.PAUSE_REASON },
          }}
          value={reason}
          onChange={(e) => {
            setReason(e.target.value);
          }}
        />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.PAUSE_CANCEL}
          sx={{ color: t.muted }}
          onClick={cancel}
        >
          Cancel
        </Button>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.PAUSE_CONFIRM}
          disabled={pending || trimmed.length === 0}
          sx={{ fontFamily: t.mono, fontWeight: 700, textTransform: 'none' }}
          variant="contained"
          onClick={() => {
            onConfirm(trimmed);
            setReason('');
          }}
        >
          Pause — record reason
        </Button>
      </DialogActions>
    </Dialog>
  );
}
