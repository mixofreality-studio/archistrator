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
import {
  advancePhase,
  requestArtifactDraft,
  submitReviewDecision,
  type ReviewDecisionDetail,
} from '../api/systemDesign';
import type { ArtifactKind, PhaseAdvanceResponse, ReviewDecision } from '../api/types';
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
    mutationFn: (vars) => requestArtifactDraft(projectId, vars.kind, vars.feedback),
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
      await submitReviewDecision(projectId, vars.kind, vars.decision, vars.detail);
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
    mutationFn: () => advancePhase(projectId),
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
