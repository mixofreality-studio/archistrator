/**
 * The full-screen Construction console (`/project/$projectId/construction`) — the
 * Phase-3 (UC3 superviseConstruction) console, the SIBLING of the Phase-1/2 design
 * experiences. It reuses the SAME ExperienceChrome shell, but swaps the ordered
 * slim-spine for THREE TABS — Tracker · Interventions · Artifacts — because
 * construction is not an ordered sequence of authored artifacts behind a single
 * gate; it is a SUPERVISED PUMP.
 *
 * It binds to the REAL backend:
 *   - the committed Phase-2 head-state (network × activityList slots, via useProject)
 *     drives the Tracker graph (CPM under a build lens);
 *   - the live construction session (GetSessionState, polled) drives the active-
 *     activity detail, the variance/interventions, and the reviewer-set artifacts;
 *   - the pause + override controls call the real POST endpoints.
 *
 * The construction pump that fills sessions is gated on a build cluster (R-CPR) not
 * provisioned here, so the session is usually quiet — every surface degrades to an
 * honest awaiting state rather than an error.
 */
import { useState, useMemo, useEffect, useRef, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import BoltOutlinedIcon from '@mui/icons-material/BoltOutlined';
import Inventory2OutlinedIcon from '@mui/icons-material/Inventory2Outlined';
import PlayArrowRoundedIcon from '@mui/icons-material/PlayArrowRounded';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { ApiError } from '../api/client';
import type { ConstructionRow, GitRow, ProjectArtifactModelEnvelope, ProjectStateWithGit } from '../api/types';
import { gitFor } from '../api/types';
import type { OverrideKind } from '../api/construction';
import { slotStageFromOrdinal } from '../api/adapters';
import {
  buildStatusForStage,
  sessionIsLive,
  activeActivityId,
  computeActivityStatuses,
  type BuildStatus,
} from '../api/constructionAdapters';
import { toNetworkView, narrowProject } from '../api/projectAdapters';
import { useProject } from '../hooks/useProject';
import { useConstructionSession } from '../hooks/useConstructionSession';
import {
  usePauseConstruction,
  useOverrideActivity,
  useBeginConstruction,
} from '../hooks/useConstructionMutations';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { ConstructionTracker } from '../components/construction/ConstructionTracker';
import { InterventionsTab } from '../components/construction/InterventionsTab';
import { ArtifactsTab } from '../components/construction/ArtifactsTab';
import { ActivityLifecyclePanel } from '../components/construction/ActivityLifecyclePanel';
import { CommentProvider } from '../components/comments/CommentContext';

import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/construction');

type TabId = 'tracker' | 'interventions' | 'artifacts';

const TABS: { id: TabId; title: string; icon: ReactNode; testid: string }[] = [
  { id: 'tracker', title: 'Tracker', icon: <AccountTreeOutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Construction.TAB_TRACKER },
  { id: 'interventions', title: 'Interventions', icon: <BoltOutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Construction.TAB_INTERVENTIONS },
  { id: 'artifacts', title: 'Artifacts', icon: <Inventory2OutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Construction.TAB_ARTIFACTS },
];

/** The committed Phase-2 slot's typed envelope, for the tracker CPM derivation. */
function committedEnvelope(
  project: ProjectStateWithGit | undefined,
  kind: 'network' | 'activityList'
): ProjectArtifactModelEnvelope | undefined {
  const slot = (project?.slots ?? []).find((s) => s.kind === kind);
  if (slot === undefined || slotStageFromOrdinal(slot.stage) !== 'committed') return undefined;
  return slot.model as unknown as ProjectArtifactModelEnvelope;
}

export function ConstructionConsoleScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  return <ConstructionConsoleBody projectId={projectId} />;
}

