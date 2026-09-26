/**
 * WHO IS ON THIS GATE — the strip above the artifact under review.
 *
 * TWO ROSTER SHAPES, BOTH RENDERED, AND THEY SAY DIFFERENT THINGS
 * ---------------------------------------------------------------
 *   `reviewSet.reviewers`   `ConstructionReviewer { role, perspective, mayAmend }`
 *                           — the review engine's LIVE proposal for the gate the
 *                           activity is sitting at, present only while a session
 *                           awaits approval.
 *   `revision.reviewers`    `ConstructionReviewRosterSeat { role, actor, required }`
 *                           — the roster the SELECTED round was actually opened
 *                           with, and the actor who filled each seat.
 *
 * They are NOT collapsed into one type: "who we would ask" and "who we did ask"
 * are different claims, and a persisted round with a live gate above it has
 * both. Whichever is present is drawn; both when both are.
 *
 * `required` and `mayAmend` are TEXT on the chip, never colour alone — the whole
 * point of the chip is the reader knowing whether that reviewer can block or
 * change the artifact, and that cannot ride on a hue.
 *
 * The engine's refusal (`reviewSetError`) is a defect in the Manager's call or
 * in the engine, never an operator error, and the gate itself is unaffected —
 * so it is said out loud under `role="status"` and nothing is disabled.
 *
 * Pure and props-only.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import GavelOutlinedIcon from '@mui/icons-material/GavelOutlined';

import {
  reviewerChipLabel,
  reviewSetRefused,
  rosterSeatLabel,
  verdictLine,
} from './activityCopy.ts';
import type { components } from '../../contracts/schema.ts';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

type ReviewSet = components['schemas']['DeliveryReviewSet'];
type RosterSeat = components['schemas']['DeliveryReviewRosterSeat'];
type Verdict = components['schemas']['DeliveryReviewVerdictView'];

function Label({ text }: { text: string }): ReactNode {
  const t = useTokens();
  return (
    <Typography
      sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.18em', color: t.muted }}
    >
      {text}
    </Typography>
  );
}

export function ReviewersStrip({
  reviewSet,
  error,
  roster,
  verdicts,
}: {
  /** The LIVE set, while a gate is open. */
  reviewSet?: ReviewSet | undefined;
  /** The engine's refusal, when it refused (`reviewSetError`). */
  error?: string | undefined;
  /** The HISTORICAL roster of the selected revision — a different wire shape. */
  roster?: readonly RosterSeat[] | undefined;
  verdicts?: readonly Verdict[] | undefined;
}): ReactNode {
  const t = useTokens();
  const proposed = reviewSet?.reviewers ?? [];
  const seats = roster ?? [];
  const answers = verdicts ?? [];
  const refusal = error !== undefined && error.length > 0 && reviewSet === undefined;

  // Nothing known about this gate at all. An empty bordered strip would read as
  // "nobody reviews this", which is a claim and not one the read supports.
  if (proposed.length === 0 && seats.length === 0 && answers.length === 0 && !refusal) return null;

  return (
    <Paper data-testid={UI_IDENTIFIERS.Activity.REVIEWERS_STRIP} sx={{ p: 2 }}>
      {proposed.length > 0 ? (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
          <GavelOutlinedIcon sx={{ fontSize: 16, color: t.muted }} />
          <Label text="REVIEWERS" />
          {proposed.map((r) => (
            <Chip
              data-testid={UI_IDENTIFIERS.Activity.reviewerChip(r.role)}
              key={`proposed-${r.role}`}
              label={reviewerChipLabel(r)}
              size="small"
              sx={{
                fontFamily: t.mono,
                fontSize: 11,
                color: t.ink,
                bgcolor: t.paperAlt,
                border: `1px solid ${t.line}`,
              }}
            />
          ))}
        </Box>
      ) : null}

      {reviewSet?.reason !== undefined && reviewSet.reason.length > 0 ? (
        <Typography sx={{ mt: 1, fontSize: 13.5, color: t.ink, lineHeight: 1.45 }}>
          {reviewSet.reason}
        </Typography>
      ) : null}

      {refusal ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Activity.REVIEW_SET_ERROR}
          role="status"
          sx={{ mt: proposed.length > 0 ? 1 : 0, fontSize: 13.5, color: t.ink, lineHeight: 1.45 }}
        >
          {reviewSetRefused(error)}
        </Typography>
      ) : null}

      {seats.length > 0 ? (
        <Box
          sx={{
            mt: proposed.length > 0 || refusal ? 1.5 : 0,
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            flexWrap: 'wrap',
          }}
        >
          <Label text="THIS ROUND" />
          {seats.map((s) => (
            <Chip
              key={`seat-${s.role}-${s.actor}`}
              label={rosterSeatLabel(s)}
              size="small"
              sx={{
                fontFamily: t.mono,
                fontSize: 11,
                color: t.ink,
                bgcolor: 'transparent',
                border: `1px solid ${t.line}`,
              }}
              variant="outlined"
            />
          ))}
        </Box>
      ) : null}

      {answers.length > 0 ? (
        <Box sx={{ mt: 1.5, display: 'flex', flexDirection: 'column', gap: 0.4 }}>
          <Label text="VERDICTS" />
          {answers.map((v, i) => (
            <Typography
              key={`${v.reviewerRole}-${v.at}-${String(i)}`}
              sx={{ fontFamily: t.mono, fontSize: 12, color: t.ink, lineHeight: 1.5 }}
            >
              {verdictLine(v)}
            </Typography>
          ))}
        </Box>
      ) : null}
    </Paper>
  );
}
