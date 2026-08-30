/* eslint-disable react-refresh/only-export-components -- provider + hook colocated */
/**
 * Delivery channel for the LIVE deployment-health overlay the deployment diagram
 * tints itself with (spec D10) — the StructureFindingsContext idiom, and here for
 * the same reason: `DeploymentFlow` now renders inside `ArchitectureView`, a
 * components-layer file that may not import `src/hooks` under the layer DAG
 * (eslint.platform.config.js), so the ORCHESTRATORS that may (SystemDesignContainer,
 * McpSystemDesignContainer, the HomeBase route — the same three that already mount
 * StructureFindingsProvider) own the fetch and hand the result down.
 *
 * WHY THIS EXISTS: the overlay used to be fetched by the Deployment & Operations
 * step view, which was on the LEGACY_COMPONENTS_HOOKS_FILES allowlist and so could
 * self-fetch. That step retired (2026-08-30, Phase 1 → Requirements + Architecture)
 * and its diagram moved to the Architecture step's Deployment lens. Re-homing the
 * diagram without this provider silently dropped the tinting — visible, because the
 * lens opens on the FIRST committed environment, which is `cloud`, the one and only
 * environment D10 ever colours. This restores it without touching the allowlist:
 * that list only shrinks.
 *
 * ── Absence is neutral, never unhealthy ─────────────────────────────────────
 * Every arm of "no data" collapses to the same render the diagram had before any
 * overlay existed, and none of them can read as a failing node:
 *   • no provider mounted (tests, an unwired shell) → undefined,
 *   • operations capability off (D9 — the local profile) or no operated-app id yet
 *     → useDeploymentHealth never fires (deploymentHealthQueryEnabled) → undefined,
 *   • still loading, or the read errored → TanStack `data` is undefined,
 *   • a non-`cloud` profile → DeploymentFlow's own environmentIsObservable guard
 *     discards whatever it was handed.
 * DeploymentFlow already treats `undefined`/`{}` as "colour nothing", and
 * deploymentHealth.ts's healthColorName maps an absent key to `neutral` — the
 * ordinary line colour — so a missing observation is indistinguishable from the
 * pre-overlay diagram rather than being painted red.
 */
import { createContext, useContext, type ReactNode } from 'react';
import type { HealthState } from '../../contracts/types';

const Ctx = createContext<Record<string, HealthState> | undefined>(undefined);

export function DeploymentHealthProvider({
  healthByKey,
  children,
}: {
  /** modelKey → live health (useDeploymentHealth data); undefined while loading,
   *  on error, or whenever the query is dormant — consumers then colour nothing. */
  healthByKey: Record<string, HealthState> | undefined;
  children: ReactNode;
}): ReactNode {
  return <Ctx.Provider value={healthByKey}>{children}</Ctx.Provider>;
}

/**
 * The live deployment-health overlay in scope; `undefined` when absent (no
 * provider / dormant query / loading / error). Deliberately NOT defaulted to `{}`:
 * `DeploymentFlow.healthByKey` is an optional prop whose own `?? {}` is the single
 * place that decision belongs, and passing an object through would make "no
 * provider" and "observed nothing" indistinguishable at the call site.
 */
export function useDeploymentHealthOverlay(): Record<string, HealthState> | undefined {
  return useContext(Ctx);
}
