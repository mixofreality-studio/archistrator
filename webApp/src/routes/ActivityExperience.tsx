/**
 * The ACTIVITY EXPERIENCE route
 * (`/project/$projectId/activity/$activityId?task=&rev=`) — a thin composition
 * root: read the params and the search, hand them to the container. No local
 * component definitions here (see router.tsx's header).
 *
 * Both search params are OPTIONAL. Absent, the experience opens the default task
 * (spec §7.2) at its latest revision — which is why `activitySearch` DROPS a junk
 * `rev` instead of coercing it: `undefined` means "latest", `NaN` means nothing.
 */
import type { ReactNode } from 'react';
import { getRouteApi } from '@tanstack/react-router';

import { ActivityExperienceContainer } from '../containers/ActivityExperienceContainer';

const routeApi = getRouteApi('/project/$projectId/activity/$activityId');

export function ActivityExperienceScreen(): ReactNode {
  const { projectId, activityId } = routeApi.useParams();
  const { task, rev } = routeApi.useSearch();
  return (
    <ActivityExperienceContainer
      activityId={activityId}
      projectId={projectId}
      rev={rev}
      task={task}
    />
  );
}
