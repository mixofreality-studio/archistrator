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

/**
 * The probes that FAILED without ever answering: no view, no established absence,
 * and the query is in error (its retry spent). A probe still in its first fetch is
 * not here — it is pending, which the TASKS lens says differently. A probe that
 * answered once and errored since keeps its answer (above) and is not here either.
 */
export function erroredProbesFor(
  ids: readonly string[],
  results: readonly { data?: unknown; status?: 'pending' | 'error' | 'success' }[]
): string[] {
  return ids.filter((_, i) => {
    const r = results[i];
    return r !== undefined && r.data === undefined && r.status === 'error';
  });
}

/** What the fan-out hands the route: the answers, and which probes failed. */
export interface SessionProbes {
  sessions: SessionsById;
  errored: readonly string[];
}
