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
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { ApiError } from '../api/client';
import { PHASE1_ARTIFACTS } from '../api/types';
import type {
  ArtifactKind,
  ArtifactModelEnvelope,
  Finding,
  ProjectState,
  ResearchInput,
  SessionStateResponse,
} from '../api/types';
import { slotStageFromOrdinal } from '../api/adapters';
import { METHOD_METADATA } from '../constants/MethodMetadata';

import { useProject } from '../hooks/useProject';
import { useSessionState } from '../hooks/useSessionState';
import {
  useRequestArtifactDraft,
  useSubmitReviewDecision,
} from '../hooks/useDesignMutations';
import { useSetResearchInput, useStartSystemDesign } from '../hooks/useStartDesign';

import { ArtifactRenderer } from '../components/ArtifactRenderer';
import { ArtifactIntro } from '../components/design/ArtifactIntro';
import { StageChip } from '../components/StageChip';
import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { SlimSpine, type SpineStep } from '../components/design/SlimSpine';
import { GeneratingScene } from '../components/design/GeneratingScene';
import { DraftFailedPanel } from '../components/design/DraftFailedPanel';
import { GatePanel } from '../components/design/GatePanel';
import { ChatRail } from '../components/design/ChatRail';
import { ResearchInputPanel } from '../components/design/ResearchInputPanel';
import {
  CommentProvider,
  useComments,
} from '../components/comments/CommentContext';

