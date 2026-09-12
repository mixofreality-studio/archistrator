/**
 * Has construction started for this project? — the Begin/Resume answer, reduced
 * from the per-activity construction-session probes (the session endpoint is the
 * SINGLE SOURCE; see useConstructionStarted).
 *
 * Why not rows or attempts: the backfill wrote 214 reconstructed attempts onto 23
 * activities that no pump ever ran. Counting them read "Resume construction" on a
 * project whose pump had never started — and because the rows arrive a beat after
 * the page, the button said "Begin" first and flipped to "Resume" ~1.1s later. A
 * session exists only when the pump actually dispatched an activity, so it is the
 * one fact that answers the question the label asks.
 *
 * Pure (no React, no fetch) so node:test can pin it; value imports would carry an
 * explicit `.ts` extension, and this module has none.
 */
import type { ConstructionSessionState } from '../contracts/types';

/**
 * - `loading`    — a probe has not answered yet; the label must not commit.
 * - `started`    — some activity has a construction session.
 * - `notStarted` — every probe answered "no session".
 * - `unknown`    — every probe settled, none found a session, and at least one
 *                  FAILED (a real fault, not the no-session 404) — so "not
 *                  started" cannot be claimed either.
 */
export type ConstructionStarted = 'loading' | 'started' | 'notStarted' | 'unknown';

/** One probe as react-query reports it: `null` is established absence (the
 *  no-session 404 resolved to a value — see sessionProbeQueryFn), `undefined`
 *  is no answer yet (or a failure with nothing cached). */
export interface SessionProbe {
  data: ConstructionSessionState | null | undefined;
  isError: boolean;
}

export function constructionStartedFrom(probes: readonly SessionProbe[]): ConstructionStarted {
  // One real session settles it, however many probes are still in flight.
  if (probes.some((p) => p.data !== undefined && p.data !== null)) return 'started';
  if (probes.some((p) => p.data === undefined && !p.isError)) return 'loading';
  if (probes.some((p) => p.isError)) return 'unknown';
  return 'notStarted';
}
