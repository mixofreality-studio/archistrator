/**
 * Pure health-state → colour mapping for the deployment diagram's live overlay
 * (operations-argocd-deployment Task 12, spec D10). No React, no fetch — testable
 * under `node --test`.
 *
 * D10 is deliberately a two-state colourable domain: `Healthy` and `Unhealthy`.
 * The server (QueryDeploymentHealth, Task 10) has already decided which diagram
 * nodes are even archistrator's to grade — every node it doesn't deploy (the
 * architect's own laptop, their browser, another app's namespace) comes back
 * Neutral, and `useDeploymentHealth` drops those Neutral entries before they ever
 * reach this file. So a modelKey simply ABSENT from the caller's health map —
 * whether because the server never observed it or because it explicitly reported
 * Neutral — is `undefined` here, and `undefined` always reads neutral, never red.
 * This file does not re-derive who counts; it only colours what it is given.
 *
 * There is no amber: mid-rollout a node reads red until it settles, because the
 * founder chose glance-readability over a third visual state.
 */
import type { Tokens } from '../../utilities/theme/themes';
import type { HealthState } from '../../contracts/types';

// HealthState lives in contracts/types.ts, not here: the hooks layer
// (useDeploymentHealth.ts) needs the SAME type, and the import boundary DAG lets
// hooks reach contracts but not components (and components not reach hooks) —
// see contracts/types.ts's doc comment on HealthState for the full rationale.
// Re-exported so callers of this module keep importing it from here.
export type { HealthState };

export type HealthColorName = 'green' | 'red' | 'neutral';

/** The colour NAME for a health state — pure, theme-independent, which is what
 *  makes it testable under `node --test`. */
export function healthColorName(state: HealthState | undefined): HealthColorName {
  if (state === 'Healthy') return 'green';
  if (state === 'Unhealthy') return 'red';
  return 'neutral';
}

/** True only for the `cloud` environment — the diagram's `test` and `local`
 *  environments are never coloured (D10 constraint: only cloud is observable). */
export function environmentIsObservable(envKey: string): boolean {
  return envKey === 'cloud';
}

/**
 * Maps a health state onto the live theme's tokens — consumed by the diagram's
 * node renderers, not by the pure tests above (see {@link healthColorName}).
 * Neutral resolves to the theme's ordinary line colour, the same border colour a
 * node without any live health observation has always carried.
 */
export function healthColor(state: HealthState | undefined, t: Tokens): string {
  switch (healthColorName(state)) {
    case 'green':
      return t.bandGreen;
    case 'red':
      return t.dangerFg;
    case 'neutral':
      return t.line;
  }
}
