/**
 * The Activity Experience's single read: one activity's lifecycle DAG, each task's
 * state and revisions, and the live review set (constructionManager.QueryActivityView).
 * Follows useConstructionSession's conventions: a key factory, exported query options
 * (so a fan-out can share the cache), and a pure cadence rule (activityViewPolling.ts).
 *
 * Unlike the session probe, absence is an ERROR here, not a value: a 404 means the
 * activity is not in the committed plan, which the screen must say, and it never
 * flips back on its own — so it is neither retried nor polled.
 */
import { useQuery, type UseQueryOptions, type UseQueryResult } from '@tanstack/react-query';
import type { OpsClient } from '../api/ops.gen';
import { useOpsClient } from '../api/opsContext';
import type { OpResult } from '../api/opTypes';
import { activityViewPollIntervalMs } from './activityViewPolling';
import { isNoSessionError } from './sessionPolling';

export type ActivityView = OpResult<'constructionQueryActivityView'>;

export function activityViewKey(projectId: string, activityId: string): readonly unknown[] {
  return ['activityView', projectId, activityId];
}

/** The prefix every activity view of one project shares — what a decision, an
 *  override or a Begin invalidates. */
export function activityViewsKey(projectId: string): readonly unknown[] {
  return ['activityView', projectId];
}

export function activityViewQueryOptions(
  /** The transport the read rides (useOpsClient().ops). */
  ops: OpsClient,
  projectId: string,
  activityId: string | undefined,
  enabled: boolean,
  notStartedPollMs: number | false = false
): UseQueryOptions<ActivityView, Error, ActivityView> {
  const id = activityId ?? '';
  return {
    queryKey: activityViewKey(projectId, id),
    queryFn: () =>
      ops.callForBody<ActivityView>('constructionQueryActivityView', {
        path: { projectID: projectId, activityID: id },
      }),
    enabled: enabled && projectId.length > 0 && id.length > 0,
    retry: (count, error) => !isNoSessionError(error) && count < 1,
    refetchInterval: (query): number | false =>
      activityViewPollIntervalMs(query.state.data, query.state.error, notStartedPollMs),
  };
}

export function useActivityView(
  projectId: string,
  activityId?: string,
  enabled = true
): UseQueryResult<ActivityView> {
  const { ops } = useOpsClient();
  return useQuery(activityViewQueryOptions(ops, projectId, activityId, enabled));
}
