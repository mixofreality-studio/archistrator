/**
 * The per-active-activity detail panel — the live technical view of the ONE
 * activity the construction pump is currently supervising, read from the
 * GetSessionState query: the technical stage, the construction-pipeline phase, the
 * computed reviewer set (reviewEngine fan-out), the flagged variance, and the
 * operator override controls (takeover / retry / skip / reassign) wired to the real
 * POST endpoint. Ported from ux-mock ActivityTrackingDetail, bound to real data.
 *
 * When the pump is dormant (no live session) the parent renders the awaiting state
 * instead of this panel, so this panel always has an active activity.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import Alert from '@mui/material/Alert';
import ReplayIcon from '@mui/icons-material/Replay';
import SwapHorizIcon from '@mui/icons-material/SwapHoriz';
import SkipNextIcon from '@mui/icons-material/SkipNext';
import OpenWithIcon from '@mui/icons-material/OpenWith';
import type { ConstructionSessionState, OverrideKind } from '../../api/types';
import type { GitRow } from '../../api/types';
import { STAGE_LABEL } from '../../api/constructionAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { GitRowMeta } from '../GitStatus';

const OVERRIDE_KINDS: { kind: OverrideKind; label: string; detail: string; icon: ReactNode }[] = [
  { kind: 'takeover', label: 'Take over', detail: 'Platform re-dispatches under a changed arrangement / resets the durable execution.', icon: <OpenWithIcon sx={{ fontSize: 16 }} /> },
  { kind: 'retry', label: 'Retry', detail: 'Re-enter the dispatch path for this activity.', icon: <ReplayIcon sx={{ fontSize: 16 }} /> },
  { kind: 'reassign', label: 'Reassign', detail: 'Re-cast the worker class (operator-chosen).', icon: <SwapHorizIcon sx={{ fontSize: 16 }} /> },
  { kind: 'skip', label: 'Skip', detail: 'Record the activity exited with an operator-skip outcome.', icon: <SkipNextIcon sx={{ fontSize: 16 }} /> },
];

function Field({ t, label, value }: { t: Tokens; label: string; value: string }): ReactNode {
  return (
    <Box>
      <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.12em', color: t.muted }}>
        {label}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 13, color: t.ink, fontWeight: 700 }}>
        {value}
      </Typography>
    </Box>
  );
}

export function ActivityTrackingDetail({
  session,
  git,
  onOverride,
  overridePending,
  overrideError,
}: {
  session: ConstructionSessionState;
  /** The git head-state for the active activity (C-CW-GIT), or undefined when not-yet-branched. */
  git: GitRow | undefined;
  onOverride: (activityId: string, kind: OverrideKind, notes: string) => void;
  overridePending: boolean;
  overrideError: string | undefined;
}): ReactNode {
  const t = useTokens();
  const [notes, setNotes] = useState('');
  const activityId = session.view.activityId ?? session.activityId ?? '';
  const reviewers = session.view.reviewSet?.reviewers ?? [];
  const variance = session.view.variance;
  const pipelinePhase = session.pipelinePhase;

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Construction.ACTIVE_DETAIL}
      sx={{ p: 2.5, display: 'flex', flexDirection: 'column', gap: 2, border: `1.5px solid ${t.accent}` }}
    >
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1.5, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.display, fontWeight: 800, fontSize: 18, color: t.ink }}>
          {activityId.length > 0 ? activityId : 'Active activity'}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          live construction session · GetSessionState
        </Typography>
      </Box>

      {/* the git-forward row — one branch per activity (activity/<id>); honest-empty
          when the project read omits a gitRow for this activity (not-yet-branched). */}
      {git !== undefined && (
        <Box sx={{ mt: -0.5 }}>
          <GitRowMeta git={git} />
        </Box>
      )}

      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 3 }}>
        <Field label="STAGE" t={t} value={STAGE_LABEL[session.stage]} />
        <Field
          label="PIPELINE"
          t={t}
          value={pipelinePhase ?? 'no pipeline in flight'}
        />
        <Field label="REVIEWERS" t={t} value={reviewers.length > 0 ? String(reviewers.length) : '—'} />
      </Box>

      {variance !== undefined && (
        <Alert severity="warning" sx={{ fontFamily: t.mono, fontSize: 12.5 }}>
          Flagged variance — {variance.summary}
        </Alert>
      )}

      {reviewers.length > 0 && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.08em', color: t.muted }}>
            COMPUTED REVIEWER SET · reviewEngine
          </Typography>
          {reviewers.map((r, i) => (
            <Box
              key={`${r.role}-${String(i)}`}
              sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 1, bgcolor: t.paperAlt, border: `1px solid ${t.line}`, borderRadius: 1, flexWrap: 'wrap' }}
            >
              <Typography sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink }}>
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
              {r.mayAmend ? <Box sx={{ fontFamily: t.mono, fontSize: 9, fontWeight: 700, color: t.chatArchitectFg, bgcolor: t.chatArchitectBg, borderRadius: 99, px: 0.6, py: 0.1 }}>
                  MAY AMEND
                </Box> : null}
            </Box>
          ))}
        </Box>
      )}

      {/* operator override — the SAME decide→execute machinery as the automatic path */}
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.08em', color: t.muted }}>
          OPERATOR OVERRIDE · constructionManager.OverrideActivity
        </Typography>
        <TextField
          fullWidth
          multiline
          data-testid={UI_IDENTIFIERS.Construction.OVERRIDE_NOTES}
          minRows={2}
          placeholder="Notes (optional) — your steer is fed into the same decide→execute machinery as the automatic variance path."
          size="small"
          value={notes}
          onChange={(e) => { setNotes(e.target.value); }}
        />
        {overrideError !== undefined && (
          <Alert severity="error" sx={{ fontFamily: t.mono, fontSize: 12 }}>{overrideError}</Alert>
        )}
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
          {OVERRIDE_KINDS.map((o) => (
            <Button
              data-testid={UI_IDENTIFIERS.Construction.overrideKind(o.kind)}
              disabled={overridePending || activityId.length === 0}
              key={o.kind}
              size="small"
              startIcon={o.icon}
              title={o.detail}
              variant="outlined"
              onClick={() => { onOverride(activityId, o.kind, notes); }}
            >
              {o.label}
            </Button>
          ))}
        </Box>
      </Box>
    </Paper>
  );
}