import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

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
  let priorCommitted = true;
  return PHASE1_ARTIFACTS.map((kind) => {
    const isCommitted = committed.has(kind);
    const locked = !isCommitted && !priorCommitted;
    priorCommitted = isCommitted;
    return { kind, title: METHOD_METADATA[kind].title, committed: isCommitted, locked };
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
  const { comments, reset, toWire, freeformNotes, requestId } = useComments();

  const { data: project } = useProject(projectId);
  const spine = useMemo(() => buildSpine(project), [project]);

  // Default active step: first non-committed, else last.
  const firstOpen = spine.findIndex((s) => !s.committed);
  const [activeIndex, setActiveIndex] = useState(firstOpen < 0 ? spine.length - 1 : firstOpen);
  const safeIndex = Math.min(activeIndex, PHASE1_ARTIFACTS.length - 1);
  const activeKind: ArtifactKind = PHASE1_ARTIFACTS[safeIndex] ?? 'mission';

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
  const startDesign = useStartSystemDesign(projectId);
  const setResearch = useSetResearchInput(projectId);

  const sessionMissing =
    session.error instanceof ApiError && session.error.status === 404;
  const view = session.data?.view;
  const stage = session.data?.stage;
  // Committed envelope from head-state: used as read-only fallback when there is no
  // co-author session (sessionMissing) but the slot is already committed.
  const committedEnvelope = project?.slots.find((s) => s.kind === activeKind)?.model;
  const hasDraft = view?.draft.model !== undefined;
  const findings: Finding[] = view?.findings ?? [];
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

  const submitResearch = (research: ResearchInput): void => {
    setResearch.mutate(research, {
      onSuccess: () => {
        startDesign.mutate(undefined);
      },
    });
  };

  const approve = (): void => {
    submitReview.mutate(
      { kind: activeKind, decision: 'approve' },
      {
        onSuccess: () => {
          reset();
          // Auto-advance to the next non-committed step.
          const next = Math.min(safeIndex + 1, PHASE1_ARTIFACTS.length - 1);
          setActiveIndex(next);
        },
      }
    );
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
      { onSuccess: () => { reset(); } }
    );
  };

  const withdraw = (): void => {
    submitReview.mutate({ kind: activeKind, decision: 'withdraw' }, { onSuccess: () => { reset(); } });
  };

  const meta = METHOD_METADATA[activeKind];
  const decisionPending = submitReview.isPending;

  return (
    <ExperienceChrome
      chat={chatOpen ? <ChatRail onCollapse={() => { setChatOpen(false); }} /> : undefined}
      chatOpen={chatOpen}
      phaseNum={1}
      phaseTitle="System Design"
      projectName={project?.name}
      spine={<SlimSpine activeIndex={safeIndex} steps={spine} onSelect={selectStep} />}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      onOpenChat={() => { setChatOpen(true); }}
    >
      <Box sx={{ flexGrow: 1, minWidth: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
        {/* artifact header */}
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2 }}>
          <Box>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
              <Typography sx={{ color: t.ink }} variant="h4">
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
          <Chip label="architect" size="small" sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }} variant="outlined" />
          {meta.hasPmCritic ? <Chip label="pm" size="small" sx={{ bgcolor: t.chatPmBg, color: t.chatPmFg }} variant="outlined" /> : null}
        </Box>

        {/* body */}
        <StepBody
          activeKind={activeKind}
          asyncFailed={asyncFailed}
          beginPending={startDesign.isPending || requestDraft.isPending}
          blurb={meta.blurb}
          commentCount={comments.length}
          committedEnvelope={committedEnvelope}
          committed={spine[safeIndex]?.committed === true}
          decisionPending={decisionPending}
          draftFailed={draftFailed}
          failureReason={failureReason}
          findings={findings}
          generating={generating}
          hasDraft={hasDraft}
          loading={session.isLoading}
          needsResearch={needsResearch}
          researchPending={setResearch.isPending || startDesign.isPending}
          retryPending={requestDraft.isPending}
          sessionMissing={sessionMissing}
          stage={stage}
          t={t}
          title={meta.title}
          view={view}
          withdrawPending={decisionPending}
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
  loading,
  generating,
  needsResearch,
  draftFailed,
  asyncFailed,
  failureReason,
  hasDraft,
  sessionMissing,
  stage,
  title,
  blurb,
  view,
  findings,
  commentCount,
  decisionPending,
  beginPending,
  researchPending,
  retryPending,
  withdrawPending,
  onBegin,
  onRetry,
  onSubmitResearch,
  onApprove,
  onSendBack,
  onWithdraw,
}: {
  t: Tokens;
  activeKind: ArtifactKind;
  committed: boolean;
  committedEnvelope: ArtifactModelEnvelope | undefined;
  loading: boolean;
  generating: boolean;
  needsResearch: boolean;
  draftFailed: boolean;
  asyncFailed: boolean;
  failureReason: string | undefined;
  hasDraft: boolean;
  sessionMissing: boolean;
  stage: string | undefined;
  title: string;
  blurb: string;
  view: SessionStateResponse['view'] | undefined;
  findings: Finding[];
  commentCount: number;
  decisionPending: boolean;
  beginPending: boolean;
  researchPending: boolean;
  retryPending: boolean;
  withdrawPending: boolean;
  onBegin: () => void;
  onRetry: () => void;
  onSubmitResearch: (research: ResearchInput) => void;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
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
        withdrawPending={withdrawPending}
        onRetry={onRetry}
        onWithdraw={asyncFailed ? onWithdraw : undefined}
      />
    );
  }
  if (generating) {
    return <GeneratingScene artifact={title} />;
  }
  if (loading && view === undefined) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
        <CircularProgress />
      </Box>
    );
  }
  // When the session is missing (404) but the slot is committed in the project
  // head-state, render the committed model read-only — no co-author chrome.
  if (sessionMissing && committed && committedEnvelope !== undefined) {
    return <ArtifactRenderer envelope={committedEnvelope} height={620} title={title} />;
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
      <Box sx={{ mb: gateOpen ? 3 : 0 }}>
        <ArtifactRenderer envelope={view?.draft} height={620} title={title} />
      </Box>
      {gateOpen ? <GatePanel
          commentCount={commentCount}
          findings={findings}
          pending={decisionPending}
          onApprove={onApprove}
          onSendBack={onSendBack}
          onWithdraw={onWithdraw}
        /> : null}
    </>
  );
}
