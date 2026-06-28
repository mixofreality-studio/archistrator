/**
 * Phase-3 construction supervision mutations: dispatch the next activity (the
 * "Begin construction" tick), pause the project's construction, and override one
 * in-flight activity. Each invalidates the construction-session / project queries
 * so the console re-reads fresh server state (never setQueryData).
 *
 * "Begin construction" now maps onto construction/execute-next-activity: the pump
 * dispatches the next eligible activity for the supplied tickID (idempotency key).
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { apiClient, toApiError } from '../api/client';
import { overrideKindToOrdinal } from '../api/enums';
import type { OverrideKind } from '../api/types';
import { constructionSessionKey } from './useConstructionSession';
import { projectKey } from './useProject';

export function useBeginConstruction(
  projectId: string
): UseMutationResult<undefined, Error, void> {
  const client = useQueryClient();
  return useMutation<undefined>({
    mutationFn: async () => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/execute-next-activity/{projectID}',
        { params: { path: { projectID: projectId } }, body: { tickID: crypto.randomUUID() } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    // Refresh the project read so the just-dispatched activity (flipping to
    // in-construction) shows up; the console's cascade poll keeps it fresh.
    onSuccess: () => client.invalidateQueries({ queryKey: projectKey(projectId) }),
  });
}

export function usePauseConstruction(
  projectId: string
): UseMutationResult<undefined, Error, string> {
  const client = useQueryClient();
  return useMutation<undefined, Error, string>({
    mutationFn: async (reason) => {
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/pause-project/{projectID}',
        { params: { path: { projectID: projectId } }, body: { reason } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ['constructionSession', projectId] }),
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
      const { error, response } = await apiClient.POST(
        '/api/v1/construction/override-activity/{projectID}/{activityID}',
        {
          params: { path: { projectID: projectId, activityID: vars.activityId } },
          body: { override: { kind: overrideKindToOrdinal(vars.kind), notes: vars.notes ?? '' } },
        }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return undefined;
    },
    onSuccess: (_data, vars) =>
      client.invalidateQueries({
        queryKey: constructionSessionKey(projectId, vars.activityId),
      }),
  });
}
