/**
 * THE GRACE-WINDOW REVOKE on a decided Project Design M0 gate (spec §12 B7; use case
 * commit-to-a-project-option, revoke-window [yes] → revoke).
 *
 * Once the plan of record is committed the gate has no live round, so it has no
 * submit bar; this strip is the one verb a committed M0 still offers. It sends
 * `SubmitReviewDecision{withdraw}` on the gate, and the server — which owns the
 * grace window — reopens the SDP decision, or refuses once the window has closed.
 * The screen does not guess the window: it offers the verb on a committed plan and
 * reports a refusal verbatim (`SdpReview.DECISION_ERROR`, rendered by the body).
 *
 * Pure and props-only.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import UndoIcon from '@mui/icons-material/Undo';

import { REVOKE_LABEL, REVOKE_NOTE } from './activityCopy.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function RevokeStrip({
  pending,
  onRevoke,
}: {
  /** The revoke is in flight. */
  pending: boolean;
  onRevoke: () => void;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.SdpReview.REVOKE_STRIP}
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        flexWrap: 'wrap',
        px: 2,
        py: 1.5,
        bgcolor: t.paperAlt,
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
      }}
    >
      <Typography sx={{ fontSize: 13, color: t.ink, lineHeight: 1.5, minWidth: 240, flex: 1 }}>
        {REVOKE_NOTE}
      </Typography>
      <Button
        data-testid={UI_IDENTIFIERS.SdpReview.REVOKE}
        disabled={pending}
        size="small"
        startIcon={<UndoIcon sx={{ fontSize: 16 }} />}
        sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
        variant="outlined"
        onClick={onRevoke}
      >
        {REVOKE_LABEL}
      </Button>
    </Box>
  );
}
