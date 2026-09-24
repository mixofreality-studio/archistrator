/**
 * The PLAN route (`/project/$projectId/plan?lens=list|graph|tasks`) — a thin
 * composition root, exactly like DesignExperience.tsx: read the params and the
 * search, hand them to the container. No local component definitions (router.tsx's
 * header bars them for fast refresh, and the same rule holds for every route file).
 *
 * The route api is built from the PATH LITERAL at module scope, never from a
 * generic, so TanStack's module augmentation resolves the params and search types
 * against the real route tree.
 */
import type { ReactNode } from 'react';
import { getRouteApi } from '@tanstack/react-router';

import { PlanContainer } from '../containers/PlanContainer';

const routeApi = getRouteApi('/project/$projectId/plan');

export function PlanScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  // `lens` is always present: the route's validateSearch emits it even for the
  // default `list` (contracts/routePaths.planSearch).
  const { lens } = routeApi.useSearch();
  return <PlanContainer lens={lens ?? 'list'} projectId={projectId} />;
}
