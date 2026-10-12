/**
 * The per-task AgentTaskSpec read: `QueryProjectView{kind:'taskSpec', activityId,
 * taskId, revision}` — the computed model, prompt, tools, edit scope and checks
 * of one task, with the latest CheckRun for it. Same shape as `useTestRun`: the
 * delivery rail's one read through the OpsClient.
 *
 * `revision` is the task revision N (a number). With one given and no run
 * matching it the view carries no `checkRun`, which the panel shows as "not run".
 */
import {
  useQueries,
  useQuery,
  type UseQueryOptions,
  type UseQueryResult,
} from '@tanstack/react-query';
import { fetchTestRunFileText, testRunFileUrl } from '../api/testRunFiles';
import type { OpsClient } from '../api/ops.gen';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { mapTaskSpec } from '../contracts/wire';
import type { TaskSpecView } from '../contracts/types';
import type { AsyncView } from './useTestRun';

type ProjectView = OpResult<'deliveryQueryProjectView'>;
type ProjectViewQuery = OpBody<'deliveryQueryProjectView'>['query'];

async function queryProjectView(ops: OpsClient, query: ProjectViewQuery): Promise<ProjectView> {
  return ops.callForBody<ProjectView>('deliveryQueryProjectView', { body: { query } });
}

export function taskSpecKey(
  projectId: string,
  activityId: string,
  taskId: string | undefined,
  revision: number | undefined
): readonly unknown[] {
  return ['taskSpec', projectId, activityId, taskId, revision];
}

/** The query's options, apart from the hook so the key and gate can be read without React. */
export function taskSpecQueryOptions(
  ops: OpsClient,
  projectId: string,
  activityId: string,
  taskId: string | undefined,
  revision: number | undefined
): UseQueryOptions<TaskSpecView> & { queryKey: readonly unknown[]; enabled: boolean } {
  return {
    queryKey: taskSpecKey(projectId, activityId, taskId, revision),
    queryFn: async (): Promise<TaskSpecView> => {
      const view = await queryProjectView(ops, {
        kind: 'taskSpec',
        projectId,
        activityId,
        taskId: taskId ?? '',
        ...(revision !== undefined ? { revision } : {}),
      });
      if (view.taskSpec === undefined) {
        throw new ApiError(200, 'empty_body', 'the taskSpec project view carried no taskSpec');
      }
      return mapTaskSpec(view.taskSpec);
    },
    enabled: projectId.length > 0 && activityId.length > 0 && (taskId ?? '').length > 0,
    // The spec is computed from committed state; the panel is not a live surface.
    staleTime: 60_000,
  };
}

export function useTaskSpec(
  projectId: string,
  activityId: string,
  taskId: string | undefined,
  revision: number | undefined
): UseQueryResult<TaskSpecView> {
  const { ops } = useOpsClient();
  return useQuery<TaskSpecView>(taskSpecQueryOptions(ops, projectId, activityId, taskId, revision));
}

/** The distinct output logs a spec's run recorded, in result order. */
export function checkOutputRefsOf(spec: TaskSpecView | undefined): string[] {
  const refs: string[] = [];
  for (const r of spec?.checkRun?.results ?? []) {
    const ref = r.outputRef ?? '';
    if (ref.length > 0 && !refs.includes(ref)) refs.push(ref);
  }
  return refs;
}

/** The file endpoint's URL for one file of the spec's run ('' with no run). */
export function checkRunFileUrl(
  projectId: string,
  spec: TaskSpecView | undefined
): (path: string) => string {
  const runId = spec?.checkRun?.id ?? '';
  return (path) => (runId.length > 0 ? testRunFileUrl(projectId, runId, path) : '');
}

/**
 * The run's check logs, one query per distinct `outputRef`, keyed by the ref —
 * the same arrangement `useTestRunOutputs` keeps for a test run, against the
 * spec's CheckRun (a test run IS a check run, so the one file endpoint serves both).
 */
export function useCheckRunOutputs(
  projectId: string,
  spec: TaskSpecView | undefined
): Record<string, AsyncView<string>> {
  const refs = checkOutputRefsOf(spec);
  const runId = spec?.checkRun?.id ?? '';
  const fileUrl = checkRunFileUrl(projectId, spec);
  return useQueries({
    queries: refs.map((ref) => ({
      queryKey: ['testRunFile', runId, ref],
      queryFn: async (): Promise<string> => fetchTestRunFileText(fileUrl(ref)),
      enabled: projectId.length > 0 && runId.length > 0,
      staleTime: Infinity,
    })),
    combine: (results) =>
      Object.fromEntries(
        results.map((r, i) => [
          refs[i] ?? '',
          { data: r.data, error: r.error, isLoading: r.isLoading },
        ])
      ),
  });
}
