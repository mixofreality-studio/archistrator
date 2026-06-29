/**
 * The Interventions tab — the operator's variance / steer surface plus the
 * REAL approval queue (activities whose status is `'in-review'`).
 *
 * Three sections, top to bottom:
 *   1. PolicyPanel          — collapsible client-only intervention-policy config.
 *   2. Project-level pause  — the always-present Pause Project control (wired to real POST).
 *   3. Approval queue       — InterventionQueue: all in-review activities derived from
 *                             real constructionRows.  When no activities are in-review,
 *                             the existing AwaitingPanel renders instead (honest empty).
 *   4. InterventionDrawer   — the per-activity right-anchored drawer (opens from QueueCard).
 *
 * The Drawer's OperatorBar steer buttons (Approve / Send back / Take over / Reassign /
 * Skip / Pause / Replay) are INERT until the live construction pump (R-CPR) is
 * provisioned — disabled with a tooltip that makes this explicit. No mutation is wired
 * for per-activity steer.
 *
 * The existing variance/override path (session-live path via ActivityTrackingDetail)
 * is kept for when a live session IS running. The approval queue is ADDITIVE — it
 * surfaces in-review activities from the head-state regardless of session liveness.
 *
 * Ported from ux-mock InterventionsTab; real data for the queue; session-derived
 * override controls for the live-pump path.
 */
