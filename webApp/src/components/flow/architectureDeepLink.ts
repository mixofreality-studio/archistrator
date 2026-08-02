/**
 * Pure resolution for the Architecture step's ?view=&step= deep link (the
 * use-case → call-chain jump from the Core Use Cases carousel, now landing on
 * a specific step of the chain — Task 11). Decides whether an explicit search
 * param should preselect the Dynamic lens + a specific dynamic view (+ step),
 * or yield to the module-memory lens persistence:
 *
 *  - an explicit param naming an existing view WINS on mount (fresh navigation);
 *  - once consumed at a location, the SAME location's remounts (background
 *    refetch — the flow-view remount gotcha) fall back to module memory, so the
 *    deep link never fights a lens the reader chose afterwards;
 *  - a new navigation carries a new history location key, so re-clicking the
 *    link re-applies even with an identical param;
 *  - blank / dangling params never apply;
 *  - `step` (1-based global sequence position) rides the SAME location gating
 *    as `view` — it never applies without a view that itself applies, and a
 *    non-positive-integer value degrades to undefined rather than blocking the
 *    view from applying (a bad step is just ignored, not a dead link).
 *
 * Kept React-free so the rules are unit-testable (architectureDeepLink.test.ts).
 */

export interface DeepLinkDecision {
  /** True → preselect the Dynamic lens with `key`. False → module memory rules. */
  apply: boolean;
  /** The dynamic-view key to select (only meaningful when apply is true). */
  key: string;
  /** The 1-based step to land on, when the param parses as a positive integer
   *  (only meaningful when apply is true) — undefined otherwise. */
  step: number | undefined;
}

/** A positive integer only ('0', negatives, decimals, blank, non-numeric → undefined). */
function parsePositiveStep(raw: string): number | undefined {
  const n = Number(raw);
  return Number.isInteger(n) && n > 0 ? n : undefined;
}

export function resolveDeepLinkView({
  viewParam,
  stepParam,
  locationKey,
  consumedLocationKey,
  availableKeys,
}: {
  /** The ?view= search param on the system-step route ('' when absent). */
  viewParam: string;
  /** The ?step= search param on the system-step route ('' when absent). */
  stepParam: string;
  /** The history entry's unique key ('' when the router exposes none). */
  locationKey: string;
  /** The location key at which a deep link was last consumed (module memory). */
  consumedLocationKey: string;
  /** The current System model's dynamic-view keys. */
  availableKeys: string[];
}): DeepLinkDecision {
  const none: DeepLinkDecision = { apply: false, key: '', step: undefined };
  if (viewParam.length === 0) return none;
  if (!availableKeys.includes(viewParam)) return none;
  // Same location, already consumed → this is a remount, not a navigation. A
  // blank location key can never mark consumption (best-effort: the param wins).
  if (locationKey.length > 0 && locationKey === consumedLocationKey) return none;
  return { apply: true, key: viewParam, step: parsePositiveStep(stepParam) };
}
