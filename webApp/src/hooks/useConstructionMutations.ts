/**
 * Phase-3 construction supervision mutations: pause the project's construction and
 * override one in-flight activity. Each invalidates the construction-session query
 * so the console re-reads fresh server state (never setQueryData). The Phase-3 TWIN
 * of useProjectDesignMutations.ts.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import {
  beginConstruction,
  pauseConstruction,
  overrideActivity,
  type OverrideKind,
} from '../api/construction';
import { constructionSessionKey } from './useConstructionSession';
import { projectKey } from './useProject';

export function useBeginConstruction(
  projectId: string
): UseMutationResult<undefined, Error, void> {
  const client = useQueryClient();
  return useMutation<undefined, Error, void>({
    mutationFn: async () => {
      await beginConstruction(projectId);
      return undefined;
    },
    // Refresh the project read so the just-written construction status (the pump's
    // first eligible activity flipping to in-construction) shows up; the console's
    // cascade poll then keeps it fresh as the pump drains the network.
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}

export function usePauseConstruction(
  projectId: string
): UseMutationResult<undefined, Error, string> {
  const client = useQueryClient();
  return useMutation<undefined, Error, string>({
    mutationFn: async (reason) => {
      await pauseConstruction(projectId, reason);
      return undefined;
    },
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ['constructionSession', projectId] }),
  });
}

export interface OverrideActivityVars {
  activityId: string;
  kind: OverrideKind;
  notes?: string;
}

export function useOverrideActivity(
  projectId: string
): UseMutationResult<undefined, Error, OverrideActivityVars> {
  const client = useQueryClient();
  return useMutation<undefined, Error, OverrideActivityVars>({
    mutationFn: async (vars) => {
      await overrideActivity(projectId, vars.activityId, vars.kind, vars.notes);
      return undefined;
    },
    onSuccess: (_data, vars) =>
      client.invalidateQueries({
        queryKey: constructionSessionKey(projectId, vars.activityId),
      }),
  });
}
