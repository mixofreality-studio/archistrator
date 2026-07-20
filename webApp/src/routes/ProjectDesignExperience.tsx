/**
 * The full-screen Project Design experience (`/project/$projectId/design/project`)
 * — the Phase-2 TWIN of SystemDesignScreen. It reuses the SAME ExperienceChrome
 * shell and the SAME session-state / gate loop pattern, wired to the REAL Phase-2
 * backend (projectDesignManager) via api/projectDesign + the Phase-2 hooks.
 *
 * The Phase-2 progression has nine spine steps: the eight DRAFTABLE artifacts
 * (planningAssumptions … riskModel), co-authored exactly like Phase-1 (request a
 * draft → ProjectArtifactRenderer of the typed candidate → GatePanel approve /
 * send back / withdraw), then the assembled SDP review:
 *
 *   • the eight draftable steps reuse the draft/redraft/refused/awaitingReview loop.
 *   • the `network` step joins the committed activity-list slot so its CPM graph
 *     can derive floats / the critical path over (ActivityList × Network).
 *   • the `sdpReview` step is ASSEMBLED (requestSDPCommit), not drafted: an
 *     "Assemble SDP review" CTA kicks the spine workflow; once awaitingReview the
 *     SdpReviewView renders the options + curves + the decision gate
 *     (submitSDPDecision commit <optionId> / rejectAll <feedback>); once committed
 *     an "Advance to construction" affordance calls advanceToConstruction.
 */
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Alert from '@mui/material/Alert';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';
import RocketLaunchIcon from '@mui/icons-material/RocketLaunch';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { ApiError } from '../contracts/errors';
import { PHASE2_ORDER } from '../contracts/methodMetadata';
import { METHOD_METADATA } from '../contracts/methodMetadata';
import { slotStageFromOrdinal } from '../contracts/adapters';
import type {
  ProjectArtifactKind,
  ProjectArtifactModelEnvelope,
  ProjectPhaseAdvanceResponse,
  ProjectSessionStateView,
  ProjectState,
  Finding,
  ReviewCommentView,
} from '../contracts/types';

import { useProject } from '../hooks/useProject';
import { isSessionAbsent } from '../hooks/sessionPolling';
import { useProjectSessionState } from '../hooks/useProjectSessionState';
import {
  useRequestProjectArtifactDraft,
  useSubmitProjectReviewDecision,
  useSetProjectReviewCommentStatus,
  useRequestSDPCommit,
  useSubmitSDPDecision,
  useAdvanceToConstruction,
  useAcknowledgeProjectStaleBasis,
} from '../hooks/useProjectDesignMutations';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { gateDecisionErrorMessage } from '../components/design/gateFaultLogic';
import { SlimSpine, type SpineStep } from '../components/design/SlimSpine';
import { DesignExperienceSkeleton, SkeletonContentCard } from '../components/design/DesignSkeleton';
import { GeneratingScene } from '../components/design/GeneratingScene';
import { DraftFailedPanel } from '../components/design/DraftFailedPanel';
import { GatePanel } from '../components/design/GatePanel';
import { ApproveFaultBanner } from '../components/design/SystemDesignView';
import { ChatRail } from '../components/design/ChatRail';
import { CommittedArtifactPanel } from '../components/design/CommittedArtifactPanel';
import { StaleBasisHeaderChip } from '../components/design/StaleBasisChip';
import { StageChip } from '../components/StageChip';
import { ProjectArtifactRenderer } from '../components/project/ProjectArtifactRenderer';
import { CommentProvider, useComments } from '../components/comments/CommentContext';

import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const projectRouteApi = getRouteApi('/project/$projectId/design/project');

const PHASE2_KINDS = PHASE2_ORDER as readonly ProjectArtifactKind[];

/** Build the Phase-2 spine from the project head-state slots: committed / current / locked. */
function buildSpine(project: ProjectState | undefined): SpineStep[] {
  const committed = new Set(
    (project?.slots ?? [])
      .filter((s) => slotStageFromOrdinal(s.stage) === 'committed')
      .map((s) => s.kind)
  );
  const stale = new Set(
    (project?.slots ?? []).filter((s) => s.staleBasis === true).map((s) => s.kind)
  );
  let priorCommitted = true;
  return PHASE2_KINDS.map((kind) => {
    const isCommitted = committed.has(kind);
    const locked = !isCommitted && !priorCommitted;
    priorCommitted = isCommitted;
    return {
      kind,
      title: METHOD_METADATA[kind].title,
      committed: isCommitted,
      locked,
      stale: stale.has(kind),
    };
  });
}

