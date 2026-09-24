/**
 * The SPA container for the PLAN (`/project/$projectId/plan`) — the one surface
 * that replaces the construction console's three lenses (spec §7.4).
 *
 * ONE READ, THREE LENSES. `useProject` is the whole data source for LIST and
 * GRAPH: the committed `activityList` and `network` slots and the construction
 * rows, shaped by `planTiles.planActivitiesFrom`. There is no per-activity
 * fetch here — 32 activities on their own polls is what the batched read exists
 * to avoid — so a tile's mini lifecycle is derived from the phase completions
 * the project read already carries (miniLifecycleFromRow).
 *
 * THE TASKS LENS SURVIVES AS IS (spec §7.3). Its whole feeding pipeline was
 * MOVED here from `routes/ConstructionConsole.tsx`, not rewritten:
 * `probeCandidatesFor` → `useConstructionSessions` → `owedWorkFor` →
 * `rankOwed`, the begin/resume control (`beginControl.ts`, its module-memory
 * failure/dispatch records and their two holds) and the empty state's counts.
 * `TasksLensProps` is unchanged. What did NOT come with it is the decision
 * machinery — the gate occurrences, `decisionFlow`, `submit-phase-decision`,
 * and the DetailPane they drove: a verdict is given on the ACTIVITY screen now,
 * so [Review] navigates to `ACTIVITY_PATH?task=<gate>` (R8) instead of opening
 * a pane. Nothing on this screen submits a decision.
 *
 * DESIGN REVIEWS ARE IN THE OWED SET, from a source that exists: the three
 * design activities never open a construction session, so `owedWorkFor` reads
 * their artifact SLOT instead (`designOwedFor`) — which is why `project.slots`
 * is handed to it.
 *
 * THE LENS IS THE URL's. `lens` comes from the route's search and nowhere else;
 * the toggle NAVIGATES. Local state would make `?lens=graph` decorative, and a
 * shared link is the point of putting it in the address bar at all.
 *
 * BEGIN / RESUME LIVE HERE. The activity screen does not mount them: starting
 * the pump is a decision about the PLAN, not about one activity.
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Typography from '@mui/material/Typography';
import PlayArrowRoundedIcon from '@mui/icons-material/PlayArrowRounded';
import { useNavigate } from '@tanstack/react-router';

import type { ProjectArtifactModelEnvelope, ProjectStateWithGit } from '../contracts/types';
import { slotStageFromOrdinal, toC4View } from '../contracts/adapters';
import { narrowProject } from '../contracts/projectAdapters';
import { gitFor } from '../contracts/types';
import { computeActivityStatuses } from '../contracts/constructionAdapters';
import { contractJoinFor } from '../contracts/serviceContracts';
import {
  ACTIVITY_PATH,
  PLAN_PATH,
  LENS_IDS,
  activitySearch,
  isPlanLensId,
  planSearch,
  type PlanLensId,
} from '../contracts/routePaths';
import { useProject } from '../hooks/useProject';
import { useReadRequestedAt } from '../hooks/readRequestTimes';
import { projectKey } from '../hooks/useProject';
import { orderedNow } from '../utilities/orderedNow';
import { TASKS_FRESHNESS_MS, useConstructionSessions } from '../hooks/useConstructionSessions';
import {
  failureStatusOf,
  useBeginConstruction,
  useBeginConstructionPending,
  useResumeConstruction,
} from '../hooks/useConstructionMutations';
import { ApiError } from '../contracts/errors';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { BeginConfirmDialog } from '../components/construction/lens/BeginConfirmDialog';
import {
  DEFAULT_TOOLBAR,
  useLensToolbar,
  toolbarSignatureOf,
} from '../components/construction/lens/useLensSelection';
import {
  anyRowInFlight,
  awaitingPickup,
  beginConfirmAllowed,
  type BeginControl,
  beginControlFor,
  beginHoldFor,
  beginRunning,
  consolePollMs,
  dispatchOutcomeCopy,
  dispatchOutcomeFor,
  failureLeavesMemory,
  holdExpiredCopy,
  liveSessionIdsOf,
  NOTHING_TO_DISPATCH,
  notStartedActivities,
  pausedControlFor,
  pumpDispatched,
  pumpEvidencedSince,
  resumeOutcomeCopy,
  resumeOutcomeFor,
  type ResumeOutcome,
  UNKNOWN_OUTCOME_HOLD_MS,
} from '../components/construction/lens/beginControl';
import {
  failureAwaitsPump,
  readBeginFailure,
  useBeginDispatched,
  useBeginFailure,
  writeBeginDispatched,
  writeBeginFailure,
} from '../components/construction/lens/beginFailureMemory';
import { inFlightActivityIds } from '../components/construction/list/activityScope';
import { waitingActivityIds } from '../components/construction/list/pendingResume';
import { pendingNoteLineFor } from '../components/construction/list/pendingNotes';
import { activityRowState } from '../components/construction/list/activityRowPresentation';
import { owedMarksFor } from '../components/construction/tasks/owedChip';
import { owedWorkFor, probeCandidatesFor } from '../components/construction/tasks/owedWork';
import { rankOwed, type RankedOwed } from '../components/construction/tasks/owedRanking';
import { emptyStateCounts, shapeFor } from '../components/construction/tasks/tasksLensCopy';
import { TasksLens } from '../components/construction/tasks/TasksLens';

import { PlanGraph } from '../components/activity/PlanGraph';
import { PlanList } from '../components/activity/PlanList';
import { LENS_LABEL } from '../components/activity/planCopy';
import { planActivitiesFrom } from '../components/activity/planTiles';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/** The committed Phase-2 slot's typed envelope. Ported from the console. */
function committedEnvelope(
  project: ProjectStateWithGit | undefined,
  kind: 'network' | 'activityList'
): ProjectArtifactModelEnvelope | undefined {
  const slot = (project?.slots ?? []).find((s) => s.kind === kind);
  if (slot === undefined || slotStageFromOrdinal(slot.stage) !== 'committed') return undefined;
  return slot.model as unknown as ProjectArtifactModelEnvelope;
}

