/**
 * Terminal-failure panel, the anti-wedge counterpart of the server's draftFailed
 * stage. Shown when a co-authoring session reaches a terminal failure:
 *
 *   • `refused` (ordinal 6) — a terminal worker fault (the AI worker is out of
 *     credits / unavailable) on the inline path.
 *   • `draftFailed` — the dispatched ASYNC design job (a GitHub Action in the
 *     user's CI) reached a typed terminal failure phase (the draft job failed or a
 *     required CI check went red).
 *
 * Either way this replaces the otherwise-wedged GeneratingScene with a clear "Draft
 * failed" heading, the server's human `failureReason`, and human-actionable exits:
 * a prominent Retry (re-dispatches drafting on the same live session — a fresh
 * idempotency key) and, when wired, a Withdraw that abandons the artifact session.
 * Recolored from tokens to match the retro chrome.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import ReplayIcon from '@mui/icons-material/Replay';
import CloseIcon from '@mui/icons-material/Close';
import ReportProblemOutlinedIcon from '@mui/icons-material/ReportProblemOutlined';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

const FALLBACK_REASON = "The AI worker couldn't produce a draft.";
const ASYNC_FALLBACK_REASON =
  'The design job failed in your CI. Retry to dispatch a fresh run, or withdraw this artifact.';

export function DraftFailedPanel({
  artifact,
  reason,
  pending,
  onRetry,
  onWithdraw,
  withdrawPending = false,
  async: isAsync = false,
}: {
  /** Title of the artifact whose draft failed (for the header label). */
  artifact: string;
  /** Server's human explanation; falls back to a generic message when empty. */
  reason: string | undefined;
  /** A retry mutation is in flight — disable the button. */
  pending: boolean;
  onRetry: () => void;
  /** Optional withdraw exit — abandons the artifact session. Hidden when absent. */
  onWithdraw?: (() => void) | undefined;
  /** A withdraw mutation is in flight — disable the button. */
  withdrawPending?: boolean;
  /**
   * The async-CI variant (the `draftFailed` stage): tunes the copy/labels to the
   * "your design job failed" framing rather than the inline worker-fault framing.
   */
  async?: boolean;
}): ReactNode {
  const t = useTokens();
  const fallback = isAsync ? ASYNC_FALLBACK_REASON : FALLBACK_REASON;
  const message = reason !== undefined && reason.length > 0 ? reason : fallback;
  const heading = isAsync ? 'Design job failed' : 'Draft failed';

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.DesignExperience.DRAFT_FAILED}
      sx={{
        p: 0,
        overflow: 'hidden',
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
      }}
    >
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 2.5,
          py: 1.5,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
        }}
      >
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, letterSpacing: '0.16em', color: 'error.main' }}>
          {heading.toUpperCase()} · {artifact.toUpperCase()}
        </Typography>
      </Box>

      <Box sx={{ px: 3, py: 4, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 1.5, textAlign: 'center' }}>
        <ReportProblemOutlinedIcon sx={{ fontSize: 38, color: 'error.main' }} />
        <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 22, color: t.ink }}>
          {heading}
        </Typography>
        <Typography
          data-testid={UI_IDENTIFIERS.DesignExperience.DRAFT_FAILURE_REASON}
          sx={{ color: t.muted, maxWidth: 520, lineHeight: 1.6 }}
        >
          {message}
        </Typography>
        <Box sx={{ display: 'flex', gap: 1.5, mt: 1 }}>
          <Button
            color="primary"
            data-testid={UI_IDENTIFIERS.DesignExperience.RETRY_DRAFT}
            disabled={pending || withdrawPending}
            startIcon={<ReplayIcon />}
            variant="contained"
            onClick={onRetry}
          >
            {isAsync ? 'Retry design job' : 'Retry draft'}
          </Button>
          {onWithdraw !== undefined ? (
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.DesignExperience.WITHDRAW_DRAFT}
              disabled={pending || withdrawPending}
              startIcon={<CloseIcon />}
              sx={{ color: t.muted }}
              variant="outlined"
              onClick={onWithdraw}
            >
              Withdraw
            </Button>
          ) : null}
        </Box>
      </Box>
    </Paper>
  );
}
