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
import { useState, useMemo, useEffect, useRef, type ReactElement, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Tabs from '@mui/material/Tabs';
import Tab from '@mui/material/Tab';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import BoltOutlinedIcon from '@mui/icons-material/BoltOutlined';
import Inventory2OutlinedIcon from '@mui/icons-material/Inventory2Outlined';
import PlayArrowRoundedIcon from '@mui/icons-material/PlayArrowRounded';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import type { GitRow, ProjectArtifactModelEnvelope, ProjectStateWithGit } from '../contracts/types';
import { gitFor } from '../contracts/types';
import type { OverrideKind } from '../contracts/types';
import { slotStageFromOrdinal } from '../contracts/adapters';
import { narrowProject } from '../contracts/projectAdapters';
import { useProject } from '../hooks/useProject';
import { isSessionAbsent } from '../hooks/sessionPolling';
import { useConstructionSession } from '../hooks/useConstructionSession';
import {
  usePauseConstruction,
  useOverrideActivity,
  useBeginConstruction,
  useSubmitPhaseDecision,
} from '../hooks/useConstructionMutations';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { ChatRail } from '../components/design/ChatRail';
// ConstructionTracker (the CPM graph under a build lens, the EV curves, the
// head-state rollup and the near-critical float table) is no longer the LIST
// lens's body — the lens is defined as "every activity, its lifecycle phases and
// its tasks", and the tree below IS that. The component is kept, not deleted:
// the graph is the GRAPH lens's body in Stage D, and Task 13 decides where the
// EV/rollup/float panels land.
import {
  ConstructionShell,
  LensComingLater,
} from '../components/construction/lens/ConstructionShell';
import { ActivityTreeView } from '../components/construction/list/ActivityTreeView';
import { buildActivityTree, type ActivityMeta } from '../components/construction/list/activityTree';
import {
  useLensSelection,
  useLensToolbar,
  toolbarSignatureOf,
  type LensId,
} from '../components/construction/lens/useLensSelection';
import { KIND_META, type ActivityKind } from '../components/construction/KindBadge';
import { InterventionsTab } from '../components/construction/InterventionsTab';
import { ArtifactsTab } from '../components/construction/ArtifactsTab';
import { DetailPane } from '../components/construction/detail/DetailPane';
import { PhaseGatePanel } from '../components/construction/PhaseGatePanel';
import { CommentProvider, useComments } from '../components/comments/CommentContext';

import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/construction');

type TabId = 'tracker' | 'interventions' | 'artifacts';

const TABS: { id: TabId; title: string; icon: ReactElement; testid: string }[] = [
  {
    id: 'tracker',
    title: 'Tracker',
    icon: <AccountTreeOutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Construction.TAB_TRACKER,
  },
  {
    id: 'interventions',
    title: 'Interventions',
    icon: <BoltOutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Construction.TAB_INTERVENTIONS,
  },
  {
    id: 'artifacts',
    title: 'Artifacts',
    icon: <Inventory2OutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Construction.TAB_ARTIFACTS,
  },
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
  // The CommentProvider wraps the body (mirrors Phase-1 SystemDesignScreen) so the
  // body itself can read useComments() — the accumulated anchored comments + the
  // toWire()/freeformNotes() the phase-gate "Send back" carries into the redraft.
  return (
    <CommentProvider>
      <ConstructionConsoleBody projectId={projectId} />
    </CommentProvider>
  );
}

function ConstructionConsoleBody({ projectId }: { projectId: string }): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const [tab, setTab] = useState<TabId>('tracker');
  const { reset, toWire, freeformNotes, requestId } = useComments();

  // Co-author rail open-state, driven by (requestId, manual toggles) exactly like
  // the Phase-1 design experience — but DEFAULT-CLOSED here (construction is a
  // supervision console, not a co-authoring flow): the rail stays collapsed until
  // the operator arms an anchor (any commentable surface bumps requestId), then it
  // auto-opens so the armed comment has somewhere to go. A manual collapse records
  // the requestId it happened at; a newer anchor re-opens the rail.
  const [closedAt, setClosedAt] = useState<number | null>(0);
  const chatOpen = closedAt === null || requestId > closedAt;
  const setChatOpen = (open: boolean): void => {
    setClosedAt(open ? null : requestId);
  };

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
    return (): void => {
      clearInterval(id);
    };
  }, [cascading]);

  const sessionQuery = useConstructionSession(projectId);
  // QA 2026-07-19 (REOPENED fix): absence is the probe VALUE null — never inferred
  // from an error or an in-flight refetch, so a poll tick can never flip this and
  // remount the console (see isSessionAbsent / sessionProbeQueryFn).
  const sessionMissing = isSessionAbsent(sessionQuery.data);
  const session = sessionQuery.data ?? undefined;

  const pause = usePauseConstruction(projectId);
  const override = useOverrideActivity(projectId);
  const begin = useBeginConstruction(projectId);
  const submitPhaseDecision = useSubmitPhaseDecision(projectId);

  // Phase-gate detection: find the currently in-construction activity and poll its
  // session. When the workflow suspends at a phase gate (StageAwaitingApproval = 7),
  // the PhaseGatePanel is shown so the human can Approve or Send back.
  const constructionRows = project?.constructionRows;
  const activeInConstructionId = useMemo(() => {
    if (constructionRows === undefined) return undefined;
    return Object.values(constructionRows).find((r): boolean => r.status === 'in-construction')
      ?.activityId;
  }, [constructionRows]);

  const phaseGateSessionQuery = useConstructionSession(projectId, activeInConstructionId);
  const phaseGateSession = phaseGateSessionQuery.data;
  const isAwaitingApproval = phaseGateSession?.stage === 'awaitingApproval';

  // The construction row for the gated activity — provides phase + kind for the panel.
  const phaseGateRow =
    isAwaitingApproval && activeInConstructionId !== undefined
      ? project?.constructionRows?.[activeInConstructionId]
      : undefined;
  // A phase gate cannot exist without the server having dispatched at least one
  // phase for a CLASSIFIED activity, so this is always defined in practice —
  // but currentLifecyclePhase is optional on the type, so narrow it explicitly
  // rather than asserting: never submit a decision with no real phase to name.
  const gatePhase = phaseGateRow?.currentLifecyclePhase;

  const approvePhase = (): void => {
    if (
      activeInConstructionId === undefined ||
      phaseGateRow === undefined ||
      gatePhase === undefined
    )
      return;
    submitPhaseDecision.mutate(
      {
        activityId: activeInConstructionId,
        phase: gatePhase,
        decision: 'approve',
      },
      // Clear any accumulated anchors/comments once the gate is decided so they do
      // not bleed into the next activity's gate cycle.
      {
        onSuccess: () => {
          reset();
        },
      }
    );
  };

  const sendBackPhase = (): void => {
    if (
      activeInConstructionId === undefined ||
      phaseGateRow === undefined ||
      gatePhase === undefined
    )
      return;
    // Attach the accumulated anchored comments + free-form notes to the phase-gate
    // redraft, exactly like Phase-1's send-back. The submit-phase-decision endpoint
    // already carries ConstructionReviewFeedback { notes, comments } — no server
    // contract change needed. The Manager weaves the comments beneath the notes into
    // the role redraft prompt (jsonPath is opaque, human-meaningful guidance).
    const wireComments = toWire();
    const freeform = freeformNotes();
    // notes is required on the wire; when the operator only anchored comments (no
    // free-form note) synthesize the notes from them so the redraft always carries
    // actionable guidance (mirrors DesignExperience.sendBack).
    const notes = freeform.length > 0 ? freeform : wireComments.map((c) => c.text).join('\n');
    const hasFeedback = wireComments.length > 0 || notes.length > 0;
    submitPhaseDecision.mutate(
      {
        activityId: activeInConstructionId,
        phase: gatePhase,
        decision: 'sendBack',
        ...(hasFeedback
          ? { feedback: { notes, ...(wireComments.length > 0 ? { comments: wireComments } : {}) } }
          : {}),
      },
      {
        onSuccess: () => {
          reset();
        },
      }
    );
  };

  const onBegin = (): void => {
    lastProgressAtRef.current = Date.now();
    setCascading(true);
    begin.mutate();
  };
  const beginActive = cascading || begin.isPending;

  // Whether construction has already been started at all: a live/known session
  // exists, or any activity has advanced beyond not-started (the seeded rows only
  // hold integrated/in-review/in-construction). Drives Begin→Resume so an active
  // project (60 integrated) never invites a fresh "Begin".
  const constructionStarted =
    (session !== undefined && !sessionMissing) ||
    Object.keys(project?.constructionRows ?? {}).length > 0;
  const beginLabel = beginActive
    ? 'Construction running…'
    : constructionStarted
      ? 'Resume construction'
      : 'Begin construction';

  const overrideError = override.error instanceof Error ? override.error.message : undefined;
  const pauseError = pause.error instanceof Error ? pause.error.message : undefined;

  const onOverride = (activityId: string, kind: OverrideKind, notes: string): void => {
    // Attach any anchored intervention comments the operator armed in the rail to
    // the steer, mirroring the phase-gate send-back. ActivityOverride now carries
    // `comments` end-to-end (contract → manager signal), so a send-back-style steer
    // no longer drops the operator's per-item feedback.
    const wireComments = toWire();
    override.mutate(
      {
        activityId,
        kind,
        ...(notes.trim().length > 0 ? { notes: notes.trim() } : {}),
        ...(wireComments.length > 0 ? { comments: wireComments } : {}),
      },
      {
        onSuccess: () => {
          reset();
        },
      }
    );
  };
  const onPause = (reason: string): void => {
    pause.mutate(reason);
  };

  // Per-activity git head-state lookup (C-CW-GIT) — rides the project read's
  // gitRows map, keyed by ActivityID. Undefined for any not-yet-branched activity
  // (honest-empty — the row renders no git cluster).
  const gitForActivity = (activityId: string): GitRow | undefined => gitFor(project, activityId);

  const activeTitle =
    tab === 'tracker' ? 'Tracker' : tab === 'interventions' ? 'Interventions' : 'Artifacts';

  // --- Lens state (Stage B) -------------------------------------------------
  // Selection is NOT component state: it lives in the URL's search params
  // (?lens=&a=&p=&k=&n=), so neither the 1.5s cascade poll's remount nor a lens
  // switch can wipe it, and a link addresses exactly one task attempt. The
  // toolbar (search/scope/kind/layer/sort) lives in a module store keyed by a
  // content signature — the NetworkView pattern — so it survives the same churn.
  const { lens, selection, setLens, select, clear } = useLensSelection();
  const selectedActivityId = selection.activityId ?? null;

  // The activity IDENTITIES, not their statuses: a cascade completing an activity
  // must NOT reset the operator's toolbar. The array is rebuilt on every poll tick
  // (project's identity churns) but toolbarSignatureOf collapses it to the same
  // STRING, which is what useLensToolbar compares.
  const activityIds = useMemo(() => Object.keys(project?.constructionRows ?? {}), [project]);
  const toolbarSignature = useMemo(
    () => toolbarSignatureOf(projectId, activityIds),
    [projectId, activityIds]
  );
  const { toolbar, setToolbar } = useLensToolbar(toolbarSignature);

  // Facet options come from the REAL rows — an absent kind (unclassified) or an
  // undrawn layer contributes nothing rather than a fabricated bucket.
  const kindOptions = useMemo(() => {
    const kinds = new Set<ActivityKind>();
    for (const row of Object.values(project?.constructionRows ?? {})) {
      if (row.kind !== undefined) kinds.add(row.kind);
    }
    return [...kinds]
      .sort((a, b) => KIND_META[a].label.localeCompare(KIND_META[b].label))
      .map((k) => ({ value: k, label: KIND_META[k].label }));
  }, [project]);

  const layerOptions = useMemo(() => {
    const layers = new Set<string>();
    for (const row of Object.values(project?.constructionRows ?? {})) {
      if (row.layer !== undefined) layers.add(row.layer);
    }
    return [...layers].sort((a, b) => a.localeCompare(b)).map((l) => ({ value: l, label: l }));
  }, [project]);

  // The TASKS badge — the only lens that asserts something is owed. Counted from
  // the real head-state: an in-review activity has reached the human code-review
  // gate. Nothing is inferred for rows with no evidence.
  const tasksOwed = useMemo(
    () =>
      Object.values(project?.constructionRows ?? {}).filter((r) => r.status === 'in-review').length,
    [project]
  );

  const networkEnvelope = committedEnvelope(project, 'network');
  const activityEnvelope = committedEnvelope(project, 'activityList');

  // titleForId: resolves activityId → human-readable title from the committed
  // activity-list slot, falling back to the id when no title is present.
  const activityListModel = useMemo(
    () => narrowProject(activityEnvelope, 'activityList'),
    [activityEnvelope]
  );
  const titleForId = useMemo((): ((id: string) => string | undefined) => {
    const items = activityListModel?.activities ?? [];
    const byId = new Map<string, string | undefined>(items.map((a) => [a.name, a.title]));
    return (id: string): string | undefined => byId.get(id);
  }, [activityListModel]);

  // --- The LIST lens's tree ------------------------------------------------
  // Per-activity network/activity-list facts, joined by activity id for the
  // tree's magnitude channels (effort → bar length, float → rail + numeral,
  // criticality → border weight).
  //
  // Read from the committed MODELS rather than from toNetworkView: that view
  // defaults a missing CPM entry to `float: 0` and a missing activity-list entry
  // to `days: 0`, and a fabricated zero float renders as "on the critical path"
  // — the loudest possible lie on this surface. Here an unjoined activity gets
  // NO metadata at all, and the tree renders absence as absence.
  //
  // Only 9 of the 69 construction rows join today: the committed network and
  // activity list carry the DERIVED 40 (`C-artifact-access`, …) while the
  // construction head-state is still keyed by the legacy ids (`C-AA`, …). That
  // seam is real, is not this task's to close, and is made visible in Task 12.
  const networkModel = useMemo(() => narrowProject(networkEnvelope, 'network'), [networkEnvelope]);
  const activityMeta = useMemo((): Record<string, ActivityMeta> => {
    const computed = networkModel?.computed ?? {};
    const byId: Record<string, ActivityMeta> = {};
    for (const a of activityListModel?.activities ?? []) {
      const cpm = computed[a.name];
      byId[a.name] = {
        ...(a.title !== undefined && a.title.length > 0 ? { label: a.title } : {}),
        effortDays: a.effortDays,
        ...(cpm !== undefined
          ? { float: cpm.totalFloat, onCriticalPath: cpm.onCriticalPath, band: cpm.band }
          : {}),
      };
    }
    return byId;
  }, [activityListModel, networkModel]);

  const activityTree = useMemo(
    () => buildActivityTree(Object.values(project?.constructionRows ?? {}), { meta: activityMeta }),
    [project, activityMeta]
  );

  // The shell's DETAIL slot: one pane, driven entirely by the URL's selection
  // (never owned by the pane itself), so it cannot lose it to the cascade
  // poll's remount. Beside-content at >=1200px, the existing overlay Drawer
  // below that — see DetailPane.tsx.
  const detailPane =
    selectedActivityId !== null ? (
      <DetailPane
        activityTitle={titleForId(selectedActivityId)}
        row={project?.constructionRows?.[selectedActivityId]}
        selection={selection}
        onClose={clear}
      />
    ) : undefined;

  return (
    <ExperienceChrome
      chat={
        chatOpen ? (
          <ChatRail
            onCollapse={() => {
              setChatOpen(false);
            }}
          />
        ) : undefined
      }
      chatOpen={chatOpen}
      phaseNum={3}
      phaseTitle="Construction"
      projectName={project?.name}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      onOpenChat={() => {
        setChatOpen(true);
      }}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Construction.ROOT}
        sx={{ flexGrow: 1, minWidth: 0, display: 'flex', flexDirection: 'column', minHeight: 0 }}
      >
        {/* tab bar — replaces the ordered spine */}
        <Tabs
          aria-label="Construction console sections"
          scrollButtons={false}
          sx={{
            flexShrink: 0,
            minHeight: 0,
            px: 2.5,
            bgcolor: t.paperAlt,
            borderBottom: `1.5px solid ${t.line}`,
            '& .MuiTabs-flexContainer': { gap: 0.5 },
            '& .MuiTabs-indicator': { backgroundColor: t.accent, height: 3 },
          }}
          value={tab}
          variant="scrollable"
          onChange={(_e, value: TabId) => {
            setTab(value);
          }}
        >
          {TABS.map((x) => (
            <Tab
              data-testid={x.testid}
              icon={x.icon}
              iconPosition="start"
              key={x.id}
              label={x.title}
              sx={{
                minHeight: 0,
                flexShrink: 0,
                gap: 0.75,
                px: 1.5,
                py: 1.25,
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 12.5,
                letterSpacing: '0.04em',
                textTransform: 'none',
                color: t.muted,
                '&:hover': { color: t.ink },
                '&.Mui-selected': { color: t.accent },
              }}
              value={x.id}
            />
          ))}
        </Tabs>

        <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
          <ConsoleHeader
            action={
              // Operating (Task 14): once construction is fully complete the
              // begin/resume button is hidden entirely — not relabeled, since there
              // is nothing left to begin or resume.
              tab === 'tracker' && project?.operating !== true ? (
                <Button
                  data-testid={UI_IDENTIFIERS.Construction.BEGIN_BUTTON}
                  disabled={beginActive}
                  size="small"
                  startIcon={
                    beginActive ? (
                      <CircularProgress color="inherit" size={14} />
                    ) : (
                      <PlayArrowRoundedIcon />
                    )
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
                  {beginLabel}
                </Button>
              ) : undefined
            }
            subtitle={tab === 'tracker' ? lensSubtitle(lens) : tabSubtitle(tab)}
            t={t}
            title={activeTitle}
          />

          {projectLoading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
              <CircularProgress />
            </Box>
          ) : tab === 'tracker' ? (
            <ConstructionShell
              content={
                lens === 'list' ? (
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <ActivityTreeView
                      nodes={activityTree}
                      selection={selection}
                      onSelect={select}
                    />
                    {/* Phase gate — rendered when ConstructionSessionView.stage === awaitingApproval */}
                    {phaseGateRow !== undefined && gatePhase !== undefined && (
                      <PhaseGatePanel
                        activityKind={phaseGateRow.kind}
                        pending={submitPhaseDecision.isPending}
                        phase={gatePhase}
                        reviewSet={phaseGateSession?.view.reviewSet}
                        onApprove={approvePhase}
                        onSendBack={sendBackPhase}
                      />
                    )}
                  </Box>
                ) : (
                  // Honest placeholder — NOT sample rows. GRAPH is the Stage-D
                  // layer-stack projection; TASKS is the Stage-C owed-work lens.
                  <LensComingLater lens={lens} stage={lens === 'graph' ? 'Stage D' : 'Stage C'} />
                )
              }
              detail={detailPane}
              kindOptions={kindOptions}
              layerOptions={layerOptions}
              lens={lens}
              tasksOwed={tasksOwed}
              toolbar={toolbar}
              onLens={setLens}
              onToolbar={setToolbar}
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
              projectId={projectId}
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
    </ExperienceChrome>
  );
}

/** The three lenses are three views of ONE dataset — the subtitle says which view. */
function lensSubtitle(lens: LensId): string {
  switch (lens) {
    case 'list':
      return 'Every activity, its lifecycle phases and its tasks · App-A tracking';
    case 'graph':
      return 'The committed project network under a build lens';
    case 'tasks':
      return 'Only the tasks that owe someone a decision';
  }
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
        <Typography component="h1" sx={{ color: t.ink }} variant="h4">
          {title}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>
          {subtitle}
        </Typography>
      </Box>
      {action !== undefined ? <Box sx={{ flexShrink: 0 }}>{action}</Box> : null}
    </Box>
  );
}
