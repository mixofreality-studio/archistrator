/**
 * The SPA container for the PLAN (`/project/$projectId/plan`) — the one surface
 * that replaces the construction console's three lenses (spec §7.4).
 *
 * STUB. This task (stage 5 Task 7) lands the ROUTE, so the fixtures' routes
 * resolve and the plan is navigable; Task 11 fills this in with the real
 * orchestration (the committed plan read, `planActivitiesFrom`/`planRowFor`,
 * the LIST / GRAPH / TASKS lenses and the lens toggle). It deliberately fetches
 * NOTHING and renders an honest "not built yet" body rather than a plausible
 * empty plan — a blank list would read as "this project has no activities",
 * which is a lie the preview shell would happily screenshot.
 *
 * `Plan.SCREEN` is placed HERE, at this commit, and kept by Task 11: an id
 * declared in UIIdentifiers and never placed on an element is the same failure
 * as an id asserted and never declared.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useNavigate } from '@tanstack/react-router';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { LENS_LABEL } from '../components/activity/planCopy';
import type { PlanLensId } from '../contracts/routePaths';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

export function PlanContainer({
  projectId,
  lens,
}: {
  projectId: string;
  lens: PlanLensId;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  return (
    <ExperienceChrome
      bodyScroll="shared"
      eyebrow={`PLAN · ${LENS_LABEL[lens].toUpperCase()}`}
      phaseNum={2}
      phaseTitle="Plan"
      projectName={projectId}
      onClose={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Plan.SCREEN}
        sx={{ flexGrow: 1, minWidth: 0, px: { xs: 2, md: 4 }, py: 4 }}
      >
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          The plan&apos;s {LENS_LABEL[lens]} lens is not built yet — it lands in Task 11 of the
          Activity Experience stage. This route exists now so the plan is navigable and the old
          construction and design links have somewhere to redirect to.
        </Typography>
      </Box>
    </ExperienceChrome>
  );
}
