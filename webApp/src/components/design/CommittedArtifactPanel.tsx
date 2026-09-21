/**
 * Wraps the read-only render of a COMMITTED artifact slot. The full-width
 * 'COMMITTED · revision N' strip + inline Amend button (Task 9) are gone — the
 * caller now renders `CommittedChip` (below) in the artifact header instead, at a
 * fraction of the vertical cost, and the Amend affordance moves to Task 10's
 * submit bar. This panel is left carrying only the amend composer's Dialog: its
 * `open` state is LIFTED to the caller (RULING P6 — `amendOpen`/`onAmendOpenChange`
 * props, no imperative handle, no ref) so Task 10's button can open the very same
 * dialog by flipping that state.
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
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Tooltip from '@mui/material/Tooltip';
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import TextField from '@mui/material/TextField';
import Checkbox from '@mui/material/Checkbox';
import FormControlLabel from '@mui/material/FormControlLabel';
import EditNoteIcon from '@mui/icons-material/EditNote';
import CheckIcon from '@mui/icons-material/Check';
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
 * chip beside it already reads "committed · rN", and on a pre-provenance commit
 * (no committedAt/approvedBy/draftedBy) this line used to render as nothing BUT
 * "rev N" — a caption whose entire content duplicated the chip beside it.
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

/**
 * The committed-panel header, collapsed to a chip (Task 9): 'committed' / 'committed
 * · r{n}' (the revision suffix appears once the slot has been amended, revisions >
 * 1), styled to match `StageChip`'s committed variant. The provenance line that used
 * to sit under the full-width strip is absorbed into this chip's tooltip instead of
 * taking its own line — `provenanceSummary` returns '' when there is nothing to
 * show, which renders an empty (no-op) tooltip.
 */
export function CommittedChip({
  revisions,
  provenance,
}: {
  /** Commit count; the revision suffix shows only when > 1. */
  revisions?: number | undefined;
  /** Commit provenance (PM-P2-4): who committed / when / which rail drafted it. */
  provenance?: ArtifactProvenance | undefined;
}): ReactNode {
  const t = useTokens();
  const revisionN = revisions ?? 0;
  const label = revisionN > 1 ? `committed · r${String(revisionN)}` : 'committed';
  return (
    <Tooltip title={provenanceSummary(provenance)}>
      <Chip
        icon={<CheckIcon sx={{ fontSize: 15 }} />}
        label={label}
        size="small"
        sx={{
          color: t.committedFg,
          bgcolor: t.committedBg,
          '& .MuiChip-icon': { color: t.committedFg, ml: 0.75 },
        }}
      />
    </Tooltip>
  );
}

export function CommittedArtifactPanel({
  amendOpen,
  amendPending,
  onAmend,
  onAmendOpenChange,
  fill = false,
  fillMinHeight,
  children,
}: {
  /**
   * The amend composer dialog's open state, lifted to the caller (RULING P6):
   * Task 10's submit-bar Amend button flips this directly — no imperative handle,
   * no ref.
   */
  amendOpen: boolean;
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
  onAmendOpenChange: (open: boolean) => void;
  children: ReactNode;
}): ReactNode {
  const t = useTokens();
  const { comments, reset } = useComments();
  const [rationale, setRationale] = useState('');
  const [includePending, setIncludePending] = useState(true);

  // The open trigger now lives one level up (the caller's Amend button), so seed
  // the composer's local fields whenever `amendOpen` flips to true — mirrors the
  // old openComposer('') seed that used to run from this component's own
  // (now-deleted) Amend button. Adjusted during render, not an effect, per
  // https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes.
  const [prevAmendOpen, setPrevAmendOpen] = useState(amendOpen);
  if (amendOpen !== prevAmendOpen) {
    setPrevAmendOpen(amendOpen);
    if (amendOpen) {
      setRationale('');
      setIncludePending(true);
    }
  }

  const pendingCount = comments.length;
  const willIncludePending = includePending && pendingCount > 0;
  const canSubmit = rationale.trim().length > 0 || willIncludePending;

  const close = (): void => {
    onAmendOpenChange(false);
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

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 2,
        // fill: this panel is the direct child of the experience's scroll column, so
        // it owns the grow + the short-viewport floor for the artifact card slot below.
        ...(fill ? { flexGrow: 1, minHeight: fillMinHeight ?? 0 } : {}),
      }}
    >
      {fill ? (
        <Box sx={{ flexGrow: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {children}
        </Box>
      ) : (
        children
      )}

      <Dialog fullWidth maxWidth="sm" open={amendOpen} onClose={close}>
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
