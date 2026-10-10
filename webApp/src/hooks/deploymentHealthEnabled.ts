/**
 * Pure enablement decision for the deployment-health query (useDeploymentHealth).
 * Extracted into its own module so the guard is unit-testable under `node --test`
 * without importing useDeploymentHealth.ts itself — that file pulls in the
 * apiClient (api/client.ts), which reads Vite's `import.meta.env` at module load
 * and crashes outside a Vite runtime. Same split as sessionPolling.ts /
 * useSessionState.ts.
 *
 * The query fires only when `operatedAppId` is non-empty (every profile serves
 * operations, DCT §12 B16).
 *
 * An operated app's id IS its project's id, by founder-ratified convention (one
 * operated app per project — see useDeploymentHealth.ts's module doc for the
 * reasoning). But a malformed route, a not-yet-loaded project, or a project that
 * predates this convention can still hand this an empty string, and that must
 * stay dormant rather than firing a request with a blank path parameter.
 */
export function deploymentHealthQueryEnabled(operatedAppId: string): boolean {
  return operatedAppId.length > 0;
}
