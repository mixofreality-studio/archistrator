import type { ConstructionRow, ProjectStateWithGit } from './types';

/** Lookup helper — undefined for activities with no construction head-state (honest-empty). */
export function constructionFor(
  project: ProjectStateWithGit | undefined,
  activityId: string
): ConstructionRow | undefined {
  if (project?.constructionRows === undefined || activityId.length === 0) return undefined;
  return project.constructionRows[activityId];
}
