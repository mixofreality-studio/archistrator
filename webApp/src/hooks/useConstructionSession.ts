/**
 * Polls the Phase-3 construction session's technical view. The new route is
 * per-activity (construction/get-session-state/{projectID}/{activityID}) — the
 * activityId is now REQUIRED, so the query stays disabled until one is selected
 * (the project-level supervision view no longer has a route). Polling runs every
 * 3s while the session is live and stops at a terminal stage (exited / paused). A
 * 404 (no session yet — the pump is dormant) is surfaced WITHOUT retry storms.
 */
import {
  useQueries,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseQueryOptions,
  type UseQueryResult,
} from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import { mapConstructionSession } from '../contracts/wire';
import type { ConstructionSessionState } from '../contracts/types';
import { sessionProbeQueryFn } from './sessionPolling';
import { constructionStartedFrom, type ConstructionStarted } from './constructionStarted';

const POLL_INTERVAL_MS = 3000;

/** Stages at which no further pump activity occurs for the session. */
const TERMINAL_STAGES = new Set(['exited', 'paused']);

export function constructionSessionKey(projectId: string, activityId?: string): readonly unknown[] {
  return ['constructionSession', projectId, activityId ?? null];
}

/** The prefix every per-activity session probe of one project shares — what a
 *  Begin invalidates, so the Begin/Resume answer re-reads after a dispatch. */
export function constructionSessionsKey(projectId: string): readonly unknown[] {
  return ['constructionSession', projectId];
}

/**
 * One per-activity session probe. Shared by useConstructionSession (one activity)
 * and useConstructionStarted (every committed activity), so both read the SAME
 * cache entry for the same activity rather than two copies that could disagree.
 */
function sessionQueryOptions(
  queryClient: QueryClient,
  projectId: string,
  activityId: string | undefined,
  enabled: boolean
): UseQueryOptions<ConstructionSessionState | null, Error, ConstructionSessionState | null> {
  const hasActivity = activityId !== undefined && activityId.length > 0;
  const key = constructionSessionKey(projectId, activityId);
  return {
    queryKey: key,
    queryFn: sessionProbeQueryFn<ConstructionSessionState>({
      fetch: async () => {
        const { data, error, response } = await apiClient.GET(
          '/api/v1/construction/get-session-state/{projectID}/{activityID}',
          { params: { path: { projectID: projectId, activityID: activityId ?? '' } } }
        );
        if (error !== undefined) throw toApiError(response.status, error);
        return mapConstructionSession(data);
      },
      getCached: () => queryClient.getQueryData<ConstructionSessionState | null>(key),
    }),
    enabled: enabled && projectId.length > 0 && hasActivity,
    // The dormant-pump 404 resolves to null inside the probe (never throws), so
    // retry only ever sees real faults: one retry, no storms.
    retry: (count) => count < 1,
    refetchInterval: (query): number | false => {
      // Dormant pump (absence established as null): stop polling so the console
      // does not spam a 3s 404 storm. The project-read cascade poll drives the
      // tracker meanwhile; a Begin mutation invalidates this query.
      if (query.state.data === null) return false;
      const stage = query.state.data?.stage;
      if (stage !== undefined && TERMINAL_STAGES.has(stage)) return false;
      return POLL_INTERVAL_MS;
    },
  };
}

/**
 * The probe value: a live session view, or `null` for ESTABLISHED absence (the
 * dormant-pump 404 resolved to a value — see sessionProbeQueryFn for why
 * absence must be data, not error).
 */
export function useConstructionSession(
  projectId: string,
  activityId?: string,
  enabled = true
): UseQueryResult<ConstructionSessionState | null> {
  const queryClient = useQueryClient();
  return useQuery(sessionQueryOptions(queryClient, projectId, activityId, enabled));
}

/**
 * Has construction started for this project — answered by the SESSION ENDPOINT,
 * the single source for Begin vs Resume, never by counting rows or attempts (see
 * constructionStarted.ts for why those lie here).
 *
 * The endpoint is per-activity: a construction session is keyed
 * `<project>:<activity>` and exists only once the pump dispatched that activity.
 * The project-level session query addresses the operator-pause supervision
 * workflow, which Begin never starts, and has no route at all — so the question
 * is asked of every activity the committed list names. An absent session settles
 * to `null` and stops polling (see sessionQueryOptions), so a dormant project
 * costs one probe per activity per load.
 */
export function useConstructionStarted(
  projectId: string,
  activityIds: readonly string[]
): ConstructionStarted {
  const queryClient = useQueryClient();
  return useQueries({
    queries: activityIds.map((id) => sessionQueryOptions(queryClient, projectId, id, true)),
    combine: (results) =>
      constructionStartedFrom(results.map((r) => ({ data: r.data, isError: r.isError }))),
  });
}
