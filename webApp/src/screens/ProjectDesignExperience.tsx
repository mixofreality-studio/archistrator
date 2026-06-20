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
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import Alert from '@mui/material/Alert';
import CircularProgress from '@mui/material/CircularProgress';
import AutoAwesomeIcon from '@mui/icons-material/AutoAwesome';
import RocketLaunchIcon from '@mui/icons-material/RocketLaunch';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { ApiError } from '../api/client';
import { PHASE2_ORDER } from '../constants/MethodMetadata';
import { METHOD_METADATA } from '../constants/MethodMetadata';
import { slotStageFromOrdinal } from '../api/adapters';
import type {
  ProjectArtifactKind,
  ProjectArtifactModelEnvelope,
  ProjectPhaseAdvanceResponse,
  ProjectState,
  Finding,
} from '../api/types';

import { useProject } from '../hooks/useProject';
import { useProjectSessionState } from '../hooks/useProjectSessionState';
import {
  useRequestProjectArtifactDraft,
  useSubmitProjectReviewDecision,
  useRequestSDPCommit,
  useSubmitSDPDecision,
  useAdvanceToConstruction,
} from '../hooks/useProjectDesignMutations';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { SlimSpine, type SpineStep } from '../components/design/SlimSpine';
import { GeneratingScene } from '../components/design/GeneratingScene';
import { DraftFailedPanel } from '../components/design/DraftFailedPanel';
import { GatePanel } from '../components/design/GatePanel';
import { ChatRail } from '../components/design/ChatRail';
import { StageChip } from '../components/StageChip';
import { ProjectArtifactRenderer } from '../components/project/ProjectArtifactRenderer';
import { CommentProvider, useComments } from '../components/comments/CommentContext';

import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const projectRouteApi = getRouteApi('/project/$projectId/design/project');

const PHASE2_KINDS = PHASE2_ORDER as readonly ProjectArtifactKind[];

