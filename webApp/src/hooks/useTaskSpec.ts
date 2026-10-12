/**
 * The per-task AgentTaskSpec read: `QueryProjectView{kind:'taskSpec', activityId,
 * taskId, revision}` — the computed model, prompt, tools, edit scope and checks
 * of one task, with the latest CheckRun for it. Same shape as `useTestRun`: the
 * delivery rail's one read through the OpsClient.
 *
 * `revision` is the task revision N (a number). With one given and no run
 * matching it the view carries no `checkRun`, which the panel shows as "not run".
 */
import { useEffect, useRef } from 'react';
import {
  useQueries,
  useQuery,
  useQueryClient,
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
  revision: number | undefined,
  /**
   * The poll while the selected revision's episode is running — the activity
   * screen's own live cadence — else `false` (R48). A run recorded mid-episode
   * then shows without a reload.
   */
  refetchInterval: number | false = false
): UseQueryResult<TaskSpecView> {
  const { ops } = useOpsClient();
  return useQuery<TaskSpecView>({
    ...taskSpecQueryOptions(ops, projectId, activityId, taskId, revision),
    refetchInterval,
  });
}

/**
 * The selected task's episode, said as one comparable string: which selection it
 * is, then `running`, the terminal episode (`<id>:<outcome>`), or `idle` (no
 * episode on screen yet).
 */
export function episodeSignal(
  selection: string,
  running: boolean,
  record: { episodeId: string; outcome: string } | undefined
): string {
  const state = running
    ? 'running'
    : record !== undefined
      ? `${record.episodeId}:${record.outcome}`
      : 'idle';
  return `${selection}|${state}`;
}

/**
 * Whether the spec must be re-read: the SAME selection's episode moved to a new
 * terminal state (it finished, or a new attempt finished). A change of selection
 * is a new query key already, and the first observation is not a change.
 */
export function taskSpecRefreshDue(prev: string | undefined, next: string): boolean {
  if (prev === undefined || prev === next) return false;
  const [prevSel] = prev.split('|');
  const [nextSel, nextState] = next.split('|');
  return prevSel === nextSel && nextState !== 'running' && nextState !== 'idle';
}

/**
 * Re-reads the activity's task specs when the selected task's episode reaches a
 * terminal state (R48): the run its job recorded lands under the same revision,
 * so the query key does not change and nothing else would refetch it.
 */
export function useTaskSpecRefresh(projectId: string, activityId: string, signal: string): void {
  const client = useQueryClient();
  const prev = useRef<string | undefined>(undefined);
  useEffect(() => {
    const due = taskSpecRefreshDue(prev.current, signal);
    prev.current = signal;
    if (due) void client.invalidateQueries({ queryKey: ['taskSpec', projectId, activityId] });
  }, [client, projectId, activityId, signal]);
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
 * The run's check logs, one query per WANTED `outputRef` (a row the reader
 * expanded, R48), keyed by the ref — the same arrangement `useTestRunOutputs`
 * keeps for a test run, against the spec's CheckRun (a test run IS a check run,
 * so the one file endpoint serves both). React Query caches each log, so
 * collapsing and re-expanding a row does not fetch it again.
 */
export function useCheckRunOutputs(
  projectId: string,
  spec: TaskSpecView | undefined,
  /** The refs an expanded row asked for: nothing else is fetched (R48). */
  wanted: ReadonlySet<string>
): Record<string, AsyncView<string>> {
  const refs = checkOutputRefsOf(spec).filter((ref) => wanted.has(ref));
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
