/**
 * The confirm step in front of Begin/Resume (designer P0-3). Begin is a real
 * dispatch of the construction pump, so the operator sees what it would start
 * before anything moves — the activities with no stored record, read from the
 * rows (see beginControl.notStartedActivities), never a hardcoded list.
 *
 * Cancelling is always safe: nothing is sent until the dispatch button is pressed.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import Typography from '@mui/material/Typography';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { BeginControl, DispatchCandidate } from './beginControl';

export interface BeginConfirmDialogProps {
  open: boolean;
  verb: BeginControl['verb'];
  candidates: readonly DispatchCandidate[];
  /** The session answer was `unknown`: a probe failed, so a pump may already run. */
  sessionUnknown: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

export function BeginConfirmDialog({
  open,
  verb,
  candidates,
  sessionUnknown,
  onCancel,
  onConfirm,
}: BeginConfirmDialogProps): ReactElement {
  const t = useTokens();
  return (
    <Dialog fullWidth maxWidth="sm" open={open} onClose={onCancel}>
      <DialogTitle sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 15 }}>
        {verb} construction?
      </DialogTitle>
      <DialogContent
        data-testid={UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_DIALOG}
        sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}
      >
        {candidates.length > 0 ? (
          <>
            <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.5 }}>
              This dispatches the construction pump. As their dependencies allow, it starts the{' '}
              {candidates.length === 1 ? 'activity' : `${String(candidates.length)} activities`}{' '}
              nothing has been recorded for yet:
            </Typography>
            <Box
              component="ul"
              sx={{ m: 0, pl: 2.5, display: 'flex', flexDirection: 'column', gap: 0.5 }}
            >
              {candidates.map((c) => (
                <Box
                  component="li"
                  data-testid={UI_IDENTIFIERS.Construction.beginConfirmCandidate(c.activityId)}
                  key={c.activityId}
                  sx={{ fontSize: 12.5, color: t.ink, lineHeight: 1.45 }}
                >
                  <Box component="span" sx={{ fontFamily: t.mono, fontWeight: 700 }}>
                    {c.activityId}
                  </Box>
                  {c.title !== undefined ? (
                    <Box component="span" sx={{ color: t.muted }}>
                      {' '}
                      — {c.title}
                    </Box>
                  ) : null}
                </Box>
              ))}
            </Box>
          </>
        ) : (
          <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.5 }}>
            This dispatches the construction pump. Every activity already has a record, so there is
            nothing new for it to start — it only picks up work already under way.
          </Typography>
        )}
        {sessionUnknown ? (
          <Typography sx={{ color: t.muted, fontSize: 12.5, lineHeight: 1.5 }}>
            The construction session could not be read, so this may resume a pump that is already
            running.
          </Typography>
        ) : null}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_CANCEL}
          sx={{ color: t.muted }}
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_DISPATCH}
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            textTransform: 'none',
            color: t.bg,
            bgcolor: t.accent,
            '&:hover': { bgcolor: t.accent2 },
          }}
          variant="contained"
          onClick={onConfirm}
        >
          {verb} — dispatch
        </Button>
      </DialogActions>
    </Dialog>
  );
}
