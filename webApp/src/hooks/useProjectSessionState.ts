/**
 * Polls one Phase-2 co-authoring (or SDP-review) session's state. Polling runs
 * every 2s while the session is live (drafting / assemblingSdp / awaitingReview /
 * redrafting) and stops at a terminal stage (committed / withdrawn / refused). The
 * Phase-2 TWIN of useSessionState.ts.
 */
import { useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { toApiError } from '../contracts/errors';
import { artifactKindToOrdinal, mapProjectSessionState } from '../contracts/wire';
import { PROJECT_TERMINAL_STAGES } from '../contracts/types';
import type { ProjectArtifactKind, ProjectSessionState } from '../contracts/types';
import { DEGRADED_POLL_INTERVAL_MS, isNoSessionError, sessionProbeQueryFn } from './sessionPolling';

const POLL_INTERVAL_MS = 2000;

export function projectSessionStateKey(
  projectId: string,
  kind: ProjectArtifactKind
): readonly unknown[] {
  return ['projectSessionState', projectId, kind];
}

/**
 * The probe value: a live session view, or `null` for ESTABLISHED absence (the
 * no-session 404 resolved to a value — see sessionProbeQueryFn for why absence
 * must be data, not error: a data-less query re-enters pending on every
 * refetch, remounting the experience).
 */
export function useProjectSessionState(
  projectId: string,
  kind: ProjectArtifactKind,
  enabled: boolean
): UseQueryResult<ProjectSessionState | null> {
  const queryClient = useQueryClient();
  const key = projectSessionStateKey(projectId, kind);
  return useQuery<ProjectSessionState | null>({
    queryKey: key,
    queryFn: sessionProbeQueryFn<ProjectSessionState>({
      fetch: async () => {
        const { data, error, response } = await apiClient.GET(
          '/api/v1/project-design/get-session-state/{projectID}',
          {
            params: {
              path: { projectID: projectId },
              query: { kind: artifactKindToOrdinal(kind) },
            },
          }
        );
        if (error !== undefined) throw toApiError(response.status, error);
        return mapProjectSessionState(data);
      },
      getCached: () => queryClient.getQueryData<ProjectSessionState | null>(key),
    }),
    enabled: enabled && projectId.length > 0,
    // The no-session 404 resolves to null inside the probe (never throws), so
    // retry only ever sees real faults: one retry, no storms.
    retry: (count) => count < 1,
    // Poll only while a live session exists. Established absence (null) or a
    // terminal stage stops the poll: otherwise a committed / not-yet-started
    // Phase-2 artifact refetches every 2s for nothing. A mutation
    // (start/redraft) invalidates this query, so polling resumes once a live
    // non-terminal stage returns. (Phase-2 keeps its deliberate absence-stops
    // rule — no auto-start incident here — so it does not share the Phase-1
    // decision table wholesale.)
    //
    // F-QA2-28 (parallel to sessionPolling.ts): any NON-404 error must never stop
    // the poll — one no-poll decision is permanent until a mutation invalidates, so
    // a transient fault (dev-server restart mid-poll) froze a stale live view
    // forever. Degrade to 5s and self-heal instead.
    refetchInterval: (query) => {
      const { data } = query.state;
      if (data === null) return false;
      const stage = data?.stage;
      if (stage !== undefined && PROJECT_TERMINAL_STAGES.includes(stage)) return false;
      const { error } = query.state;
      if (error !== null && !isNoSessionError(error)) return DEGRADED_POLL_INTERVAL_MS;
      if (stage === undefined) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
