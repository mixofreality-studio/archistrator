/**
 * The pure half of useConstructionSessions — pairing N per-activity session
 * probes back to their activity ids. Plain `.ts` with no React/query import, so
 * node:test reaches it (the sessionPolling.ts pattern).
 */
import type { ConstructionSessionState } from '../contracts/types.ts';

/**
 * Per activity: the live session, `null` where the probe ESTABLISHED there is none
 * (the dormant-pump 404, which the probe resolves to a value), and ABSENT where the
 * probe has not answered yet. A query that errored after answering keeps its last
 * answer — TanStack Query's own semantics — so a transient fault does not make a
 * waiting gate blink out of the TASKS lens.
 */
export type SessionsById = Readonly<Record<string, ConstructionSessionState | null>>;

export function sessionsByActivity(
  ids: readonly string[],
  results: readonly { data?: ConstructionSessionState | null | undefined }[]
): SessionsById {
  const out: Record<string, ConstructionSessionState | null> = {};
  ids.forEach((id, i) => {
    const data = results[i]?.data;
    if (data !== undefined) out[id] = data;
  });
  return out;
}

/** The fields of one probe's query result read here (UseQueryResult, structurally). */
export interface ProbeResult {
  data?: unknown;
  /** How many of its fetches have ended in error. TanStack keeps it through a refetch. */
  errorUpdateCount?: number;
  fetchStatus?: 'fetching' | 'paused' | 'idle';
}

/**
 * The probes that FAILED without ever answering: no view, no established absence,
 * and at least one fetch that ended in error (its retry spent). A probe in its first
 * fetch has never erred — it is pending, which the TASKS lens says differently. A
 * probe that answered once and errored since keeps its answer (above) and is not
 * here either.
 *
 * NOT `status === 'error'` (designer re-check B1): TanStack resets a data-less query
 * to `pending` the moment it refetches, so on every re-ask "Couldn't check N" became
 * "Checking N…" and its Retry vanished, every ~4s. The error COUNT survives the
 * refetch, so a probe that has failed stays failed until it answers.
 */
export function erroredProbesFor(
  ids: readonly string[],
  results: readonly ProbeResult[]
): string[] {
  return ids.filter((_, i) => {
    const r = results[i];
    return r !== undefined && r.data === undefined && (r.errorUpdateCount ?? 0) > 0;
  });
}

/** The failed probes being asked again right now — the lens says "Retrying…". */
export function retryingProbesFor(
  ids: readonly string[],
  results: readonly ProbeResult[]
): string[] {
  const errored = new Set(erroredProbesFor(ids, results));
  return ids.filter((id, i) => errored.has(id) && results[i]?.fetchStatus === 'fetching');
}

/** The first re-ask after a probe has only ever failed, and the most it backs off to. */
export const ERRORED_PROBE_BACKOFF_MS = 10_000;
export const ERRORED_PROBE_BACKOFF_MAX_MS = 60_000;

/**
 * How long a probe that has only ever failed waits before asking again (designer
 * re-check B1): 10s, doubling per failed fetch, at most 60s — not the 3s live
 * cadence, which kept a failing endpoint busy and the lens blinking. Retry asks at
 * once, whatever the backoff.
 */
export function erroredProbeBackoffMs(errorUpdateCount: number): number {
  const doublings = Math.max(0, errorUpdateCount - 1);
  return Math.min(ERRORED_PROBE_BACKOFF_MS * 2 ** doublings, ERRORED_PROBE_BACKOFF_MAX_MS);
}

/** What the fan-out hands the route: the answers, which probes failed, and which of
 *  those are being asked again. */
export interface SessionProbes {
  sessions: SessionsById;
  errored: readonly string[];
  retrying: readonly string[];
}
