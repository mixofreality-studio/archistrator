/**
 * The full-screen System Design experience (`/project/$projectId/design/system`).
 *
 * Own chrome (NOT the AppShell): an accent strip, a prominent ✕ close back to the
 * home base, the phase title, an enter transition, the SlimSpine progress rail,
 * an active-step body, the awaitingReview GatePanel, and a collapsible ChatRail
 * for anchored comments. Everything is driven from the typed head-state:
 *
 *   useProject(projectId).slots  → spine steps (committed / current / locked)
 *   useSessionState(projectId, activeKind) → the live candidate draft + findings
 *
 * Step body:
 *   • no session (404) on the FIRST step → start the workflow; if the server
 *     reports research-input missing (409 failed_precondition) show the
 *     ResearchInputPanel, then retry start.
 *   • no session on a later step / no draft → a "request draft" CTA.
 *   • stage drafting|redrafting → GeneratingScene loader.
 *   • a staged draft → ArtifactRenderer (typed candidate).
 *   • stage awaitingReview → GatePanel (Approve → auto-advance / Send back with
 *     the accumulated anchored comments / Withdraw).
 *
 * Phase-2 (`/design/project`) reuses this shell with a "coming soon" stub body.
 */
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { ApiError } from '../contracts/errors';
import { PHASE1_ARTIFACTS } from '../contracts/types';
import type {
  ArtifactKind,
  ArtifactModelEnvelope,
  Finding,
  ProjectState,
  ResearchInput,
  ReviewCommentView,
  SessionStateResponse,
} from '../contracts/types';
import { slotStageFromOrdinal } from '../contracts/adapters';
import { METHOD_METADATA } from '../contracts/methodMetadata';

import { useProject } from '../hooks/useProject';
import { useSessionState } from '../hooks/useSessionState';
import {
  useAcknowledgeStaleBasis,
  useAskQuestions,
  useRequestArtifactDraft,
  useSetReviewCommentStatus,
  useSubmitReviewDecision,
} from '../hooks/useDesignMutations';
import { useSetResearchInput, useStartSystemDesign } from '../hooks/useStartDesign';

import { ArtifactRenderer } from '../components/ArtifactRenderer';
import { ArtifactIntro } from '../components/design/ArtifactIntro';
import { StageChip } from '../components/StageChip';
import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { SlimSpine, type SpineStep } from '../components/design/SlimSpine';
import { DesignExperienceSkeleton, SkeletonContentCard } from '../components/design/DesignSkeleton';
import { GeneratingScene } from '../components/design/GeneratingScene';
import { DraftFailedPanel } from '../components/design/DraftFailedPanel';
import { GatePanel } from '../components/design/GatePanel';
import { ChatRail } from '../components/design/ChatRail';
import { CommittedArtifactPanel } from '../components/design/CommittedArtifactPanel';
import { StaleBasisBanner } from '../components/design/StaleBasisChip';
import { ResearchInputPanel } from '../components/design/ResearchInputPanel';
import { CommentProvider, useComments } from '../components/comments/CommentContext';

// Prose (markdown) artifact kinds get a paper surface in the full-screen design
// experience; diagram kinds (volatilities/system/coreUseCases/operationalConcepts)
// render on their own bordered canvases, so they stay unwrapped.
const PROSE_ARTIFACT_KINDS = new Set<string>(['mission', 'scrubbedRequirements', 'standardCheck']);

/** Seed rationale for a reconcile-via-amendment fired from the stale banner (F45). */
const RECONCILE_RATIONALE = 'Reconcile with amended upstream basis.';

function proseSurface(kind: string | undefined, node: ReactNode): ReactNode {
  return kind !== undefined && PROSE_ARTIFACT_KINDS.has(kind) ? (
    <Paper sx={{ p: { xs: 2.5, md: 4 } }}>{node}</Paper>
  ) : (
    node
  );
}

import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

export { ProjectDesignScreen } from './ProjectDesignExperience';

const systemRouteApi = getRouteApi('/project/$projectId/design/system');

/** Did this request fail because a precondition (research input) is unmet? */
function isPreconditionError(error: Error | null): boolean {
  return error instanceof ApiError && error.status === 409;
}

