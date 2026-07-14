/**
 * The full-screen System Design experience (`/project/$projectId/design/system`).
 *
 * This route is now a thin composition root: `CommentProvider →
 * SystemDesignContainer`. All the orchestration (data fetching, mutations,
 * active-step/chat/gate UI state, the CommentContext bridge) lives in
 * `containers/SystemDesignContainer`; all the rendering (chrome, spine, artifact
 * header, step body) lives in the pure `components/design/SystemDesignView`
 * (Task 8 — see both files' headers for the split rationale).
 *
 * Phase-2 (`/design/project`) reuses the same ExperienceChrome shell through
 * ProjectDesignExperience, wired to the real Phase-2 session/gate loop — it has
 * not been containerized (Task 8 is a System Design pilot only).
 */
import type { ReactNode } from 'react';
import { getRouteApi } from '@tanstack/react-router';

import { CommentProvider } from '../components/comments/CommentContext';
import { SystemDesignContainer } from '../containers/SystemDesignContainer';

export { ProjectDesignScreen } from './ProjectDesignExperience';

const systemRouteApi = getRouteApi('/project/$projectId/design/system');

// ── System Design (Phase-1) ─────────────────────────────────────────────────

export function SystemDesignScreen(): ReactNode {
  const { projectId } = systemRouteApi.useParams();
  return (
    <CommentProvider>
      <SystemDesignContainer projectId={projectId} />
    </CommentProvider>
  );
}
