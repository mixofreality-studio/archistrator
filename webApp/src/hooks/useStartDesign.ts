/**
 * Phase-1 bootstrap mutations: start the system-design workflow and (its
 * precondition) record the research input. startSystemDesign fails with a 409
 * failed_precondition when no ResearchInput is present yet — the experience reads
 * that to reveal the research-input affordance, then retries start. Both
 * invalidate the project head-state so downstream reads refresh.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import { toResearchInputWire } from '../contracts/wire';
import type { ResearchInput } from '../contracts/types';
import { projectKey } from './useProject';
import { sessionStateProjectKey } from './useSessionState';

/** No-arg start trigger — TVariables is undefined (mirrors useAdvancePhase). */
export function useStartSystemDesign(
  projectId: string
): UseMutationResult<string, Error, undefined> {
  const client = useQueryClient();
  return useMutation<string, Error, undefined>({
    mutationFn: async () => {
      const { data, error, response } = await apiClient.POST(
        '/api/v1/system-design/start-system-design/{projectID}',
        { params: { path: { projectID: projectId } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return data;
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: projectKey(projectId) });
      // Start creates the first co-authoring session; invalidate the project's
      // session probes so the (previously 404-cached) session query refetches and
      // the drafting stage appears — required now the no-session answer is cached
      // with staleTime:Infinity (R6).
      void client.invalidateQueries({ queryKey: sessionStateProjectKey(projectId) });
    },
  });
}

export function useSetResearchInput(
  projectId: string
): UseMutationResult<undefined, Error, ResearchInput> {
  const client = useQueryClient();
  return useMutation<undefined, Error, ResearchInput>({
    mutationFn: async (research) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/system-design/set-research-input/{projectID}',
        { params: { path: { projectID: projectId } }, body: { research: toResearchInputWire(research) } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
