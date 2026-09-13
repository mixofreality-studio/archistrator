/**
 * THE REVIEW BODY — artifact above, verdict below.
 *
 * A gate task is the one place a human decision is owed, so the body puts the
 * thing under review at the top (the SAME `ArtifactRender` the artifact body
 * uses — one renderer reached two ways, so a review can never show a different
 * artifact from the one the artifact body shows) and what is known about the
 * decision underneath.
 *
 * WHAT IS KNOWN IS LESS THAN IT LOOKS, AND THIS SAYS SO
 * ----------------------------------------------------
 * `awaitPhaseDecision` never dereferences `sig.Feedback`: the reviewer's
 * structured verdict is carried on the signal, never read, never stored. So
 * there is no PASS / PASS-WITH-NOTES / FAIL to render — not because this surface
 * has not got to it, but because the value was dropped upstream. The verdict
 * block therefore shows:
 *
 *   - the REVIEWER SET (who the reviewEngine asked), styled as a roster and
 *     never as an outcome, and
 *   - any surviving PROSE on the produced records, stamped
 *     `≈ reconstructed from the produced-record note` and rendered as prose —
 *     never with a verdict chip synthesized from its wording.
 *
 * The rule, the stamp and the anchor paths live in reviewVerdict.ts, where they
 * are tested without a renderer.
 *
 * COMMENTS ARE ITEM-GRANULAR
 * --------------------------
 * Both lists render through `CommentableList`, so each reviewer row and each
 * note arms its own anchor in `CommentProvider` (`setAnchor`) and a send-back
 * carries per-item feedback rather than one undifferentiated blob. That
 * mechanism already works end to end — the construction console's `sendBackPhase`
 * pulls `toWire()` into the phase-decision feedback — so this is a matter of
 * arming the right anchors, not of building a pipe.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

import type { ConstructionReviewSet } from '../../../../contracts/types';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { CommentableList } from '../../../comments/CommentableList';
import { RoleAvatar } from '../../../RoleAvatar';
import { ArtifactRender, type ArtifactBodyProps } from './ArtifactBody';
import {
  producedNoteAnchorPath,
  reviewerAnchorPath,
  reviewVerdictFor,
  type ReconstructedNote,
  type ReviewerRow,
} from './reviewVerdict.ts';

export interface ReviewBodyProps extends ArtifactBodyProps {
  /** The live reviewEngine set, when this activity is the one at a phase gate. */
  reviewSet?: ConstructionReviewSet | undefined;
}

export function ReviewBody({ reviewSet, ...artifact }: ReviewBodyProps): ReactElement {
  const t = useTokens();
  const verdict = reviewVerdictFor(artifact.row, reviewSet);
  const activityId = artifact.row?.activityId ?? '—';

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY_REVIEW}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      <ArtifactRender {...artifact} />

      <Box
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_VERDICT}
        sx={{ pt: 1.25, borderTop: `1.5px solid ${t.line}` }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 10,
            letterSpacing: '0.08em',
            color: t.muted,
          }}
        >
          VERDICT
        </Typography>
        <Tooltip title={verdict.detail}>
          <Typography
            sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5, mt: 0.5 }}
          >
            {verdict.statement}
          </Typography>
        </Tooltip>

        {verdict.reviewers.length > 0 ? (
          <Box sx={{ mt: 1.25 }}>
            <BlockLabel t={t} text={`Reviewer set · ${String(verdict.reviewers.length)}`} />
            <CommentableList
              ariaLabel="Reviewer set"
              gap={0.5}
              getAnchor={(reviewer: ReviewerRow, index) => ({
                kind: 'node',
                label: reviewer.role,
                source: `Construction · ${activityId} review`,
                jsonPath: reviewerAnchorPath(activityId, index),
                anchorText: `${reviewer.role} — ${reviewer.perspective}`,
              })}
              getKey={(reviewer: ReviewerRow, index) => `${reviewer.role}-${String(index)}`}
              getLabel={(reviewer: ReviewerRow) => reviewer.role}
              getLabelKind={() => 'reviewer'}
              items={verdict.reviewers}
              renderItem={(reviewer: ReviewerRow) => <ReviewerLine reviewer={reviewer} t={t} />}
            />
          </Box>
        ) : null}

        {verdict.notes.length > 0 ? (
          <Box sx={{ mt: 1.5 }}>
            {/* The stamp is not decoration: it is the whole claim this block is
                allowed to make. A produced-record note is prose that survived,
                not a verdict that was recorded. */}
            <Typography
              data-testid={UI_IDENTIFIERS.Construction.DETAIL_VERDICT_STAMP}
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 9.5,
                letterSpacing: '0.06em',
                color: t.ink,
                mb: 0.75,
              }}
            >
              {verdict.stamp}
            </Typography>
            <CommentableList
              ariaLabel="Produced-record notes"
              gap={0.5}
              getAnchor={(note: ReconstructedNote) => ({
                kind: 'text',
                label: note.title,
                source: `Construction · ${activityId} produced record`,
                jsonPath: producedNoteAnchorPath(activityId, note.index),
                anchorText: note.text,
              })}
              getKey={(note: ReconstructedNote) => `note-${String(note.index)}`}
              getLabel={(note: ReconstructedNote) => note.title}
              getLabelKind={() => 'produced record'}
              items={verdict.notes}
              renderItem={(note: ReconstructedNote) => <NoteLine note={note} t={t} />}
            />
          </Box>
        ) : null}
      </Box>
    </Box>
  );
}

function BlockLabel({ text, t }: { text: string; t: Tokens }): ReactElement {
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 9.5,
        letterSpacing: '0.06em',
        color: t.muted,
        mb: 0.5,
      }}
    >
      {text.toUpperCase()}
    </Typography>
  );
}

/**
 * One reviewer. A ROSTER row, deliberately: no chip, no colour, nothing that
 * could be read as an outcome, because no outcome was recorded. `mayAmend` is
 * shown because it is a real, stored property of the reviewer's mandate.
 */
function ReviewerLine({ reviewer, t }: { reviewer: ReviewerRow; t: Tokens }): ReactElement {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
      <RoleAvatar seed={reviewer.role} size={22} />
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700, color: t.ink }}>
          {reviewer.role}
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.4 }}>
          {reviewer.perspective}
        </Typography>
      </Box>
      <Typography
        sx={{
          flexShrink: 0,
          fontFamily: t.mono,
          fontSize: 9,
          color: t.muted,
          letterSpacing: '0.06em',
        }}
      >
        {reviewer.mayAmend ? 'MAY AMEND' : 'ADVISORY'}
      </Typography>
    </Box>
  );
}

function NoteLine({ note, t }: { note: ReconstructedNote; t: Tokens }): ReactElement {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
        {note.kind}
        {note.source.length > 0 ? ` · ${note.source}` : ''}
      </Typography>
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}>
        {note.text}
      </Typography>
    </Box>
  );
}
