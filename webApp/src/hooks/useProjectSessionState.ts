/**
 * Polls one Phase-2 co-authoring (or SDP-review) session's state. Polling runs
 * every 2s while the session is live (drafting / assemblingSdp / awaitingReview /
 * redrafting) and stops at a terminal stage (committed / withdrawn / refused). The
 * Phase-2 TWIN of useSessionState.ts.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { getProjectSessionState } from '../api/projectDesign';
import { ApiError } from '../api/client';
import { PROJECT_TERMINAL_STAGES } from '../api/types';
import type { ProjectArtifactKind, ProjectSessionState } from '../api/types';

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
    queryFn: () => getProjectSessionState(projectId, kind),
    enabled: enabled && projectId.length > 0,
    // A 404 means "no session started yet" — surface it without retry storms.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      if (stage !== undefined && PROJECT_TERMINAL_STAGES.includes(stage)) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
