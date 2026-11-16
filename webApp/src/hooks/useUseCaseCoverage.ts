/**
 * The product manager's coverage read behind the core-use-cases review task (FPC Task 2.14):
 * `QueryProjectView{kind:'useCaseCoverage', projectId, activityId}` — per use case and per
 * derived path, the door (the Client the actor's first stimulus enters through), the outcome
 * nodes the actor is shown and the derivation's defect codes. The server derives it on read
 * from the activity's branch (the draft the gate is about) and stores nothing, so the hook
 * only reads; there is no mutation to invalidate it on.
 *
 * Same shape as `useTaskSpec`: the delivery rail's one read through the OpsClient.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { OpsClient } from '../api/ops.gen';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { mapUseCaseCoverage } from '../contracts/wire';
import type { UseCaseCoverageView } from '../contracts/types';

type ProjectView = OpResult<'deliveryQueryProjectView'>;
type ProjectViewQuery = OpBody<'deliveryQueryProjectView'>['query'];

async function queryProjectView(ops: OpsClient, query: ProjectViewQuery): Promise<ProjectView> {
  return ops.callForBody<ProjectView>('deliveryQueryProjectView', { body: { query } });
}

export function useCaseCoverageKey(projectId: string, activityId: string): readonly unknown[] {
  return ['useCaseCoverage', projectId, activityId];
}

/** Disabled on an undefined activity: only the core-use-cases review task reads it. */
export function useUseCaseCoverage(
  projectId: string,
  activityId: string | undefined
): UseQueryResult<UseCaseCoverageView> {
  const { ops } = useOpsClient();
  const activity = activityId ?? '';
  return useQuery<UseCaseCoverageView>({
    queryKey: useCaseCoverageKey(projectId, activity),
    queryFn: async (): Promise<UseCaseCoverageView> => {
      const view = await queryProjectView(ops, {
        kind: 'useCaseCoverage',
        projectId,
        activityId: activity,
      });
      if (view.useCaseCoverage === undefined) {
        throw new ApiError(
          200,
          'empty_body',
          'the useCaseCoverage project view carried no useCaseCoverage'
        );
      }
      return mapUseCaseCoverage(view.useCaseCoverage);
    },
    enabled: projectId.length > 0 && activity.length > 0,
    // Derived from the draft under review: a redraft changes it, so keep it short-lived.
    staleTime: 15_000,
  });
}
