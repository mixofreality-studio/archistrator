/**
 * THE AMEND EXIT on a COMMITTED design slot (spec §12 B7) — the Activity
 * Experience's surface for use case add-a-use-case-to-an-in-flight-project
 * (capture-uc) and replan-under-scope-change (back-to-design).
 *
 * A committed slot has no live gate, so it has no submit bar; before this the only
 * way to change one from the SPA was the stale-basis reconcile, which needs an
 * upstream change to have happened first. Amend is the architect's own reason: the
 * rationale (a new use case's actor, trigger and outcome, or the change asked for)
 * seeds a redraft of this artifact through `DispatchActivityTask`, the redraft comes
 * back to this same gate for review, and re-committing it flags downstream slots
 * stale — where `MarkReviewedUnaffected` or another amendment reconciles them.
 *
 * It reuses the DesignExperience AMEND_COMPOSER / AMEND_RATIONALE / AMEND_SUBMIT /
 * AMEND_CANCEL ids, so the amendment is named by one selector set on every surface
 * that offers it; only the trigger is this screen's own (`Activity.AMEND_OPEN`).
 * The rationale's id rides on the textarea itself, so a black-box `fill` lands on
 * the field it names.
 *
 * Pure and props-only: the container owns the op and its pending/error state.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Paper from '@mui/material/Paper';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import EditNoteIcon from '@mui/icons-material/EditNote';

import {
  AMEND_CANCEL,
  AMEND_INTRO,
  AMEND_OPEN,
  AMEND_RATIONALE_LABEL,
  AMEND_SUBMIT,
  AMEND_TITLE,
  amendFailed,
} from './activityCopy.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function AmendComposer({
  pending,
  error,
  onSubmit,
}: {
  /** The amendment's dispatch is in flight. */
  pending: boolean;
  /** What the server said when it refused the last amendment; absent otherwise. */
  error: string | undefined;
  /** Send the trimmed, non-empty rationale. */
  onSubmit: (rationale: string) => void;
}): ReactNode {
  const t = useTokens();
  const [open, setOpen] = useState(false);
  const [rationale, setRationale] = useState('');
  const trimmed = rationale.trim();

  if (!open) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          data-testid={UI_IDENTIFIERS.Activity.AMEND_OPEN}
          size="small"
          startIcon={<EditNoteIcon sx={{ fontSize: 18 }} />}
          sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
          variant="outlined"
          onClick={() => {
            setOpen(true);
          }}
        >
          {AMEND_OPEN}
        </Button>
      </Box>
    );
  }

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_COMPOSER}
      sx={{ p: 2.5, display: 'flex', flexDirection: 'column', gap: 1.5 }}
    >
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.ink }}>
        {AMEND_TITLE}
      </Typography>
      <Typography sx={{ fontSize: 13, color: t.muted, lineHeight: 1.5 }}>{AMEND_INTRO}</Typography>
      <TextField
        fullWidth
        multiline
        label={AMEND_RATIONALE_LABEL}
        minRows={3}
        slotProps={{
          htmlInput: { 'data-testid': UI_IDENTIFIERS.DesignExperience.AMEND_RATIONALE },
        }}
        value={rationale}
        onChange={(e) => {
          setRationale(e.target.value);
        }}
      />
      {error !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Activity.AMEND_ERROR}
          role="alert"
          sx={{ fontSize: 13, color: t.dangerFg, lineHeight: 1.5 }}
        >
          {amendFailed(error)}
        </Typography>
      ) : null}
      <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1.5 }}>
        <Button
          data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_CANCEL}
          disabled={pending}
          sx={{ color: t.muted, textTransform: 'none' }}
          onClick={() => {
            setOpen(false);
            setRationale('');
          }}
        >
          {AMEND_CANCEL}
        </Button>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_SUBMIT}
          disabled={pending || trimmed.length === 0}
          startIcon={<EditNoteIcon />}
          sx={{ textTransform: 'none' }}
          variant="contained"
          onClick={() => {
            onSubmit(trimmed);
          }}
        >
          {AMEND_SUBMIT}
        </Button>
      </Box>
    </Paper>
  );
}
