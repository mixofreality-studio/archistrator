/**
 * The ACTIVITY EXPERIENCE route
 * (`/project/$projectId/activity/$activityId?task=&rev=`) — a thin composition
 * root: read the params and the search, hand them to the container. No local
 * component definitions here (see router.tsx's header).
 *
 * Both search params are OPTIONAL. Absent, the experience opens the default task
 * (spec §7.2) at its latest revision — which is why `activitySearch` DROPS a junk
 * `rev` instead of coercing it: `undefined` means "latest", `NaN` means nothing.
 * They are passed down as REQUIRED props typed `| undefined`: "the URL named
 * none" is a value the container rules on (`selectionFor`), not a prop a caller
 * may forget to pass.
 *
 * The `CommentProvider` is mounted HERE, above the container, for the same
 * reason `SystemDesignScreen` and `ProjectDesignScreen` mount it above theirs:
 * the container itself calls `useComments()` (the staged counts the submit bar
 * renders, the anchor it disarms on a task change), and a provider it rendered
 * would be below its own hook. There is exactly ONE on this screen — the review
 * body's artifact panel enrols its anchors inside this scope.
 */
import type { ReactNode } from 'react';
import { getRouteApi } from '@tanstack/react-router';

import { CommentProvider } from '../components/comments/CommentContext';
import { ActivityExperienceContainer } from '../containers/ActivityExperienceContainer';

const routeApi = getRouteApi('/project/$projectId/activity/$activityId');

export function ActivityExperienceScreen(): ReactNode {
  const { projectId, activityId } = routeApi.useParams();
  const { task, rev } = routeApi.useSearch();
  return (
    <CommentProvider>
      <ActivityExperienceContainer
        activityId={activityId}
        projectId={projectId}
        rev={rev}
        task={task}
      />
    </CommentProvider>
  );
}
