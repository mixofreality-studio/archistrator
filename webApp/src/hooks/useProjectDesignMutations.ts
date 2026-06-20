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
import {
  advanceToConstruction,
  requestProjectArtifactDraft,
  requestSDPCommit,
  submitProjectReviewDecision,
  submitSDPDecision,
  type SDPDecisionDetail,
} from '../api/projectDesign';
import type {
  ProjectArtifactKind,
  ProjectPhaseAdvanceResponse,
  ReviewDecision,
  SDPDecision,
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
    mutationFn: (vars) => requestProjectArtifactDraft(projectId, vars.kind, vars.feedback),
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
      await submitProjectReviewDecision(projectId, vars.kind, vars.decision, vars.feedback);
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
    mutationFn: () => requestSDPCommit(projectId),
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
      await submitSDPDecision(projectId, vars.decision, vars.detail);
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
    mutationFn: () => advanceToConstruction(projectId),
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
