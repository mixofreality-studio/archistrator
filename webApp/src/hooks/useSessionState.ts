/**
 * Polls one Phase-1 co-authoring session's state. Polling runs every 2s while the
 * session is live (drafting / redrafting / awaitingReview) and stops once it
 * reaches a terminal stage (committed / withdrawn / refused / draftFailed).
 *
 * draftFailed is the async-design-job failure stage: terminal-at-the-Manager (it
 * does NOT auto-retry / keep generating), human-actionable via Retry or Withdraw —
 * so polling stops and the SPA renders the DraftFailedPanel rather than spinning.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { getSessionState } from '../api/systemDesign';
import { ApiError } from '../api/client';
import type { ArtifactKind, SessionStage, SessionStateResponse } from '../api/types';

const POLL_INTERVAL_MS = 2000;

/** Terminal session stages — no further action possible, polling stops. */
const TERMINAL_STAGES: readonly SessionStage[] = [
  'committed',
  'withdrawn',
  'refused',
  'draftFailed',
];

export function sessionStateKey(projectId: string, kind: ArtifactKind): readonly unknown[] {
  return ['sessionState', projectId, kind];
}

export function useSessionState(
  projectId: string,
  kind: ArtifactKind,
  enabled: boolean
): UseQueryResult<SessionStateResponse> {
  return useQuery<SessionStateResponse>({
    queryKey: sessionStateKey(projectId, kind),
    queryFn: () => getSessionState(projectId, kind),
    enabled: enabled && projectId.length > 0,
    // A 404 means "no session started yet" — surface it without retry storms.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      if (stage !== undefined && TERMINAL_STAGES.includes(stage)) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
