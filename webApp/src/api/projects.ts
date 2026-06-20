/**
 * Project catalog + head-state API service (project-scoped routes). One typed
 * function per server op; each returns parsed data or throws an ApiError. No
 * React / caching here — TanStack Query owns that in the hooks layer.
 */
import { apiClient, toApiError } from './client';
import type { ProjectStateWithGit, ProjectSummary } from './types';

/** GET /api/v1/projects — the authenticated owner's catalog rows (newest-first). */
export async function listProjects(): Promise<ProjectSummary[]> {
  const { data, error, response } = await apiClient.GET('/api/v1/projects');
  if (error !== undefined) throw toApiError(response.status, error);
  return data;
}

/**
 * POST /api/v1/projects — create a project. The owner is derived from the
 * authenticated principal (never a body field); the projectId is server-minted.
 * Returns the new projectId the SPA scopes its phase routes under.
 */
export async function createProject(name: string): Promise<string> {
  const { data, error, response } = await apiClient.POST('/api/v1/projects', {
    body: { name },
  });
  if (error !== undefined) throw toApiError(response.status, error);
  return data.projectId;
}

/**
 * GET /api/v1/projects/{projectId} — one project's full typed head-state.
 *
 * The wire response additionally carries the per-activity git head-state map
 * (`gitRows`, C-CW-GIT) that the generated OpenAPI schema does not model (codegen
 * gap). The runtime JSON includes it; we widen the typed result to
 * ProjectStateWithGit so the U-SPA-GIT row cluster can read `gitRows` directly.
 * When the project has no git head-state the server omits `gitRows` entirely and
 * the field is simply undefined (honest-empty).
 */
export async function getProject(projectId: string): Promise<ProjectStateWithGit> {
  const { data, error, response } = await apiClient.GET('/api/v1/projects/{projectId}', {
    params: { path: { projectId } },
  });
  if (error !== undefined) throw toApiError(response.status, error);
  // `data` is the generated ProjectState; the runtime JSON additionally carries
  // `gitRows` (codegen gap). ProjectState is assignable to ProjectStateWithGit
  // (gitRows optional), so the widening needs no assertion.
  return data;
}
