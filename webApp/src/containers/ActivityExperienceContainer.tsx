/**
 * The SPA container for the ACTIVITY EXPERIENCE
 * (`/project/$projectId/activity/$activityId?task=&rev=`) — one activity's
 * whole lifecycle on one full-screen surface (spec §7.2).
 *
 * STUB. This task (stage 5 Task 7) lands the ROUTE, so the fixtures' routes
 * resolve and an activity is navigable; Tasks 8–10 fill this in (the lifecycle
 * graph, the task header, the dispatch and review bodies, the artifact panel
 * and the read-only revision history). It fetches NOTHING and says so, rather
 * than rendering a plausible-but-empty lifecycle.
 *
 * `Activity.SCREEN` is placed HERE, at this commit, and kept by Task 8 — a
 * declared-but-unplaced id is a Task-12 failure.
 *
 * `phaseNum` is ExperienceChrome's required prop for the DEFAULT eyebrow only;
 * an activity is not a phase, so the eyebrow is passed explicitly (Task 8
 * swaps this literal for `activityCopy.eyebrowFor` once the activity's type
 * and plan index are read).
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useNavigate } from '@tanstack/react-router';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

export function ActivityExperienceContainer({
  projectId,
  activityId,
  task,
  rev,
}: {
  projectId: string;
  activityId: string;
  task?: string | undefined;
  rev?: number | undefined;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const addressed =
    task === undefined
      ? 'the default task at its latest revision'
      : `task ${task}${rev === undefined ? '' : ` at revision ${String(rev)}`}`;
  return (
    <ExperienceChrome
      bodyScroll="shared"
      eyebrow={`${activityId.toUpperCase()} · ACTIVITY`}
      phaseNum={3}
      phaseTitle={activityId}
      projectName={projectId}
      onClose={() => void navigate({ to: '/project/$projectId/plan', params: { projectId } })}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Activity.SCREEN}
        sx={{ flexGrow: 1, minWidth: 0, px: { xs: 2, md: 4 }, py: 4 }}
      >
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          The Activity Experience is not built yet — it lands in Tasks 8–10 of the Activity
          Experience stage. This route exists now so the activity is navigable; it addresses{' '}
          {addressed}.
        </Typography>
      </Box>
    </ExperienceChrome>
  );
}
