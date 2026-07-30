/**
 * Pure resolution for the Architecture step's ?view= deep link (the use-case →
 * call-chain jump from the Core Use Cases carousel). Decides whether an explicit
 * search param should preselect the Dynamic lens + a specific dynamic view, or
 * yield to the module-memory lens persistence:
 *
 *  - an explicit param naming an existing view WINS on mount (fresh navigation);
 *  - once consumed at a location, the SAME location's remounts (background
 *    refetch — the flow-view remount gotcha) fall back to module memory, so the
 *    deep link never fights a lens the reader chose afterwards;
 *  - a new navigation carries a new history location key, so re-clicking the
 *    link re-applies even with an identical param;
 *  - blank / dangling params never apply.
 *
 * Kept React-free so the rules are unit-testable (architectureDeepLink.test.ts).
 */

export interface DeepLinkDecision {
  /** True → preselect the Dynamic lens with `key`. False → module memory rules. */
  apply: boolean;
  /** The dynamic-view key to select (only meaningful when apply is true). */
  key: string;
}

export function resolveDeepLinkView({
  viewParam,
  locationKey,
  consumedLocationKey,
  availableKeys,
}: {
  /** The ?view= search param on the system-step route ('' when absent). */
  viewParam: string;
  /** The history entry's unique key ('' when the router exposes none). */
  locationKey: string;
  /** The location key at which a deep link was last consumed (module memory). */
  consumedLocationKey: string;
  /** The current System model's dynamic-view keys. */
  availableKeys: string[];
}): DeepLinkDecision {
  const none: DeepLinkDecision = { apply: false, key: '' };
  if (viewParam.length === 0) return none;
  if (!availableKeys.includes(viewParam)) return none;
  // Same location, already consumed → this is a remount, not a navigation. A
  // blank location key can never mark consumption (best-effort: the param wins).
  if (locationKey.length > 0 && locationKey === consumedLocationKey) return none;
  return { apply: true, key: viewParam };
}
