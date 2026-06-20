/**
 * TanStack Query wrapper over getProject — one project's full typed head-state.
 * Disabled until a projectId is present so callers can mount unconditionally.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { getProject } from '../api/projects';
import type { ProjectStateWithGit } from '../api/types';

export function projectKey(projectId: string): readonly unknown[] {
  return ['project', projectId];
}

/**
 * refetchInterval (ms) polls the project read — used by the Construction console to
 * animate the live pump cascade (per-activity status flips in constructionRows). Pass
 * false (the default) for the normal one-shot read.
 */
export function useProject(
  projectId: string,
  refetchInterval: number | false = false
): UseQueryResult<ProjectStateWithGit> {
  return useQuery<ProjectStateWithGit>({
    queryKey: projectKey(projectId),
    queryFn: () => getProject(projectId),
    enabled: projectId.length > 0,
    refetchInterval,
  });
}
