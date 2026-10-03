/**
 * The reads behind a component's testing lifecycle (spec §6.4):
 *
 *   useComponentScenarios  `QueryProjectView{kind:'scenarios', componentId}` —
 *                          the deterministic scenarios projected onto one
 *                          component (framework-go/scenario.ForComponent), each
 *                          with its committed binding when the test plan has one.
 *                          The `stp` / `stpReview` tasks' plan lens.
 *   useTestRun             `QueryProjectView{kind:'testRun', activityId}` — the
 *                          latest recorded run for the activity, joined with the
 *                          projection (missing / unknown / expired decided
 *                          server-side). The `testing` task's results panel.
 *   useTestRunOutputs      the run's `go test -json` streams, one query per
 *                          distinct `outputRef`, so the panel can filter a
 *                          scenario's output without a fetch per selection.
 *
 * Both views ride the delivery rail's one read (`queryProjectView`, body
 * selector) through the OpsClient like every other hook in useDeliveryQueries;
 * the file bytes are the one read that is not an op (api/testRunFiles.ts).
 */
import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query';
import { fetchTestRunFileText, testRunFileUrl } from '../api/testRunFiles';
import type { OpsClient } from '../api/ops.gen';
import { useOpsClient } from '../api/opsContext';
import type { OpBody, OpResult } from '../api/opTypes';
import { ApiError } from '../contracts/errors';
import { mapScenarioView, mapTestRunPanel } from '../contracts/wire';
import type { ScenarioView, TestRunPanelView } from '../contracts/types';

type ProjectView = OpResult<'deliveryQueryProjectView'>;
type ProjectViewQuery = OpBody<'deliveryQueryProjectView'>['query'];

async function queryProjectView(ops: OpsClient, query: ProjectViewQuery): Promise<ProjectView> {
  return ops.callForBody<ProjectView>('deliveryQueryProjectView', { body: { query } });
}

/** A query's state as a pure component can take it — no QueryObserver, no refetch. */
export interface AsyncView<T> {
  data: T | undefined;
  error: Error | null;
  isLoading: boolean;
}

export function componentScenariosKey(projectId: string, componentId: string): readonly unknown[] {
  return ['componentScenarios', projectId, componentId];
}

export function useComponentScenarios(
  projectId: string,
  componentId: string | undefined
): UseQueryResult<ScenarioView[]> {
  const { ops } = useOpsClient();
  const component = componentId ?? '';
  return useQuery<ScenarioView[]>({
    queryKey: componentScenariosKey(projectId, component),
    queryFn: async () => {
      const view = await queryProjectView(ops, {
        kind: 'scenarios',
        projectId,
        componentId: component,
      });
      // The scenarios member is a nullable array: an empty projection is a real
      // answer ("nothing reaches this component"), not a missing member.
      if (view.scenarios === undefined) {
        throw new ApiError(200, 'empty_body', 'the scenarios project view carried no scenarios');
      }
      return (view.scenarios ?? []).map(mapScenarioView);
    },
    enabled: projectId.length > 0 && component.length > 0,
    staleTime: 30_000,
  });
}

export function testRunKey(projectId: string, activityId: string): readonly unknown[] {
  return ['testRun', projectId, activityId];
}

export function useTestRun(
  projectId: string,
  activityId: string | undefined
): UseQueryResult<TestRunPanelView> {
  const { ops } = useOpsClient();
  const activity = activityId ?? '';
  return useQuery<TestRunPanelView>({
    queryKey: testRunKey(projectId, activity),
    queryFn: async () => {
      const view = await queryProjectView(ops, {
        kind: 'testRun',
        projectId,
        activityId: activity,
      });
      const run = view.testRun;
      if (run === undefined) {
        throw new ApiError(200, 'empty_body', 'the testRun project view carried no testRun');
      }
      return mapTestRunPanel(run, (path) => testRunFileUrl(run.run.id, path));
    },
    enabled: projectId.length > 0 && activity.length > 0,
    // A run is written once by the venue; the panel is not a live surface.
    staleTime: 60_000,
  });
}

/** The distinct `go test -json` streams a run's scenarios name, in first-seen order. */
export function outputRefsOf(panel: TestRunPanelView | undefined): string[] {
  const refs: string[] = [];
  for (const s of panel?.scenarios ?? []) {
    if (s.outputRef.length > 0 && !refs.includes(s.outputRef)) refs.push(s.outputRef);
  }
  return refs;
}

export function useTestRunOutputs(
  panel: TestRunPanelView | undefined
): Record<string, AsyncView<string>> {
  const refs = outputRefsOf(panel);
  const runId = panel?.run.id ?? '';
  return useQueries({
    queries: refs.map((ref) => ({
      queryKey: ['testRunFile', runId, ref],
      queryFn: async (): Promise<string> => {
        if (panel === undefined) return '';
        return fetchTestRunFileText(panel.fileUrl(ref));
      },
      enabled: panel !== undefined && !panel.expired,
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
