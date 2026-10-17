/**
 * State history (`/project/$projectId/history`) — the web entry of use case
 * view-the-project-state-log (spec §12 B7): every recorded change in the project's
 * head state, newest first, each drillable into what it recorded. Wrapped in the
 * project-scoped AppShell, like Billing / Team.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { getRouteApi } from '@tanstack/react-router';
import { AppShell } from '../components/AppShell';
import { StateHistoryContainer } from '../containers/StateHistoryContainer';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/history');

export function StateHistoryScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  const t = useTokens();
  return (
    <AppShell projectId={projectId}>
      <Box
        data-testid={UI_IDENTIFIERS.StateHistory.SCREEN}
        sx={{ maxWidth: 1080, mx: 'auto', px: { xs: 2, md: 4 }, py: 4 }}
      >
        <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 2, mb: 2, flexWrap: 'wrap' }}>
          <Box>
            <Typography sx={{ color: t.muted }} variant="overline">
              Every recorded change · newest first
            </Typography>
            <Typography component="h1" sx={{ color: t.ink }} variant="h3">
              State history
            </Typography>
          </Box>
        </Box>
        <StateHistoryContainer projectId={projectId} />
      </Box>
    </AppShell>
  );
}
