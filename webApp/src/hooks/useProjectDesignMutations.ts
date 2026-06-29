/**
 * Phase-2 co-authoring mutations: request an artifact draft, submit a per-artifact
 * gate decision, assemble the SDP review, submit the SDP option decision, and
 * advance the phase. Each invalidates the affected project head-state + Phase-2
 * session queries so the UI re-reads fresh server state (never setQueryData). The
 * Phase-2 TWIN of useDesignMutations.ts.
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
  sdpDecisionToOrdinal,
  projectArtifactKindFromOrdinal,
} from '../api/enums';
import type {
  ProjectArtifactKind,
  ProjectPhaseAdvanceResponse,
  ReviewDecision,
  SDPDecision,
  SDPDecisionDetail,
} from '../api/types';
import { SDP_REVIEW_KIND } from '../api/types';
import { projectKey } from './useProject';
import { projectSessionStateKey } from './useProjectSessionState';

function invalidateArtifact(
  client: QueryClient,
  projectId: string,
  kind: ProjectArtifactKind
): Promise<void> {
  return Promise.all([
    client.invalidateQueries({ queryKey: projectKey(projectId) }),
    client.invalidateQueries({ queryKey: projectSessionStateKey(projectId, kind) }),
  ]).then(() => undefined);
}

export interface RequestProjectDraftVars {
  kind: ProjectArtifactKind;
  feedback?: string;
}

export function useRequestProjectArtifactDraft(
  projectId: string
): UseMutationResult<string, Error, RequestProjectDraftVars> {
  const client = useQueryClient();
  return useMutation<string, Error, RequestProjectDraftVars>({
    mutationFn: async (vars) => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/project-design/request-artifact-draft/{projectID}',
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

export interface ProjectReviewDecisionVars {
  kind: ProjectArtifactKind;
  decision: ReviewDecision;
  feedback?: string;
}

export function useSubmitProjectReviewDecision(
  projectId: string
): UseMutationResult<undefined, Error, ProjectReviewDecisionVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, ProjectReviewDecisionVars>({
    mutationFn: async (vars) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/project-design/submit-review-decision/{projectID}',
        {
          params: { path: { projectID: projectId } },
          body: {
            kind: artifactKindToOrdinal(vars.kind),
            decision: reviewDecisionToOrdinal(vars.decision),
            ...(vars.feedback !== undefined ? { feedback: { notes: vars.feedback } } : {}),
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: (_data, vars) => invalidateArtifact(client, projectId, vars.kind),
  });
}

/** No-arg assemble trigger — kicks the AssembleSDPReviewWorkflow. */
export function useRequestSDPCommit(
  projectId: string
): UseMutationResult<string, Error, undefined> {
  const client = useQueryClient();
  return useMutation<string, Error, undefined>({
    mutationFn: async () => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/project-design/request-sdp-commit/{projectID}',
        { params: { path: { projectID: projectId } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return data;
    },
    onSuccess: () => invalidateArtifact(client, projectId, SDP_REVIEW_KIND),
  });
}

export interface SDPDecisionVars {
  decision: SDPDecision;
  detail?: SDPDecisionDetail;
}

export function useSubmitSDPDecision(
  projectId: string
): UseMutationResult<undefined, Error, SDPDecisionVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, SDPDecisionVars>({
    mutationFn: async (vars) => {
      // optionID is a path param. On commit it names the chosen option; on rejectAll
      // there is no option, but the route still requires a non-empty segment — pass
      // a sentinel the Manager ignores for rejectAll.
      const optionID =
        vars.detail?.optionId !== undefined && vars.detail.optionId.length > 0
          ? vars.detail.optionId
          : 'none';
      const { error, response } = await apiClient.POST(
        '/api/v1/project-design/submit-sdp-decision/{projectID}/{optionID}',
        {
          params: { path: { projectID: projectId, optionID } },
          body: {
            decision: sdpDecisionToOrdinal(vars.decision),
            ...(vars.detail?.feedback !== undefined
              ? { feedback: { notes: vars.detail.feedback } }
              : {}),
          },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => invalidateArtifact(client, projectId, SDP_REVIEW_KIND),
  });
}

/** No-arg advance trigger — TVariables is undefined to avoid an invalid void. */
export function useAdvanceToConstruction(
  projectId: string
): UseMutationResult<ProjectPhaseAdvanceResponse, Error, undefined> {
  const client = useQueryClient();
  return useMutation<ProjectPhaseAdvanceResponse, Error, undefined>({
    mutationFn: async () => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/project-design/advance-to-construction/{projectID}',
        { params: { path: { projectID: projectId } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return {
        advanced: data.advanced,
        missingArtifacts: (data.missingArtifacts ?? []).map(projectArtifactKindFromOrdinal),
      };
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
