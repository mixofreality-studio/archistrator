/**
 * The test-run file endpoint's path, built to the server handler's contract
 * (server/cmd/server/hooks.go handleTestRunFile):
 *
 *   GET /api/v1/delivery/test-run/{runID}/file?projectId=&path=
 *
 * `projectId` is not decoration: the handler asks the PDP the SAME question the
 * `testRun` project view is guarded by — query-project-view on
 * {project, projectId} — and answers 400 without it. Pure (no config, no fetch)
 * so a node test can pin the shape; testRunFiles.ts prefixes the API origin.
 */
export function testRunFilePath(projectId: string, runId: string, path: string): string {
  if (projectId.length === 0) {
    throw new Error('testRunFilePath: projectId is required (the handler answers 400 without it)');
  }
  const query = new URLSearchParams({ projectId, path });
  return `/api/v1/delivery/test-run/${encodeURIComponent(runId)}/file?${query.toString()}`;
}
