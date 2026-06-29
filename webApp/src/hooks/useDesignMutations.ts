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
import { apiClient, toApiError } from '../api/client';
import {
  artifactKindToOrdinal,
  reviewDecisionToOrdinal,
  systemArtifactKindFromOrdinal,
} from '../api/enums';
import type {
  ArtifactKind,
  PhaseAdvanceResponse,
  ReviewDecision,
  ReviewDecisionDetail,
} from '../api/types';
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

/** No-arg advance trigger — TVariables is undefined to avoid an invalid void. */
export function useAdvancePhase(
  projectId: string
): UseMutationResult<PhaseAdvanceResponse, Error, undefined> {
  const client = useQueryClient();
  return useMutation<PhaseAdvanceResponse, Error, undefined>({
    mutationFn: async () => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/system-design/advance-phase/{projectID}',
        { params: { path: { projectID: projectId } } }
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
