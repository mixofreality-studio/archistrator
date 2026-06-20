/**
 * Service-contract lookup helpers — thin, honest-empty.
 *
 * contractForComponent: direct map lookup by component name.
 * contractForActivity:  maps an activityId → component by parsing the
 *   service-contract artifact's source path ("implementation/contracts/<component>.md")
 *   from the activity's produced artifacts.  This reuses real data (constructionRows)
 *   and needs no hard-coded abbreviation map.
 */
import type { ProjectStateWithGit, ServiceContract } from './types';

/**
 * Look up a service contract by component name.
 * Returns undefined when the project has no contracts or the component is absent
 * (honest-empty — no fabricated default).
 */
export function contractForComponent(
  project: ProjectStateWithGit | undefined,
  component: string
): ServiceContract | undefined {
  if (project?.serviceContracts === undefined || component.length === 0) return undefined;
  return project.serviceContracts[component];
}

/**
 * Resolve a service contract for a given activityId by finding the activity's
 * produced service-contract artifact and parsing the component stem from its
 * source path ("implementation/contracts/<component>.md").
 *
 * This avoids duplicating any abbreviation map: the constructionRow carries the
 * artifact source path, which encodes the component name directly.
 *
 * Returns undefined when:
 *  - the activity has no constructionRow
 *  - the row has no service-contract artifact
 *  - the source path doesn't match the expected pattern
 *  - no contract exists for the resolved component
 */
export function contractForActivity(
  project: ProjectStateWithGit | undefined,
  activityId: string
): ServiceContract | undefined {
  if (project?.constructionRows === undefined || project.serviceContracts === undefined) {
    return undefined;
  }
  const row = project.constructionRows[activityId];
  if (row === undefined) return undefined;

  // Find the service-contract artifact in the produced list.
  const scArtifact = row.produced?.find((a) => a.kind === 'service-contract');
  if (scArtifact === undefined) return undefined;

  // Parse the component stem from source path: "implementation/contracts/<component>.md"
  const match = /implementation\/contracts\/([^/]+)\.md$/.exec(scArtifact.source);
  if (match?.[1] === undefined) return undefined;

  return contractForComponent(project, match[1]);
}
