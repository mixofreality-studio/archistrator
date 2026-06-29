/**
 * Polls the Phase-3 construction session's technical view. The new route is
 * per-activity (construction/get-session-state/{projectID}/{activityID}) — the
 * activityId is now REQUIRED, so the query stays disabled until one is selected
 * (the project-level supervision view no longer has a route). Polling runs every
 * 3s while the session is live and stops at a terminal stage (exited / paused). A
 * 404 (no session yet — the pump is dormant) is surfaced WITHOUT retry storms.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient, ApiError, toApiError } from '../api/client';
import { mapConstructionSession } from '../api/wire';
import type { ConstructionSessionState } from '../api/types';

const POLL_INTERVAL_MS = 3000;

/** Stages at which no further pump activity occurs for the session. */
const TERMINAL_STAGES = new Set(['exited', 'paused']);

export function constructionSessionKey(
  projectId: string,
  activityId?: string
): readonly unknown[] {
  return ['constructionSession', projectId, activityId ?? null];
}

export function useConstructionSession(
  projectId: string,
  activityId?: string,
  enabled = true
): UseQueryResult<ConstructionSessionState> {
  const hasActivity = activityId !== undefined && activityId.length > 0;
  return useQuery<ConstructionSessionState>({
    queryKey: constructionSessionKey(projectId, activityId),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/construction/get-session-state/{projectID}/{activityID}',
        { params: { path: { projectID: projectId, activityID: activityId ?? '' } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return mapConstructionSession(data);
    },
    enabled: enabled && projectId.length > 0 && hasActivity,
    // A 404 means "no session started yet" (dormant pump) — surface without retries.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    refetchInterval: (query) => {
      // Dormant pump (last read 404'd): stop polling so the console does not spam a
      // 3s 404 storm. The project-read cascade poll drives the tracker meanwhile.
      if (query.state.error instanceof ApiError && query.state.error.status === 404) return false;
      const stage = query.state.data?.stage;
      if (stage !== undefined && TERMINAL_STAGES.has(stage)) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
