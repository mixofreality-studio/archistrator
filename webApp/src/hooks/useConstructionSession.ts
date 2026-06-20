/**
 * Polls the Phase-3 construction session's technical view. The Phase-3 TWIN of
 * useProjectSessionState.ts. Polling runs every 3s while the session is live and
 * stops at a terminal stage (exited / paused). A 404 (no session yet — the pump is
 * dormant) is surfaced WITHOUT retry storms so the console can render its awaiting
 * state. An optional activityId selects the per-activity child session.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { getConstructionSession, type ConstructionSessionState } from '../api/construction';
import { ApiError } from '../api/client';

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
  return useQuery<ConstructionSessionState>({
    queryKey: constructionSessionKey(projectId, activityId),
    queryFn: () => getConstructionSession(projectId, activityId),
    enabled: enabled && projectId.length > 0,
    // A 404 means "no session started yet" (dormant pump) — surface without retries.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    refetchInterval: (query) => {
      // Dormant pump (last read 404'd): stop polling entirely so the console does not
      // spam the network/console with a 3s 404 storm while awaiting a session. The
      // project-read cascade poll (ConstructionConsole) drives the tracker meanwhile;
      // a Begin re-mounts/▸invalidates and resumes this query.
      if (query.state.error instanceof ApiError && query.state.error.status === 404) return false;
      const stage = query.state.data?.stage;
      if (stage !== undefined && TERMINAL_STAGES.has(stage)) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
