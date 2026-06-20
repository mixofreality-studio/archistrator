/**
 * The SUBPROJECT journey (`/project/$projectId/changes/$subprojectId`) — a change
 * request re-entering The Method, SCALED. In the design (ux-mock SubprojectFlow)
 * this is a triage-dependent step spine (reassess volatilities → new components →
 * design-validation gate → project design → construction → redeploy), each step
 * embedding a reused Method renderer.
 *
 * The subproject registry is NOT yet carried by the project read projection (the
 * ProjectState read has no subproject slot — verified). So this screen renders the
 * honest NOT-READY state from the design rather than inventing a subproject
 * endpoint, the same way the mock's SubprojectFlow renders NotReady for an unknown
 * / untriaged id. The full triage-spine journey lands once the subproject read is
 * provisioned.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/changes/$subprojectId');

export function SubprojectFlowScreen(): ReactNode {
  const { projectId, subprojectId } = routeApi.useParams();
  const t = useTokens();
  const navigate = useNavigate();

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Subproject.ROOT}
      sx={{
        height: '100vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 2,
        bgcolor: t.bg,
        p: 4,
      }}
    >
      <Paper
        data-testid={UI_IDENTIFIERS.Subproject.NOT_READY}
        sx={{ p: 4, maxWidth: 560, textAlign: 'center', borderStyle: 'dashed' }}
      >
        <LockOutlinedIcon sx={{ fontSize: 34, color: t.muted, mb: 1 }} />
        <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, mb: 1 }}>
          Subproject “{subprojectId}” is not available yet
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, mb: 2, lineHeight: 1.5 }}>
          A change request becomes a subproject — a fresh, scaled System-Design → Project-Design → Construction → Redeploy cycle — only after triage
          sets its disposition. The subproject registry is not carried by the project read projection yet, so its journey cannot be loaded. The full
          triage-dependent spine lands once the subproject read is provisioned.
        </Typography>
        <Button
          color="primary"
          data-testid={UI_IDENTIFIERS.Subproject.BACK}
          variant="contained"
          onClick={() => void navigate({ to: '/project/$projectId/changes', params: { projectId } })}
        >
          Back to Change requests
        </Button>
      </Paper>
    </Box>
  );
}
