/**
 * Polls one Phase-1 co-authoring session's state. Polling runs every 2s while the
 * session is live (drafting / redrafting), watches the review gate AND the human
 * failure gates (refused / draftFailed) at the slow 8s gate cadence (awaitingReview
 * is NOT terminal — F-QA2-48; the failure gates move IN PLACE on Retry — F-QA2-50),
 * and stops only at the REST stages (committed / withdrawn).
 *
 * draftFailed is the async-design-job failure stage: terminal-at-the-Manager,
 * human-actionable via Retry or Withdraw. The SPA renders the DraftFailedPanel and
 * keeps a slow safety-net poll: a Retry resumes the SAME session server-side
 * (failed → redrafting → awaitingReview within seconds, no new CI job), so a
 * stopped poll racing the retry mutation's single invalidation refetch used to
 * freeze a stale view until a hard reload (F-QA2-50).
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
import { useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import { artifactKindToOrdinal, mapSessionState } from '../contracts/wire';
import type { ArtifactKind, SessionStateResponse } from '../contracts/types';
import type { components } from '../contracts/schema';
import { sessionPollIntervalMs, sessionProbeQueryFn } from './sessionPolling';

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

/**
 * The probe value: a live session view, or `null` for ESTABLISHED absence (the
 * server's deterministic no-session 404, resolved to a value by
 * sessionProbeQueryFn — see its doc for why absence must be data, not error).
 */
export function useSessionState(
  projectId: string,
  kind: ArtifactKind,
  enabled: boolean
): UseQueryResult<SessionStateResponse | null> {
  const { ops, transport } = useOpsClient();
  const queryClient = useQueryClient();
  const key = sessionStateKey(projectId, kind);
  return useQuery<SessionStateResponse | null>({
    queryKey: key,
    queryFn: sessionProbeQueryFn<SessionStateResponse>({
      fetch: async () => {
        const data = await ops.call<components['schemas']['SystemDesignSessionStateView']>(
          'systemDesignGetSessionState',
          {
            path: { projectID: projectId },
            query: { kind: artifactKindToOrdinal(kind) },
          }
        );
        return mapSessionState(data);
      },
      getCached: () => queryClient.getQueryData<SessionStateResponse | null>(key),
    }),
    enabled: enabled && projectId.length > 0,
    // The no-session 404 resolves to null inside the probe (never throws), so
    // retry only ever sees real faults: one retry, no storms.
    retry: (count) => count < 1,
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
    // `false` here is PERMANENT until a mutation invalidates), 8s at the FAILURE
    // gates (F-QA2-50: Retry moves them in place), and not at all once at REST
    // (committed / withdrawn).
    // On a failed refetch react-query keeps state.data (the last good view) and
    // sets state.error — this callback reads both, so a stale-but-live stage keeps
    // polling and self-heals. Established absence is the DATA value null (QA
    // 2026-07-19 REOPENED: a 404-as-error probe never held data, so every poll
    // reset the query to pending and remounted the artifact view — see
    // sessionProbeQueryFn), which polls at the gentle no-session cadence.
    refetchInterval: (query) => {
      if (transport === 'mcp') return false;
      return sessionPollIntervalMs(query.state.data, query.state.error);
    },
  });
}
