/**
 * Wraps the read-only render of a COMMITTED artifact slot with a header bar that
 * carries the commit provenance and the amendment affordance.
 *
 * Header:
 *   • 'COMMITTED · revision N' — the revision suffix appears once the slot has
 *     been amended (revisions > 1).
 *   • a 'basis changed — reconcile' chip when the upstream basis has drifted
 *     (staleBasis); its Reconcile button opens the composer pre-filled.
 *   • an 'Amend' button.
 *
 * Amend composer (a small dialog, mirroring the rail composer: a free-form
 * rationale plus, optionally, the pending anchored comments already accumulated
 * in the rail). Submitting calls `onAmend(feedback)` — the caller fires the
 * existing RequestArtifactDraft mutation, which the server turns into an
 * -amend-N session seeded into the review ledger; the session view then flips to
 * the normal generating/review loop. Pending comments folded into the amendment
 * are cleared so they do not also ride a later send-back.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import TextField from '@mui/material/TextField';
import Checkbox from '@mui/material/Checkbox';
import FormControlLabel from '@mui/material/FormControlLabel';
import EditNoteIcon from '@mui/icons-material/EditNote';
import { useComments } from '../comments/CommentContext';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import type { ArtifactProvenance } from '../../contracts/types';

/**
 * Compose the quiet one-line provenance summary (PM-P2-4) from the optional record —
 * 'committed <date> · approved by X · drafted by Y' — omitting any absent field.
 * committedAt (RFC3339) is rendered as a locale date; an unparseable value falls back to the
 * raw string. Returns '' when nothing is worth showing.
 *
 * Deliberately excludes the revision number (fix 7, founder QA round 5): the
 * strip immediately above already reads "COMMITTED · revision N", and on a
 * pre-provenance commit (no committedAt/approvedBy/draftedBy) this line used
 * to render as nothing BUT "rev N" — a caption whose entire content
 * duplicated the banner ~20px above it.
 */
function provenanceSummary(provenance: ArtifactProvenance | undefined): string {
  const parts: string[] = [];
  const committedAt = provenance?.committedAt;
  if (committedAt !== undefined && committedAt.length > 0) {
    const d = new Date(committedAt);
    parts.push(`committed ${Number.isNaN(d.getTime()) ? committedAt : d.toLocaleDateString()}`);
  }
  if (provenance?.approvedBy !== undefined && provenance.approvedBy.length > 0) {
    parts.push(`approved by ${provenance.approvedBy}`);
  }
  if (provenance?.draftedBy !== undefined && provenance.draftedBy.length > 0) {
    parts.push(`drafted by ${provenance.draftedBy}`);
  }
  return parts.join(' · ');
}

