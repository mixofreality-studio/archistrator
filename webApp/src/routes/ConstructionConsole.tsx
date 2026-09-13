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
import { useProject } from '../hooks/useProject';
import { useConstructionSession } from '../hooks/useConstructionSession';
import { useConstructionSessions } from '../hooks/useConstructionSessions';
import { owedItemsFor, probeCandidatesFor } from '../components/construction/tasks/owedWork';
import { rankOwed, type RankedOwed } from '../components/construction/tasks/owedRanking';
import { emptyStateCounts, shapeFor } from '../components/construction/tasks/tasksLensCopy';
import { TasksLens } from '../components/construction/tasks/TasksLens';
import { computeActivityStatuses } from '../contracts/constructionAdapters';
import { contractForActivity } from '../contracts/serviceContracts';
import { gitFor } from '../contracts/types';
import { useBeginConstruction, useSubmitPhaseDecision } from '../hooks/useConstructionMutations';

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
  awaitingRefreshAfter,
  beginControlFor,
  dispatchOutcomeCopy,
  dispatchOutcomeFor,
  notStartedActivities,
  type DispatchOutcome,
} from '../components/construction/lens/beginControl';
import { ApiError } from '../contracts/errors';
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
import { PhaseGatePanel } from '../components/construction/PhaseGatePanel';
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
  return (
    <CommentProvider>
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
  const [cascading, setCascading] = useState(false);
  const {
    data: project,
    isLoading: projectLoading,
    dataUpdatedAt: projectReadAt,
  } = useProject(projectId, cascading ? 1500 : false);

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

  // Begin is a real dispatch, so the button only opens a confirm step that names
  // what would be started (BeginConfirmDialog). Each opening mints ONE tickID, which
  // CORRELATES the request (logs, traces, the trapped specs) — it is not what keeps
  // a second pump from starting: the server runs one pump workflow per project
  // (architect I1 ruling). The client guards here and in the dialog are UX
  // debouncing, so one press sends one request. `null` is "closed".
  const [beginTick, setBeginTick] = useState<string | null>(null);
  // A ref, not only begin.isPending: clicks delivered in one task all land before a
  // re-render could report the first as pending (pinned by the same-task triple
  // click in construction-begin-confirm.spec).
  const beginInFlightRef = useRef(false);
  // A failed dispatch is LOUD (spec §6 "Failures must be loud", fix-B review M3) —
  // and HONEST about what it knows (fix-C review). The server starts the pump before
  // it answers and can still answer 5xx after that, or the response can be dropped,
  // so only a 4xx means nothing started (dispatchOutcomeFor). On an unknown outcome
  // the console keeps polling, as it does after a success, and Begin stays off until
  // a project read newer than the failure has answered (awaitingRefreshAfter) — a
  // stale "Begin" there was a second pump one click away. `at` is when the console
  // learned of the failure; `dismissed` hides the alert without lifting that gate.
  const [beginFailure, setBeginFailure] = useState<{
    outcome: DispatchOutcome;
    at: number;
    dismissed: boolean;
  } | null>(null);
  const onBegin = (tickId: string): void => {
    if (beginInFlightRef.current) return;
    beginInFlightRef.current = true;
    lastProgressAtRef.current = Date.now();
    setBeginFailure(null);
    setCascading(true);
    begin.mutate(tickId, {
      onError: (err) => {
        const outcome = dispatchOutcomeFor(
          err instanceof ApiError ? err.status : undefined,
          err.message
        );
        const at = Date.now();
        if (outcome.kind === 'rejected') {
          // Refused: nothing started, so there is nothing to wait on.
          setCascading(false);
        } else {
          // Keep polling, exactly as after a success: the list shows the pump if it
          // started. The progress window restarts from here.
          lastProgressAtRef.current = at;
        }
        setBeginFailure({ outcome, at, dismissed: false });
      },
      onSettled: () => {
        beginInFlightRef.current = false;
      },
    });
  };
  // Running only on the strength of a dispatch that SUCCEEDED: a failed one may
  // still be polled, but the button's state is then the refreshed project's.
  const beginActive = begin.isPending || (cascading && beginFailure === null);
  const awaitingRefresh = awaitingRefreshAfter(beginFailure, projectReadAt);
  const beginFailureCopy =
    beginFailure !== null && !beginFailure.dismissed
      ? dispatchOutcomeCopy(beginFailure.outcome)
      : undefined;

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
    awaitingRefresh,
  });
  const dispatchCandidates = useMemo(
    () => notStartedActivities(project?.constructionRows, titleForId),
    [project, titleForId]
  );

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
  const sessionsByActivity = useConstructionSessions(projectId, probeIds);
  const owedItems = useMemo(
    () => owedItemsFor({ rows: viewRows, sessions: sessionsByActivity, titleFor: titleForId }),
    [viewRows, sessionsByActivity, titleForId]
  );
  const tasksOwed = owedItems.length;
  const activityTree = useMemo(
    () => buildActivityTree(Object.values(viewRows ?? {}), { meta: activityMeta }),
    [viewRows, activityMeta]
  );

  // Scope/kind/layer/search/sort — every rule in one pure
  // pipeline (see activityScope.ts) so ActivityTreeView renders exactly what
  // the toolbar says and nothing this file has to keep in sync by hand.
  const visibleActivityTree = useMemo(
    () => applyToolbarToActivities(activityTree, toolbar),
    [activityTree, toolbar]
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
        episodeSlot={({ activityId, attemptId }) => (
          <ConstructionEpisodeBodyContainer
            activityId={activityId}
            attemptId={attemptId}
            projectId={projectId}
          />
        )}
        hiddenAttempts={evidenceView.hidden[selectedActivityId]}
        project={project}
        // ONLY when the selected activity IS the one at a phase gate. Another
        // activity's reviewer set rendered under this one's review body would be
        // the most direct mis-attribution available on this surface.
        // The selected activity's OWN session, when it is at a gate (Stage C) —
        // any activity, not only the one the old single-activity lookup found.
        reviewSet={
          sessionsByActivity[selectedActivityId]?.stage === 'awaitingApproval'
            ? sessionsByActivity[selectedActivityId].view.reviewSet
            : activeInConstructionId === selectedActivityId
              ? phaseGateSession?.view.reviewSet
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
      gitOf={(id) => gitFor(project, id)}
      items={visibleOwed}
      policy={project?.reviewPolicy}
      projectId={projectId}
      selection={selection}
      shapeOf={shapeOf}
      supervisionCap={project?.constructionProgress?.supervisionCap}
      totalOwed={rankedOwed.length}
      onClearFilters={() => {
        setToolbar({ ...DEFAULT_TOOLBAR, sort: toolbar.sort });
      }}
      onReview={(item) => {
        // [Review] opens the shared pane in place: on the gate TASK where the
        // profile names one (the review body), else on the activity.
        const g = item.gate;
        select(
          g?.task !== undefined && g.lifecyclePhase !== undefined
            ? { activityId: item.activityId, lifecyclePhase: g.lifecyclePhase, task: g.task }
            : { activityId: item.activityId }
        );
      }}
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
              data-outcome={beginFailure.outcome.kind}
              data-testid={UI_IDENTIFIERS.Construction.BEGIN_ERROR}
              severity="error"
              sx={{ mb: 2, fontFamily: t.mono, fontSize: 12 }}
              onClose={() => {
                setBeginFailure({ ...beginFailure, dismissed: true });
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
                ) : lens === 'tasks' ? (
                  tasksContent
                ) : (
                  // Honest placeholder — NOT sample rows. GRAPH is the Stage-D
                  // layer-stack projection.
                  <LensComingLater lens={lens} stage="Stage D" />
                )
              }
              detail={detailPane}
              expandToCurrentPhase={expandToCurrentPhaseControl(visibleActivityTree)}
              kindOptions={kindOptions}
              layerOptions={layerOptions}
              lens={lens}
              tasksOwed={tasksOwed}
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
