/**
 * TanStack Query wrapper over project/get-project — one project's full typed
 * head-state. Disabled until a projectId is present so callers can mount
 * unconditionally.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient, toApiError } from '../api/client';
import { mapProjectState } from '../api/wire';
import type { ProjectStateWithGit } from '../api/types';

export function projectKey(projectId: string): readonly unknown[] {
  return ['project', projectId];
}

/**
 * refetchInterval (ms) polls the project read — used by the Construction console to
 * animate the live pump cascade (per-activity status flips). Pass false (the
 * default) for the normal one-shot read.
 */
export function useProject(
  projectId: string,
  refetchInterval: number | false = false
): UseQueryResult<ProjectStateWithGit> {
  return useQuery<ProjectStateWithGit>({
    queryKey: projectKey(projectId),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/project/get-project/{projectID}',
        { params: { path: { projectID: projectId } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return mapProjectState(data);
    },
    enabled: projectId.length > 0,
    refetchInterval,
  });
}