export function CommittedArtifactPanel({
  revisions,
  provenance,
  amendPending,
  onAmend,
  fill = false,
  fillMinHeight,
  children,
}: {
  /** Commit count; the revision suffix shows only when > 1. */
  revisions?: number | undefined;
  /**
   * Commit provenance (PM-P2-4): who committed / when / which rail drafted it. Absent on
   * pre-provenance commits; a quiet muted one-line summary renders under the strip when
   * present.
   */
  provenance?: ArtifactProvenance | undefined;
  /** An amend RequestArtifactDraft is in flight — disable the composer submit. */
  amendPending: boolean;
  /**
   * Grow the panel (and the artifact card slot inside it) to fill a flex-column
   * parent, instead of the artifact card sitting at its fixed height with dead
   * space below. Opt-in: the System Design experience sets this for a committed
   * self-scrolling card (the glossary); the Phase-2 caller leaves it false and
   * keeps the natural, content-sized panel.
   */
  fill?: boolean;
  /** Floor for the fill panel so a short viewport scrolls the outer container
   *  instead of collapsing the card. Only consulted when `fill` is true. */
  fillMinHeight?: number | undefined;
  /**
   * Fire the amendment with the composed feedback (rationale + pending notes). `onAccepted`
   * runs ONLY once the server has accepted the request (the mutation's onSuccess) — the
   * composer consumes the folded pending comments there, so a FAILED amend keeps them (and
   * the rationale) intact for a retry instead of silently dropping them.
   */
  onAmend: (feedback: string, onAccepted: () => void) => void;
  children: ReactNode;
}): ReactNode {
  const t = useTokens();
  const { comments, reset } = useComments();
  const [open, setOpen] = useState(false);
  const [rationale, setRationale] = useState('');
  const [includePending, setIncludePending] = useState(true);

  const pendingCount = comments.length;
  const willIncludePending = includePending && pendingCount > 0;
  const canSubmit = rationale.trim().length > 0 || willIncludePending;

  const openComposer = (seed: string): void => {
    setRationale(seed);
    setIncludePending(true);
    setOpen(true);
  };

  const close = (): void => {
    setOpen(false);
    setRationale('');
  };

  const submit = (): void => {
    if (!canSubmit || amendPending) return;
    const parts: string[] = [];
    if (rationale.trim().length > 0) parts.push(rationale.trim());
    const clearRail = willIncludePending;
    if (clearRail) parts.push(...comments.map((c) => c.text));
    onAmend(parts.join('\n'), () => {
      // Only NOW — once the server has ACCEPTED the amend — consume the pending
      // comments folded into it (so they do not double-ride the next send-back)
      // and close the composer. A FAILED amend runs neither, keeping the rail and
      // the rationale intact for a retry instead of silently dropping them.
      if (clearRail) reset();
      close();
    });
  };

  const revisionN = revisions ?? 0;
  const provLine = provenanceSummary(provenance);

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 2,
        // fill: this panel is the direct child of the experience's scroll column, so
        // it owns the grow + the short-viewport floor; the header strip stays natural
        // and the artifact card slot (below) takes the remaining height.
        ...(fill ? { flexGrow: 1, minHeight: fillMinHeight ?? 0 } : {}),
      }}
    >
      <Paper
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1.5,
          flexWrap: 'wrap',
          px: 2,
          py: 1.25,
          bgcolor: t.committedBg,
          border: `1.5px solid ${t.line}`,
        }}
      >
        <Typography
          data-testid={UI_IDENTIFIERS.DesignExperience.COMMITTED_REVISION}
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 12,
            letterSpacing: '0.08em',
            color: t.committedFg,
          }}
        >
          COMMITTED{revisionN > 1 ? ` · revision ${String(revisionN)}` : ''}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <Button
          data-testid={UI_IDENTIFIERS.DesignExperience.AMEND}
          size="small"
          startIcon={<EditNoteIcon sx={{ fontSize: 18 }} />}
          sx={{ color: t.ink, borderColor: t.line, textTransform: 'none' }}
          variant="outlined"
          onClick={() => {
            openComposer('');
          }}
        >
          Amend
        </Button>
      </Paper>

      {provLine.length > 0 ? (
        <Typography sx={{ mt: -1.25, fontSize: 12, color: t.muted, lineHeight: 1.4 }}>
          {provLine}
        </Typography>
      ) : null}

      {fill ? (
        <Box sx={{ flexGrow: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {children}
        </Box>
      ) : (
        children
      )}

      <Dialog fullWidth maxWidth="sm" open={open} onClose={close}>
        <DialogTitle sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 15 }}>
          Amend committed artifact
        </DialogTitle>
        <DialogContent
          data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_COMPOSER}
          sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}
        >
          <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.5 }}>
            Send this committed artifact back for a revision. Your rationale seeds a fresh amend
            draft, then the normal review gate resumes.
          </Typography>
          <TextField
            fullWidth
            multiline
            data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_RATIONALE}
            label="Amendment rationale"
            minRows={3}
            placeholder="What should change, and why?"
            value={rationale}
            onChange={(e) => {
              setRationale(e.target.value);
            }}
          />
          {pendingCount > 0 ? (
            <Box>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={includePending}
                    data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_INCLUDE_PENDING}
                    size="small"
                    onChange={(e) => {
                      setIncludePending(e.target.checked);
                    }}
                  />
                }
                label={
                  <Typography sx={{ fontSize: 13, color: t.ink }}>
                    Include {pendingCount} pending comment{pendingCount === 1 ? '' : 's'} from the
                    rail
                  </Typography>
                }
              />
              {includePending ? (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75, mt: 0.5, pl: 1 }}>
                  {comments.map((c, i) => (
                    <Box
                      key={i}
                      sx={{
                        borderLeft: `2px solid ${t.accent}`,
                        pl: 1,
                        fontSize: 12.5,
                        color: t.muted,
                        lineHeight: 1.45,
                      }}
                    >
                      {c.text}
                    </Box>
                  ))}
                </Box>
              ) : null}
            </Box>
          ) : null}
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 2 }}>
          <Button
            data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_CANCEL}
            disabled={amendPending}
            sx={{ color: t.muted }}
            onClick={close}
          >
            Cancel
          </Button>
          <Button
            color="primary"
            data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_SUBMIT}
            disabled={!canSubmit || amendPending}
            startIcon={<EditNoteIcon />}
            variant="contained"
            onClick={submit}
          >
            Amend
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
