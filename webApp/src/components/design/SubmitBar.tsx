/**
 * The ONE bar every review verb converges through (Task 10). Before this, "I want
 * this changed" landed in three different places depending on the artifact's
 * lifecycle stage: the committed header's Amend button, the gate panel's Send
 * back, and the margin foot's Ask. `resolveSubmitVerb` (submitVerb.ts) picks the
 * single primary verb from what is staged and whether the slot is committed; this
 * component renders it, the consequence line beneath it (founder: "I want to know
 * what this button will actually do"), and an overflow menu for the verbs that
 * are never primary (Withdraw, Retry).
 *
 * Mounted sticky at the bottom of the System Design scroll column
 * (SystemDesignView) whenever there is a live review surface to act on — the
 * draft's gate (awaitingReview) or a committed slot. Renders nothing when there
 * is truly nothing to do: a clean committed slot offers no primary verb, and
 * (in that mounting) no overflow action either.
 */
import { useState, type ReactNode } from 'react';
import Paper from '@mui/material/Paper';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Menu from '@mui/material/Menu';
import MenuItem from '@mui/material/MenuItem';
import CircularProgress from '@mui/material/CircularProgress';
import CheckIcon from '@mui/icons-material/Check';
import ReplayIcon from '@mui/icons-material/Replay';
import EditNoteIcon from '@mui/icons-material/EditNote';
import QuestionAnswerOutlinedIcon from '@mui/icons-material/QuestionAnswerOutlined';
import MoreVertIcon from '@mui/icons-material/MoreVert';
import RestartAltIcon from '@mui/icons-material/RestartAlt';
import UndoIcon from '@mui/icons-material/Undo';

import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { resolveSubmitVerb, type SubmitAction } from './submitVerb';

/** The icon that fronts each primary verb — purely decorative, keyed by action. */
function verbIcon(action: SubmitAction): ReactNode {
  switch (action) {
    case 'sendBack':
      return <ReplayIcon sx={{ fontSize: 17 }} />;
    case 'amend':
      return <EditNoteIcon sx={{ fontSize: 17 }} />;
    case 'ask':
      return <QuestionAnswerOutlinedIcon sx={{ fontSize: 16 }} />;
    case 'approve':
      return <CheckIcon sx={{ fontSize: 17 }} />;
    case 'none':
      return null;
  }
}

/** Which in-flight mutation, if any, gates the primary button for this action. */
function primaryBusy(action: SubmitAction, decisionPending: boolean, askPending: boolean): boolean {
  switch (action) {
    case 'ask':
      return askPending;
    case 'sendBack':
    case 'approve':
      return decisionPending;
    // 'amend' only OPENS the composer dialog (the dialog's own submit owns
    // amendPending) — this verb is never blocked by a mutation in flight.
    case 'amend':
    case 'none':
      return false;
  }
}

export interface SubmitBarProps {
  committed: boolean;
  stage: 'drafted' | 'awaitingReview' | 'other';
  /** Locally accumulated, not-yet-sent CHANGE-REQUEST comments this gate cycle. */
  stagedChangeRequests: number;
  /** Locally accumulated, not-yet-sent QUESTION comments this gate cycle. */
  stagedQuestions: number;
  /** Open entries on the server review thread — blocks Approve when non-zero. */
  openThreads: number;
  /** The submitReview decision mutation (approve/reject) is in flight. */
  pending: boolean;
  /** The AskQuestions mutation is in flight. */
  askPending?: boolean;
  onApprove: () => void;
  onSendBack: () => void;
  onAsk: () => void;
  /** Opens the amend composer dialog (RULING P6) — never submits directly. */
  onAmend: () => void;
  /**
   * SPA default (false): the primary verb is exactly what `resolveSubmitVerb`
   * resolves, so Send back stays absent until a change request is staged. MCP
   * (see McpSystemDesignContainer) has no client-side comment accumulator —
   * `stagedChangeRequests` is always 0 there, so without this carve-out an
   * uncommitted MCP session could only ever Approve, and its own reject-feedback
   * composer (opened by a Send back click) would be unreachable. Mirrors
   * GatePanel's identical `allowEmptySendBack` doc comment.
   */
  allowEmptySendBack?: boolean;
  /** Omitted where withdrawing does not apply (e.g. a clean committed slot). */
  onWithdraw?: (() => void) | undefined;
  withdrawPending?: boolean;
  /** Omitted where retrying does not apply. */
  onRetry?: (() => void) | undefined;
  retryPending?: boolean;
}

