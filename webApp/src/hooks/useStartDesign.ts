/**
 * Phase-1 bootstrap mutations: start the system-design workflow and (its
 * precondition) record the research input. startSystemDesign fails with a 409
 * failed_precondition when no ResearchInput is present yet — the experience reads
 * that to reveal the research-input affordance, then retries start. Both
 * invalidate the project head-state so downstream reads refresh.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { toResearchInputWire } from '../contracts/wire';
import type { ResearchInput } from '../contracts/types';
import { projectKey } from './useProject';
import { sessionStateProjectKey } from './useSessionState';

/** No-arg start trigger — TVariables is undefined (mirrors useAdvancePhase). */
export function useStartSystemDesign(
  projectId: string
): UseMutationResult<string, Error, undefined> {
  const client = useQueryClient();
  const { ops } = useOpsClient();
  return useMutation<string, Error, undefined>({
    mutationFn: async () => {
      const data = await ops.callForBody<OpResult<'systemDesignStartSystemDesign'>>(
        'systemDesignStartSystemDesign',
        { path: { projectID: projectId } }
      );
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
  const { ops } = useOpsClient();
  return useMutation<undefined, Error, ResearchInput>({
    mutationFn: async (research) => {
      await ops.call('systemDesignSetResearchInput', {
        path: { projectID: projectId },
        body: {
          research: toResearchInputWire(research),
        } satisfies OpBody<'systemDesignSetResearchInput'>,
      });
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}