/** Build the spine steps from the project slots: committed / current / locked. */
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
  return PHASE1_ARTIFACTS.map((kind) => {
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

// ── System Design (Phase-1) ─────────────────────────────────────────────────

export function SystemDesignScreen(): ReactNode {
  const { projectId } = systemRouteApi.useParams();
  return (
    <CommentProvider>
      <SystemDesignBody projectId={projectId} />
    </CommentProvider>
  );
}

function SystemDesignBody({ projectId }: { projectId: string }): ReactNode {
  const navigate = useNavigate();
  const t = useTokens();
  const {
    comments,
    reset,
    toWire,
    freeformNotes,
    pendingQuestions,
    clearQuestions,
    requestId,
    setAnchor,
    setActiveKey,
  } = useComments();

  const { data: project, isLoading: projectLoading } = useProject(projectId);
  const spine = useMemo(() => buildSpine(project), [project]);

  // Default active step: first non-committed, else last.
  const firstOpen = spine.findIndex((s) => !s.committed);
  const [activeIndex, setActiveIndex] = useState(firstOpen < 0 ? spine.length - 1 : firstOpen);
  const safeIndex = Math.min(activeIndex, PHASE1_ARTIFACTS.length - 1);
  const activeKind: ArtifactKind = PHASE1_ARTIFACTS[safeIndex] ?? 'mission';

  // Disarm any pending anchor when the active artifact changes, so an anchor
  // armed on one step never bleeds onto the next (it would attach a comment to a
  // stale, unrelated location).
  useEffect(() => {
    setAnchor(null);
  }, [activeKind, setAnchor]);

  // Bind the pending-comment accumulator to this (project, kind) localStorage slot
  // so unsent notes survive a reload and swap when the architect changes steps.
  useEffect(() => {
    setActiveKey(`${projectId}:${activeKind}`);
  }, [projectId, activeKind, setActiveKey]);

  // The rail auto-opens whenever the architect arms an anchor (requestId bumps).
  // We derive open-state from (requestId, manual toggles) rather than an effect:
  // a manual collapse records the requestId it happened at; a newer anchor (a
  // higher requestId) re-opens the rail.
  const [closedAt, setClosedAt] = useState<number | null>(null);
  const chatOpen = closedAt === null || requestId > closedAt;
  const setChatOpen = (open: boolean): void => {
    setClosedAt(open ? null : requestId);
  };

  const session = useSessionState(projectId, activeKind, true);
  const requestDraft = useRequestArtifactDraft(projectId);
  const submitReview = useSubmitReviewDecision(projectId);
  const setCommentStatus = useSetReviewCommentStatus(projectId);
  const askQuestionsMut = useAskQuestions(projectId);
  const acknowledgeStale = useAcknowledgeStaleBasis(projectId);
  const startDesign = useStartSystemDesign(projectId);
  const setResearch = useSetResearchInput(projectId);

  // Graceful FailedPrecondition surface: an approve that races an open thread
  // entry fails; we refetch the thread and name the message rather than wedge.
  const [gateError, setGateError] = useState<string | undefined>(undefined);

  const sessionMissing = session.error instanceof ApiError && session.error.status === 404;
  const view = session.data?.view;
  const stage = session.data?.stage;
  // Committed slot from head-state: its envelope is the read-only fallback when
  // there is no co-author session (sessionMissing) but the slot is committed; its
  // revisions / staleBasis drive the committed-panel header.
  const committedSlot = project?.slots.find((s) => s.kind === activeKind);
  const committedEnvelope = committedSlot?.model;
  const committedRevisions = committedSlot?.revisions;
  const committedStale = committedSlot?.staleBasis === true;
  const hasDraft = view?.draft.model !== undefined;
  const findings: Finding[] = view?.findings ?? [];
  const reviewThread: ReviewCommentView[] = view?.reviewThread ?? [];
  const openCommentCount = reviewThread.filter((c) => c.status === 'open').length;
  const isFirstStep = safeIndex === 0;
  const needsResearch = isFirstStep && isPreconditionError(startDesign.error);
  const generating = stage === 'drafting' || stage === 'redrafting';
  // Terminal failure: either the inline worker `refused` (out of credits /
  // unavailable) OR the dispatched async design job landed in `draftFailed` (the CI
  // Action failed or a required check went red). Both surface the DraftFailedPanel
  // (anti-wedge) instead of a perpetual generating spinner; draftFailed uses the
  // async-CI framing and adds a Withdraw exit alongside Retry.
  const asyncFailed = stage === 'draftFailed';
  const draftFailed = stage === 'refused' || asyncFailed;
  const failureReason = view?.failureReason;
  const failureRunUrl = view?.failureRunUrl;

  const selectStep = (i: number): void => {
    setActiveIndex(i);
  };

  const beginOrDraft = (): void => {
    if (isFirstStep && sessionMissing) {
      startDesign.mutate(undefined);
      return;
    }
    requestDraft.mutate({ kind: activeKind });
  };

  // Retry after a terminal Refused: re-enter drafting on the same live session.
  // The mutation invalidates the session query, which refetches once and — now
  // that the stage has left the terminal Refused — re-enables the 2s poll.
  const retryDraft = (): void => {
    requestDraft.mutate({ kind: activeKind });
  };

  // Amend a committed artifact: the same RequestArtifactDraft mutation, carrying
  // the composed rationale as feedback. The server (Ruling B) opens an -amend-N
  // session seeded into the review ledger; invalidation drops sessionMissing and
  // the existing poll drives the generating/review loop from here.
  const amend = (feedback: string): void => {
    requestDraft.mutate({ kind: activeKind, feedback });
  };

  const onAcknowledgeStale = (note: string): void => {
    acknowledgeStale.mutate({ kind: activeKind, note });
  };

  const submitResearch = (research: ResearchInput): void => {
    setResearch.mutate(research, {
      onSuccess: () => {
        startDesign.mutate(undefined);
      },
    });
  };

  const approve = (): void => {
    setGateError(undefined);
    submitReview.mutate(
      { kind: activeKind, decision: 'approve' },
      {
        onSuccess: () => {
          reset();
          // Auto-advance to the next non-committed step.
          const next = Math.min(safeIndex + 1, PHASE1_ARTIFACTS.length - 1);
          setActiveIndex(next);
        },
        onError: (err) => {
          // A FailedPrecondition (open thread entries) or any other approve fault:
          // surface the message and refetch the thread so the gate reflects truth.
          setGateError(err.message);
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

  // "Ask" — submit ONLY the pending questions, grouped by addressee, without a redraft.
  // Change-requests stay pending for a later Send back; a successful Ask clears the
  // questions it sent (they now live on the server review thread).
  const askQuestions = (): void => {
    const pending = pendingQuestions();
    if (pending.length === 0) return;
    const byAddressee = new Map<'pm' | 'architect', typeof pending>();
    for (const q of pending) {
      const key: 'pm' | 'architect' = q.addressee === 'architect' ? 'architect' : 'pm';
      byAddressee.set(key, [...(byAddressee.get(key) ?? []), q]);
    }
    for (const [addressee, group] of byAddressee) {
      askQuestionsMut.mutate({
        kind: activeKind,
        addressee,
        questions: group.map((q) => ({
          jsonPath: q.jsonPath,
          text: q.text,
          anchorText: q.anchorText,
        })),
      });
    }
    clearQuestions();
  };

  const sendBack = (): void => {
    const wireComments = toWire();
    const notes = freeformNotes();
    // The Manager requires non-empty reject feedback; when the architect only
    // anchored comments (no free-form note), synthesize the notes from them so the
    // redraft always carries actionable guidance and the reject validates.
    const feedback = notes.length > 0 ? notes : wireComments.map((c) => c.text).join('\n');
    submitReview.mutate(
      { kind: activeKind, decision: 'reject', detail: { feedback, comments: wireComments } },
      {
        onSuccess: () => {
          reset();
        },
      }
    );
  };

  const withdraw = (): void => {
    submitReview.mutate(
      { kind: activeKind, decision: 'withdraw' },
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
        phaseNum={1}
        phaseTitle="System Design"
        steps={PHASE1_ARTIFACTS.length}
        onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      />
    );
  }

  return (
    <ExperienceChrome
      chat={
        chatOpen ? (
          <ChatRail
            askPending={askQuestionsMut.isPending}
            statusPending={setCommentStatus.isPending}
            thread={reviewThread}
            onAsk={askQuestions}
            onCollapse={() => {
              setChatOpen(false);
            }}
            onReopen={reopenComment}
            onWaive={waiveComment}
          />
        ) : undefined
      }
      chatOpen={chatOpen}
      phaseNum={1}
      phaseTitle="System Design"
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
          <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
              <Typography component="h1" sx={{ color: t.ink }} variant="h4">
                {meta.title}
              </Typography>
              <StageChip
                stage={
                  spine[safeIndex]?.committed === true
                    ? 'committed'
                    : stage === 'awaitingReview'
                      ? 'awaitingReview'
                      : 'empty'
                }
              />
            </Box>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>
              {meta.file} · step {safeIndex + 1} of {PHASE1_ARTIFACTS.length}
            </Typography>
          </Box>
          <Box sx={{ flexGrow: 1 }} />
          <Chip
            label="architect"
            size="small"
            sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            variant="outlined"
          />
          {meta.hasPmCritic ? (
            <Chip
              label="pm"
              size="small"
              sx={{ bgcolor: t.chatPmBg, color: t.chatPmFg }}
              variant="outlined"
            />
          ) : null}
        </Box>

        {/* body */}
        <StepBody
          acknowledgePending={acknowledgeStale.isPending}
          activeKind={activeKind}
          amendPending={requestDraft.isPending}
          asyncFailed={asyncFailed}
          beginPending={startDesign.isPending || requestDraft.isPending}
          blurb={meta.blurb}
          commentCount={comments.length}
          committed={spine[safeIndex]?.committed === true}
          committedEnvelope={committedEnvelope}
          committedRevisions={committedRevisions}
          committedStale={committedStale}
          decisionPending={decisionPending}
          draftFailed={draftFailed}
          failureReason={failureReason}
          failureRunUrl={failureRunUrl}
          findings={findings}
          gateError={gateError}
          generating={generating}
          hasDraft={hasDraft}
          loading={session.isLoading}
          needsResearch={needsResearch}
          openCommentCount={openCommentCount}
          researchPending={setResearch.isPending || startDesign.isPending}
          retryPending={requestDraft.isPending}
          sessionMissing={sessionMissing}
          stage={stage}
          t={t}
          title={meta.title}
          view={view}
          withdrawPending={decisionPending}
          onAcknowledgeStale={onAcknowledgeStale}
          onAmend={amend}
          onApprove={approve}
          onBegin={beginOrDraft}
          onRetry={retryDraft}
          onSendBack={sendBack}
          onSubmitResearch={submitResearch}
          onWithdraw={withdraw}
        />
      </Box>
    </ExperienceChrome>
  );
}

function StepBody({
  t,
  activeKind,
  committed,
  committedEnvelope,
  committedRevisions,
  committedStale,
  loading,
  generating,
  needsResearch,
  draftFailed,
  asyncFailed,
  failureReason,
  failureRunUrl,
  hasDraft,
  sessionMissing,
  stage,
  title,
  blurb,
  view,
  findings,
  commentCount,
  openCommentCount,
  gateError,
  decisionPending,
  beginPending,
  researchPending,
  retryPending,
  withdrawPending,
  amendPending,
  acknowledgePending,
  onBegin,
  onRetry,
  onSubmitResearch,
  onApprove,
  onSendBack,
  onWithdraw,
  onAmend,
  onAcknowledgeStale,
}: {
  t: Tokens;
  activeKind: ArtifactKind;
  committed: boolean;
  committedEnvelope: ArtifactModelEnvelope | undefined;
  committedRevisions: number | undefined;
  committedStale: boolean;
  loading: boolean;
  generating: boolean;
  needsResearch: boolean;
  draftFailed: boolean;
  asyncFailed: boolean;
  failureReason: string | undefined;
  failureRunUrl: string | undefined;
  hasDraft: boolean;
  sessionMissing: boolean;
  stage: string | undefined;
  title: string;
  blurb: string;
  view: SessionStateResponse['view'] | undefined;
  findings: Finding[];
  commentCount: number;
  openCommentCount: number;
  gateError: string | undefined;
  decisionPending: boolean;
  beginPending: boolean;
  researchPending: boolean;
  retryPending: boolean;
  withdrawPending: boolean;
  amendPending: boolean;
  acknowledgePending: boolean;
  onBegin: () => void;
  onRetry: () => void;
  onSubmitResearch: (research: ResearchInput) => void;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
  onAmend: (feedback: string) => void;
  onAcknowledgeStale: (note: string) => void;
}): ReactNode {
  if (needsResearch) {
    return <ResearchInputPanel pending={researchPending} onSubmit={onSubmitResearch} />;
  }
  // Terminal failure takes precedence over the generating loader so a failed
  // session surfaces an error + Retry/Withdraw instead of an infinite generating
  // screen. The async `draftFailed` variant frames it as a CI-job failure and
  // offers Withdraw alongside Retry.
  if (draftFailed) {
    return (
      <DraftFailedPanel
        artifact={title}
        async={asyncFailed}
        pending={retryPending}
        reason={failureReason}
        runUrl={failureRunUrl}
        withdrawPending={withdrawPending}
        onRetry={onRetry}
        onWithdraw={asyncFailed ? onWithdraw : undefined}
      />
    );
  }
  if (generating) {
    // A committed slot that is generating is an amendment-in-flight: frame it so the
    // committed header + this scene read honestly (the committed revision stays current).
    const scene = (
      <GeneratingScene
        amendingRevision={committed ? (committedRevisions ?? 0) : undefined}
        artifact={title}
      />
    );
    // Reviewers must be able to READ the committed revision while its amendment
    // drafts — don't blank the pane. Render the committed model read-only (dimmed,
    // labeled "current") above the generating scene, in a disabled comment context
    // so it carries zero comment affordances.
    if (committed && committedEnvelope !== undefined) {
      const revN = committedRevisions ?? 0;
      return (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <Box>
            <Box
              data-testid={UI_IDENTIFIERS.DesignExperience.AMEND_CURRENT_LABEL}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                px: 2,
                py: 1,
                bgcolor: t.committedBg,
                border: `1.5px solid ${t.line}`,
                borderBottom: 'none',
              }}
            >
              <Typography
                sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, letterSpacing: '0.08em', color: t.committedFg }}
              >
                COMMITTED{revN > 1 ? ` · revision ${String(revN)}` : ''} — current
              </Typography>
              <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
                stays live until the amendment is approved
              </Typography>
            </Box>
            <Box
              aria-hidden
              sx={{ opacity: 0.6, pointerEvents: 'none', border: `1.5px solid ${t.line}`, p: 1 }}
            >
              <CommentProvider enabled={false}>
                {proseSurface(
                  committedEnvelope.kind,
                  <ArtifactRenderer envelope={committedEnvelope} height={480} title={title} />
                )}
              </CommentProvider>
            </Box>
          </Box>
          {scene}
        </Box>
      );
    }
    return scene;
  }
  // The project head-state has resolved by now (the screen renders the full-screen
  // skeleton while it is in flight), so the surrounding header/chip/spine are already
  // truthful. Only the co-author session is still loading — sketch the content card
  // instead of a bare spinner so it stays consistent with the design system.
  if (loading && view === undefined) {
    return <SkeletonContentCard t={t} />;
  }
  // When the session is missing (404) but the slot is committed in the project
  // head-state, render the committed model read-only under the committed panel
  // (revision meta + stale-basis reconcile + Amend affordance).
  if (sessionMissing && committed && committedEnvelope !== undefined) {
    return (
      <CommittedArtifactPanel
        acknowledgePending={acknowledgePending}
        amendPending={amendPending}
        revisions={committedRevisions}
        staleBasis={committedStale}
        onAcknowledgeStale={onAcknowledgeStale}
        onAmend={onAmend}
      >
        {proseSurface(
          committedEnvelope.kind,
          <ArtifactRenderer envelope={committedEnvelope} height={620} title={title} />
        )}
      </CommittedArtifactPanel>
    );
  }

  if (!hasDraft || sessionMissing) {
    return (
      <Paper sx={{ p: 6, textAlign: 'center', borderStyle: 'dashed' }}>
        <AutoAwesomeIcon sx={{ fontSize: 30, color: t.accent }} />
        <Typography sx={{ fontFamily: t.mono, mt: 1, color: t.ink }}>No draft yet.</Typography>
        <Typography sx={{ color: t.muted, display: 'block', mb: 2 }} variant="caption">
          {blurb}
        </Typography>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.DesignExperience.REQUEST_DRAFT}
          disabled={beginPending}
          startIcon={<AutoAwesomeIcon />}
          variant="contained"
          onClick={onBegin}
        >
          Request draft
        </Button>
      </Paper>
    );
  }

  const gateOpen = stage === 'awaitingReview';
  return (
    <>
      <ArtifactIntro committed={committed} kind={activeKind} />
      {/* F45/F64: a committed artifact whose upstream basis drifted shows the stale banner in
          the pane (not just the stepper ⚠). Reconcile fires the amend directly here; "mark
          reviewed — unaffected" clears StaleBasis with an audit note, no redraft. */}
      {committed && committedStale ? (
        <Box sx={{ mb: 2 }}>
          <StaleBasisBanner
            acknowledgePending={acknowledgePending}
            onAcknowledge={onAcknowledgeStale}
            onReconcile={() => {
              onAmend(RECONCILE_RATIONALE);
            }}
          />
        </Box>
      ) : null}
      <Box sx={{ mb: gateOpen ? 3 : 0 }}>
        {proseSurface(
          view?.draft.kind ?? activeKind,
          <ArtifactRenderer envelope={view?.draft} height={620} title={title} />
        )}
      </Box>
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
