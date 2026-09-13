/**
 * TanStack Query wrapper over project/get-project — one project's full typed
 * head-state. Disabled until a projectId is present so callers can mount
 * unconditionally.
 *
 * Rides the transport-blind OpsClient (like useSessionState / useDesignMutations)
 * rather than the REST apiClient directly, so the same hook serves both the
 * standalone SPA (REST) and the MCP-hosted app (server tool calls over the host
 * bridge — a sandboxed iframe has no direct network to the API origin).
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import { mapProjectState } from '../contracts/wire';
import type { ProjectStateWithGit } from '../contracts/types';
import type { components } from '../contracts/schema';

export function projectKey(projectId: string): readonly unknown[] {
  return ['project', projectId];
}

/**
 * refetchInterval (ms) polls the project read — used by the Construction console to
 * animate the live pump cascade (per-activity status flips). Pass false (the
 * default) for the normal one-shot read. A function is given the latest read, for a
 * caller whose cadence depends on what the read says (the console polls while the
 * state shows work in flight, and a caller cannot know that before this hook runs).
 */
export function useProject(
  projectId: string,
  refetchInterval:
    | number
    | false
    | ((project: ProjectStateWithGit | undefined) => number | false) = false
): UseQueryResult<ProjectStateWithGit> {
  const { ops } = useOpsClient();
  const interval =
    typeof refetchInterval === 'function'
      ? (query: { state: { data: ProjectStateWithGit | undefined } }): number | false =>
          refetchInterval(query.state.data)
      : refetchInterval;
  return useQuery<ProjectStateWithGit>({
    queryKey: projectKey(projectId),
    queryFn: async () => {
      const data = await ops.call<components['schemas']['SystemDesignProjectState']>(
        'systemDesignGetProject',
        { path: { projectID: projectId } }
      );
      return mapProjectState(data);
    },
    enabled: projectId.length > 0,
    refetchInterval: interval,
  });
}
