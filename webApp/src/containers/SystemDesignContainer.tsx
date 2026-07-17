/**
 * The SPA container for the System Design (Phase-1) experience. Owns every bit
 * of orchestration the pure `SystemDesignView` (src/components/design) must NOT
 * own: data fetching (useProject/useSessionState), mutations
 * (useDesignMutations/useStartDesign), the active-step / chat-open / gate-error
 * UI state, and the CommentContext bridge (this renders inside the route's
 * CommentProvider, so it may call `useComments()` directly — the pure View may
 * not). Maps all of that down to `SystemDesignViewProps`.
 *
 * Extracted (Task 8) from `routes/DesignExperience.tsx`'s former
 * `SystemDesignBody`, which mixed this orchestration directly into the render
 * tree. The route file is now just `CommentProvider → SystemDesignContainer`.
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from '@tanstack/react-router';

import { ApiError } from '../contracts/errors';
import type { ArtifactKind, ProjectState, ResearchInput, ReviewDecision } from '../contracts/types';
import { slotStageFromOrdinal } from '../contracts/adapters';
import { PHASE1_ORDER, METHOD_METADATA } from '../contracts/methodMetadata';

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

import { ChatRail } from '../components/design/ChatRail';
import { DesignExperienceSkeleton } from '../components/design/DesignSkeleton';
import { SystemDesignView, type SpineStep } from '../components/design/SystemDesignView';
import { gateDecisionErrorMessage } from '../components/design/gateFaultLogic';
import { useComments } from '../components/comments/CommentContext';

const PHASE1_KINDS = PHASE1_ORDER as readonly ArtifactKind[];

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
  return PHASE1_KINDS.map((kind) => {
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

export function SystemDesignContainer({ projectId }: { projectId: string }): ReactNode {
  const navigate = useNavigate();
  const {
    comments,
    enabled: commentsEnabled,
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
  const safeIndex = Math.min(activeIndex, PHASE1_KINDS.length - 1);
  const activeKind: ArtifactKind = PHASE1_KINDS[safeIndex] ?? 'mission';

  // F-GTD-14: the useState initializer above runs BEFORE the project query
  // resolves (empty spine → firstOpen = 0), so the experience always opened at
  // Mission regardless of progress — and every later pip read as locked until
  // the data arrived, silently swallowing clicks. Snap to the real first-open
  // step exactly once, when the project first loads, unless the founder has
  // already navigated somewhere on their own.
  const userNavigatedRef = useRef(false);
  const snappedRef = useRef(false);
  useEffect(() => {
    if (snappedRef.current || project === undefined) return;
    snappedRef.current = true;
    if (!userNavigatedRef.current) {
      setActiveIndex(firstOpen < 0 ? spine.length - 1 : firstOpen);
    }
  }, [project, firstOpen, spine.length]);

  // Disarm any pending anchor when the active artifact changes, so an anchor
  // armed on one step never bleeds onto the next (it would attach a comment to a
  // stale, unrelated location).
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
  // Any failed gate decision (approve / send back / withdraw) names its error here.
  const [gateError, setGateError] = useState<string | undefined>(undefined);

  const sessionMissing = session.error instanceof ApiError && session.error.status === 404;
  const reviewThread = session.data?.view.reviewThread ?? [];
  const isFirstStep = safeIndex === 0;
  const needsResearch = isFirstStep && isPreconditionError(startDesign.error);

  const selectStep = (i: number): void => {
    // Clear any held gate error so a prior step's failed decision never bleeds
    // onto the next step's gate (F79).
    setGateError(undefined);
    userNavigatedRef.current = true;
    setActiveIndex(i);
  };

  // Unifies "begin the first session" and "request a[nother] draft": the pure
  // View's onRequestDraft(feedback?) covers both a fresh "Request draft" click
  // (feedback undefined) and an amendment / reconcile (feedback = rationale).
  const handleRequestDraft = (feedback?: string): void => {
    if (feedback === undefined && isFirstStep && sessionMissing) {
      startDesign.mutate(undefined);
      return;
    }
    requestDraft.mutate(
      feedback !== undefined ? { kind: activeKind, feedback } : { kind: activeKind }
    );
  };

  // Retry after a terminal Refused/draftFailed: re-enter drafting on the same
  // live session (or revive a dead one — the server starts a fresh run). The
  // mutation invalidates the session query for an immediate refetch; that refetch
  // RACES the server-side resume (failed → redrafting → awaitingReview within
  // seconds), so it is NOT the only recovery path — the failure gates keep an 8s
  // safety-net poll (F-QA2-50, sessionPolling.ts) that guarantees the polled
  // server stage wins within one interval even when the refetch loses the race.
  // A FAILED retry must not be invisible (2026-07-16 incident: dead-session
  // recovery clicks rendered zero feedback): name its error on the failed panel.
  const retryDraft = (): void => {
    setGateError(undefined);
    requestDraft.mutate(
      { kind: activeKind },
      {
        onError: (err) => {
          setGateError(err.message);
        },
      }
    );
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

  const onSubmitReview = (decision: ReviewDecision): void => {
    setGateError(undefined);
    if (decision === 'reject') {
      const wireComments = toWire();
      const notes = freeformNotes();
      // The Manager requires non-empty reject feedback; when the architect only
      // anchored comments (no free-form note), synthesize the notes from them so
      // the redraft always carries actionable guidance and the reject validates.
      const feedback = notes.length > 0 ? notes : wireComments.map((c) => c.text).join('\n');
      submitReview.mutate(
        { kind: activeKind, decision, detail: { feedback, comments: wireComments } },
        {
          onSuccess: () => {
            reset();
          },
          onError: (err) => {
            // A failed send-back must not be invisible (F79): keep the accumulated
            // notes (no reset), stay on the gate, and name the error inline. The
            // mutation settles, so the buttons re-enable for a retry. An
            // indeterminate transport fault (F-QA2-47: a 503 whose signal WAS
            // delivered) gets the cause-neutral copy, and the refetch renders
            // whatever the server actually did with the decision.
            setGateError(gateDecisionErrorMessage(err));
            void session.refetch();
          },
        }
      );
      return;
    }
    submitReview.mutate(
      { kind: activeKind, decision },
      {
        onSuccess: () => {
          reset();
          if (decision === 'approve') {
            // Auto-advance to the next non-committed step.
            const next = Math.min(safeIndex + 1, PHASE1_KINDS.length - 1);
            setActiveIndex(next);
          }
        },
        onError: (err) => {
          // A FailedPrecondition (open thread entries) keeps its precise message; an
          // indeterminate transport fault gets the cause-neutral copy (F-QA2-47).
          // Either way refetch the session so the gate reflects server truth.
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

  // While the project head-state is in flight we cannot yet know any step's
  // committed/locked status, the active artifact, or its stage — render the themed
  // skeleton rather than chrome that guesses (a "NOT DRAFTED" chip / step-1 rail)
  // and then contradicts itself once the data lands. This is a container-level
  // gate: the pure View's `project` prop is never optional (it only ever mounts
  // once project data exists).
  if (projectLoading || project === undefined) {
    return (
      <DesignExperienceSkeleton
        phaseNum={1}
        phaseTitle="System Design"
        steps={PHASE1_KINDS.length}
        onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      />
    );
  }

  const chat = chatOpen ? (
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
  ) : undefined;

  return (
    <SystemDesignView
      acknowledgeStaleError={acknowledgeStale.error?.message}
      acknowledgeStalePending={acknowledgeStale.isPending}
      activeIndex={safeIndex}
      amendPending={requestDraft.isPending}
      beginPending={startDesign.isPending || requestDraft.isPending}
      chat={chat}
      chatOpen={chatOpen}
      commentSurface={{ enabled: commentsEnabled, commentCount: comments.length, setAnchor }}
      decisionPending={submitReview.isPending}
      gateError={gateError}
      needsResearch={needsResearch}
      project={project}
      researchPending={setResearch.isPending || startDesign.isPending}
      retryPending={requestDraft.isPending}
      session={session.data}
      sessionLoading={session.isLoading}
      sessionMissing={sessionMissing}
      spine={spine}
      onAcknowledgeStale={onAcknowledgeStale}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
      onOpenChat={() => {
        setChatOpen(true);
      }}
      onRequestDraft={handleRequestDraft}
      onRetry={retryDraft}
      onSelectStep={selectStep}
      onSubmitResearch={submitResearch}
      onSubmitReview={onSubmitReview}
    />
  );
}
