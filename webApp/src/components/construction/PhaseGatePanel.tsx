/**
 * Construction PhaseGatePanel — shown when the construction session stage is
 * `awaitingApproval` (StageAwaitingApproval = 7). Mirrors design/GatePanel.tsx
 * but scoped to the construction phase-gate workflow:
 *   Approve & continue → submitPhaseDecision(approve) — resumes the workflow
 *   Send back          → submitPhaseDecision(sendBack) — redrafts this phase
 *
 * No Withdraw and no machine-findings section (those belong to the design gate).
 * Shows the canonical phase label + the reviewEngine-computed reviewer set from
 * ConstructionSessionView.reviewSet.
 *
 * The `phase` prop MUST be the exact ActivityMethodPhase wire name the server uses
 * (e.g. "detailed_design", "integration", "test_plan") — taken directly from
 * ConstructionRow.phase (CurrentPhase on the server). The SubmitPhaseDecision
 * signal is phase-multiplexed on the server; the wrong phase string is silently
 * discarded (kept awaiting). Use constructionRow.phase — never derive from display.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import CheckIcon from '@mui/icons-material/Check';
import ReplayIcon from '@mui/icons-material/Replay';
import type { ConstructionReviewSet } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

// ---------------------------------------------------------------------------
// Phase label map — canonical ActivityMethodPhase wire names → display strings.
// Wire names are defined in server/internal/resourceaccess/projectstate/
// activityconstructionstatus.go (MethodPhase* constants).
// ---------------------------------------------------------------------------

const PHASE_LABELS: Record<string, string> = {
  requirements: 'Requirements review',
  detailed_design: 'Detailed design / Contract freeze',
  test_plan: 'Test plan sign-off',
  construction: 'Construction review',
  integration: 'Code review / Integration',
};

// ---------------------------------------------------------------------------
// PhaseGatePanel
// ---------------------------------------------------------------------------

export function PhaseGatePanel({
  phase,
  activityKind,
  reviewSet,
  pending,
  onApprove,
  onSendBack,
}: {
  /**
   * Canonical ActivityMethodPhase wire name (e.g. "detailed_design").
   * Must be taken from ConstructionRow.phase — never hardcoded or derived.
   */
  phase: string;
  /** Activity kind badge label (service / frontend / testing). */
  activityKind?: string | undefined;
  /** The reviewEngine-computed reviewer set from ConstructionSessionView. */
  reviewSet?: ConstructionReviewSet | undefined;
  /** A decision mutation is in flight — disable the buttons. */
  pending: boolean;
  onApprove: () => void;
  onSendBack: () => void;
}): ReactNode {
  const t = useTokens();
  const phaseLabel = PHASE_LABELS[phase] ?? phase;

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Construction.PHASE_GATE_PANEL}
      sx={{ p: 0, overflow: 'hidden' }}
    >
      {/* Header — phase identity */}
      <Box
        sx={{
          px: 2.5,
          py: 1.5,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          flexWrap: 'wrap',
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}
        >
          PHASE GATE
        </Typography>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 11,
            color: t.awaitingFg,
            border: `1.5px solid ${t.line}`,
            borderRadius: 1,
            px: 0.75,
          }}
        >
          {phaseLabel}
        </Typography>
        {activityKind !== undefined && (
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
            · {activityKind}
          </Typography>
        )}
      </Box>

      {/* Reviewer set — from reviewEngine.ProposeReviews */}
      {reviewSet?.reviewers !== undefined && reviewSet.reviewers.length > 0 && (
        <Box sx={{ px: 2.5, py: 1.5, borderBottom: `1.5px solid ${t.line}` }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: '0.08em',
              color: t.muted,
              mb: 0.75,
            }}
          >
            REVIEWER SET · reviewEngine
          </Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
            {reviewSet.reviewers.map((r, i) => (
              <Box
                key={`${r.role}-${String(i)}`}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1,
                  p: 1,
                  bgcolor: t.paper,
                  border: `1px solid ${t.line}`,
                  borderRadius: 1,
                  flexWrap: 'wrap',
                }}
              >
                <Typography
                  sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink }}
                >
                  {r.role}
                </Typography>
                <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                  {r.perspective}
                </Typography>
                {r.referenceArtifact !== undefined && r.referenceArtifact.length > 0 && (
                  <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
                    · {r.referenceArtifact}
                  </Typography>
                )}
                <Box sx={{ flexGrow: 1 }} />
                {r.mayAmend ? (
                  <Box
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 9,
                      fontWeight: 700,
                      color: t.chatArchitectFg,
                      bgcolor: t.chatArchitectBg,
                      borderRadius: 99,
                      px: 0.6,
                      py: 0.1,
                    }}
                  >
                    MAY AMEND
                  </Box>
                ) : null}
              </Box>
            ))}
          </Box>
        </Box>
      )}

      {/* Action bar */}
      <Box
        sx={{
          px: 2.5,
          py: 2,
          borderTop:
            reviewSet?.reviewers !== undefined && reviewSet.reviewers.length > 0
              ? 'none'
              : `1.5px solid ${t.line}`,
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
            You are the commit authority for this phase gate
          </Typography>
          <Typography sx={{ color: t.awaitingFg, opacity: 0.85 }} variant="caption">
            Approve to advance the phase, or send back for a redraft. Note: policy edits apply to
            newly-started activities only.
          </Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        <Button
          color="inherit"
          data-testid={UI_IDENTIFIERS.Construction.PHASE_GATE_SENDBACK}
          disabled={pending}
          startIcon={<ReplayIcon />}
          sx={{ color: t.ink, borderColor: t.line }}
          variant="outlined"
          onClick={onSendBack}
        >
          Send back
        </Button>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.Construction.PHASE_GATE_APPROVE}
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
