/**
 * The full-screen Construction console (`/project/$projectId/construction`) — the
 * Phase-3 (UC3 superviseConstruction) console, the SIBLING of the Phase-1/2 design
 * experiences. It reuses the SAME ExperienceChrome shell, but swaps the ordered
 * slim-spine for the Stage-B LENS SHELL (`ConstructionShell`: a LIST | GRAPH |
 * TASKS control, a shared toolbar and a persistent detail pane) — because
 * construction is not an ordered sequence of authored artifacts behind a single
 * gate; it is a SUPERVISED PUMP.
 *
 * Task 13 retires the THREE-TAB shell (Tracker · Interventions · Artifacts) that
 * stood in for the lens shell while Stage B built it one lens at a time — the lens
 * shell IS the console now, mounted directly with no tab bar around it. GRAPH and
 * TASKS render `LensComingLater` until Stage D and Stage C build their bodies.
 *
 * It binds to the REAL backend:
 *   - the committed Phase-2 head-state (network × activityList slots, via useProject)
 *     drives the LIST lens's activity tree (CPM facts joined per activity);
 *   - the live construction session (GetSessionState, polled) drives the active-
 *     activity detail and the phase-gate panel;
 *   - the begin + phase-decision controls call the real POST endpoints. The
 *     pause/override controls move with Stage C's Tasks lens, which is where
 *     InterventionQueue/PolicyPanel/InterventionDrawer are rewired in.
 *
 * The construction pump that fills sessions is gated on a build cluster (R-CPR) not
 * provisioned here, so the session is usually quiet — every surface degrades to an
 * honest awaiting state rather than an error.
 */
import { useState, useMemo, useEffect, useRef, type ReactNode } from 'react';
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import PlayArrowRoundedIcon from '@mui/icons-material/PlayArrowRounded';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import type { ProjectArtifactModelEnvelope, ProjectStateWithGit } from '../contracts/types';
import { slotStageFromOrdinal } from '../contracts/adapters';
import { narrowProject } from '../contracts/projectAdapters';
import { projectKey, useProject } from '../hooks/useProject';
import { useReadRequestedAt } from '../hooks/readRequestTimes';
import { TASKS_FRESHNESS_MS, useConstructionSessions } from '../hooks/useConstructionSessions';
import { useMutationState, useQueryClient } from '@tanstack/react-query';
import { useGateOccurrences } from '../hooks/useGateOccurrences';
import { occurrenceKey } from '../hooks/gateOccurrences';
import {
  decidedFor,
  decisionViewFor,
  gateControlFor,
  observedGateFor,
  type DecidedMark,
  type DecisionView,
  type GateControl,
  type GateDecision,
  type ObservedGate,
  type PaneDecision,
} from '../components/construction/tasks/decisionFlow';
import {
  decisionActivityOf,
  decisionEntriesFrom,
  pendingDecisionActivities,
  type DecisionMutationState,
} from '../components/construction/tasks/decisionRecords';
import { owedWorkFor, probeCandidatesFor } from '../components/construction/tasks/owedWork';
import {
  nextOwedAfter,
  rankOwed,
  type RankedOwed,
} from '../components/construction/tasks/owedRanking';
import {
  emptyStateCounts,
  reasonSentenceFor,
  shapeFor,
} from '../components/construction/tasks/tasksLensCopy';
import { owedMarksFor } from '../components/construction/tasks/owedChip';
import { TasksLens } from '../components/construction/tasks/TasksLens';
import { computeActivityStatuses } from '../contracts/constructionAdapters';
import { contractForActivity } from '../contracts/serviceContracts';
import { gitFor } from '../contracts/types';
import {
  phaseDecisionFilters,
  useBeginConstruction,
  useBeginConstructionPending,
  useSubmitPhaseDecision,
} from '../hooks/useConstructionMutations';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { ChatRail } from '../components/design/ChatRail';
// ConstructionTracker (the CPM graph under a build lens, the EV curves, the
// head-state rollup and the near-critical float table) is no longer the LIST
// lens's body — the lens is defined as "every activity, its lifecycle phases and
// its tasks", and the tree below IS that. The component is kept, not deleted
// (zero importers today, same as EvTrackingChart/HeadStateRollup/NearCritical-
// Float underneath it): it is the Stage D GRAPH lens's reference implementation
// (founder ruling, Stage B progress log) — Stage D rebuilds the GRAPH lens body
// from these pieces rather than reusing this exact composition wholesale.
import {
  ConstructionShell,
  LensComingLater,
} from '../components/construction/lens/ConstructionShell';
import { BeginConfirmDialog } from '../components/construction/lens/BeginConfirmDialog';
import {
  anyRowInFlight,
  awaitingPickup,
  beginControlFor,
  beginHoldFor,
  beginRunning,
  constructionInFlight,
  consolePollMs,
  dispatchOutcomeCopy,
  dispatchOutcomeFor,
  failureLeavesMemory,
  holdExpiredCopy,
  newestLiveSession,
  notStartedActivities,
  pumpEvidencedSince,
  UNKNOWN_OUTCOME_HOLD_MS,
} from '../components/construction/lens/beginControl';
import { ApiError } from '../contracts/errors';
import {
  failureAwaitsPump,
  readBeginFailure,
  useBeginDispatched,
  useBeginFailure,
  writeBeginDispatched,
  writeBeginFailure,
} from '../components/construction/lens/beginFailureMemory';
import { ActivityTreeView } from '../components/construction/list/ActivityTreeView';
import { buildActivityTree, type ActivityMeta } from '../components/construction/list/activityTree';
import {
  applyToolbarToActivities,
  expandToCurrentPhaseControl,
} from '../components/construction/list/activityScope';
import { evidenceViewFor } from '../components/construction/list/observedOnly';
import {
  DEFAULT_TOOLBAR,
  useLensSelection,
  useLensToolbar,
  toolbarSignatureOf,
  type LensId,
} from '../components/construction/lens/useLensSelection';
import { KIND_META, type ActivityKind } from '../components/construction/KindBadge';
import { DetailPane } from '../components/construction/detail/DetailPane';
import { ConstructionEpisodeBodyContainer } from '../containers/ConstructionEpisodeBodyContainer';
import { CommentProvider, useComments } from '../components/comments/CommentContext';