const LENS_TESTID: Readonly<Record<PlanLensId, string>> = {
  list: UI_IDENTIFIERS.Plan.LENS_LIST,
  graph: UI_IDENTIFIERS.Plan.LENS_GRAPH,
  tasks: UI_IDENTIFIERS.Plan.LENS_TASKS,
};

/** The three lenses are three views of ONE read — the subtitle says which. */
function lensSubtitle(lens: PlanLensId): string {
  switch (lens) {
    case 'list':
      return 'Every activity of the committed plan, in Table 11-1 order';
    case 'graph':
      return 'The plan as a network, in build order — M0 gates all construction';
    case 'tasks':
      return 'Only the tasks that owe someone a decision';
  }
}

export function PlanContainer({
  projectId,
  lens,
}: {
  projectId: string;
  lens: PlanLensId;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();

  // --- The read, and its cadence (ported verbatim from the console) ----------
  // Fast (1.5s) while a dispatch is pending, while a failure or a success in
  // module memory still awaits the pump, or while cascading; 5s while a ROW
  // shows work in flight; otherwise the TASKS freshness cadence, so a gate
  // opened by the sweep, another tab or MCP cannot stay invisible.
  const beginPending = useBeginConstructionPending(projectId);
  const beginFailure = useBeginFailure(projectId);
  const beginDispatched = useBeginDispatched(projectId);
  const [cascading, setCascading] = useState(false);
  const failureAwaitsPumpNow = failureAwaitsPump(beginFailure);
  const { data: project, isLoading: projectLoading } = useProject(projectId, (read) => {
    const ms = consolePollMs({
      pending: beginPending,
      awaitsPump: failureAwaitsPumpNow || beginDispatched !== null,
      cascading,
      inFlight: anyRowInFlight(read?.constructionRows),
    });
    return ms === false ? TASKS_FRESHNESS_MS : ms;
  });

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

  // --- The committed plan ----------------------------------------------------
  const activityEnvelope = committedEnvelope(project, 'activityList');
  const networkEnvelope = committedEnvelope(project, 'network');
  const activityListModel = useMemo(
    () => narrowProject(activityEnvelope, 'activityList'),
    [activityEnvelope]
  );
  const networkModel = useMemo(() => narrowProject(networkEnvelope, 'network'), [networkEnvelope]);
  const rows = project?.constructionRows;
  const activities = useMemo(
    () =>
      planActivitiesFrom({
        activityList: activityEnvelope,
        network: networkEnvelope,
        rows: rows ?? {},
      }),
    [activityEnvelope, networkEnvelope, rows]
  );
  const titleForId = useMemo((): ((id: string) => string | undefined) => {
    const byId = new Map((activityListModel?.activities ?? []).map((a) => [a.name, a.title]));
    return (id: string): string | undefined => byId.get(id);
  }, [activityListModel]);

  // The committed Phase-1 `system` slot — the contract JOIN's second hop, which
  // is all `shapeOf` needs it for ("Contract · 12 ops").
  const systemEnvelope = useMemo(
    () => (project?.slots ?? []).find((s) => s.kind === 'system')?.model ?? undefined,
    [project]
  );
  const contractJoinInput = useMemo(
    () => ({
      activities: activityListModel?.activities ?? undefined,
      components: toC4View(systemEnvelope).components,
      contracts: project?.serviceContracts,
    }),
    [activityListModel, systemEnvelope, project?.serviceContracts]
  );

  // --- The owed set (Stage C's pipeline, moved) ------------------------------
  const probeIds = useMemo(() => probeCandidatesFor(rows), [rows]);
  const {
    sessions: sessionsByActivity,
    errored: erroredProbes,
    retrying: retryingProbes,
    retryErrored,
  } = useConstructionSessions(projectId, probeIds);
  const slots = project?.slots;
  const owedWork = useMemo(
    () =>
      owedWorkFor({
        rows,
        sessions: sessionsByActivity,
        erroredProbes,
        // The design activities' only owed evidence: a slot at awaitingReview
        // (owedWork.designOwedFor). No probe, no op, no second poll.
        slots: slots ?? [],
        titleFor: titleForId,
      }),
    [rows, sessionsByActivity, erroredProbes, slots, titleForId]
  );
  const owedItems = owedWork.items;
  const owedMarks = useMemo(() => owedMarksFor(owedItems), [owedItems]);
  const inFlightIds = useMemo(
    () =>
      inFlightActivityIds({
        rows,
        owed: owedMarks,
        liveSessionIds: liveSessionIdsOf(sessionsByActivity),
        pendingProbeIds: owedWork.unchecked.pending,
      }),
    [rows, owedMarks, sessionsByActivity, owedWork]
  );
  const inFlight = inFlightIds.size > 0;
  const probesFailing = owedWork.unchecked.errored.length > 0;

  const doneIds = useMemo(
    () =>
      new Set(
        Object.values(rows ?? {})
          .filter((r) => r.status === 'integrated')
          .map((r) => r.activityId)
      ),
    [rows]
  );
  const rankedOwed = useMemo(
    () =>
      rankOwed(owedItems, { network: networkModel, done: doneIds, policy: project?.reviewPolicy }),
    [owedItems, networkModel, doneIds, project]
  );
  const emptyCounts = useMemo(() => {
    const statuses =
      networkModel !== undefined
        ? computeActivityStatuses(
            networkModel,
            (id) => gitFor(project, id),
            undefined,
            'not-started',
            (id) => rows?.[id]
          )
        : new Map<string, never>();
    return emptyStateCounts(statuses, {
      inFlight: inFlightIds,
      waiting: waitingActivityIds(rows),
    });
  }, [networkModel, project, rows, inFlightIds]);

  // The toolbar's module store, kept so "Clear filters" is a real reset rather
  // than a no-op. The plan mounts NO toolbar (ConstructionShell, which owned
  // it, is retired by §7.4), so nothing is filtered out and TasksLens's
  // FilteredOut branch — its only caller — is unreachable today.
  const activityIds = useMemo(() => Object.keys(rows ?? {}), [rows]);
  const toolbarSignature = useMemo(
    () => toolbarSignatureOf(projectId, activityIds),
    [projectId, activityIds]
  );
  const { toolbar, setToolbar } = useLensToolbar(toolbarSignature);

  // --- Begin / Resume (ported verbatim) -------------------------------------
  const projectRequestedAt = useReadRequestedAt(projectKey(projectId));
  const begin = useBeginConstruction(projectId, {
    onError: (err, atFailure) => {
      writeBeginFailure(projectId, {
        outcome: dispatchOutcomeFor(err instanceof ApiError ? err.status : undefined, err.message),
        at: orderedNow(),
        dismissed: false,
        holdExpired: false,
        startedAtFailure: atFailure.constructionStarted,
      });
    },
    onSuccess: (result) => {
      if (pumpDispatched(result)) writeBeginDispatched(projectId, { at: orderedNow() });
    },
  });
  const [beginTick, setBeginTick] = useState<string | null>(null);
  const [nothingToDispatch, setNothingToDispatch] = useState(false);
  const beginInFlightRef = useRef(false);
  const beginControlRef = useRef<BeginControl | undefined>(undefined);
  const onBegin = (tickId: string): void => {
    if (beginInFlightRef.current || beginPending) return;
    const control = beginControlRef.current;
    if (control !== undefined && !beginConfirmAllowed(control, beginPending)) return;
    beginInFlightRef.current = true;
    lastProgressAtRef.current = Date.now();
    writeBeginFailure(projectId, null);
    writeBeginDispatched(projectId, null);
    setNothingToDispatch(false);
    setCascading(true);
    begin.mutate(tickId, {
      onSuccess: (result) => {
        lastProgressAtRef.current = Date.now();
        const started = pumpDispatched(result);
        setNothingToDispatch(!started);
        setCascading(started);
      },
      onError: () => {
        if (readBeginFailure(projectId)?.outcome.kind === 'rejected') setCascading(false);
      },
      onSettled: () => {
        beginInFlightRef.current = false;
      },
    });
  };

  // The pump's pickup is judged from the PROJECT read alone here. The console
  // read a probed session's request time out of the gate-OCCURRENCE store,
  // which went with the decision pane; a missing session term can only make the
  // hold LONGER (Begin stays disabled), never offer Begin beside a live pump.
  const pickupReads = {
    projectRequestedAt,
    rowsInFlight: anyRowInFlight(rows, owedMarks),
    sessionRequestedAt: 0,
    sessionStage: undefined,
  };
  const pumpEvidenced =
    beginFailure !== null &&
    pumpEvidencedSince(beginFailure.at, {
      ...pickupReads,
      constructionStarted: project?.constructionStarted,
      startedAtFailure: beginFailure.startedAtFailure,
    });
  const beginHold = beginHoldFor(beginFailure, pumpEvidenced);
  const awaitingPickupNow = awaitingPickup(beginDispatched, pickupReads);
  const beginActive = beginRunning({
    pending: beginPending,
    inFlight,
    awaitingPickup: awaitingPickupNow,
  });
  const pickedUpAt =
    beginDispatched !== null && !awaitingPickupNow ? beginDispatched.at : undefined;
  useEffect(() => {
    if (pickedUpAt === undefined) return;
    writeBeginDispatched(projectId, (d) => (d !== null && d.at === pickedUpAt ? null : d));
  }, [pickedUpAt, projectId]);
  const dispatchedAt =
    awaitingPickupNow && beginDispatched !== null ? beginDispatched.at : undefined;
  useEffect(() => {
    if (dispatchedAt === undefined) return undefined;
    const id = setTimeout(
      () => {
        writeBeginDispatched(projectId, (d) => (d !== null && d.at === dispatchedAt ? null : d));
      },
      Math.max(0, dispatchedAt + UNKNOWN_OUTCOME_HOLD_MS - Date.now())
    );
    return (): void => {
      clearTimeout(id);
    };
  }, [dispatchedAt, projectId]);
  const leavingAt =
    beginFailure !== null && failureLeavesMemory(beginHold, inFlight) ? beginFailure.at : undefined;
  useEffect(() => {
    if (leavingAt === undefined) return;
    writeBeginFailure(projectId, (f) => (f !== null && f.at === leavingAt ? null : f));
  }, [leavingAt, projectId]);
  const heldSince = beginHold === 'held' && beginFailure !== null ? beginFailure.at : undefined;
  useEffect(() => {
    if (heldSince === undefined) return undefined;
    const id = setTimeout(
      () => {
        writeBeginFailure(projectId, (f) =>
          f !== null && f.at === heldSince ? { ...f, holdExpired: true, dismissed: false } : f
        );
      },
      Math.max(0, heldSince + UNKNOWN_OUTCOME_HOLD_MS - Date.now())
    );
    return (): void => {
      clearTimeout(id);
    };
  }, [heldSince, projectId]);

  // The poll's watchdog: it falls quiet ~30s after progress stops, and never
  // while a dispatch is pending or Begin is held for the pump.
  const keepPolling = beginPending || beginHold === 'held' || awaitingPickupNow;
  useEffect(() => {
    if (!cascading || keepPolling) return undefined;
    const id = setInterval(() => {
      const since = Math.max(lastProgressAtRef.current, readBeginFailure(projectId)?.at ?? 0);
      if (Date.now() - since > 30000) setCascading(false);
    }, 1500);
    return (): void => {
      clearInterval(id);
    };
  }, [cascading, keepPolling, projectId]);

  const beginFailureCopy =
    beginFailure !== null && !beginFailure.dismissed
      ? beginHold === 'expired'
        ? holdExpiredCopy(beginFailure.outcome)
        : dispatchOutcomeCopy(beginFailure.outcome)
      : undefined;

  const beginControl = beginControlFor({
    constructionStarted: project?.constructionStarted,
    projectLoading,
    running: beginActive,
    awaitingPump: beginHold === 'held',
    probesFailing,
  });
  useEffect(() => {
    beginControlRef.current = beginControl;
  }, [beginControl]);

  const resume = useResumeConstruction(projectId);
  const pausedControl = pausedControlFor({
    operatorPaused: project?.operatorPaused,
    pauseReason: project?.pauseReason,
    projectLoading,
    pending: resume.isPending,
  });
  const [resumeOutcome, setResumeOutcome] = useState<ResumeOutcome | null>(null);
  const onResume = (): void => {
    setResumeOutcome(null);
    resume.mutate(undefined, {
      onSuccess: () => {
        setResumeOutcome({ kind: 'resumed' });
      },
      onError: (err) => {
        setResumeOutcome(resumeOutcomeFor(failureStatusOf(err), err.message));
      },
    });
  };
  const resumeCopy = resumeOutcome !== null ? resumeOutcomeCopy(resumeOutcome) : undefined;
  const dispatchCandidates = useMemo(
    () => notStartedActivities(rows, titleForId),
    [rows, titleForId]
  );

  // --- Navigation -----------------------------------------------------------
  const openActivity = (activityId: string, task?: string): void => {
    void navigate({
      to: ACTIVITY_PATH,
      params: { projectId, activityId },
      search: () => activitySearch(task, undefined),
    });
  };
  const openLens = (next: PlanLensId): void => {
    void navigate({ to: PLAN_PATH, params: { projectId }, search: () => planSearch(next) });
  };
  // [Review] opens the activity ON its gate task (R8). The pane it used to
  // select into is gone: a verdict is given on the activity screen now.
  const onReview = (item: RankedOwed): void => {
    openActivity(item.activityId, item.gate?.task);
  };

  const shapeOf = (item: RankedOwed): string => {
    const join = contractJoinFor(contractJoinInput, item.activityId);
    const contract = join.kind === 'contract' ? join.contract : undefined;
    const scenarios = project?.testingState?.systemTestPlan?.scenarios?.length;
    const isPlan = rows?.[item.activityId]?.variant === 'plan';
    return shapeFor(item.kind, {
      ...(contract?.ops !== undefined ? { contractOps: contract.ops.length } : {}),
      ...(isPlan && scenarios !== undefined ? { scenarios } : {}),
    });
  };

  const beginAction =
    project?.operating === true ? null : pausedControl !== undefined ? (
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.25 }}>
        <Box
          component="span"
          data-testid={UI_IDENTIFIERS.Construction.PAUSED_LABEL}
          role="status"
          sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700 }}
          title={pausedControl.reason}
        >
          {pausedControl.statusLabel}
        </Box>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.RESUME_BUTTON}
          disabled={pausedControl.disabled}
          size="small"
          startIcon={
            pausedControl.busy ? (
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
          onClick={onResume}
        >
          {pausedControl.label}
        </Button>
      </Box>
    ) : (
      <>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.BEGIN_BUTTON}
          disabled={beginControl.disabled}
          size="small"
          startIcon={
            beginControl.busy ? (
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
          onClick={() => {
            setBeginTick(crypto.randomUUID());
          }}
        >
          {beginControl.label}
        </Button>
        <BeginConfirmDialog
          candidates={dispatchCandidates}
          tickId={beginTick}
          verb={beginControl.verb}
          onCancel={() => {
            setBeginTick(null);
          }}
          onConfirm={(tickId) => {
            setBeginTick(null);
            onBegin(tickId);
          }}
        />
      </>
    );

  return (
    <ExperienceChrome
      bodyScroll="shared"
      eyebrow={`PLAN · ${LENS_LABEL[lens].toUpperCase()}`}
      phaseNum={2}
      phaseTitle="Plan"
      projectName={project?.name ?? projectId}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Plan.SCREEN}
        sx={{ flexGrow: 1, minWidth: 0, px: { xs: 2, md: 4 }, pt: 3, pb: 3 }}
      >
        <Box
          sx={{
            display: 'flex',
            alignItems: 'flex-start',
            gap: 1.5,
            flexWrap: 'wrap',
            mb: 2,
          }}
        >
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography component="h1" sx={{ color: t.ink }} variant="h4">
              Plan
            </Typography>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>
              {lensSubtitle(lens)}
            </Typography>
          </Box>
          <ToggleButtonGroup
            exclusive
            aria-label="Plan lens"
            size="small"
            value={lens}
            onChange={(_e, next: unknown) => {
              // The lens is the URL's: the toggle NAVIGATES, so a deep link and
              // the control can never disagree. `null` is the de-select click.
              if (isPlanLensId(next)) openLens(next);
            }}
          >
            {LENS_IDS.map((id) => (
              <ToggleButton
                data-testid={LENS_TESTID[id]}
                key={id}
                sx={{ fontFamily: t.mono, fontWeight: 700, px: 1.5 }}
                value={id}
              >
                {LENS_LABEL[id].toUpperCase()}
              </ToggleButton>
            ))}
          </ToggleButtonGroup>
          {beginAction !== null ? <Box sx={{ flexShrink: 0 }}>{beginAction}</Box> : null}
        </Box>

        {beginFailure !== null && beginFailureCopy !== undefined ? (
          <Alert
            data-hold={beginHold}
            data-outcome={beginFailure.outcome.kind}
            data-testid={UI_IDENTIFIERS.Construction.BEGIN_ERROR}
            severity="error"
            sx={{ mb: 2, fontFamily: t.mono, fontSize: 12 }}
            onClose={() => {
              writeBeginFailure(projectId, { ...beginFailure, dismissed: true });
            }}
          >
            <Box component="span" sx={{ display: 'block', fontWeight: 700 }}>
              {beginFailureCopy.headline}
            </Box>
            <Box component="span" sx={{ display: 'block' }}>
              {beginFailureCopy.detail}
            </Box>
          </Alert>
        ) : null}

        {nothingToDispatch ? (
          <Alert
            data-testid={UI_IDENTIFIERS.Construction.BEGIN_NOTE}
            severity="info"
            sx={{ mb: 2, fontFamily: t.mono, fontSize: 12 }}
            onClose={() => {
              setNothingToDispatch(false);
            }}
          >
            {NOTHING_TO_DISPATCH}
          </Alert>
        ) : null}

        {resumeOutcome !== null && resumeCopy !== undefined ? (
          <Alert
            data-outcome={resumeOutcome.kind}
            data-testid={UI_IDENTIFIERS.Construction.RESUME_OUTCOME}
            severity={resumeOutcome.kind === 'resumed' ? 'success' : 'error'}
            sx={{ mb: 2, fontFamily: t.mono, fontSize: 12 }}
            onClose={() => {
              setResumeOutcome(null);
            }}
          >
            <Box component="span" sx={{ display: 'block', fontWeight: 700 }}>
              {resumeCopy.headline}
            </Box>
            <Box component="span" sx={{ display: 'block' }}>
              {resumeCopy.detail}
            </Box>
          </Alert>
        ) : null}

        {projectLoading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
            <CircularProgress />
          </Box>
        ) : lens === 'tasks' ? (
          <TasksLens
            empty={{
              counts: emptyCounts,
              ...(project?.operating !== true
                ? {
                    resume:
                      pausedControl !== undefined
                        ? {
                            label: pausedControl.label,
                            disabled: pausedControl.disabled,
                            onClick: onResume,
                          }
                        : {
                            label: beginControl.label,
                            disabled: beginControl.disabled,
                            onClick: (): void => {
                              setBeginTick(crypto.randomUUID());
                            },
                          },
                  }
                : {}),
            }}
            gitOf={(id) => gitFor(project, id)}
            items={rankedOwed}
            noteOf={(id) => {
              const row = rows?.[id];
              return row !== undefined
                ? pendingNoteLineFor(
                    row.pendingOperatorNotes,
                    activityRowState(row, owedMarks.get(id))
                  )
                : undefined;
            }}
            paused={
              pausedControl !== undefined
                ? { label: pausedControl.statusLabel, reason: pausedControl.reason }
                : undefined
            }
            policy={project?.reviewPolicy}
            projectId={projectId}
            // Nothing is selected on the plan: selection moved to the activity
            // route, and this lens keeps the prop for the row it highlights
            // right after a decision is made there.
            selection={{}}
            shapeOf={shapeOf}
            supervisionCap={project?.constructionProgress?.supervisionCap}
            totalOwed={rankedOwed.length}
            unchecked={{
              pending: owedWork.unchecked.pending.length,
              errored: owedWork.unchecked.errored.length,
              retrying: retryingProbes.filter((id) => owedWork.unchecked.errored.includes(id))
                .length,
              onRetry: retryErrored,
            }}
            onClearFilters={() => {
              setToolbar({ ...DEFAULT_TOOLBAR, sort: toolbar.sort });
            }}
            onReview={onReview}
          />
        ) : lens === 'graph' ? (
          <PlanGraph activities={activities} projectId={projectId} onOpen={openActivity} />
        ) : (
          <PlanList
            activities={activities}
            emptyReason={activityListModel === undefined ? 'noPlan' : 'noRows'}
            onOpen={openActivity}
          />
        )}
      </Box>
    </ExperienceChrome>
  );
}
