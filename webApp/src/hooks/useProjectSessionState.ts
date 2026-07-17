/**
 * Polls one Phase-2 co-authoring (or SDP-review) session's state. Polling runs
 * every 2s while the session is live (drafting / assemblingSdp / awaitingReview /
 * redrafting) and stops at a terminal stage (committed / withdrawn / refused). The
 * Phase-2 TWIN of useSessionState.ts.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { ApiError, toApiError } from '../contracts/errors';
import { artifactKindToOrdinal, mapProjectSessionState } from '../contracts/wire';
import { PROJECT_TERMINAL_STAGES } from '../contracts/types';
import type { ProjectArtifactKind, ProjectSessionState } from '../contracts/types';
import { DEGRADED_POLL_INTERVAL_MS, isNoSessionError } from './sessionPolling';

const POLL_INTERVAL_MS = 2000;

export function projectSessionStateKey(
  projectId: string,
  kind: ProjectArtifactKind
): readonly unknown[] {
  return ['projectSessionState', projectId, kind];
}

export function useProjectSessionState(
  projectId: string,
  kind: ProjectArtifactKind,
  enabled: boolean
): UseQueryResult<ProjectSessionState> {
  return useQuery<ProjectSessionState>({
    queryKey: projectSessionStateKey(projectId, kind),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/project-design/get-session-state/{projectID}',
        { params: { path: { projectID: projectId }, query: { kind: artifactKindToOrdinal(kind) } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return mapProjectSessionState(data);
    },
    enabled: enabled && projectId.length > 0,
    // A 404 means "no session started yet" — surface it without retry storms.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    // Poll only while a live session exists. A no-session 404 or a terminal stage
    // stops the poll: otherwise a committed / not-yet-started Phase-2 artifact 404s
    // every 2s, and each refetch briefly flips the experience into its loading
    // spinner — remounting the view and resetting it (the flicker/reload). A
    // mutation (start/redraft) invalidates this query, so polling resumes once a
    // live non-terminal stage returns.
    //
    // F-QA2-28 (parallel to sessionPolling.ts): any NON-404 error must never stop
    // the poll — one no-poll decision is permanent until a mutation invalidates, so
    // a transient fault (dev-server restart mid-poll) froze a stale live view
    // forever. Degrade to 5s and self-heal instead. (Phase-2 keeps its deliberate
    // 404-stops rule — no auto-start incident here — so it does not share the
    // Phase-1 decision table wholesale.)
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      if (stage !== undefined && PROJECT_TERMINAL_STAGES.includes(stage)) return false;
      const { error } = query.state;
      if (error !== null && !isNoSessionError(error)) return DEGRADED_POLL_INTERVAL_MS;
      if (stage === undefined) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
