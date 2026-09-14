/**
 * TanStack Query mutation over project/create-project. The owner scope (the
 * authenticated subject) is now an explicit body field alongside the repo name.
 * On success it invalidates the landing catalog so the new project appears, and
 * resolves to the server-minted projectId the caller navigates to.
 *
 * OPERATING MODEL (founder ruling 2026-07-05): the caller chooses the operating
 * model at creation. A fresh project is born selfOperated on the server, so this
 * mutation only issues the follow-up set-operating-model call when the user picks
 * archistratorOperated — keeping create + choose atomic from the caller's view.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { useUser } from '../utilities/auth/UserContext';
import { projectsKey } from './useProjects';

/** WHO operates the built app — selfOperated (customer's own infra; the default) or
 * archistratorOperated (archistrator operates it on the platform, constraining the
 * deployment design to the platform palette). */
export type OperatingModel = 'selfOperated' | 'archistratorOperated';

export interface CreateProjectVars {
  name: string;
  operatingModel: OperatingModel;
}

export function useCreateProject(): UseMutationResult<string, Error, CreateProjectVars> {
  const queryClient = useQueryClient();
  const owner = useUser().sub;
  const { ops } = useOpsClient();
  return useMutation<string, Error, CreateProjectVars>({
    mutationFn: async ({ name, operatingModel }: CreateProjectVars) => {
      const data = await ops.callForBody<OpResult<'systemDesignCreateProject'>>(
        'systemDesignCreateProject',
        { body: { name, owner } satisfies OpBody<'systemDesignCreateProject'> }
      );
      const projectId = data;
      // Born selfOperated; only issue the set call when the user chose otherwise.
      if (operatingModel !== 'selfOperated') {
        await ops.call('systemDesignSetOperatingModel', {
          path: { projectID: projectId },
          body: { model: operatingModel } satisfies OpBody<'systemDesignSetOperatingModel'>,
        });
      }
      return projectId;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectsKey() });
    },
  });
}
