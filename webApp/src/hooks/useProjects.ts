/**
 * TanStack Query wrapper over listProjects — the landing catalog. Reference-like
 * data, so a modest staleTime keeps re-fetches calm.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { listProjects } from '../api/projects';
import type { ProjectSummary } from '../api/types';

export function projectsKey(): readonly unknown[] {
  return ['projects'];
}

export function useProjects(): UseQueryResult<ProjectSummary[]> {
  return useQuery<ProjectSummary[]>({
    queryKey: projectsKey(),
    queryFn: () => listProjects(),
    staleTime: 30_000,
  });
}
