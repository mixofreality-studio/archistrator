/**
 * Polls one Phase-1 co-authoring session's state. Polling runs every 2s while the
 * session is live (drafting / redrafting), watches the review gate at the slow 8s
 * gate cadence (awaitingReview is NOT terminal — F-QA2-48), and stops once it
 * reaches a terminal stage (committed / withdrawn / refused / draftFailed).
 *
 * draftFailed is the async-design-job failure stage: terminal-at-the-Manager,
 * human-actionable via Retry or Withdraw — so polling stops and the SPA renders
 * the DraftFailedPanel rather than spinning.
 *
 * A no-session 404 polls GENTLY (4s) instead of stopping (QA incident 2026-07-15):
 * the phase workflow auto-starts the next step's session after a gate approve, and a
 * cached 404 left the SPA showing "Request draft" against an already-running session
 * (whose queued redraft signal later stale-consumed a failure gate). Any OTHER error
 * polls DEGRADED (5s) instead of stopping (F-QA2-28, 2026-07-16): this query is its
 * own single refresh authority, so one no-poll decision is permanent — a transient
 * fetch failure used to freeze a stale DRAFTING… view forever. The cadence rules
 * live in sessionPolling.ts (pure, unit-tested — see its decision table).
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import { ApiError } from '../contracts/errors';
import { artifactKindToOrdinal, mapSessionState } from '../contracts/wire';
import type { ArtifactKind, SessionStateResponse } from '../contracts/types';
import type { components } from '../contracts/schema';
import { sessionPollIntervalMs } from './sessionPolling';

export function sessionStateKey(projectId: string, kind: ArtifactKind): readonly unknown[] {
  return ['sessionState', projectId, kind];
}

/**
 * The project-scoped prefix of every per-kind session-state query. Invalidating it
 * refetches ALL of a project's session probes at once — used by the start-design
 * mutation and the approve decision (which auto-advances the phase workflow and
 * auto-starts the NEXT step's session server-side), neither of which knows or
 * tracks each per-kind query.
 */
export function sessionStateProjectKey(projectId: string): readonly unknown[] {
  return ['sessionState', projectId];
}

export function useSessionState(
  projectId: string,
  kind: ArtifactKind,
  enabled: boolean
): UseQueryResult<SessionStateResponse> {
  const { ops, transport } = useOpsClient();
  return useQuery<SessionStateResponse>({
    queryKey: sessionStateKey(projectId, kind),
    queryFn: async () => {
      const data = await ops.call<components['schemas']['SystemDesignSessionStateView']>(
        'systemDesignGetSessionState',
        {
          path: { projectID: projectId },
          query: { kind: artifactKindToOrdinal(kind) },
        }
      );
      return mapSessionState(data);
    },
    enabled: enabled && projectId.length > 0,
    // A 404 means "no session started yet" — surface it without retry storms.
    retry: (count, error) => !(error instanceof ApiError && error.status === 404) && count < 1,
    // No re-probe on window-focus / remount — the poll cadence below is the single
    // refresh authority (refetchInterval overrides staleTime), so the 404 probe never
    // bursts on tab switches and a live session keeps its steady 2s poll.
    refetchOnWindowFocus: false,
    staleTime: Infinity,
    // MCP context never background-polls (spec §3.4) — an MCP host drives its own
    // refresh cadence around tool calls, so a client-side poll would just be
    // redundant traffic. The SPA (REST transport) polls per sessionPolling.ts:
    // 2s while a session is LIVE, 8s while parked at the review GATE (F-QA2-48:
    // awaitingReview is not terminal — another tab or a lost-response decision
    // submit moves the stage server-side), 4s while there is NO session yet (an
    // approve auto-starts the next step's session server-side — the QA-incident
    // fix), 5s DEGRADED on any other error (F-QA2-28: a transient fault must never
    // stop the poll — with staleTime Infinity and focus-refetch off, a single
    // `false` here is PERMANENT until a mutation invalidates), and not at all once
    // terminal.
    // On a failed refetch react-query keeps state.data (the last good view) and
    // sets state.error — this callback reads both, so a stale-but-live stage keeps
    // polling and self-heals. A 404 refetch keeps the query in error state (data
    // stays undefined, status 'error'), so the view renders steadily — no loading
    // flash, no artifact-view remount.
    refetchInterval: (query) => {
      if (transport === 'mcp') return false;
      return sessionPollIntervalMs(query.state.data?.stage, query.state.error);
    },
  });
}
