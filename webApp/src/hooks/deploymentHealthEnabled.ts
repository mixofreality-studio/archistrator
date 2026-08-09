/**
 * Pure enablement decision for the deployment-health query (useDeploymentHealth).
 * Extracted into its own module so the guard is unit-testable under `node --test`
 * without importing useDeploymentHealth.ts itself — that file pulls in the
 * apiClient (api/client.ts), which reads Vite's `import.meta.env` at module load
 * and crashes outside a Vite runtime. Same split as sessionPolling.ts /
 * useSessionState.ts.
 *
 * The query fires only when BOTH hold:
 *  - the operations capability is on (D9 — the local profile has no operations
 *    surface to query), and
 *  - `operatedAppId` is non-empty.
 *
 * An operated app's id IS its project's id, by founder-ratified convention (one
 * operated app per project — see useDeploymentHealth.ts's module doc for the
 * reasoning). But a malformed route, a not-yet-loaded project, or a project that
 * predates this convention can still hand this an empty string, and that must
 * stay dormant rather than firing a request with a blank path parameter.
 */
export function deploymentHealthQueryEnabled(
  operationsCapabilityEnabled: boolean,
  operatedAppId: string
): boolean {
  return operationsCapabilityEnabled && operatedAppId.length > 0;
}
