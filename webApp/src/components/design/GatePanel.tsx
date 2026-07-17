/**
 * The human review gate, shown when the session stage is awaitingReview. Surfaces
 * the machine-validation findings (severity-colored) and the commit authority:
 *   Approve & continue → submitReviewDecision(approve) then auto-advance
 *   Send back          → submitReviewDecision(reject, { feedback, comments })
 *   Withdraw           → submitReviewDecision(withdraw)
 * Send-back is disabled until at least one feedback entry — a free-form note or an
 * anchored comment — is accumulated, so the redraft always carries guidance. That
 * accumulation happens client-side (SPA: CommentContext's pending-comment count;
 * MCP: there is no such client-side accumulator — see `allowEmptySendBack`).
 * Findings are the real engine.Finding[] from the
 * session view; an empty findings list reads as "all checks passed".
 */
import { useId, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Collapse from '@mui/material/Collapse';
import Alert from '@mui/material/Alert';
import AlertTitle from '@mui/material/AlertTitle';
import CheckIcon from '@mui/icons-material/Check';
import ReplayIcon from '@mui/icons-material/Replay';
import UndoIcon from '@mui/icons-material/Undo';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import type { Finding, PmCritiqueView } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { sendBackDisabled } from './sendBackLogic';
import { pmReviewPresentation } from './pmReviewLogic';

export function GatePanel({
  findings,
  critique,
  commentCount,
  openCommentCount = 0,
  gateError,
  pending,
  allowEmptySendBack = false,
  onApprove,
  onSendBack,
  onWithdraw,
}: {
  findings: Finding[];
  /**
   * The surfaced PM-critique conclusion for the draft under review (F-QA2-7):
   * rendered as the "PM REVIEW" section so the founder never approves a
   * PM-reviewed artifact blind to what the PM concluded. Absent for
   * architect-owned kinds (no PM critic) and on Phase-2/construction gates —
   * the section simply does not render.
   */
  critique?: PmCritiqueView | undefined;
  /** Number of accumulated (client-side, unsent) anchored send-back comments. */
  commentCount: number;
  /**
   * Open entries on the SERVER review thread. While > 0 the server rejects Approve
   * (FailedPrecondition), so we disable it here and name the count. Each must be
   * addressed (agent response) or waived first.
   */
  openCommentCount?: number;
  /** Graceful message from a FailedPrecondition approve race — refetch + surface. */
  gateError?: string | undefined;
  /** A decision mutation is in flight — disable the buttons. */
  pending: boolean;
  /**
   * SPA default (false): "Send back" stays disabled until `commentCount > 0`, so
   * the redraft always carries client-accumulated guidance. MCP has no client-side
   * comment accumulator (feedback text is collected AFTER the click, in the host's
   * own composer) — `commentCount` there is always 0, which would make send-back
   * permanently unreachable. MCP passes `true` here to unblock the click; the
   * composer that follows enforces non-empty feedback before it actually submits
   * the reject decision, so the "redraft always carries guidance" invariant still
   * holds — it's just enforced one step later, by the composer, not this button.
   */
  allowEmptySendBack?: boolean;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
}): ReactNode {
  const t = useTokens();
  const [showFindings, setShowFindings] = useState(true);
  const findingsRegionId = useId();
  const [showPmReview, setShowPmReview] = useState(true);
  const pmReviewRegionId = useId();
  const pmReview = critique !== undefined ? pmReviewPresentation(critique) : undefined;
  const approveBlocked = openCommentCount > 0;
  // Two-step approve when notes are pending: accumulated comments ride the next
  // "Send back", so approving discards them. We make that loss explicit (never
  // block it) by flipping the primary button into an inline confirm strip.
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const errors = findings.filter((f) => f.severity === 'error').length;
  const warnings = findings.filter((f) => f.severity === 'warning').length;
  const oks = findings.length - errors - warnings;

  const onApproveClick = (): void => {
    if (commentCount > 0 && !confirmDiscard) {
      setConfirmDiscard(true);
      return;
    }
    onApprove();
  };

  return (
    <Paper data-testid={UI_IDENTIFIERS.GatePanel.ROOT} sx={{ p: 0, overflow: 'hidden' }}>
      {/* A real <button> (not a click-only Box) so the disclosure is keyboard
          operable and announces its expanded state to assistive tech. */}
      <Box
        aria-controls={findingsRegionId}
        aria-expanded={showFindings}
        component="button"
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 2.5,
          py: 1.5,
          cursor: 'pointer',
          width: '100%',
          textAlign: 'left',
          font: 'inherit',
          color: 'inherit',
          border: 'none',
          bgcolor: t.paperAlt,
          borderBottom: showFindings ? `1.5px solid ${t.line}` : 'none',
        }}
        type="button"
        onClick={() => {
          setShowFindings((v) => !v);
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}
        >
          MACHINE VALIDATION
        </Typography>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 11,
            color: errors > 0 ? t.awaitingFg : warnings > 0 ? t.awaitingFg : t.committedFg,
            border: `1.5px solid ${t.line}`,
            borderRadius: 1,
            px: 0.75,
          }}
        >
          {errors} ERR · {warnings} WARN · {oks} INFO
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <ExpandMoreIcon
          sx={{ transform: showFindings ? 'rotate(180deg)' : 'none', transition: '120ms' }}
        />
      </Box>

      <Collapse id={findingsRegionId} in={showFindings}>
        <Box
          data-testid={UI_IDENTIFIERS.GatePanel.FINDINGS}
          sx={{ p: 2.5, pt: 2, display: 'flex', flexDirection: 'column', gap: 1.25 }}
        >
          {findings.length === 0 ? (
            <Alert severity="success" sx={{ alignItems: 'flex-start' }}>
              All machine checks passed — no findings on this draft.
            </Alert>
          ) : (
            findings.map((f, i) => (
              <Alert
                key={`${f.ruleId}-${String(i)}`}
                severity={f.severity}
                sx={{ alignItems: 'flex-start' }}
              >
                <AlertTitle
                  sx={{ fontFamily: t.mono, fontSize: 12, letterSpacing: '0.06em', mb: 0.25 }}
                >
                  {f.ruleId}
                  {f.location != null && f.location.section.length > 0
                    ? ` · ${f.location.section}`
                    : ''}
                </AlertTitle>
                {f.message}
              </Alert>
            ))
          )}
        </Box>
      </Collapse>

      {/* PM REVIEW (F-QA2-7): the PM critique's actual conclusion — verdict badge +
          rationale — rendered as its own disclosure, visually distinct from the
          deterministic MACHINE VALIDATION above (pm-tinted header, agent-review
          framing). Mirrors the findings disclosure's a11y pattern: a real <button>
          with aria-expanded/aria-controls. */}
      {pmReview !== undefined ? (
        <>
          <Box
            aria-controls={pmReviewRegionId}
            aria-expanded={showPmReview}
            component="button"
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              px: 2.5,
              py: 1.5,
              cursor: 'pointer',
              width: '100%',
              textAlign: 'left',
              font: 'inherit',
              color: 'inherit',
              border: 'none',
              bgcolor: t.chatPmBg,
              borderTop: `1.5px solid ${t.line}`,
              borderBottom: showPmReview ? `1.5px solid ${t.line}` : 'none',
            }}
            type="button"
            onClick={() => {
              setShowPmReview((v) => !v);
            }}
          >
            <Typography
              sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}
            >
              {pmReview.heading}
            </Typography>
            <Typography
              data-testid={UI_IDENTIFIERS.GatePanel.PM_REVIEW_BADGE}
              sx={{
                fontFamily: t.mono,
                fontSize: 11,
                fontWeight: 700,
                color: pmReview.approved ? t.committedFg : t.awaitingFg,
                border: `1.5px solid ${t.line}`,
                borderRadius: 1,
                px: 0.75,
              }}
            >
              {pmReview.badge}
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            <ExpandMoreIcon
              sx={{ transform: showPmReview ? 'rotate(180deg)' : 'none', transition: '120ms' }}
            />
          </Box>
          <Collapse id={pmReviewRegionId} in={showPmReview}>
            <Box
              data-testid={UI_IDENTIFIERS.GatePanel.PM_REVIEW}
              sx={{ p: 2.5, pt: 2, display: 'flex', flexDirection: 'column', gap: 0.75 }}
            >
              <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                {pmReview.caption}
              </Typography>
              <Typography sx={{ color: t.ink, whiteSpace: 'pre-wrap' }} variant="body2">
                {pmReview.summary}
              </Typography>
            </Box>
          </Collapse>
        </>
      ) : null}

      {/* Open server-thread entries block approve until addressed or waived. */}
      {approveBlocked ? (
        <Box data-testid={UI_IDENTIFIERS.GatePanel.OPEN_BLOCK} sx={{ px: 2.5, pt: 2 }}>
          <Alert severity="warning" sx={{ alignItems: 'flex-start' }}>
            {openCommentCount} open comment{openCommentCount === 1 ? '' : 's'} must be addressed or
            waived before approve.
          </Alert>
        </Box>
      ) : null}

      {/* Graceful FailedPrecondition surface after an approve race (thread refetched). */}
      {gateError !== undefined && gateError.length > 0 ? (
        <Box data-testid={UI_IDENTIFIERS.GatePanel.GATE_ERROR} sx={{ px: 2.5, pt: 2 }}>
          <Alert severity="error" sx={{ alignItems: 'flex-start' }}>
            {gateError}
          </Alert>
        </Box>
      ) : null}

      <Box
        sx={{
          px: 2.5,
          py: 2,
          borderTop: `1.5px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 1.5,
          bgcolor: t.awaitingBg,
          flexWrap: 'wrap',
        }}
      >
        <Box sx={{ minWidth: 0 }}>
          <Typography
            sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.awaitingFg }}
          >
            You are the commit authority
          </Typography>
          <Typography sx={{ color: t.awaitingFg, opacity: 0.85 }} variant="caption">
            {commentCount > 0
              ? `${String(commentCount)} note${commentCount === 1 ? '' : 's'} ready to send back.`
              : 'Approve to seal and auto-advance, or type feedback then send back for a redraft.'}
          </Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        {confirmDiscard ? (
          <Box
            data-testid={UI_IDENTIFIERS.GatePanel.APPROVE_CONFIRM}
            sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}
          >
            <Typography sx={{ fontSize: 13, color: t.awaitingFg, fontWeight: 600 }}>
              {commentCount} note{commentCount === 1 ? '' : 's'} will be discarded on approve — send
              back first to keep {commentCount === 1 ? 'it' : 'them'}.
            </Typography>
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.GatePanel.APPROVE_CANCEL}
              disabled={pending}
              sx={{ color: t.muted }}
              variant="text"
              onClick={() => {
                setConfirmDiscard(false);
              }}
            >
              Cancel
            </Button>
            <Button
              color="primary"
              data-testid={UI_IDENTIFIERS.GatePanel.APPROVE}
              disabled={pending || approveBlocked}
              startIcon={<CheckIcon />}
              variant="contained"
              onClick={onApproveClick}
            >
              Approve anyway
            </Button>
          </Box>
        ) : (
          <>
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.GatePanel.WITHDRAW}
              disabled={pending}
              startIcon={<UndoIcon />}
              sx={{ color: t.muted }}
              variant="text"
              onClick={onWithdraw}
            >
              Withdraw
            </Button>
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.GatePanel.SENDBACK}
              disabled={sendBackDisabled(pending, commentCount, allowEmptySendBack)}
              startIcon={<ReplayIcon />}
              sx={{ color: t.ink, borderColor: t.line }}
              variant="outlined"
              onClick={onSendBack}
            >
              Send back
            </Button>
            <Button
              color="primary"
              data-testid={UI_IDENTIFIERS.GatePanel.APPROVE}
              disabled={pending || approveBlocked}
              startIcon={<CheckIcon />}
              variant="contained"
              onClick={onApproveClick}
            >
              Approve &amp; continue
            </Button>
          </>
        )}
      </Box>
    </Paper>
  );
}
