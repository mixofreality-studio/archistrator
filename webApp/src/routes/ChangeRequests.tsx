/**
 * Change Requests (`/project/$projectId/changes`) — the door from a delivered /
 * operating system back into The Method. A change request on a released system
 * does NOT edit the original (now-closed) project — it spawns a SCALED subproject
 * (System-Design → Project-Design → Construction → Redeploy), forward-only off
 * `main` under a cr-NN label.
 *
 * The change-request / subproject registry is NOT yet carried by the project read
 * projection (GET /api/v1/projects/{projectId} carries only the Phase-1/2 artifact
 * slots — verified: ProjectState has no change-request slot). So this screen
 * renders the explainer and an HONEST empty state rather than inventing an
 * endpoint — the same way the Construction console degrades when its pump is
 * dormant. The "New change request" entry is shown DISABLED with a visible
 * "coming soon" note: the old mock intake form opened fields whose submit could
 * never be pressed, which only confused. It returns once the change-request write lands.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import CloseIcon from '@mui/icons-material/Close';
import AddIcon from '@mui/icons-material/Add';
import SwapCallsOutlinedIcon from '@mui/icons-material/SwapCallsOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { useProject } from '../hooks/useDeliveryQueries';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/changes');

export function ChangeRequestsScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  const t = useTokens();
  const navigate = useNavigate();
  const { data: project } = useProject(projectId);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.ChangeRequests.ROOT}
      sx={{
        minHeight: '100vh',
        display: 'flex',
        flexDirection: 'column',
        bgcolor: t.bg,
        animation: 'enterExp 240ms cubic-bezier(0.2,0.7,0.2,1)',
        '@keyframes enterExp': {
          from: { opacity: 0, transform: 'scale(0.985) translateY(8px)' },
          to: { opacity: 1, transform: 'none' },
        },
      }}
    >
      {/* experience header — same chrome as the consoles */}
      <Box
        component="header"
        sx={{
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          gap: 2,
          px: 2,
          py: 1.25,
          bgcolor: t.paper,
          borderBottom: `1.5px solid ${t.line}`,
          borderTop: `4px solid ${t.accent}`,
        }}
      >
        <Tooltip title="Close — back to the project overview">
          <IconButton
            aria-label="Close change requests — back to the project overview"
            data-testid={UI_IDENTIFIERS.ChangeRequests.CLOSE}
            sx={{
              width: 44,
              height: 44,
              flexShrink: 0,
              bgcolor: t.accent,
              color: t.accentText,
              border: `1.5px solid ${t.hardShadow ? t.shadowColor : t.line}`,
              borderRadius: t.radius / 8 + 0.5,
              boxShadow: t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
              '&:hover': { bgcolor: t.accent2 },
              '&:focus-visible': { outline: `2px solid ${t.ink}`, outlineOffset: 2 },
            }}
            onClick={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
          >
            <CloseIcon sx={{ fontSize: 22 }} />
          </IconButton>
        </Tooltip>

        <SwapCallsOutlinedIcon aria-hidden sx={{ fontSize: 26, color: t.accent, flexShrink: 0 }} />
        <Box sx={{ minWidth: 0 }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: '0.22em',
              color: t.muted,
              lineHeight: 1,
            }}
          >
            AFTER DELIVERY
          </Typography>
          <Typography
            component="h1"
            sx={{
              fontFamily: t.display,
              fontWeight: 700,
              fontSize: 20,
              color: t.ink,
              lineHeight: 1.15,
            }}
          >
            Change Requests
          </Typography>
        </Box>

        {project?.name !== undefined && (
          <Chip
            label={project.name}
            size="small"
            sx={{ bgcolor: t.paperAlt, color: t.ink, display: { xs: 'none', md: 'flex' } }}
            variant="outlined"
          />
        )}
      </Box>

      {/* body */}
      <Box component="main" sx={{ flexGrow: 1, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
        <Box sx={{ maxWidth: 1080 }}>
          <Box
            sx={{
              p: 1.5,
              bgcolor: t.chatArchitectBg,
              border: `1.5px solid ${t.line}`,
              borderRadius: t.radius / 8 + 0.5,
              mb: 2.5,
            }}
          >
            <Typography sx={{ fontSize: 13.5, color: t.chatArchitectFg, lineHeight: 1.55 }}>
              Need to change a system that&rsquo;s already delivered? A change request doesn&rsquo;t
              reopen the finished project. Once triaged, it becomes a small follow-on project — a{' '}
              <b>subproject</b> — that runs only the steps the change needs: a bug fix or scope
              tweak goes straight to planning and building; a new use case first re-checks the
              architecture. Each subproject builds on the current code and is redeployed when done.
            </Typography>
          </Box>

          {/* INTAKE — the submit backend is not provisioned, so the entry point is shown
              disabled and SAYS so, rather than opening a form that can never be sent. */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 1.5, flexWrap: 'wrap' }}>
            <Typography
              component="h2"
              sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink }}
            >
              Requests
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            <Typography id="cr-intake-note" sx={{ fontSize: 12.5, color: t.muted }}>
              Submitting requests is coming soon
            </Typography>
            <Button
              disabled
              aria-describedby="cr-intake-note"
              color="primary"
              data-testid={UI_IDENTIFIERS.ChangeRequests.INTAKE_OPEN}
              startIcon={<AddIcon sx={{ fontSize: 16 }} />}
              variant="contained"
            >
              New change request
            </Button>
          </Box>

          {/* honest empty state */}
          <Paper
            data-testid={UI_IDENTIFIERS.ChangeRequests.EMPTY_STATE}
            sx={{ p: 4, textAlign: 'center', borderStyle: 'dashed' }}
          >
            <SwapCallsOutlinedIcon
              aria-hidden
              sx={{ fontSize: 30, color: t.muted, opacity: 0.5 }}
            />
            <Typography
              component="h3"
              sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, mt: 1 }}
            >
              No change requests yet
            </Typography>
            <Box sx={{ maxWidth: 520, mx: 'auto' }}>
              <Typography
                sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, mt: 0.5, lineHeight: 1.5 }}
              >
                When a delivered system needs a change, each request will be listed here with its
                triage decision and a link to the subproject that carries it out.
              </Typography>
            </Box>
          </Paper>
        </Box>
      </Box>
    </Box>
  );
}
