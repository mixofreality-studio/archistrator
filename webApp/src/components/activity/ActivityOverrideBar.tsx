/**
 * THE OPERATOR'S TWO OVERRIDES, as two actions — Steer a live escalation, or Reopen a
 * finished activity (stage 4b1 Task 14, controller ruling 3).
 *
 * WHY IT IS ITS OWN SURFACE and not a verb on the submit bar: neither override
 * belongs to a GATE. A steer answers an escalation — a dispatch that failed, where no
 * round is open and nobody is being asked to approve anything — and a reopen answers
 * an activity that is OVER, which has no live task at all. The submit bar renders
 * only while a decision is owed on the revision on screen, so a verb for either would
 * have been unreachable exactly when it is needed.
 *
 * ONE ACTION AT A TIME, and the other is ABSENT rather than disabled: the house rule
 * is that a control that cannot be used is not a control, and `activityOverride.ts`
 * answers which of the two an activity can take (never both — a live escalation has
 * not exited, and an exited activity has no child to steer).
 *
 * THE NOTE IS REQUIRED, and this bar says so before the click rather than after: the
 * Manager refuses an override with empty notes — *"it is the operator's durable record
 * of WHY the automatic path was steered"* — so the button stays disabled until there
 * is one. That refusal is not a validation error to surface; it is a sentence the
 * screen can honour.
 *
 * Pure and props-only (the note is this surface's own draft state, which nothing
 * outside it reads).
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Paper from '@mui/material/Paper';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import RestartAltIcon from '@mui/icons-material/RestartAlt';
import ReplayIcon from '@mui/icons-material/Replay';

import {
  OVERRIDE_NOTE_LABEL,
  OVERRIDE_NOTE_REQUIRED,
  overrideFailed,
  REOPEN_CONSEQUENCE,
  REOPEN_HEADING,
  REOPEN_LABEL,
  STEER_CONSEQUENCE,
  STEER_HEADING,
  STEER_LABEL,
} from './activityCopy.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function ActivityOverrideBar({
  action,
  pending,
  error,
  onSubmit,
}: {
  /** Which of the two this activity can take. `none` is not passed — the container
   *  renders nothing at all then. */
  action: 'steer' | 'reopen';
  pending: boolean;
  /** The Manager's own refusal, verbatim, where one arrived. */
  error: string | undefined;
  onSubmit: (note: string) => void;
}): ReactNode {
  const t = useTokens();
  const [note, setNote] = useState('');
  const steering = action === 'steer';
  const ready = note.trim().length > 0 && !pending;
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Activity.OVERRIDE_BAR}
      sx={{
        p: 2,
        display: 'flex',
        flexDirection: 'column',
        gap: 1.25,
        bgcolor: t.paperAlt,
        border: `1.5px solid ${t.awaitingFg}`,
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12,
          letterSpacing: '0.06em',
          color: t.awaitingFg,
        }}
      >
        {steering ? STEER_HEADING : REOPEN_HEADING}
      </Typography>
      <Typography sx={{ fontSize: 13, color: t.ink, lineHeight: 1.5 }}>
        {steering ? STEER_CONSEQUENCE : REOPEN_CONSEQUENCE}
      </Typography>
      <TextField
        multiline
        data-testid={UI_IDENTIFIERS.Activity.OVERRIDE_NOTE}
        helperText={OVERRIDE_NOTE_REQUIRED}
        label={OVERRIDE_NOTE_LABEL}
        minRows={2}
        size="small"
        value={note}
        onChange={(e) => {
          setNote(e.target.value);
        }}
      />
      {error !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Activity.OVERRIDE_ERROR}
          role="alert"
          sx={{ fontSize: 12.5, color: t.dangerFg, lineHeight: 1.5 }}
        >
          {overrideFailed(error)}
        </Typography>
      ) : null}
      <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          color="inherit"
          data-testid={steering ? UI_IDENTIFIERS.Activity.STEER : UI_IDENTIFIERS.Activity.REOPEN}
          disabled={!ready}
          size="small"
          startIcon={
            steering ? (
              <ReplayIcon sx={{ fontSize: 16 }} />
            ) : (
              <RestartAltIcon sx={{ fontSize: 16 }} />
            )
          }
          sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
          variant="outlined"
          onClick={() => {
            onSubmit(note.trim());
          }}
        >
          {steering ? STEER_LABEL : REOPEN_LABEL}
        </Button>
      </Box>
    </Paper>
  );
}