export function SubmitBar({
  committed,
  stage,
  stagedChangeRequests,
  stagedQuestions,
  openThreads,
  pending,
  askPending = false,
  onApprove,
  onSendBack,
  onAsk,
  onAmend,
  allowEmptySendBack = false,
  onWithdraw,
  withdrawPending = false,
  onRetry,
  retryPending = false,
}: SubmitBarProps): ReactNode {
  const t = useTokens();
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);

  const resolved = resolveSubmitVerb({
    committed,
    stage,
    stagedChangeRequests,
    stagedQuestions,
    openThreads,
  });
  // MCP compatibility carve-out (see `allowEmptySendBack`'s doc comment): with
  // nothing staged, offer Send back instead of the resolved Approve so the click
  // that opens MCP's own feedback composer stays reachable.
  const verb =
    allowEmptySendBack && !committed && resolved.action === 'approve'
      ? { action: 'sendBack' as const, label: 'Send back', consequence: 'Opens the feedback composer', disabled: false }
      : resolved;

  const busy = primaryBusy(verb.action, pending, askPending);
  const menuOpen = menuAnchor !== null;
  const hasMenu = onWithdraw !== undefined || onRetry !== undefined;

  const onPrimaryClick = (): void => {
    switch (verb.action) {
      case 'sendBack':
        onSendBack();
        return;
      case 'approve':
        onApprove();
        return;
      case 'amend':
        onAmend();
        return;
      case 'ask':
        onAsk();
        return;
      case 'none':
        return;
    }
  };

  if (verb.action === 'none' && !hasMenu) return null;

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.DesignExperience.SUBMIT_BAR}
      sx={{
        position: 'sticky',
        bottom: 0,
        zIndex: 2,
        mt: 2,
        flexShrink: 0,
        px: 2.5,
        py: 1.75,
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        flexWrap: 'wrap',
        borderTop: `1.5px solid ${t.line}`,
      }}
    >
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        {verb.action !== 'none' ? (
          <Button
            color={verb.action === 'approve' ? 'primary' : 'inherit'}
            data-testid={UI_IDENTIFIERS.DesignExperience.SUBMIT_BAR_PRIMARY}
            disabled={verb.disabled || busy}
            startIcon={busy ? <CircularProgress color="inherit" size={14} /> : verbIcon(verb.action)}
            sx={
              verb.action === 'approve'
                ? {}
                : { color: t.ink, borderColor: t.line, bgcolor: t.paperAlt, '&:hover': { bgcolor: t.paperAlt } }
            }
            variant={verb.action === 'approve' ? 'contained' : 'outlined'}
            onClick={onPrimaryClick}
          >
            {verb.label}
          </Button>
        ) : (
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
            Nothing staged — up to date
          </Typography>
        )}
        {verb.consequence.length > 0 ? (
          <Typography
            data-testid={UI_IDENTIFIERS.DesignExperience.SUBMIT_BAR_CONSEQUENCE}
            sx={{ mt: 0.5, color: t.muted, display: 'block' }}
            variant="caption"
          >
            {verb.consequence}
          </Typography>
        ) : null}
      </Box>

      {hasMenu ? (
        <>
          <IconButton
            data-testid={UI_IDENTIFIERS.DesignExperience.SUBMIT_BAR_MENU_BUTTON}
            size="small"
            onClick={(e) => {
              setMenuAnchor(e.currentTarget);
            }}
          >
            <MoreVertIcon sx={{ fontSize: 18 }} />
          </IconButton>
          <Menu
            anchorEl={menuAnchor}
            data-testid={UI_IDENTIFIERS.DesignExperience.SUBMIT_BAR_MENU}
            open={menuOpen}
            onClose={() => {
              setMenuAnchor(null);
            }}
          >
            {onWithdraw !== undefined ? (
              <MenuItem
                data-testid={UI_IDENTIFIERS.DesignExperience.submitBarMenuItem('withdraw')}
                disabled={withdrawPending}
                onClick={() => {
                  setMenuAnchor(null);
                  onWithdraw();
                }}
              >
                <UndoIcon sx={{ fontSize: 16, mr: 1 }} />
                Withdraw
              </MenuItem>
            ) : null}
            {onRetry !== undefined ? (
              <MenuItem
                data-testid={UI_IDENTIFIERS.DesignExperience.submitBarMenuItem('retry')}
                disabled={retryPending}
                onClick={() => {
                  setMenuAnchor(null);
                  onRetry();
                }}
              >
                <RestartAltIcon sx={{ fontSize: 16, mr: 1 }} />
                Retry
              </MenuItem>
            ) : null}
          </Menu>
        </>
      ) : null}
    </Paper>
  );
}