import { useState, useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import Alert from '@mui/material/Alert';
import PauseCircleOutlineIcon from '@mui/icons-material/PauseCircleOutline';
import type { ConstructionSessionState, OverrideKind } from '../../api/types';
import type { ConstructionRows, GitRow, ProjectArtifactModelEnvelope, ProjectStateWithGit } from '../../api/types';
import { sessionIsLive, activeActivityId } from '../../api/constructionAdapters';
import { narrowProject } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { ActivityTrackingDetail } from './ActivityTrackingDetail';
import { AwaitingPanel } from './AwaitingPanel';
import { PolicyPanel } from './PolicyPanel';
import { InterventionQueue } from './InterventionQueue';
import { InterventionDrawer } from './InterventionDrawer';

export function InterventionsTab({
  session,
  sessionMissing,
  gitFor,
  onOverride,
  overridePending,
  overrideError,
  onPause,
  pausePending,
  pauseError,
  constructionRows,
  activityEnvelope,
  project,
}: {
  session: ConstructionSessionState | undefined;
  sessionMissing: boolean;
  /** Per-activity git head-state lookup (C-CW-GIT); undefined for not-yet-branched. */
  gitFor: (activityId: string) => GitRow | undefined;
  onOverride: (activityId: string, kind: OverrideKind, notes: string) => void;
  overridePending: boolean;
  overrideError: string | undefined;
  onPause: (reason: string) => void;
  pausePending: boolean;
  pauseError: string | undefined;
  /** constructionRows from the project head-state — drives the approval queue. */
  constructionRows: ConstructionRows | undefined;
  /** The committed activityList envelope — used for resolving activity display names. */
  activityEnvelope?: ProjectArtifactModelEnvelope | undefined;
  /** The full project state — passed to InterventionDrawer for contract resolution. */
  project?: ProjectStateWithGit | undefined;
}): ReactNode {
  const t = useTokens();
  const [pauseOpen, setPauseOpen] = useState(false);
  const [reason, setReason] = useState('');
  const live = sessionIsLive(session);
  const activeId = activeActivityId(session);

  // Drawer state — activityId of the open drawer (null = closed).
  const [drawerActivityId, setDrawerActivityId] = useState<string | null>(null);

  // Resolve the activity-list model so we can look up display names by id.
  const activityListModel = useMemo(
    () => narrowProject(activityEnvelope, 'activityList'),
    [activityEnvelope],
  );

  // nameForId: resolves activityId → human-readable title (falls back to the id itself).
  const nameForId = useMemo((): ((id: string) => string) => {
    const items = activityListModel?.activities ?? [];
    const byName = new Map<string, string>(items.map((a) => [a.name, a.title ?? a.name]));
    return (id: string): string => byName.get(id) ?? id;
  }, [activityListModel]);

  // Derive whether there are any in-review activities for the queue.
  const hasInReview = useMemo(
    () =>
      constructionRows !== undefined &&
      Object.values(constructionRows).some((r) => r.status === 'in-review'),
    [constructionRows],
  );

  // The row for the currently open drawer.
  const drawerRow =
    drawerActivityId !== null && constructionRows !== undefined
      ? constructionRows[drawerActivityId]
      : undefined;
  const drawerName = drawerActivityId !== null ? nameForId(drawerActivityId) : '';
  const drawerGit = drawerActivityId !== null ? gitFor(drawerActivityId) : undefined;

  return (
    <Box data-testid={UI_IDENTIFIERS.Construction.INTERVENTIONS} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {/* Intervention Policy — client-only config; collapsed by default */}
      <PolicyPanel />

      {/* project-level pause — always available while supervising construction */}
      <Paper sx={{ p: 2, display: 'flex', flexDirection: 'column', gap: 1.25 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, letterSpacing: '0.08em', color: t.muted }}>
            PROJECT CONTROL · constructionManager.PauseProject
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          <Button
            color="warning"
            data-testid={UI_IDENTIFIERS.Construction.PAUSE_BUTTON}
            size="small"
            startIcon={<PauseCircleOutlineIcon />}
            variant="outlined"
            onClick={() => { setPauseOpen((v) => !v); }}
          >
            Pause project
          </Button>
        </Box>
        {pauseOpen ? <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <Typography sx={{ color: t.muted, fontSize: 12 }}>
              Signals operatorPauseRequested — cancels in-flight pipelines and records the project operator-paused. A reason is required.
            </Typography>
            <TextField
              fullWidth
              data-testid={UI_IDENTIFIERS.Construction.PAUSE_REASON}
              placeholder="Why are you pausing construction?"
              size="small"
              value={reason}
              onChange={(e) => { setReason(e.target.value); }}
            />
            {pauseError !== undefined && (
              <Alert severity="error" sx={{ fontFamily: t.mono, fontSize: 12 }}>{pauseError}</Alert>
            )}
            <Box>
              <Button
                color="warning"
                data-testid={UI_IDENTIFIERS.Construction.PAUSE_CONFIRM}
                disabled={pausePending || reason.trim().length === 0}
                variant="contained"
                onClick={() => { onPause(reason.trim()); }}
              >
                Confirm pause
              </Button>
            </Box>
          </Box> : null}
      </Paper>

      {/* Approval queue — derived from real in-review construction rows */}
      {constructionRows !== undefined && hasInReview ? (
        <InterventionQueue
          constructionRows={constructionRows}
          gitFor={gitFor}
          nameForId={nameForId}
          onOpenDrawer={(id) => { setDrawerActivityId(id); }}
        />
      ) : (
        /* No in-review activities — render the honest awaiting state */
        live && session !== undefined ? (
          <ActivityTrackingDetail
            git={activeId !== undefined ? gitFor(activeId) : undefined}
            overrideError={overrideError}
            overridePending={overridePending}
            session={session}
            onOverride={onOverride}
          />
        ) : (
          <AwaitingPanel
            detail={
              sessionMissing
                ? 'No construction session is active, so there is no flagged variance to steer. The replanSweep surfaces over-threshold variances on a 5-min tick once the supervised pump is running (gated on the R-CPR build cluster). In-review activities will appear here when the pump reports them.'
                : 'No activities are currently in-review. The approval queue surfaces activities whose status is in-review (reached the Code Review gate). When one appears, the queue and its review controls render here.'
            }
            title="No interventions pending"
          />
        )
      )}

      {/* Per-activity intervention drawer */}
      <InterventionDrawer
        activityId={drawerActivityId}
        git={drawerGit}
        name={drawerName}
        project={project}
        row={drawerRow}
        onClose={() => { setDrawerActivityId(null); }}
      />
    </Box>
  );
}
