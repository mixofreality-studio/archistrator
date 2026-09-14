/**
 * TanStack Query wrapper over the landing catalog (project/list-projects). The
 * owner scope (the authenticated subject) is now an explicit query param — read
 * from the signed-in principal. Reference-like data, so a modest staleTime keeps
 * re-fetches calm.
 *
 * Rides the transport-blind OpsClient (like useProject), not apiClient directly,
 * so the landing previews over the fixture transport (design-renderer-data.md
 * §2′.1). The REST transport's status check (throwUnlessOk) decides failure; the
 * empty-2xx refusal bodyUnlessError used to add is kept below.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import { ApiError } from '../contracts/errors';
import { mapProjectSummary } from '../contracts/wire';
import { useUser } from '../utilities/auth/UserContext';
import type { ProjectSummary } from '../contracts/types';
import type { components } from '../contracts/schema';

/** Base key — owner-scoped queries hang under it so invalidation by prefix works. */
export function projectsKey(): readonly unknown[] {
  return ['projects'];
}

export function useProjects(): UseQueryResult<ProjectSummary[]> {
  const owner = useUser().sub;
  const { ops } = useOpsClient();
  return useQuery<ProjectSummary[]>({
    queryKey: [...projectsKey(), owner],
    queryFn: async () => {
      const data = await ops.call<
        components['schemas']['SystemDesignProjectSummary'][] | undefined
      >('systemDesignListProjects', { query: { owner } });
      if (data === undefined) {
        // A 2xx with no body where the catalog is owed (bodyUnlessError's rule).
        throw new ApiError(200, 'empty_body', 'the project catalog response carried no body');
      }
      return data.map(mapProjectSummary);
    },
    staleTime: 30_000,
  });
}
