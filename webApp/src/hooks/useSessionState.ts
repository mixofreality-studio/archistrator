/**
 * Polls one Phase-1 co-authoring session's state. Polling runs every 2s while the
 * session is live (drafting / redrafting / awaitingReview) and stops once it
 * reaches a terminal stage (committed / withdrawn / refused / draftFailed).
 *
 * draftFailed is the async-design-job failure stage: terminal-at-the-Manager,
 * human-actionable via Retry or Withdraw — so polling stops and the SPA renders
 * the DraftFailedPanel rather than spinning.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { apiClient } from '../api/client';
import { ApiError, toApiError } from '../contracts/errors';
import { artifactKindToOrdinal, mapSessionState } from '../contracts/wire';
import type { ArtifactKind, SessionStage, SessionStateResponse } from '../contracts/types';

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

/**
 * The project-scoped prefix of every per-kind session-state query. Invalidating it
 * refetches ALL of a project's session probes at once — used by the start-design
 * mutation, which creates the first session but does not know (or track) each
 * per-kind query. Pairs with staleTime:Infinity below so a known-no-session answer
 * is cached until exactly such an action could create a session (R6).
 */
export function sessionStateProjectKey(projectId: string): readonly unknown[] {
  return ['sessionState', projectId];
}

export function useSessionState(
  projectId: string,
  kind: ArtifactKind,
  enabled: boolean
): UseQueryResult<SessionStateResponse> {
  return useQuery<SessionStateResponse>({
    queryKey: sessionStateKey(projectId, kind),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET(
        '/api/v1/system-design/get-session-state/{projectID}',
        { params: { path: { projectID: projectId }, query: { kind: artifactKindToOrdinal(kind) } } }
      );
      if (error !== undefined) throw toApiError(response.status, error);
      return mapSessionState(data);
    },
    enabled: enabled && projectId.length > 0,
    // A 404 means "no session started yet" — surface it without retry storms.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    // R6: a committed step with no live session 404s deterministically. Treat that
    // known-no-session answer as effectively permanent — cache it and never
    // re-probe on window-focus or remount. A user action that could create a
    // session (start / request-draft mutations) invalidates this query, which
    // ignores staleTime and refetches, so live polling still resumes on its own;
    // and the refetchInterval below already refuses to poll a 404 (undefined
    // stage). Live sessions keep polling because refetchInterval overrides
    // staleTime. Net effect: at most one probe per kind per page life, so the 404
    // network-log noise stops repeating.
    refetchOnWindowFocus: false,
    staleTime: Infinity,
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      // Poll only while a LIVE session sits in a non-terminal stage. If there is no
      // session data at all — a committed / not-yet-started artifact 404s, leaving
      // stage undefined — do NOT poll: otherwise we hammer a 404 every 2s forever,
      // and each refetch briefly flips the design experience to its loading spinner,
      // which unmounts and remounts the artifact view (the reload/flicker that reset
      // the diagram's local state). A user action that starts a session invalidates
      // this query, so live polling resumes on its own. On a transient error during
      // a live session React Query keeps the last good data, so `stage` stays defined
      // and polling continues.
      if (stage === undefined || TERMINAL_STAGES.includes(stage)) return false;
      return POLL_INTERVAL_MS;
    },
  });
}
