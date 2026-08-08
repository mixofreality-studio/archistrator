/**
 * Server-reported deployment capabilities (operations-argocd-deployment Task
 * 11, spec D9). The local profile holds no deployment credential and must not
 * SURFACE operations at all — not a disabled console, not a simulated one
 * (operatedRuntimeAccess binds local -> Local (dry-run), cloud -> Real; this is
 * the UI-side honesty seam over that same fact, read from GET
 * /api/v1/capabilities — see useCapabilities.ts).
 *
 * Pure — no React, no fetch — so operationsEnabled is unit-testable under
 * `node --test` with zero setup.
 */

/** The wire shape GET /api/v1/capabilities answers. */
export interface Capabilities {
  readonly operations: boolean;
}

/**
 * Whether the Operations surface should be shown. `undefined` (capabilities
 * still loading, or the read failed/never completed) is treated as HIDDEN —
 * the safe direction. A flash of an Operations tab that then vanishes once the
 * real profile is known is worse than a tab appearing a moment late; and an
 * unreachable capabilities read must never fail OPEN into showing a console
 * that then 404s against unmounted local routes.
 */
export function operationsEnabled(c: Capabilities | undefined): boolean {
  return c?.operations === true;
}