/** Build the Phase-2 spine from the project head-state slots: committed / current / locked. */
function buildSpine(project: ProjectState | undefined): SpineStep[] {
  const committed = new Set(
    (project?.slots ?? [])
      .filter((s) => slotStageFromOrdinal(s.stage) === 'committed')
      .map((s) => s.kind)
  );
  let priorCommitted = true;
  return PHASE2_KINDS.map((kind) => {
    const isCommitted = committed.has(kind);
    const locked = !isCommitted && !priorCommitted;
    priorCommitted = isCommitted;
    return { kind, title: METHOD_METADATA[kind].title, committed: isCommitted, locked };
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
  const { comments, reset, toWire, freeformNotes, requestId } = useComments();

  const { data: project } = useProject(projectId);
  const spine = useMemo(() => buildSpine(project), [project]);
  const activityEnvelope = useMemo(() => committedActivityEnvelope(project), [project]);

  const firstOpen = spine.findIndex((s) => !s.committed);
  const [activeIndex, setActiveIndex] = useState(firstOpen < 0 ? spine.length - 1 : firstOpen);
  const safeIndex = Math.min(activeIndex, PHASE2_KINDS.length - 1);
  const activeKind: ProjectArtifactKind = PHASE2_KINDS[safeIndex] ?? 'planningAssumptions';
  const isSdpStep = activeKind === 'sdpReview';

  // Chat rail open-state mirrors the Phase-1 derivation (newer anchor re-opens it).
  const [closedAt, setClosedAt] = useState<number | null>(null);
  const chatOpen = closedAt === null || requestId > closedAt;
  const setChatOpen = (open: boolean): void => {
    setClosedAt(open ? null : requestId);
  };

  const session = useProjectSessionState(projectId, activeKind, true);
  const requestDraft = useRequestProjectArtifactDraft(projectId);
  const submitReview = useSubmitProjectReviewDecision(projectId);
  const assembleSdp = useRequestSDPCommit(projectId);
  const submitSdp = useSubmitSDPDecision(projectId);
  const advance = useAdvanceToConstruction(projectId);

  const sessionMissing = session.error instanceof ApiError && session.error.status === 404;
  const view = session.data?.view;
  const stage = session.data?.stage;
  const hasDraft = view?.draft.model !== undefined;
  const findings: Finding[] = view?.findings ?? [];
  const generating = stage === 'drafting' || stage === 'redrafting' || stage === 'assemblingSdp';
  // Terminal failure (anti-wedge): inline worker `refused` OR the async design job
  // landed in `draftFailed`. Both surface the DraftFailedPanel; draftFailed uses the
  // CI-job framing and adds a Withdraw exit alongside Retry.
  const asyncFailed = stage === 'draftFailed';
  const draftFailed = stage === 'refused' || asyncFailed;
  const committed = spine[safeIndex]?.committed === true;
  const failureReason = view?.failureReason;
  // Committed envelope from head-state: used as read-only fallback when there is no
  // co-author session (sessionMissing) but the slot is already committed.
  const committedEnvelope = project?.slots.find((s) => s.kind === activeKind)?.model as unknown as ProjectArtifactModelEnvelope | undefined;

  const selectStep = (i: number): void => {
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

  const approve = (): void => {
    submitReview.mutate(
      { kind: activeKind, decision: 'approve' },
      {
        onSuccess: () => {
          reset();
          setActiveIndex(Math.min(safeIndex + 1, PHASE2_KINDS.length - 1));
        },
      }
    );
  };

  const sendBack = (): void => {
    const wireComments = toWire();
    const notes = freeformNotes();
    const feedback = notes.length > 0 ? notes : wireComments.map((c) => c.text).join('\n');
    submitReview.mutate(
      { kind: activeKind, decision: 'reject', feedback },
      { onSuccess: () => { reset(); } }
    );
  };

  const withdraw = (): void => {
    submitReview.mutate({ kind: activeKind, decision: 'withdraw' }, { onSuccess: () => { reset(); } });
  };

  const sdpCommit = (optionId: string): void => {
    submitSdp.mutate({ decision: 'commit', detail: { optionId } });
  };

  const sdpRejectAll = (feedback: string): void => {
    submitSdp.mutate({ decision: 'rejectAll', detail: { feedback } }, { onSuccess: () => { reset(); } });
  };

  const meta = METHOD_METADATA[activeKind];
  const decisionPending = submitReview.isPending;

  return (
    <ExperienceChrome
      chat={chatOpen ? <ChatRail onCollapse={() => { setChatOpen(false); }} /> : undefined}
      chatOpen={chatOpen}
      phaseNum={2}
      phaseTitle="Project Design"
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
              <Typography sx={{ color: t.ink }} variant="h4">{meta.title}</Typography>
              <StageChip stage={committed ? 'committed' : stage === 'awaitingReview' ? 'awaitingReview' : 'empty'} />
            </Box>
            <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>
              {meta.file} · step {safeIndex + 1} of {PHASE2_KINDS.length}
            </Typography>
          </Box>
          <Box sx={{ flexGrow: 1 }} />
          <Chip label="architect" size="small" sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }} variant="outlined" />
        </Box>

        <ProjectStepBody
          activeKind={activeKind}
          activityEnvelope={activityEnvelope}
          advancePending={advance.isPending}
          advanceResult={advance.data}
          asyncFailed={asyncFailed}
          beginPending={requestDraft.isPending || assembleSdp.isPending}
          blurb={meta.blurb}
          commentCount={comments.length}
          committed={committed}
          committedEnvelope={committedEnvelope}
          decisionPending={decisionPending}
          draftFailed={draftFailed}
          failureReason={failureReason}
          findings={findings}
          generating={generating}
          hasDraft={hasDraft}
          isSdpStep={isSdpStep}
          loading={session.isLoading}
          retryPending={requestDraft.isPending || assembleSdp.isPending}
          sdpPending={submitSdp.isPending}
          sessionMissing={sessionMissing}
          stage={stage}
          t={t}
          title={meta.title}
          view={view}
          withdrawPending={decisionPending}
          onAdvance={() => { advance.mutate(undefined); }}
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
  failureReason,
  hasDraft,
  sessionMissing,
  stage,
  title,
  blurb,
  view,
  activityEnvelope,
  findings,
  commentCount,
  decisionPending,
  beginPending,
  retryPending,
  withdrawPending,
  sdpPending,
  advancePending,
  advanceResult,
  onBegin,
  onRetry,
  onApprove,
  onSendBack,
  onWithdraw,
  onSdpCommit,
  onSdpRejectAll,
  onAdvance,
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
  failureReason: string | undefined;
  hasDraft: boolean;
  sessionMissing: boolean;
  stage: string | undefined;
  title: string;
  blurb: string;
  view: { draft: ProjectArtifactModelEnvelope } | undefined;
  activityEnvelope: ProjectArtifactModelEnvelope | undefined;
  findings: Finding[];
  commentCount: number;
  decisionPending: boolean;
  beginPending: boolean;
  retryPending: boolean;
  withdrawPending: boolean;
  sdpPending: boolean;
  advancePending: boolean;
  advanceResult: ProjectPhaseAdvanceResponse | undefined;
  onBegin: () => void;
  onRetry: () => void;
  onApprove: () => void;
  onSendBack: () => void;
  onWithdraw: () => void;
  onSdpCommit: (optionId: string) => void;
  onSdpRejectAll: (feedback: string) => void;
  onAdvance: () => void;
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
    return <GeneratingScene artifact={title} />;
  }
  if (loading && view === undefined) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
        <CircularProgress />
      </Box>
    );
  }

  // The SDP step, once committed, offers the advance-to-construction seal.
  if (isSdpStep && committed) {
    return <AdvancePanel pending={advancePending} result={advanceResult} t={t} onAdvance={onAdvance} />;
  }

  // When the session is missing (404) but the slot is committed in the project
  // head-state, render the committed model read-only — no co-author chrome.
  if (sessionMissing && committed && committedEnvelope !== undefined) {
    return (
      <ProjectArtifactRenderer
        activityEnvelope={activityEnvelope}
        envelope={committedEnvelope}
        kind={activeKind}
        networkHeight={560}
      />
    );
  }

  if (!hasDraft || sessionMissing) {
    return (
      <Paper sx={{ p: 6, textAlign: 'center', borderStyle: 'dashed' }}>
        <AutoAwesomeIcon sx={{ fontSize: 30, color: t.accent }} />
        <Typography sx={{ fontFamily: t.mono, mt: 1, color: t.ink }}>
          {isSdpStep ? 'The SDP review is not assembled yet.' : 'No draft yet.'}
        </Typography>
        <Typography sx={{ color: t.muted, display: 'block', mb: 2 }} variant="caption">{blurb}</Typography>
        <Button
          color="primary"
          data-testid={isSdpStep ? UI_IDENTIFIERS.ProjectDesign.SDP_ASSEMBLE : UI_IDENTIFIERS.DesignExperience.REQUEST_DRAFT}
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
        />
      </Box>
      {gateOpen ? (
        <GatePanel
          commentCount={commentCount}
          findings={findings}
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
  onAdvance,
}: {
  t: Tokens;
  pending: boolean;
  result: ProjectPhaseAdvanceResponse | undefined;
  onAdvance: () => void;
}): ReactNode {
  return (
    <Paper sx={{ p: 4, maxWidth: 720, mx: 'auto', textAlign: 'center', border: `2px solid ${t.accent}` }}>
      <RocketLaunchIcon sx={{ fontSize: 34, color: t.accent }} />
      <Typography sx={{ color: t.ink, mt: 1 }} variant="h5">SDP committed — plan of record bound</Typography>
      <Typography sx={{ color: t.muted, mt: 1, mb: 2, lineHeight: 1.6 }}>
        Seal Project Design and advance to Construction. A non-advanced result lists the slots still owed.
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
      {result?.advanced === true ? <Alert data-testid={UI_IDENTIFIERS.ProjectDesign.ADVANCE_RESULT} severity="success" sx={{ textAlign: 'left', mb: 2 }}>
          Advanced to Construction — Phase 3 is unlocked.
        </Alert> : null}
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