/** The committed activity-list slot's typed envelope, for the network CPM derivation. */
function committedActivityEnvelope(
  project: ProjectState | undefined
): ProjectArtifactModelEnvelope | undefined {
  const slot = (project?.slots ?? []).find((s) => s.kind === 'activityList');
  if (slot === undefined || slotStageFromOrdinal(slot.stage) !== 'committed') return undefined;
  return slot.model as unknown as ProjectArtifactModelEnvelope;
}

/** The committed planning-assumptions slot's typed envelope — used by solution
 * views as a display fallback for the shared calendarDaysPerWeek. */
function committedPlanningAssumptionsEnvelope(
  project: ProjectState | undefined
): ProjectArtifactModelEnvelope | undefined {
  const slot = (project?.slots ?? []).find((s) => s.kind === 'planningAssumptions');
  if (slot === undefined || slotStageFromOrdinal(slot.stage) !== 'committed') return undefined;
  return slot.model as unknown as ProjectArtifactModelEnvelope;
}

export function ProjectDesignScreen(): ReactNode {
  const { projectId } = projectRouteApi.useParams();
  return (
    <CommentProvider>
      <ProjectDesignBody projectId={projectId} />
    </CommentProvider>
  );
}

function ProjectDesignBody({ projectId }: { projectId: string }): ReactNode {
  const navigate = useNavigate();
  const t = useTokens();
  const { comments, reset, toWire, freeformNotes, requestId, setAnchor, setActiveKey } =
    useComments();

  const { data: project, isLoading: projectLoading } = useProject(projectId);
  const spine = useMemo(() => buildSpine(project), [project]);
  const activityEnvelope = useMemo(() => committedActivityEnvelope(project), [project]);
  const planningAssumptionsEnvelope = useMemo(
    () => committedPlanningAssumptionsEnvelope(project),
    [project]
  );

  const firstOpen = spine.findIndex((s) => !s.committed);
  const [activeIndex, setActiveIndex] = useState(firstOpen < 0 ? spine.length - 1 : firstOpen);
  const safeIndex = Math.min(activeIndex, PHASE2_KINDS.length - 1);
  const activeKind: ProjectArtifactKind = PHASE2_KINDS[safeIndex] ?? 'planningAssumptions';
  const isSdpStep = activeKind === 'sdpReview';

  // Disarm any pending anchor when the active artifact changes, so an anchor
  // armed on one step never bleeds onto the next (it would attach a comment to a
  // stale, unrelated location). Mirrors SystemDesignBody.
  useEffect(() => {
    setAnchor(null);
  }, [activeKind, setAnchor]);

  // Bind the pending-comment accumulator to this (project, kind) localStorage slot
  // so unsent notes survive a reload and swap when the architect changes steps.
  // Deferred until the project loads so the bind carries the head-state Version —
  // the incarnation stamp that invalidates drafts persisted by a previous
  // incarnation of the same ProjectID (F-QA2-5; see pendingCommentsStore.ts).
  const projectVersion = project?.version;
  useEffect(() => {
    if (projectVersion === undefined) return;
    setActiveKey(`${projectId}:${activeKind}`, projectVersion);
  }, [projectId, activeKind, projectVersion, setActiveKey]);

  // Chat rail open-state mirrors the Phase-1 derivation (newer anchor re-opens it).
  const [closedAt, setClosedAt] = useState<number | null>(null);
  const chatOpen = closedAt === null || requestId > closedAt;
  const setChatOpen = (open: boolean): void => {
    setClosedAt(open ? null : requestId);
  };

  const session = useProjectSessionState(projectId, activeKind, true);
  const requestDraft = useRequestProjectArtifactDraft(projectId);
  const submitReview = useSubmitProjectReviewDecision(projectId);
  const setCommentStatus = useSetProjectReviewCommentStatus(projectId);
  const assembleSdp = useRequestSDPCommit(projectId);
  const submitSdp = useSubmitSDPDecision(projectId);
  const advance = useAdvanceToConstruction(projectId);
  const acknowledgeStale = useAcknowledgeProjectStaleBasis(projectId);

  // Graceful FailedPrecondition surface: an approve that races an open thread
  // entry fails; we refetch the thread and name the message rather than wedge.
  // Any failed gate decision (approve / send back / withdraw) names its error here.
  const [gateError, setGateError] = useState<string | undefined>(undefined);

  // QA 2026-07-19: absence is only authoritative when NO session view is cached
  // (see isSessionAbsent) — a 404 refetch while a view is held must not reset the wizard.
  const sessionMissing = isSessionAbsent(session.data !== undefined, session.error);
  const view = session.data?.view;
  const stage = session.data?.stage;
  const hasDraft = view?.draft.model !== undefined;
  const findings: Finding[] = view?.findings ?? [];
  const reviewThread: ReviewCommentView[] = view?.reviewThread ?? [];
  const openCommentCount = reviewThread.filter((c) => c.status === 'open').length;
  const generating = stage === 'drafting' || stage === 'redrafting' || stage === 'assemblingSdp';
  // Terminal failure (anti-wedge): inline worker `refused` OR the async design job
  // landed in `draftFailed`. Both surface the DraftFailedPanel; draftFailed uses the
  // CI-job framing and adds a Withdraw exit alongside Retry.
  const asyncFailed = stage === 'draftFailed';
  const draftFailed = stage === 'refused' || asyncFailed;
  const committed = spine[safeIndex]?.committed === true;
  const failureReason = view?.failureReason;
  // Committed slot from head-state: its envelope is the read-only fallback when
  // there is no co-author session (sessionMissing) but the slot is committed; its
  // revisions / staleBasis drive the committed-panel header.
  const committedSlot = project?.slots.find((s) => s.kind === activeKind);
  const committedEnvelope = committedSlot?.model as unknown as
    | ProjectArtifactModelEnvelope
    | undefined;
  const committedRevisions = committedSlot?.revisions;
  const committedStale = committedSlot?.staleBasis === true;

  const selectStep = (i: number): void => {
    // Clear any held gate error so a prior step's failed decision never bleeds
    // onto the next step's gate (F79).
    setGateError(undefined);
    setActiveIndex(i);
  };

  const beginDraft = (): void => {
    if (isSdpStep) {
      assembleSdp.mutate(undefined);
      return;
    }
    requestDraft.mutate({ kind: activeKind });
  };

  const retryDraft = (): void => {
    if (isSdpStep) {
      assembleSdp.mutate(undefined);
      return;
    }
    requestDraft.mutate({ kind: activeKind });
  };

  // Amend a committed artifact: the same RequestProjectArtifactDraft mutation
  // carrying the composed rationale as feedback. The server opens an -amend-N
  // session seeded into the review ledger; invalidation drops sessionMissing and
  // the existing poll drives the generating/review loop from here.
  const amend = (feedback: string): void => {
    requestDraft.mutate({ kind: activeKind, feedback });
  };

  // Seed rationale for a reconcile-via-amendment fired from the header stale chip.
  const reconcileRationale = 'Reconcile with amended upstream basis.';

  // The second, non-blocking way out of a stale basis: mark it reviewed —
  // unaffected, clearing StaleBasis with an audit note and no redraft. Mirrors
  // onAcknowledgeStale in the Phase-1 DesignExperience.
  const onAcknowledgeStale = (note: string): void => {
    acknowledgeStale.mutate({ kind: activeKind, note });
  };

  // F-GTD-12: while this artifact's own co-author session is LIVE (an amendment in
  // flight — a committed slot can only host an amendment), the ack would commit to
  // main and merge-conflict the amendment's review PR. The server refuses it; gate
  // the popover action too so the refusal is explained instead of discovered.
  const sessionLive =
    stage === 'drafting' ||
    stage === 'assemblingSdp' ||
    stage === 'awaitingReview' ||
    stage === 'redrafting' ||
    stage === 'draftFailed';
  const ackDisabledReason = sessionLive
    ? 'An amendment is already in flight for this artifact — reconcile rides it. Approve or withdraw the amendment first.'
    : undefined;

  const approve = (): void => {
    setGateError(undefined);
    submitReview.mutate(
      { kind: activeKind, decision: 'approve' },
      {
        onSuccess: () => {
          reset();
          setActiveIndex(Math.min(safeIndex + 1, PHASE2_KINDS.length - 1));
        },
        onError: (err) => {
          // Precise message for a definite refusal, cause-neutral copy for an
          // indeterminate transport fault (F-QA2-47).
          setGateError(gateDecisionErrorMessage(err));
          void session.refetch();
        },
      }
    );
  };

  const sendBack = (): void => {
    setGateError(undefined);
    const wireComments = toWire();
    const notes = freeformNotes();
    const feedback = notes.length > 0 ? notes : wireComments.map((c) => c.text).join('\n');
    submitReview.mutate(
      { kind: activeKind, decision: 'reject', feedback, comments: wireComments },
      {
        onSuccess: () => {
          reset();
        },
        onError: (err) => {
          // A failed send-back must not be invisible (F79): keep the accumulated
          // notes (no reset), stay on the gate, and name the error inline — with
          // the cause-neutral copy for an indeterminate transport fault
          // (F-QA2-47). Refetch so a decision whose response was lost after the
          // signal was delivered still renders the server's actual stage.
          setGateError(gateDecisionErrorMessage(err));
          void session.refetch();
        },
      }
    );
  };

  const waiveComment = (commentID: string): void => {
    setCommentStatus.mutate({ kind: activeKind, commentID, status: 'waived' });
  };

  const reopenComment = (commentID: string): void => {
    setCommentStatus.mutate({ kind: activeKind, commentID, status: 'open' });
  };

  const withdraw = (): void => {
    setGateError(undefined);
    submitReview.mutate(
      { kind: activeKind, decision: 'withdraw' },
      {
        onSuccess: () => {
          reset();
        },
        onError: (err) => {
          // A failed withdraw stays on the gate with the error named inline (F79);
          // cause-neutral for an indeterminate fault (F-QA2-47) + truth refetch.
          setGateError(gateDecisionErrorMessage(err));
          void session.refetch();
        },
      }
    );
  };

  const sdpCommit = (optionId: string): void => {
    submitSdp.mutate({ decision: 'commit', detail: { optionId } });
  };

  const sdpRejectAll = (feedback: string): void => {
    submitSdp.mutate(
      { decision: 'rejectAll', detail: { feedback } },
      {
        onSuccess: () => {
          reset();
        },
      }
    );
  };

  const meta = METHOD_METADATA[activeKind];
  const decisionPending = submitReview.isPending;

  // While the project head-state is in flight we cannot yet know any step's
  // committed/locked status, the active artifact, or its stage — render the themed
  // skeleton rather than chrome that guesses (a "NOT DRAFTED" chip / step-1 rail)
  // and then contradicts itself once the data lands.
  if (projectLoading) {
    return (
      <DesignExperienceSkeleton
        phaseNum={2}
        phaseTitle="Project Design"
        steps={PHASE2_KINDS.length}
        onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      />
    );
  }

  return (
    <ExperienceChrome
      chat={
        chatOpen ? (
          <ChatRail
            statusPending={setCommentStatus.isPending}
            thread={reviewThread}
            onCollapse={() => {
              setChatOpen(false);
            }}
            onReopen={reopenComment}
            onWaive={waiveComment}
          />
        ) : undefined
      }
      chatOpen={chatOpen}
      phaseNum={2}
      phaseTitle="Project Design"
      projectName={project?.name}
      spine={<SlimSpine activeIndex={safeIndex} steps={spine} onSelect={selectStep} />}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      onOpenChat={() => {
        setChatOpen(true);
      }}
    >
      <Box sx={{ flexGrow: 1, minWidth: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
        {/* artifact header */}
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2 }}>
          <Box sx={{ minWidth: 0 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
              <Typography component="h1" sx={{ color: t.ink }} variant="h4">
                {meta.title}
              </Typography>
              <StageChip
                stage={
                  // An amendment awaiting review must read AWAITING YOU, not COMMITTED —
                  // the body below is the DRAFT revision, and badging it committed made a
                  // reviewer approve a rev-4 data-loss draft blind (F-GTD-9/F-GTD-10).
                  stage === 'awaitingReview' ? 'awaitingReview' : committed ? 'committed' : 'empty'
                }
              />
              {/* Staleness moved off the full-width amber banner into a compact
                  header chip + popover (parity with the System Design shell). */}
              {committed && committedStale ? (
                <StaleBasisHeaderChip
                  ackDisabledReason={ackDisabledReason}
                  ackError={acknowledgeStale.error?.message}
                  acknowledgePending={acknowledgeStale.isPending}
                  cause={committedSlot.staleCause}
                  onAcknowledge={onAcknowledgeStale}
                  onReconcile={() => {
                    amend(reconcileRationale);
                  }}
                />
              ) : null}
            </Box>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>
              {meta.file} · step {safeIndex + 1} of {PHASE2_KINDS.length}
            </Typography>
          </Box>
          <Box sx={{ flexGrow: 1 }} />
          <Chip
            label="architect"
            size="small"
            sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            variant="outlined"
          />
        </Box>

        <ProjectStepBody
          activeKind={activeKind}
          activityEnvelope={activityEnvelope}
          advancePending={advance.isPending}
          advanceResult={advance.data}
          advanceStaleError={
            advance.error instanceof ApiError && advance.error.code === 'failed_precondition'
              ? advance.error.message
              : undefined
          }
          amendPending={requestDraft.isPending}
          asyncFailed={asyncFailed}
          beginPending={requestDraft.isPending || assembleSdp.isPending}
          blurb={meta.blurb}
          commentCount={comments.length}
          committed={committed}
          committedEnvelope={committedEnvelope}
          committedRevisions={committedRevisions}
          decisionPending={decisionPending}
          draftFailed={draftFailed}
          failureReason={failureReason}
          findings={findings}
          gateError={gateError}
          generating={generating}
          hasDraft={hasDraft}
          isSdpStep={isSdpStep}
          loading={session.isLoading}
          openCommentCount={openCommentCount}
          planningAssumptionsEnvelope={planningAssumptionsEnvelope}
          retryPending={requestDraft.isPending || assembleSdp.isPending}
          sdpPending={submitSdp.isPending}
          sessionMissing={sessionMissing}
          stage={stage}
          t={t}
          title={meta.title}
          view={view}
          withdrawPending={decisionPending}
          onAdvance={() => {
            advance.mutate(false);
          }}
          onAdvanceAnyway={() => {
            advance.mutate(true);
          }}
          onAmend={amend}
          onApprove={approve}
          onBegin={beginDraft}
          onRetry={retryDraft}
          onSdpCommit={sdpCommit}
          onSdpRejectAll={sdpRejectAll}
          onSendBack={sendBack}
          onWithdraw={withdraw}
        />
      </Box>
    </ExperienceChrome>
  );
}

function ProjectStepBody({
  t,
  activeKind,
  isSdpStep,
  loading,
  generating,
  draftFailed,
  asyncFailed,
  committed,
  committedEnvelope,
  committedRevisions,
  failureReason,
  hasDraft,
  sessionMissing,
  stage,
  title,
  blurb,
  view,
  activityEnvelope,
  planningAssumptionsEnvelope,
  findings,
  commentCount,
  openCommentCount,
  gateError,
  decisionPending,
  beginPending,
  retryPending,
  withdrawPending,
  amendPending,
  sdpPending,
  advancePending,
  advanceResult,
  advanceStaleError,
  onBegin,
  onRetry,
  onApprove,
  onSendBack,
  onWithdraw,
  onAmend,
  onSdpCommit,
  onSdpRejectAll,
  onAdvance,
  onAdvanceAnyway,
}: {
  t: Tokens;
  activeKind: ProjectArtifactKind;
  isSdpStep: boolean;
  loading: boolean;
  generating: boolean;
  draftFailed: boolean;
  asyncFailed: boolean;
  committed: boolean;
  committedEnvelope: ProjectArtifactModelEnvelope | undefined;
  committedRevisions: number | undefined;
  failureReason: string | undefined;
  hasDraft: boolean;
  sessionMissing: boolean;
  stage: string | undefined;
  title: string;
  blurb: string;
  view: ProjectSessionStateView | undefined;
  activityEnvelope: ProjectArtifactModelEnvelope | undefined;
  planningAssumptionsEnvelope: ProjectArtifactModelEnvelope | undefined;
  findings: Finding[];
  commentCount: number;
  openCommentCount: number;
  gateError: string | undefined;
  decisionPending: boolean;
  beginPending: boolean;
  retryPending: boolean;
  withdrawPending: boolean;
  amendPending: boolean;
  sdpPending: boolean;
  advancePending: boolean;
  advanceResult: ProjectPhaseAdvanceResponse | undefined;
  advanceStaleError: string | undefined;
  onBegin: () => void;
  onRetry: () => void;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
  onAmend: (feedback: string) => void;
  onSdpCommit: (optionId: string) => void;
  onSdpRejectAll: (feedback: string) => void;
  onAdvance: () => void;
  onAdvanceAnyway: () => void;
}): ReactNode {
  if (draftFailed) {
    return (
      <DraftFailedPanel
        artifact={title}
        async={asyncFailed}
        pending={retryPending}
        reason={failureReason}
        withdrawPending={withdrawPending}
        onRetry={onRetry}
        onWithdraw={asyncFailed ? onWithdraw : undefined}
      />
    );
  }
  if (generating) {
    // A committed slot that is generating is an amendment-in-flight: frame it so the
    // committed header + this scene read honestly (the committed revision stays current).
    return (
      <GeneratingScene
        activeRole={view?.activeRole}
        activeStep={view?.activeStep}
        amendingRevision={committed ? (committedRevisions ?? 0) : undefined}
        artifact={title}
        phrase={METHOD_METADATA[activeKind].phrase}
        round={view?.round}
      />
    );
  }
  // The project head-state has resolved by now (the screen renders the full-screen
  // skeleton while it is in flight), so the surrounding header/chip/spine are already
  // truthful. Only the co-author session is still loading — sketch the content card
  // instead of a bare spinner so it stays consistent with the design system.
  if (loading && view === undefined) {
    return <SkeletonContentCard t={t} />;
  }

  // The SDP step, once committed: render the full committed SDP content
  // read-only (options table, curves, recommendation) with its decision gate
  // disabled, THEN the advance-to-construction seal below it.
  if (isSdpStep && committed) {
    const sdpEnvelope = view?.draft ?? committedEnvelope;
    return (
      <>
        <Box sx={{ mb: 3 }}>
          <ProjectArtifactRenderer
            readOnly
            envelope={sdpEnvelope}
            kind={activeKind}
            sdpPending={false}
            onSdpCommit={() => {
              /* read-only: already committed */
            }}
            onSdpRejectAll={() => {
              /* read-only: already committed */
            }}
          />
        </Box>
        <AdvancePanel
          pending={advancePending}
          result={advanceResult}
          staleError={advanceStaleError}
          t={t}
          onAdvance={onAdvance}
          onAdvanceAnyway={onAdvanceAnyway}
        />
      </>
    );
  }

  // When the slot is committed and no review is in progress — either the co-author
  // session is gone (404) or it has reached its terminal 'committed' stage — render
  // the committed model read-only under the committed panel (revision meta +
  // stale-basis reconcile + Amend affordance). Without the stage==='committed' arm
  // a freshly-approved artifact loses its Amend affordance until the session ages
  // out (F-GTD-11): the architect could no longer reopen a clean committed slot.
  if ((sessionMissing || stage === 'committed') && committed && committedEnvelope !== undefined) {
    return (
      <CommittedArtifactPanel
        amendPending={amendPending}
        revisions={committedRevisions}
        onAmend={onAmend}
      >
        <ProjectArtifactRenderer
          activityEnvelope={activityEnvelope}
          envelope={committedEnvelope}
          kind={activeKind}
          networkHeight={560}
          planningAssumptionsEnvelope={planningAssumptionsEnvelope}
        />
      </CommittedArtifactPanel>
    );
  }

  if (!hasDraft || sessionMissing) {
    return (
      <Paper sx={{ p: 6, textAlign: 'center', borderStyle: 'dashed' }}>
        <AutoAwesomeIcon sx={{ fontSize: 30, color: t.accent }} />
        <Typography sx={{ fontFamily: t.mono, mt: 1, color: t.ink }}>
          {isSdpStep ? 'The SDP review is not assembled yet.' : 'No draft yet.'}
        </Typography>
        <Typography sx={{ color: t.muted, display: 'block', mb: 2 }} variant="caption">
          {blurb}
        </Typography>
        <Button
          color="primary"
          data-testid={
            isSdpStep
              ? UI_IDENTIFIERS.ProjectDesign.SDP_ASSEMBLE
              : UI_IDENTIFIERS.DesignExperience.REQUEST_DRAFT
          }
          disabled={beginPending}
          startIcon={<AutoAwesomeIcon />}
          variant="contained"
          onClick={onBegin}
        >
          {isSdpStep ? 'Assemble SDP review' : 'Request draft'}
        </Button>
      </Paper>
    );
  }

  const gateOpen = stage === 'awaitingReview';

  // The SDP review carries its OWN decision gate (commit option / reject all), so
  // it is NOT wrapped in the generic per-artifact GatePanel.
  if (isSdpStep) {
    return (
      <ProjectArtifactRenderer
        envelope={view?.draft}
        kind={activeKind}
        sdpPending={sdpPending}
        onSdpCommit={onSdpCommit}
        onSdpRejectAll={onSdpRejectAll}
      />
    );
  }

  return (
    <>
      <Box sx={{ mb: gateOpen ? 3 : 0 }}>
        <ProjectArtifactRenderer
          activityEnvelope={activityEnvelope}
          envelope={view?.draft}
          kind={activeKind}
          networkHeight={560}
          planningAssumptionsEnvelope={planningAssumptionsEnvelope}
        />
      </Box>
      {/* QA F35 / F-GTD-12b / F-QA2-41: a contained approve/merge-window fault returns the
          session to awaitingReview carrying failureReason — without this the reviewer just
          sees AWAITING YOU again and the approve looks like a silent no-op. Keyed by the
          reason so a NEW fault re-surfaces after a dismissal. */}
      {gateOpen && failureReason !== undefined ? (
        <ApproveFaultBanner key={failureReason} reason={failureReason} />
      ) : null}
      {gateOpen ? (
        <GatePanel
          commentCount={commentCount}
          findings={findings}
          gateError={gateError}
          openCommentCount={openCommentCount}
          pending={decisionPending}
          onApprove={onApprove}
          onSendBack={onSendBack}
          onWithdraw={onWithdraw}
        />
      ) : null}
    </>
  );
}

function AdvancePanel({
  t,
  pending,
  result,
  staleError,
  onAdvance,
  onAdvanceAnyway,
}: {
  t: Tokens;
  pending: boolean;
  result: ProjectPhaseAdvanceResponse | undefined;
  staleError: string | undefined;
  onAdvance: () => void;
  onAdvanceAnyway: () => void;
}): ReactNode {
  return (
    <Paper
      sx={{ p: 4, maxWidth: 720, mx: 'auto', textAlign: 'center', border: `2px solid ${t.accent}` }}
    >
      <RocketLaunchIcon sx={{ fontSize: 34, color: t.accent }} />
      <Typography sx={{ color: t.ink, mt: 1 }} variant="h5">
        SDP committed — plan of record bound
      </Typography>
      <Typography sx={{ color: t.muted, mt: 1, mb: 2, lineHeight: 1.6 }}>
        Seal Project Design and advance to Construction. A non-advanced result lists the slots still
        owed.
      </Typography>
      {result !== undefined && !result.advanced && result.missingArtifacts.length > 0 && (
        <Alert
          data-testid={UI_IDENTIFIERS.ProjectDesign.ADVANCE_RESULT}
          severity="warning"
          sx={{ textAlign: 'left', mb: 2 }}
        >
          Still owed before advancing: {result.missingArtifacts.join(', ')}.
        </Alert>
      )}
      {result?.advanced === true ? (
        <Alert
          data-testid={UI_IDENTIFIERS.ProjectDesign.ADVANCE_RESULT}
          severity="success"
          sx={{ textAlign: 'left', mb: 2 }}
        >
          Advanced to Construction — Phase 3 is unlocked.
        </Alert>
      ) : null}
      {staleError !== undefined ? (
        // F55: the seal is blocked because a committed slot is stale (a back-edge amendment
        // shifted its basis). Name the stale slots and offer an explicit "advance anyway" that
        // acknowledges and seals over them — mirroring the approve-with-pending-note confirm.
        <Alert
          action={
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.ProjectDesign.ADVANCE_ANYWAY}
              disabled={pending}
              size="small"
              onClick={onAdvanceAnyway}
            >
              Advance anyway
            </Button>
          }
          data-testid={UI_IDENTIFIERS.ProjectDesign.ADVANCE_STALE_ERROR}
          severity="error"
          sx={{ textAlign: 'left', mb: 2 }}
        >
          {staleError}
        </Alert>
      ) : null}
      <Button
        color="primary"
        data-testid={UI_IDENTIFIERS.ProjectDesign.ADVANCE_CONSTRUCTION}
        disabled={pending}
        startIcon={<RocketLaunchIcon />}
        variant="contained"
        onClick={onAdvance}
      >
        Advance to Construction
      </Button>
    </Paper>
  );
}
