/**
 * TanStack Query mutation over project/create-project. The owner scope (the
 * authenticated subject) is now an explicit body field alongside the repo name.
 * On success it invalidates the landing catalog so the new project appears, and
 * resolves to the server-minted projectId the caller navigates to.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { apiClient, toApiError } from '../api/client';
import { useUser } from '../auth/UserContext';
import { projectsKey } from './useProjects';

export function useCreateProject(): UseMutationResult<string, Error, string> {
  const queryClient = useQueryClient();
  const owner = useUser().sub;
  return useMutation<string, Error, string>({
    mutationFn: async (name: string) => {
      const { data, error, response } = await apiClient.POST('/api/v1/system-design/create-project', {
        body: { name, owner },
      });
      if (error !== undefined) throw toApiError(response.status, error);
      return data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectsKey() });
    },
  });
}
