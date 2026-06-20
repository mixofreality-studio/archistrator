/**
 * The human review gate, shown when the session stage is awaitingReview. Surfaces
 * the machine-validation findings (severity-colored) and the commit authority:
 *   Approve & continue → submitReviewDecision(approve) then auto-advance
 *   Send back          → submitReviewDecision(reject, { feedback, comments })
 *   Withdraw           → submitReviewDecision(withdraw)
 * Send-back is disabled until at least one feedback entry — a free-form note or an
 * anchored comment — is accumulated, so the redraft always carries guidance.
 * Findings are the real engine.Finding[] from the
 * session view; an empty findings list reads as "all checks passed".
 */
import { useState, type ReactNode } from 'react';
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
import type { Finding } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

export function GatePanel({
  findings,
  commentCount,
  pending,
  onApprove,
  onSendBack,
  onWithdraw,
}: {
  findings: Finding[];
  /** Number of accumulated anchored send-back comments. */
  commentCount: number;
  /** A decision mutation is in flight — disable the buttons. */
  pending: boolean;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
}): ReactNode {
  const t = useTokens();
  const [showFindings, setShowFindings] = useState(true);
  const errors = findings.filter((f) => f.severity === 'error').length;
  const warnings = findings.filter((f) => f.severity === 'warning').length;
  const oks = findings.length - errors - warnings;

  return (
    <Paper data-testid={UI_IDENTIFIERS.GatePanel.ROOT} sx={{ p: 0, overflow: 'hidden' }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 2.5,
          py: 1.5,
          cursor: 'pointer',
          bgcolor: t.paperAlt,
          borderBottom: showFindings ? `1.5px solid ${t.line}` : 'none',
        }}
        onClick={() => {
          setShowFindings((v) => !v);
        }}
      >
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}>
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
        <ExpandMoreIcon sx={{ transform: showFindings ? 'rotate(180deg)' : 'none', transition: '120ms' }} />
      </Box>

      <Collapse in={showFindings}>
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
              <Alert key={`${f.ruleId}-${String(i)}`} severity={f.severity} sx={{ alignItems: 'flex-start' }}>
                <AlertTitle sx={{ fontFamily: t.mono, fontSize: 12, letterSpacing: '0.06em', mb: 0.25 }}>
                  {f.ruleId}
                  {f.location != null && f.location.section.length > 0 ? ` · ${f.location.section}` : ''}
                </AlertTitle>
                {f.message}
              </Alert>
            ))
          )}
        </Box>
      </Collapse>

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
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.awaitingFg }}>
            You are the commit authority
          </Typography>
          <Typography sx={{ color: t.awaitingFg, opacity: 0.85 }} variant="caption">
            {commentCount > 0
              ? `${String(commentCount)} note${commentCount === 1 ? '' : 's'} ready to send back.`
              : 'Approve to seal and auto-advance, or type feedback then send back for a redraft.'}
          </Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
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
          disabled={pending || commentCount === 0}
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
          disabled={pending}
          startIcon={<CheckIcon />}
          variant="contained"
          onClick={onApprove}
        >
          Approve &amp; continue
        </Button>
      </Box>
    </Paper>
  );
}
