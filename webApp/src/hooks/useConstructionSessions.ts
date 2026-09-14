/**
 * The per-activity construction-session probe, fanned out over N activities (Stage
 * C). The TASKS lens asks each activity the pump started and has not finished
 * whether a human is awaited (owedWork.probeCandidatesFor — zero today, at most the
 * supervision cap while cascading), so the probe set stays tiny. Each probe is the
 * SAME query useConstructionSession runs — same key, same dormant-404-as-null
 * absence, same polling that stops at a terminal stage — so the two hooks share
 * cache entries rather than fetching twice.
 *
 * It also reports which probes FAILED without answering, and a retry for them:
 * the lens may say "Nothing needs you." only once every probe has answered
 * (owedWork.owedWorkFor's `unchecked`), and it must say which ones did not.
 */
import { useCallback } from 'react';
import { useQueries, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useOpsClient } from '../api/opsContext';
import type { ConstructionSessionState } from '../contracts/types';
import { constructionSessionKey, sessionQueryOptions } from './useConstructionSession';
import {
  erroredProbesFor,
  retryingProbesFor,
  sessionsByActivity,
  type SessionProbes,
} from './constructionSessions';

export type { SessionsById, SessionProbes } from './constructionSessions';

/**
 * The owed set's freshness cadence (review I3). The probe candidates come from the
 * project read, which otherwise polls only during a Begin cascade — so a gate on an
 * activity started later (by the sweep, another tab or MCP) stayed invisible, and a
 * probe that met the dormant 404 never asked again. While the console is mounted
 * (the TASKS badge lives in its toolbar) the project read and every absent probe
 * re-ask at this interval: modest, and at most the supervision cap of probes.
 */
export const TASKS_FRESHNESS_MS = 10_000;

export function useConstructionSessions(
  projectId: string,
  activityIds: readonly string[]
): SessionProbes & { retryErrored: () => void } {
  const queryClient = useQueryClient();
  const { ops } = useOpsClient();
  // `combine` re-runs whenever its reference changes, so it is keyed on the id
  // LIST's content: the route rebuilds the array on every 1.5s poll, and a fresh
  // reference each time would hand the owed-set derivation a new record per render.
  const idsKey = activityIds.join(' ');
  const combine = useCallback(
    (results: UseQueryResult<ConstructionSessionState | null>[]): SessionProbes => {
      const ids = idsKey.length > 0 ? idsKey.split(' ') : [];
      return {
        sessions: sessionsByActivity(ids, results),
        errored: erroredProbesFor(ids, results),
        retrying: retryingProbesFor(ids, results),
      };
    },
    [idsKey]
  );
  const probes = useQueries({
    queries: activityIds.map((id) =>
      sessionQueryOptions(queryClient, ops, projectId, id, true, TASKS_FRESHNESS_MS)
    ),
    combine,
  });
  const erroredKey = probes.errored.join(' ');
  // Refetch exactly the probes that failed — the lens's "Couldn't check N…" Retry.
  const retryErrored = useCallback((): void => {
    for (const id of erroredKey.length > 0 ? erroredKey.split(' ') : []) {
      void queryClient.refetchQueries({
        queryKey: constructionSessionKey(projectId, id),
        exact: true,
      });
    }
  }, [queryClient, projectId, erroredKey]);
  return { ...probes, retryErrored };
}