function ConstructionConsoleBody({ projectId }: { projectId: string }): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const [tab, setTab] = useState<TabId>('tracker');

  // Live-cascade poll: while the construction pump is draining the network, poll the
  // project read every 1.5s so the tracker animates eligible→in-construction→integrated.
  // `cascading` is armed by Begin and stays on while the pump is making PROGRESS — we
  // track the integrated (done) count and fall quiet ~30s after it stops rising (the
  // window must exceed the per-activity lifecycle latency — each activity is several git
  // commits over the project repo, ~10-12s — or it would trip between activities). We CANNOT
  // key off `phase === 'running'` because the corpus-seeded in-review activities are
  // permanently `running` (they are not live pump work); progress (the done count) is the
  // honest signal that the pump is actively completing activities.
  const [cascading, setCascading] = useState(false);
  const { data: project, isLoading: projectLoading } = useProject(
    projectId,
    cascading ? 1500 : false
  );

  const integratedCount = useMemo(() => {
    const rows = project?.constructionRows;
    return rows === undefined
      ? 0
      : Object.values(rows).filter((r) => r.status === 'integrated').length;
  }, [project]);

  const lastProgressAtRef = useRef(0);
  const prevIntegratedRef = useRef(integratedCount);
  useEffect(() => {
    if (integratedCount !== prevIntegratedRef.current) {
      prevIntegratedRef.current = integratedCount;
      lastProgressAtRef.current = Date.now();
    }
  }, [integratedCount]);
  useEffect(() => {
    if (!cascading) return undefined;
    const id = setInterval(() => {
      if (Date.now() - lastProgressAtRef.current > 30000) setCascading(false);
    }, 1500);
    return () => { clearInterval(id); };
  }, [cascading]);

  const sessionQuery = useConstructionSession(projectId);
  const session = sessionQuery.data;
  const sessionMissing = sessionQuery.error instanceof ApiError && sessionQuery.error.status === 404;

  const pause = usePauseConstruction(projectId);
  const override = useOverrideActivity(projectId);
  const begin = useBeginConstruction(projectId);
  const onBegin = (): void => {
    lastProgressAtRef.current = Date.now();
    setCascading(true);
    begin.mutate();
  };
  const beginActive = cascading || begin.isPending;

  const overrideError = override.error instanceof Error ? override.error.message : undefined;
  const pauseError = pause.error instanceof Error ? pause.error.message : undefined;

  const onOverride = (activityId: string, kind: OverrideKind, notes: string): void => {
    override.mutate({ activityId, kind, ...(notes.trim().length > 0 ? { notes: notes.trim() } : {}) });
  };
  const onPause = (reason: string): void => {
    pause.mutate(reason);
  };

  // Per-activity git head-state lookup (C-CW-GIT) — rides the project read's
  // gitRows map, keyed by ActivityID. Undefined for any not-yet-branched activity
  // (honest-empty — the row renders no git cluster).
  const gitForActivity = (activityId: string): GitRow | undefined => gitFor(project, activityId);

  const activeTitle = tab === 'tracker' ? 'Tracker' : tab === 'interventions' ? 'Interventions' : 'Artifacts';

  // --- Activity Lifecycle Panel (additive overlay on Tracker node click) ----
  const [selectedActivityId, setSelectedActivityId] = useState<string | null>(null);

  // Derive the NetworkView + status map so the panel can resolve the clicked node's status.
  const networkEnvelope = committedEnvelope(project, 'network');
  const activityEnvelope = committedEnvelope(project, 'activityList');
  const networkModel = useMemo(() => narrowProject(networkEnvelope, 'network'), [networkEnvelope]);
  const networkView = useMemo(
    () => toNetworkView(networkEnvelope, activityEnvelope),
    [networkEnvelope, activityEnvelope]
  );

  // titleForId: resolves activityId → human-readable title from the committed
  // activity-list slot, falling back to the id when no title is present.
  const activityListModel = useMemo(() => narrowProject(activityEnvelope, 'activityList'), [activityEnvelope]);
  const titleForId = useMemo((): ((id: string) => string | undefined) => {
    const items = activityListModel?.activities ?? [];
    const byId = new Map<string, string | undefined>(items.map((a) => [a.name, a.title]));
    return (id: string): string | undefined => byId.get(id);
  }, [activityListModel]);

  const live = sessionIsLive(session);
  const activeId = activeActivityId(session);
  const activeStatus: BuildStatus =
    session !== undefined ? buildStatusForStage(session.stage) : 'not-started';

  const constructionRowFor = useMemo(
    () =>
      project?.constructionRows !== undefined
        ? (id: string): ConstructionRow | undefined => project.constructionRows?.[id]
        : undefined,
    [project]
  );

  const statusMap = useMemo(
    () =>
      networkModel !== undefined
        ? computeActivityStatuses(
            networkModel,
            gitForActivity,
            live && activeId !== undefined ? activeId : undefined,
            activeStatus,
            constructionRowFor
          )
        : new Map<string, BuildStatus>(),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [networkModel, live, activeId, activeStatus, constructionRowFor]
  );

  return (
    <CommentProvider>
    <ExperienceChrome
      phaseNum={3}
      phaseTitle="Construction"
      projectName={project?.name}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Construction.ROOT}
        sx={{ flexGrow: 1, minWidth: 0, display: 'flex', flexDirection: 'column', minHeight: 0 }}
      >
        {/* tab bar — replaces the ordered spine */}
        <Box sx={{ flexShrink: 0, display: 'flex', alignItems: 'stretch', gap: 0.5, px: 2.5, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}`, overflowX: 'auto' }}>
          {TABS.map((x) => {
            const isActive = x.id === tab;
            return (
              <Box
                aria-selected={isActive}
                data-testid={x.testid}
                key={x.id}
                role="tab"
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 0.75,
                  px: 1.5,
                  py: 1.25,
                  cursor: 'pointer',
                  flexShrink: 0,
                  color: isActive ? t.accent : t.muted,
                  borderBottom: `3px solid ${isActive ? t.accent : 'transparent'}`,
                  '&:hover': { color: isActive ? t.accent : t.ink },
                }}
                onClick={() => { setTab(x.id); }}
              >
                {x.icon}
                <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, letterSpacing: '0.04em', whiteSpace: 'nowrap' }}>
                  {x.title}
                </Typography>
              </Box>
            );
          })}
        </Box>

        <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
          <ConsoleHeader
            action={
              tab === 'tracker' ? (
                <Button
                  data-testid={UI_IDENTIFIERS.Construction.BEGIN_BUTTON}
                  disabled={beginActive}
                  size="small"
                  startIcon={
                    beginActive ? <CircularProgress color="inherit" size={14} /> : <PlayArrowRoundedIcon />
                  }
                  sx={{
                    fontFamily: t.mono,
                    fontWeight: 700,
                    fontSize: 12,
                    textTransform: 'none',
                    color: t.bg,
                    bgcolor: t.accent,
                    px: 1.75,
                    '&:hover': { bgcolor: t.accent2 },
                  }}
                  variant="contained"
                  onClick={onBegin}
                >
                  {beginActive ? 'Construction running…' : 'Begin construction'}
                </Button>
              ) : undefined
            }
            subtitle={tabSubtitle(tab)}
            t={t}
            title={activeTitle}
          />

          {projectLoading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
              <CircularProgress />
            </Box>
          ) : tab === 'tracker' ? (
            <ConstructionTracker
              activityEnvelope={activityEnvelope}
              constructionProgress={project?.constructionProgress}
              constructionRows={project?.constructionRows}
              gitFor={gitForActivity}
              networkEnvelope={networkEnvelope}
              overrideError={overrideError}
              overridePending={override.isPending}
              session={session}
              sessionMissing={sessionMissing}
              onOverride={onOverride}
              onSelectActivity={setSelectedActivityId}
            />
          ) : tab === 'interventions' ? (
            <InterventionsTab
              activityEnvelope={activityEnvelope}
              constructionRows={project?.constructionRows}
              gitFor={gitForActivity}
              overrideError={overrideError}
              overridePending={override.isPending}
              pauseError={pauseError}
              pausePending={pause.isPending}
              project={project}
              session={session}
              sessionMissing={sessionMissing}
              onOverride={onOverride}
              onPause={onPause}
            />
          ) : (
            <ArtifactsTab
              activityEnvelope={activityEnvelope}
              constructionRows={project?.constructionRows}
              project={project}
              session={session}
              sessionMissing={sessionMissing}
            />
          )}
        </Box>
      </Box>

      {/* Activity Lifecycle Panel — additive overlay on Tracker node click. */}
      <ActivityLifecyclePanel
        activityId={selectedActivityId}
        activityTitle={selectedActivityId !== null ? titleForId(selectedActivityId) : undefined}
        derivedStatus={
          selectedActivityId !== null
            ? (statusMap.get(selectedActivityId) ?? 'not-started')
            : 'not-started'
        }
        git={selectedActivityId !== null ? gitForActivity(selectedActivityId) : undefined}
        node={
          selectedActivityId !== null
            ? networkView.nodes.find((n) => n.id === selectedActivityId)
            : undefined
        }
        row={
          selectedActivityId !== null
            ? project?.constructionRows?.[selectedActivityId]
            : undefined
        }
        onClose={() => { setSelectedActivityId(null); }}
      />
    </ExperienceChrome>
    </CommentProvider>
  );
}

function tabSubtitle(id: TabId): string {
  switch (id) {
    case 'tracker':
      return 'The committed project network under a build lens · App-A tracking';
    case 'interventions':
      return 'interventionEngine variance + operator steer · pause / override';
    case 'artifacts':
      return 'reviewEngine reviewer set + produced changes';
    default:
      return 'reviewEngine reviewer set + produced changes';
  }
}

function ConsoleHeader({
  t,
  title,
  subtitle,
  action,
}: {
  t: Tokens;
  title: string;
  subtitle: string;
  action?: ReactNode;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2 }}>
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Typography sx={{ color: t.ink }} variant="h4">{title}</Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>{subtitle}</Typography>
      </Box>
      {action !== undefined ? <Box sx={{ flexShrink: 0 }}>{action}</Box> : null}
    </Box>
  );
}
