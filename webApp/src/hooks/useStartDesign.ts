/**
 * Phase-1 bootstrap mutations: start the system-design workflow and (its
 * precondition) record the research input. startSystemDesign fails with a 409
 * failed_precondition when no ResearchInput is present yet — the experience reads
 * that to reveal the research-input affordance, then retries start. Both
 * invalidate the project head-state so downstream reads refresh.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { setResearchInput, startSystemDesign } from '../api/systemDesign';
import type { ResearchInput } from '../api/types';
import { projectKey } from './useProject';

/** No-arg start trigger — TVariables is undefined (mirrors useAdvancePhase). */
export function useStartSystemDesign(
  projectId: string
): UseMutationResult<string, Error, undefined> {
  const client = useQueryClient();
  return useMutation<string, Error, undefined>({
    mutationFn: () => startSystemDesign(projectId),
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}

export function useSetResearchInput(
  projectId: string
): UseMutationResult<undefined, Error, ResearchInput> {
  const client = useQueryClient();
  return useMutation<undefined, Error, ResearchInput>({
    mutationFn: async (research) => {
      await setResearchInput(projectId, research);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
