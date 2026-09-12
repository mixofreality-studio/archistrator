/**
 * The confirm step in front of Begin/Resume (designer P0-3). Begin is a real
 * dispatch of the construction pump, so the operator sees what it would start
 * before anything moves — the activities with no stored record, read from the
 * rows (see beginControl.notStartedActivities), never a hardcoded list.
 *
 * Cancelling is always safe: nothing is sent until the dispatch button is pressed.
 *
 * One press per opening (fix-A review I1): a double-click used to send two
 * execute-next-activity POSTs, each with its own tickID, and start two pump
 * workflows. The opening's tickID (the server's idempotency key) is minted by the
 * caller when it opens the dialog and handed back on confirm; the dispatch button
 * disables itself on the first press. The caller also refuses a second confirm
 * while one is in flight, so no single layer is load-bearing alone.
 */
import { useState, type ReactElement } from 'react';
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
  /** The tickID minted for THIS opening; `null` while the dialog is closed. */
  tickId: string | null;
  verb: BeginControl['verb'];
  candidates: readonly DispatchCandidate[];
  /** The session answer was `unknown`: a probe failed, so a pump may already run. */
  sessionUnknown: boolean;
  onCancel: () => void;
  onConfirm: (tickId: string) => void;
}

export function BeginConfirmDialog({
  tickId,
  verb,
  candidates,
  sessionUnknown,
  onCancel,
  onConfirm,
}: BeginConfirmDialogProps): ReactElement {
  const t = useTokens();
  // Which opening's dispatch was already pressed. Keyed by the tickID, so a new
  // opening starts un-pressed with no effect to reset it.
  const [pressedFor, setPressedFor] = useState<string | null>(null);
  const pressed = tickId !== null && pressedFor === tickId;
  return (
    <Dialog fullWidth maxWidth="sm" open={tickId !== null} onClose={onCancel}>
      <DialogTitle sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 15 }}>
        {verb} construction?
      </DialogTitle>
      <DialogContent
        data-testid={UI_IDENTIFIERS.Construction.BEGIN_CONFIRM_DIALOG}
        data-tick-id={tickId ?? undefined}
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
          disabled={pressed}
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            textTransform: 'none',
            color: t.bg,
            bgcolor: t.accent,
            '&:hover': { bgcolor: t.accent2 },
          }}
          variant="contained"
          onClick={() => {
            if (tickId === null || pressed) return;
            setPressedFor(tickId);
            onConfirm(tickId);
          }}
        >
          {verb} — dispatch
        </Button>
      </DialogActions>
    </Dialog>
  );
}
