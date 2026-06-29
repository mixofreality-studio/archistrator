/**
 * TanStack Query wrapper over the landing catalog (project/list-projects). The
 * owner scope (the authenticated subject) is now an explicit query param — read
 * from the signed-in principal. Reference-like data, so a modest staleTime keeps
 * re-fetches calm.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient, toApiError } from '../api/client';
import { mapProjectSummary } from '../api/wire';
import { useUser } from '../auth/UserContext';
import type { ProjectSummary } from '../api/types';

/** Base key — owner-scoped queries hang under it so invalidation by prefix works. */
export function projectsKey(): readonly unknown[] {
  return ['projects'];
}

export function useProjects(): UseQueryResult<ProjectSummary[]> {
  const owner = useUser().sub;
  return useQuery<ProjectSummary[]>({
    queryKey: [...projectsKey(), owner],
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET('/api/v1/system-design/list-projects', {
        params: { query: { owner } },
      });
      if (error !== undefined) throw toApiError(response.status, error);
      return data.map(mapProjectSummary);
    },
    staleTime: 30_000,
  });
}