import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/construction');

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
  //
  // Keyed by project (fix-G review M3). An in-app switch to another project reuses
  // this route's component, so the body's refs and state carried over: the Begin
  // in-flight ref, `cascading`, the confirm's tick, the rail and its comments. With
  // a dispatch still pending on the first project, the second's enabled Begin did
  // nothing and said nothing. Another project is another console.
  return (
    <CommentProvider key={projectId}>
      <ConstructionConsoleBody projectId={projectId} />
    </CommentProvider>
  );
}

function ConstructionConsoleBody({ projectId }: { projectId: string }): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
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
  //
  //
  // `cascading` governs only the poll's CADENCE (consolePollMs), never the label
  // (fix-F review, root-cause ruling). The poll stays on, fast, while a dispatch is
  // pending or a failure in module memory still awaits the pump, so a remount
  // cannot stop the poll a held Begin depends on (fix-E review I2). It stays on,
  // slower, while the STATE shows work in flight, however long ago the last
  // integration was, because only a read can say the work has ended.
  const beginPending = useBeginConstructionPending(projectId);
  const beginFailure = useBeginFailure(projectId);
  // A successful dispatch whose pickup no read has shown yet (fix H). Module
  // memory, like the failure, so a remount during the gap keeps its hold.
  const beginDispatched = useBeginDispatched(projectId);
  const [cascading, setCascading] = useState(false);
  // The poll's cadence is consolePollMs's. Where that would stop, the read still
  // refreshes on the TASKS freshness cadence: the owed set's probe candidates come
  // from it, and a gate on an activity started by the sweep, another tab or MCP
  // must not stay invisible (review I3). Its in-flight term reads the rows alone,
  // without the owed set (which is derived from this very read): it only sets the
  // cadence, and the owed set can only turn a running row into a failed one.
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
  // The watchdog that ends the poll sits below the Begin state, because it must
  // never end it while a dispatch is pending or Begin is held for the pump.

  // The failure is recorded from the mutation's OWN onError, into module memory, so
  // an answer that lands while the console is away is still kept (fix-E review I2).
  // A success is recorded the same way, before its refresh is requested (fix H).
  const begin = useBeginConstruction(projectId, {
    onError: (err, atFailure) => {
      writeBeginFailure(projectId, {
        outcome: dispatchOutcomeFor(err instanceof ApiError ? err.status : undefined, err.message),
        at: Date.now(),
        dismissed: false,
        holdExpired: false,
        // Only a change from "not started" can be evidence (fix-G review I1).
        startedAtFailure: atFailure.constructionStarted,
      });
    },
    onSuccess: () => {
      writeBeginDispatched(projectId, { at: Date.now() });
    },
  });
  const submitPhaseDecision = useSubmitPhaseDecision(projectId);

  // --- Gate decisions (Stage C) ----------------------------------------------
  // The single `.find()` of "the" in-construction activity is gone (spec §1: it
  // could only ever return one); decisions are made from the shared pane, for any
  // owed gate. What happened on the wire lives in the QueryClient's MUTATION CACHE
  // (every decision of this project shares one mutation key), never in component
  // state: a decision on the wire is still on the wire after the console remounts
  // — navigating home and back — so Approve cannot come back on under it (review
  // C1). What it means — sending, resumed, did not land — is derived at render
  // against the gate OCCURRENCE it answered (tasks/decisionFlow.ts; the occurrence
  // store folds every session read the cache takes, review C2), so no effect ever
  // sets state to follow the workflow.
  const queryClient = useQueryClient();
  const occurrences = useGateOccurrences();
  // When the shown project read was REQUESTED — from the ONE request-time store
  // (hooks/readRequestTimes), which the gate occurrences read too. The Begin hold's
  // evidence and the gate's "now in <phase>" both count a read from its request,
  // never its arrival.
  const projectRequestedAt = useReadRequestedAt(projectKey(projectId));
  const decisionStates = useMutationState({
    // The ONE project-scoped filter, shared with the one-click guard below.
    filters: phaseDecisionFilters(projectId),
    select: (m): DecisionMutationState => ({
      status: m.state.status,
      variables: m.state.variables,
      data: m.state.data,
      error: m.state.error,
      submittedAt: m.state.submittedAt,
    }),
  });
  // The clock the derivation reads: ticks once a second while a decision is live.
  const [decisionNow, setDecisionNow] = useState(0);

  // Begin is a real dispatch, so the button only opens a confirm step that names
  // what would be started (BeginConfirmDialog). Each opening mints ONE tickID, which
  // CORRELATES the request (logs, traces, the trapped specs) — it is not what keeps
  // a second pump from starting: the server runs one pump workflow per project
  // (architect I1 ruling). The client guards here and in the dialog are UX
  // debouncing, so one press sends one request. `null` is "closed".
  const [beginTick, setBeginTick] = useState<string | null>(null);
  // A ref, not only the pending flag: clicks delivered in one task all land before a
  // re-render could report the first as pending (pinned by the same-task triple
  // click in construction-begin-confirm.spec).
  const beginInFlightRef = useRef(false);
  // A failed dispatch is LOUD (spec §6 "Failures must be loud", fix-B review M3) —
  // and HONEST about what it knows (fix-C review). The server starts the pump before
  // it answers and can still answer 5xx after that, or the response can be dropped,
  // so only a 4xx means nothing started (dispatchOutcomeFor). On an unknown outcome
  // the console keeps polling, as it does after a success, and Begin stays held
  // until the pump is EVIDENCED or a bounded hold expires (beginHoldFor, fix-D
  // review I3). A newer read alone does not lift it: the first read after a 5xx can
  // land before the pump has stored its StartedAt, and a "Begin" there was a second
  // pump one click away. The failure (its `at`, the alert's `dismissed`, the
  // hold's `holdExpired`) lives in module memory keyed by project, so a remount
  // keeps it (beginFailureMemory, fix-E review I2).
  const onBegin = (tickId: string): void => {
    if (beginInFlightRef.current || beginPending) return;
    beginInFlightRef.current = true;
    lastProgressAtRef.current = Date.now();
    writeBeginFailure(projectId, null);
    writeBeginDispatched(projectId, null);
    setCascading(true);
    begin.mutate(tickId, {
      // The fast poll runs its full window from the ANSWER: a dispatch pending past
      // the watchdog's 30s would otherwise come back to a slow poll, or none, just
      // as the pump picks its first activity up. Cadence only; the label is state's.
      onSuccess: () => {
        lastProgressAtRef.current = Date.now();
        setCascading(true);
      },
      onError: () => {
        // The mutation's own onError has recorded the failure. A refusal started
        // nothing, so there is nothing to wait on. Anything else keeps polling,
        // exactly as after a success, with the progress window restarted from the
        // failure (the watchdog reads its `at`).
        if (readBeginFailure(projectId)?.outcome.kind === 'rejected') setCascading(false);
      },
      onSettled: () => {
        beginInFlightRef.current = false;
      },
    });
  };
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

  const networkEnvelope = committedEnvelope(project, 'network');
  const activityEnvelope = committedEnvelope(project, 'activityList');

  // The committed Phase-1 `system` slot — ServiceContractView's Dynamic tab
  // needs its dynamicViews to draw real call chains. Same lookup the Artifacts
  // tab already does; hoisted here because the detail pane's artifact body now
  // renders the same view.
  const paneSystemEnvelope = useMemo(
    () => (project?.slots ?? []).find((s) => s.kind === 'system')?.model ?? undefined,
    [project]
  );

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
  // Every row joins today: the server emits one row per activity in the
  // committed list (a planned-no-record row where nothing is recorded yet), and
  // the construction head-state is keyed by the same derived ids.
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
        // Task 11's search matches activity id / title / componentId — joined
        // the same way as `label`; a project-wide activity (N-STP, N-IT, …)
        // builds no single component, so it carries none.
        ...(a.componentId !== undefined && a.componentId.length > 0
          ? { componentId: a.componentId }
          : {}),
      };
    }
    return byId;
  }, [activityListModel, networkModel]);

  // The rows as the EVIDENCE VIEW reads them: every activity always, and with
  // "Observed only" on, reconstructed evidence set aside so an activity known only
  // from it reads as not started (observedOnly.ts, designer P1-11). The tree and
  // the pane read the same rows, so they never disagree about one activity.
  // The view also carries the attempts it set aside, so the pane can say a row's
  // record was hidden rather than call it unrecorded (designer re-check B1).
  const evidenceView = useMemo(
    () => evidenceViewFor(project?.constructionRows, toolbar.observedOnly),
    [project, toolbar.observedOnly]
  );
  const viewRows = evidenceView.rows;

  // --- Owed decisions (Stage C) ---------------------------------------------
  // Every decision the pipeline is stopped on, from the LIVE workflow stage
  // (tasks/owedWork.ts) — never head-state `in-review`, which only means some
  // phases are complete (spec §1). The session probe is asked only of activities
  // the pump started and has not finished: zero today, at most the supervision cap
  // while cascading.
  //
  // ONE place for the TASKS badge, and it reads the EVIDENCE VIEW like every other
  // surface here. "Observed only" cannot change the owed SET — a gate is the live
  // session and a failure is the pump's own record, both kept by the view — so the
  // count is the same either way; what the view may strip is a mixed-ledger row's
  // current phase, which the lens then reports as unreported rather than guessed.
  const probeIds = useMemo(() => probeCandidatesFor(viewRows), [viewRows]);
  // Each decision sent, read back from the mutation cache, and judged against the
  // gate occurrence it answered and the clock. The observation comes from the
  // occurrence store — not from this render's probe set — so what is probed can
  // depend on it without a cycle.
  const decisionEntries = useMemo(() => decisionEntriesFrom(decisionStates), [decisionStates]);
  // Every activity with a decision still on the wire, retired record or not: its
  // Approve / Send back stay off until the request settles (review I1, round 2).
  const pendingActivities = useMemo(
    () => pendingDecisionActivities(decisionStates),
    [decisionStates]
  );
  const observedFor = (activityId: string): ObservedGate =>
    observedGateFor(occurrences.get(occurrenceKey(projectId, activityId)), {
      at: projectRequestedAt,
      lifecyclePhase: project?.constructionRows?.[activityId]?.currentLifecyclePhase,
    });
  const decisionViews: Record<string, DecisionView> = {};
  for (const [key, e] of Object.entries(decisionEntries)) {
    decisionViews[key] = decisionViewFor(e.record, observedFor(e.record.activityId), decisionNow);
  }
  // An activity with a LIVE decision stays probed, so its resume can be observed
  // even once the pump marks it finished. A retired record (lingered out, or its
  // gate superseded by a later occurrence) is no reason to keep asking.
  const decidedIdsKey = [
    ...new Set(
      Object.entries(decisionEntries)
        .filter(([key]) => decisionViews[key]?.kind !== 'done')
        .map(([, e]) => e.record.activityId)
    ),
  ]
    .sort((a, b) => a.localeCompare(b))
    .join(' ');
  const sessionIds = useMemo(
    () =>
      [
        ...new Set([...probeIds, ...(decidedIdsKey.length > 0 ? decidedIdsKey.split(' ') : [])]),
      ].sort((a, b) => a.localeCompare(b)),
    [probeIds, decidedIdsKey]
  );
  const {
    sessions: sessionsByActivity,
    errored: erroredProbes,
    retrying: retryingProbes,
    retryErrored,
  } = useConstructionSessions(projectId, sessionIds);
  // A probe that has not answered is not an answer: `unchecked` counts them, and
  // the lens makes no all-clear claim while it is above zero (architect Q1).
  const owedWork = useMemo(
    () =>
      owedWorkFor({
        rows: viewRows,
        sessions: sessionsByActivity,
        erroredProbes,
        titleFor: titleForId,
      }),
    [viewRows, sessionsByActivity, erroredProbes, titleForId]
  );
  const owedItems = owedWork.items;
  const tasksOwed = owedItems.length;
  // ONE owed vocabulary for every lens and the pane (tasks/owedChip.ts, Q4): the
  // list's "Awaiting me" scope, its row chips and the pane's state chip read these
  // marks, never head-state in-review.
  const owedMarks = useMemo(() => owedMarksFor(owedItems), [owedItems]);

  // --- Begin/Resume state --------------------------------------------------------
  // It sits below the owed set because it reads it.
  //
  // What the STATE says is in flight: any activity whose owed-aware row state is
  // running or awaiting a human — "awaiting" from the owed set, never head-state
  // (Q4) — or a live session among the probed ones (constructionInFlight). It alone
  // decides "Construction running…". The probes are the owed set's: every activity
  // the pump started and has not finished, each read's stage and request time taken
  // from the gate occurrences, which the one request-time store feeds.
  const liveSession = newestLiveSession(
    sessionIds.map((id) => {
      const o = occurrences.get(occurrenceKey(projectId, id));
      return { stage: o?.stage, requestedAt: o?.requestedAt ?? 0 };
    })
  );
  const inFlight = constructionInFlight({
    rows: project?.constructionRows,
    owed: owedMarks,
    sessionStage: liveSession?.stage,
  });
  // Pump evidence counts only from reads REQUESTED after the failure, never by when
  // they arrived, and only what CHANGED after it: the project shows work in flight
  // (owed-aware) or newly says construction started, or a probed session is live
  // (pumpEvidencedSince, hooks/readRequestTimes; fix-G review I1).
  const pickupReads = {
    projectRequestedAt,
    rowsInFlight: anyRowInFlight(project?.constructionRows, owedMarks),
    sessionRequestedAt: liveSession?.requestedAt ?? 0,
    sessionStage: liveSession?.stage,
  };
  const pumpEvidenced =
    beginFailure !== null &&
    pumpEvidencedSince(beginFailure.at, {
      ...pickupReads,
      constructionStarted: project?.constructionStarted,
      startedAtFailure: beginFailure.startedAtFailure,
    });
  const beginHold = beginHoldFor(beginFailure, pumpEvidenced);
  // After a SUCCESS, Begin stays held until a read requested after it shows the
  // pickup, or the hold runs out (fix H; until B1's "pump open" read). Without it,
  // the gap between the answer and the first read showing the pickup offered an
  // enabled Begin/Resume beside a pump that had just been started.
  const awaitingPickupNow = awaitingPickup(beginDispatched, pickupReads);
  // "Construction running…", disabled: a dispatch pending, work in flight by state,
  // or a success awaiting its pickup (beginRunning). The same rule on the success
  // path, the failure path and after a remount (fix-F review, root-cause ruling).
  const beginActive = beginRunning({
    pending: beginPending,
    inFlight,
    awaitingPickup: awaitingPickupNow,
  });
  // The pickup hold's record leaves memory once a read shows the pickup (the state
  // decides from then on), keyed by the success it clears, so a newer one is never
  // the one removed.
  const pickedUpAt =
    beginDispatched !== null && !awaitingPickupNow ? beginDispatched.at : undefined;
  useEffect(() => {
    if (pickedUpAt === undefined) return;
    writeBeginDispatched(projectId, (d) => (d !== null && d.at === pickedUpAt ? null : d));
  }, [pickedUpAt, projectId]);
  // ...or when the hold runs out with no sign of the pickup: Begin/Resume come back.
  // Timed from the SUCCESS, so a remount re-arms whatever is left, or clears it at
  // once if it ran out while the console was away.
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
  // An evidenced failure has done its job once the state shows nothing in flight,
  // and leaves memory: kept, a remount would flash it and bring back a stale
  // "Outcome unknown" alert (fix-F review). The write runs in an effect, keyed by
  // the failure it clears, so a newer failure is never the one removed.
  const leavingAt =
    beginFailure !== null && failureLeavesMemory(beginHold, inFlight) ? beginFailure.at : undefined;
  useEffect(() => {
    if (leavingAt === undefined) return;
    writeBeginFailure(projectId, (f) => (f !== null && f.at === leavingAt ? null : f));
  }, [leavingAt, projectId]);
  // The bounded hold. It runs from the failure, and evidence clears it. When it
  // expires with no evidence, Begin comes back and the alert, shown again even if
  // it was dismissed, says there was no sign of the pump. The setState runs in the
  // timer's callback, never in the effect body.
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

  // The poll's watchdog: it falls quiet ~30s after progress stops. It never runs
  // while a dispatch is still PENDING, because a 5xx that arrives after 30s would
  // otherwise find the poll already stopped (fix-D review I1). It also never runs
  // while Begin is held for the pump, or for a success's pickup: only a read can
  // bring the evidence that lifts the hold.
  const keepPolling = beginPending || beginHold === 'held' || awaitingPickupNow;
  useEffect(() => {
    if (!cascading || keepPolling) return undefined;
    const id = setInterval(() => {
      // Progress, or the failure the poll resumed after (a remounted console has
      // no progress of its own to go on).
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

  // --- Begin/Resume ---------------------------------------------------------
  // The label is the project read's constructionStarted, computed once on the
  // server from the stored head-state — never counted from rows or attempts here,
  // which the backfill filled with reconstructed work no pump ever ran. It used to
  // probe one session endpoint per committed activity on every load. While the
  // project is loading the button is disabled and names neither word, so it cannot
  // read "Begin" and flip to "Resume" after load.
  const beginControl = beginControlFor({
    constructionStarted: project?.constructionStarted,
    projectLoading,
    running: beginActive,
    awaitingPump: beginHold === 'held',
  });
  const dispatchCandidates = useMemo(
    () => notStartedActivities(project?.constructionRows, titleForId),
    [project, titleForId]
  );

  const activityTree = useMemo(
    () => buildActivityTree(Object.values(viewRows ?? {}), { meta: activityMeta }),
    [viewRows, activityMeta]
  );

  // Scope/kind/layer/search/sort — every rule in one pure
  // pipeline (see activityScope.ts) so ActivityTreeView renders exactly what
  // the toolbar says and nothing this file has to keep in sync by hand.
  const visibleActivityTree = useMemo(
    () => applyToolbarToActivities(activityTree, toolbar, owedMarks),
    [activityTree, toolbar, owedMarks]
  );

  // --- The TASKS lens (Stage C) ------------------------------------------------
  // The owed set, ranked risk-floor-first then by blast radius over the committed
  // network (done = integrated in the evidence view), then passed through the SAME
  // toolbar pipeline as the list: a row shows iff its activity does.
  const doneIds = useMemo(
    () =>
      new Set(
        Object.values(viewRows ?? {})
          .filter((r) => r.status === 'integrated')
          .map((r) => r.activityId)
      ),
    [viewRows]
  );
  const rankedOwed = useMemo(
    () =>
      rankOwed(owedItems, { network: networkModel, done: doneIds, policy: project?.reviewPolicy }),
    [owedItems, networkModel, doneIds, project]
  );
  const visibleOwed = useMemo(() => {
    const shown = new Set(visibleActivityTree.map((n) => n.activityId));
    return rankedOwed.filter((i) => shown.has(i.activityId));
  }, [rankedOwed, visibleActivityTree]);
  // "Nothing needs you." is not a dead end: eligible/blocked from the network over
  // the evidence view, in flight = what the pump started and has not finished.
  const emptyCounts = useMemo(() => {
    const statuses =
      networkModel !== undefined
        ? computeActivityStatuses(
            networkModel,
            (id) => gitFor(project, id),
            undefined,
            'not-started',
            (id) => viewRows?.[id]
          )
        : new Map<string, never>();
    return emptyStateCounts(statuses, probeIds.length);
  }, [networkModel, project, viewRows, probeIds]);
  const anyDecisionLive = Object.values(decisionViews).some((v) => v.kind !== 'done');
  useEffect(() => {
    if (!anyDecisionLive) return undefined;
    const tick = (): void => {
      setDecisionNow(Date.now());
    };
    const id = setInterval(tick, 1000);
    const first = setTimeout(tick, 0);
    return (): void => {
      clearInterval(id);
      clearTimeout(first);
    };
  }, [anyDecisionLive]);
  // One gate's controls and line: its own record's view, held off while ANY decision
  // for the activity is still on the wire (decisionFlow.gateControlFor).
  const controlFor = (item: Pick<RankedOwed, 'key' | 'activityId'>): GateControl =>
    gateControlFor(
      decisionViews[item.key],
      decisionEntries[item.key]?.record.decision,
      pendingActivities.has(item.activityId)
    );
  // A decision made on a gate, while its record lives — the pane's "Decided ·
  // approved" chip and its lead line (designer P1-4).
  const decidedForKey = (key: string): DecidedMark | undefined => {
    const e = decisionEntries[key];
    const view = decisionViews[key];
    return e !== undefined && view !== undefined ? decidedFor(e.record, view) : undefined;
  };
  // A resumed row lingers in place (spec §6) after the owed set has dropped it —
  // across a remount too: the item it shows rode the decision into the cache.
  const lingering = Object.entries(decisionEntries)
    .filter(
      ([key]) => decisionViews[key]?.kind !== 'done' && !rankedOwed.some((i) => i.key === key)
    )
    .flatMap(([, e]) => (e.item !== undefined ? [e.item] : []));
  const lingeringKeys = new Set(lingering.map((i) => i.key));

  const decideGate = (item: RankedOwed, decision: GateDecision, note = ''): void => {
    const lifecyclePhase = item.gate?.lifecyclePhase;
    // Never address a decision to a guessed key: no reported phase, no signal.
    if (lifecyclePhase === undefined) return;
    const key = item.key;
    // One click, one signal — asked of the mutation cache itself, synchronously:
    // clicks delivered in one task all land before any re-render, and a remount
    // keeps a pending decision pending (review C1). Per ACTIVITY, the same rule the
    // buttons follow (controlFor): a decision on the wire holds its whole activity.
    const onTheWire = queryClient.isMutating({
      ...phaseDecisionFilters(projectId),
      predicate: (m) => decisionActivityOf(m.state.variables) === item.activityId,
    });
    if (onTheWire > 0 || controlFor(item).busy) return;
    // The occurrence this decision answers: a later one retires it (review C2).
    const epoch = occurrences.get(occurrenceKey(projectId, item.activityId))?.epoch ?? 0;
    // Send back carries the human's words: the pane's note, any free-form notes and
    // the anchored comments from the co-author rail (ConstructionReviewFeedback).
    const wireComments = decision === 'sendBack' ? toWire() : [];
    const notes =
      decision === 'sendBack'
        ? [note.trim(), freeformNotes()].filter((s) => s.length > 0).join('\n') ||
          wireComments.map((c) => c.text).join('\n')
        : '';
    submitPhaseDecision.mutate(
      {
        activityId: item.activityId,
        phase: lifecyclePhase,
        decision,
        ...(decision === 'sendBack'
          ? {
              feedback: {
                notes,
                ...(wireComments.length > 0 ? { comments: wireComments } : {}),
              },
            }
          : {}),
        // Never sent: read back from the mutation cache (decisionRecords.ts).
        occurrence: { key, epoch, snapshot: item },
      },
      {
        onSuccess: () => {
          // The anchored comments rode this decision; they must not bleed into the
          // next gate's.
          reset();
        },
      }
    );
  };

  // [Review] (and the drawer's "Next decision →") open the shared pane in place:
  // on the gate TASK where the profile names one (the review body), else on the
  // activity.
  const openOwed = (item: RankedOwed): void => {
    const g = item.gate;
    select(
      g?.task !== undefined && g.lifecyclePhase !== undefined
        ? { activityId: item.activityId, lifecyclePhase: g.lifecyclePhase, task: g.task }
        : { activityId: item.activityId }
    );
  };

  const shapeOf = (item: RankedOwed): string => {
    const contract = contractForActivity(project, item.activityId);
    const scenarios = project?.testingState?.systemTestPlan?.scenarios?.length;
    const isPlan = viewRows?.[item.activityId]?.variant === 'plan';
    return shapeFor(item.kind, {
      ...(contract?.ops !== undefined ? { contractOps: contract.ops.length } : {}),
      ...(isPlan && scenarios !== undefined ? { scenarios } : {}),
    });
  };

  // "Expand to current phase" is an IMPERATIVE action, not persisted toolbar
  // state (see ConstructionShellProps.onExpandToCurrentPhase) — a monotonic
  // signal the tree view watches, so a second click re-opens whatever the
  // operator has since collapsed.
  const [expandToPhaseSignal, setExpandToPhaseSignal] = useState(0);

  // The decision the pane can make: the selected activity's live gate, if it has
  // one with a reported phase to address the signal to (DetailPane's `decision`).
  const selectedGate =
    selectedActivityId !== null
      ? [...rankedOwed, ...lingering].find(
          (i) =>
            i.activityId === selectedActivityId &&
            i.reason === 'gate' &&
            i.gate?.lifecyclePhase !== undefined
        )
      : undefined;
  const selectedGatePhase = selectedGate?.gate?.lifecyclePhase;
  const paneDecision: PaneDecision | undefined =
    selectedGate !== undefined && selectedGatePhase !== undefined
      ? {
          lifecyclePhase: selectedGatePhase,
          open: rankedOwed.some((i) => i.key === selectedGate.key),
          gateTask: selectedGate.gate?.task,
          busy: controlFor(selectedGate).busy,
          anchoredCount: toWire().length,
          note: controlFor(selectedGate).note,
          decided: decidedForKey(selectedGate.key),
          onApprove: (): void => {
            decideGate(selectedGate, 'approve');
          },
          onSendBack: (note): void => {
            decideGate(selectedGate, 'sendBack', note);
          },
        }
      : undefined;

  // What the selected activity is owed for — its chip, and for a steer or a
  // failure the reason in the PM's words (tasks/owedChip.ts, designer P0-2).
  const selectedMark = selectedActivityId !== null ? owedMarks.get(selectedActivityId) : undefined;
  const selectedOwedItem =
    selectedActivityId !== null
      ? owedItems.find((i) => i.activityId === selectedActivityId)
      : undefined;
  const paneOwed =
    selectedMark !== undefined
      ? {
          mark: selectedMark,
          sentence:
            selectedOwedItem !== undefined ? reasonSentenceFor(selectedOwedItem) : undefined,
        }
      : undefined;

  // The next owed decision, in the lens order — the drawer's footer link below
  // 1200px, where the table is behind the drawer (designer P2).
  const nextItem = nextOwedAfter(visibleOwed, selectedActivityId ?? undefined);
  const nextDecision =
    nextItem !== undefined
      ? {
          label: `Next decision → ${nextItem.activityId}`,
          onClick: (): void => {
            openOwed(nextItem);
          },
        }
      : undefined;

  // The shell's DETAIL slot: one pane, driven entirely by the URL's selection
  // (never owned by the pane itself), so it cannot lose it to the cascade
  // poll's remount. Beside-content at >=1200px, the existing overlay Drawer
  // below that — see DetailPane.tsx.
  const detailPane =
    selectedActivityId !== null ? (
      <DetailPane
        activityTitle={titleForId(selectedActivityId)}
        // The pane is in the pure `components` layer and may not reach into
        // hooks, so the EPISODE body's queries are handed down from here as a
        // containers-layer render prop (the same reason ActivityLifecyclePanel
        // took an `episodesSlot`).
        decision={paneDecision}
        episodeSlot={({ activityId, attemptId }) => (
          <ConstructionEpisodeBodyContainer
            activityId={activityId}
            attemptId={attemptId}
            projectId={projectId}
          />
        )}
        hiddenAttempts={evidenceView.hidden[selectedActivityId]}
        // The selected activity's live stage, for a selection the ledger cannot
        // place (liveChipFor): `null` where the probe established no session.
        liveStage={
          selectedActivityId in sessionsByActivity
            ? (sessionsByActivity[selectedActivityId]?.stage ?? null)
            : undefined
        }
        nextDecision={nextDecision}
        owed={paneOwed}
        project={project}
        // The selected activity's OWN session, and only while it is at a gate —
        // another activity's reviewer set under this one's review body would be the
        // most direct mis-attribution available on this surface.
        reviewSet={
          sessionsByActivity[selectedActivityId]?.stage === 'awaitingApproval'
            ? sessionsByActivity[selectedActivityId].view.reviewSet
            : undefined
        }
        row={viewRows?.[selectedActivityId]}
        selection={selection}
        systemEnvelope={paneSystemEnvelope}
        onClose={clear}
      />
    ) : undefined;

  const tasksContent = (
    <TasksLens
      decidedOf={(key) => decisionEntries[key]?.record.decision}
      empty={{
        counts: emptyCounts,
        // The header's own Begin/Resume (its label is the server's
        // constructionStarted), opening the same confirm step — never a dispatch.
        ...(project?.operating !== true
          ? {
              resume: {
                label: beginControl.label,
                disabled: beginControl.disabled,
                onClick: (): void => {
                  setBeginTick(crypto.randomUUID());
                },
              },
            }
          : {}),
      }}
      flowOf={(item) => controlFor(item).note}
      gitOf={(id) => gitFor(project, id)}
      // Just-decided rows first, lingering in place with their evidence line.
      items={[...lingering, ...visibleOwed]}
      lingeringKeys={lingeringKeys}
      policy={project?.reviewPolicy}
      projectId={projectId}
      selection={selection}
      shapeOf={shapeOf}
      supervisionCap={project?.constructionProgress?.supervisionCap}
      totalOwed={rankedOwed.length + lingering.length}
      unchecked={{
        pending: owedWork.unchecked.pending.length,
        errored: owedWork.unchecked.errored.length,
        retrying: retryingProbes.filter((id) => owedWork.unchecked.errored.includes(id)).length,
        onRetry: retryErrored,
      }}
      onClearFilters={() => {
        setToolbar({ ...DEFAULT_TOOLBAR, sort: toolbar.sort });
      }}
      onReview={openOwed}
    />
  );

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
        {/* The lens shell IS the console (Task 13) — no tab bar mounts around it. */}
        {/* pt: 0 — the sticky lens toolbar sticks at the scroller's own top edge.
            Top padding here left a strip ABOVE the stuck toolbar that rows scrolled
            through (designer P0-2); the page header carries that spacing instead. */}
        <Box
          sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, pt: 0, pb: 3 }}
        >
          <ConsoleHeader
            action={
              // Operating (Task 14): once construction is fully complete the
              // begin/resume button is hidden entirely — not relabeled, since there
              // is nothing left to begin or resume.
              project?.operating !== true ? (
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
              ) : undefined
            }
            subtitle={lensSubtitle(lens)}
            t={t}
            title="Construction"
          />

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

          {projectLoading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
              <CircularProgress />
            </Box>
          ) : (
            <ConstructionShell
              content={
                lens === 'list' ? (
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <ActivityTreeView
                      expandToCurrentPhaseSignal={expandToPhaseSignal}
                      nodes={visibleActivityTree}
                      owed={owedMarks}
                      projectId={projectId}
                      searchQuery={toolbar.search}
                      selection={selection}
                      totalActivityCount={activityTree.length}
                      onClearFilters={() => {
                        // Every filter back to its default. The sort is an
                        // ordering, not a filter, so it stays as the operator set it.
                        setToolbar({ ...DEFAULT_TOOLBAR, sort: toolbar.sort });
                      }}
                      onSelect={select}
                    />
                    {/* The phase gate is decided in the shared pane now (Stage C),
                        for any owed gate — not in a panel under the list. */}
                  </Box>
                ) : lens === 'tasks' ? (
                  tasksContent
                ) : (
                  // Honest placeholder — NOT sample rows. GRAPH is the Stage-D
                  // layer-stack projection.
                  <LensComingLater lens={lens} stage="Stage D" />
                )
              }
              detail={detailPane}
              expandToCurrentPhase={expandToCurrentPhaseControl(visibleActivityTree, owedMarks)}
              kindOptions={kindOptions}
              layerOptions={layerOptions}
              lens={lens}
              tasksOwed={tasksOwed}
              tasksUnchecked={owedWork.unchecked.pending.length + owedWork.unchecked.errored.length}
              toolbar={toolbar}
              onExpandToCurrentPhase={() => {
                setExpandToPhaseSignal((n) => n + 1);
              }}
              onLens={setLens}
              onToolbar={setToolbar}
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
    // pt: 3 — the top spacing the scroller no longer carries (it is pt: 0 so the
    // sticky toolbar sticks flush to its edge; see the scroller above).
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, pt: 3, mb: 2 }}>
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
