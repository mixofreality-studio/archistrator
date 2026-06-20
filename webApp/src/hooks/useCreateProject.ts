/**
 * TanStack Query mutation over createProject. On success it invalidates the
 * landing catalog so the new project appears, and resolves to the server-minted
 * projectId the caller navigates to (/project/$projectId/home).
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { createProject } from '../api/projects';
import { projectsKey } from './useProjects';

export function useCreateProject(): UseMutationResult<string, Error, string> {
  const queryClient = useQueryClient();
  return useMutation<string, Error, string>({
    mutationFn: (name: string) => createProject(name),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectsKey() });
    },
  });
}
