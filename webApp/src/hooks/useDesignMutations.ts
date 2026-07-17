/**
 * Phase-1 co-authoring mutations: request a draft, submit a gate decision, and
 * advance the phase. Each invalidates the affected project head-state + session
 * queries so the UI re-reads fresh server state (never setQueryData).
 */
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
  type QueryClient,
} from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import {
  artifactKindToOrdinal,
  reviewDecisionToOrdinal,
  systemArtifactKindFromOrdinal,
} from '../contracts/wire';
import type {
  AnchoredComment,
  ArtifactKind,
  PhaseAdvanceResponse,
  ReviewCommentAddressee,
  ReviewCommentStatus,
  ReviewDecision,
  ReviewDecisionDetail,
} from '../contracts/types';
import type { components } from '../contracts/schema';
import { projectKey } from './useProject';
import { sessionStateKey, sessionStateProjectKey } from './useSessionState';

function invalidateArtifact(
  client: QueryClient,
  projectId: string,
  kind: ArtifactKind
): Promise<void> {
  return Promise.all([
    client.invalidateQueries({ queryKey: projectKey(projectId) }),
    client.invalidateQueries({ queryKey: sessionStateKey(projectId, kind) }),
  ]).then(() => undefined);
}

export interface RequestDraftVars {
  kind: ArtifactKind;
  feedback?: string;
}

export function useRequestArtifactDraft(
  projectId: string
): UseMutationResult<string, Error, RequestDraftVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<string, Error, RequestDraftVars>({
    mutationFn: async (vars) => {
      return ops.call<components['schemas']['SystemDesignSessionRef']>(
        'systemDesignRequestArtifactDraft',
        {
          path: { projectID: projectId },
          body: {
            kind: artifactKindToOrdinal(vars.kind),
            ...(vars.feedback !== undefined ? { feedback: { notes: vars.feedback } } : {}),
          },
        }
      );
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
    // A refused request (409 failed_precondition: "a draft is already generating…")
    // means the SPA's no-session view was STALE — a session is running server-side
    // (e.g. auto-started by the phase advance). Refetch the session state so the UI
    // flips to the truthful generating scene instead of a dead "Request draft" card.
    onError: (_error, vars) => invalidateArtifact(client, projectId, vars.kind),
  });
}

export interface ReviewDecisionVars {
  kind: ArtifactKind;
  decision: ReviewDecision;
  detail?: ReviewDecisionDetail;
}

export function useSubmitReviewDecision(
  projectId: string
): UseMutationResult<undefined, Error, ReviewDecisionVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, ReviewDecisionVars>({
    mutationFn: async (vars) => {
      const detail = vars.detail ?? {};
      const hasFeedback = detail.feedback !== undefined || detail.comments !== undefined;
      await ops.call('systemDesignSubmitReviewDecision', {
        path: { projectID: projectId },
        body: {
          kind: artifactKindToOrdinal(vars.kind),
          decision: reviewDecisionToOrdinal(vars.decision),
          ...(hasFeedback
            ? {
                feedback: {
                  notes: detail.feedback ?? '',
                  ...(detail.comments !== undefined ? { comments: detail.comments } : {}),
                },
              }
            : {}),
        },
      });
      return undefined;
    },
    // An APPROVE auto-advances the phase workflow, which AUTO-STARTS the next step's
    // co-author session server-side (QA incident 2026-07-15) — invalidate the whole
    // project's session probes (not just this kind) so the next step's cached
    // no-session 404 refetches immediately and discovers the auto-started session.
    onSuccess: (_data, vars) =>
      vars.decision === 'approve'
        ? Promise.all([
            client.invalidateQueries({ queryKey: projectKey(projectId) }),
            client.invalidateQueries({ queryKey: sessionStateProjectKey(projectId) }),
          ]).then(() => undefined)
        : invalidateArtifact(client, projectId, vars.kind),
  });
}

export interface SetReviewCommentStatusVars {
  kind: ArtifactKind;
  commentID: string;
  /** 'waived' dismisses an open entry; 'open' reopens an addressed one. */
  status: Extract<ReviewCommentStatus, 'open' | 'waived'>;
}

/**
 * Waive (dismiss) an open review-ledger entry, or reopen an addressed one. On
 * success it invalidates the session query so the thread re-reads with the flipped
 * status (the SPA never mutates the thread locally past the optimistic caller).
 */
export function useSetReviewCommentStatus(
  projectId: string
): UseMutationResult<undefined, Error, SetReviewCommentStatusVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, SetReviewCommentStatusVars>({
    mutationFn: async (vars) => {
      await ops.call('systemDesignSetReviewCommentStatus', {
        path: { projectID: projectId },
        body: {
          kind: artifactKindToOrdinal(vars.kind),
          commentID: vars.commentID,
          status: vars.status,
        },
      });
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
  });
}

export interface AskQuestionsVars {
  kind: ArtifactKind;
  /** The role every question in this batch is addressed to. */
  addressee: Exclude<ReviewCommentAddressee, ''>;
  questions: AnchoredComment[];
}

/**
 * Ask clarifying QUESTIONS about an artifact WITHOUT sending it back for a redraft
 * (question-comments). The questions are appended to the review ledger as question-type
 * entries and a lightweight answer job is dispatched; open questions do NOT block approve.
 * On success it invalidates the session query so the thread re-reads with the new asks.
 */
export function useAskQuestions(
  projectId: string
): UseMutationResult<undefined, Error, AskQuestionsVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, AskQuestionsVars>({
    mutationFn: async (vars) => {
      await ops.call('systemDesignAskQuestions', {
        path: { projectID: projectId },
        body: {
          kind: artifactKindToOrdinal(vars.kind),
          addressee: vars.addressee,
          questions: vars.questions,
        },
      });
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
  });
}

export interface AcknowledgeStaleVars {
  kind: ArtifactKind;
  /** Optional audit note recorded on the staleAck ledger entry. */
  note: string;
}

/**
 * Mark a stale committed artifact "reviewed — unaffected" (F45): clears its StaleBasis
 * WITHOUT a redraft, recording the note as a durable staleAck audit entry. On success it
 * invalidates the artifact query so the pane re-reads with the flag cleared.
 */
export function useAcknowledgeStaleBasis(
  projectId: string
): UseMutationResult<undefined, Error, AcknowledgeStaleVars> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, AcknowledgeStaleVars>({
    mutationFn: async (vars) => {
      await ops.call('systemDesignAcknowledgeStaleBasis', {
        path: { projectID: projectId },
        body: { kind: artifactKindToOrdinal(vars.kind), note: vars.note },
      });
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
  });
}

/**
 * Advance trigger — TVariables is `acknowledgeStale`. A normal advance sends
 * false; when the server blocks with a FailedPrecondition naming stale committed
 * slots, the caller re-invokes with true ("advance anyway") to acknowledge and seal
 * over them (F55).
 */
export function useAdvancePhase(
  projectId: string
): UseMutationResult<PhaseAdvanceResponse, Error, boolean> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<PhaseAdvanceResponse, Error, boolean>({
    mutationFn: async (acknowledgeStale: boolean) => {
      const data = await ops.call<components['schemas']['SystemDesignPhaseAdvanceResult']>(
        'systemDesignAdvancePhase',
        {
          path: { projectID: projectId },
          body: { acknowledgeStale },
        }
      );
      return {
        advanced: data.advanced,
        missingArtifacts: (data.missingArtifacts ?? []).map(systemArtifactKindFromOrdinal),
      };
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
