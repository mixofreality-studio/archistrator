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
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import {
  artifactKindToOrdinal,
  reviewDecisionToOrdinal,
  systemArtifactKindFromOrdinal,
} from '../contracts/enums';
import type {
  ArtifactKind,
  PhaseAdvanceResponse,
  ReviewCommentStatus,
  ReviewDecision,
  ReviewDecisionDetail,
} from '../contracts/types';
import { projectKey } from './useProject';
import { sessionStateKey } from './useSessionState';

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
  return useMutation<string, Error, RequestDraftVars>({
    mutationFn: async (vars) => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/system-design/request-artifact-draft/{projectID}',
        {
          params: { path: { projectID: projectId } },
          body: {
            kind: artifactKindToOrdinal(vars.kind),
            ...(vars.feedback !== undefined ? { feedback: { notes: vars.feedback } } : {}),
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return data;
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
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
  return useMutation<undefined, Error, ReviewDecisionVars>({
    mutationFn: async (vars) => {
      const detail = vars.detail ?? {};
      const hasFeedback = detail.feedback !== undefined || detail.comments !== undefined;
      const { error, response } = await apiClient.POST(
        '/api/v1/system-design/submit-review-decision/{projectID}',
        {
          params: { path: { projectID: projectId } },
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
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
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
  return useMutation<undefined, Error, SetReviewCommentStatusVars>({
    mutationFn: async (vars) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/system-design/set-review-comment-status/{projectID}',
        {
          params: { path: { projectID: projectId } },
          body: {
            kind: artifactKindToOrdinal(vars.kind),
            commentID: vars.commentID,
            status: vars.status,
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
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
  return useMutation<PhaseAdvanceResponse, Error, boolean>({
    mutationFn: async (acknowledgeStale: boolean) => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/system-design/advance-phase/{projectID}',
        {
          params: { path: { projectID: projectId } },
          body: { acknowledgeStale },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return {
        advanced: data.advanced,
        missingArtifacts: (data.missingArtifacts ?? []).map(systemArtifactKindFromOrdinal),
      };
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
