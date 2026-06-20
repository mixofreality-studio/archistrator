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
 * renders the FULL intake idiom from the design (the explainer + the "New change
 * request" intake form) over an HONEST empty state rather than inventing an
 * endpoint — the same way the Construction console degrades when its pump is
 * dormant. The intake form is a labelled mock until the change-request read lands.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import Tooltip from '@mui/material/Tooltip';
import CloseIcon from '@mui/icons-material/Close';
import AddIcon from '@mui/icons-material/Add';
import SwapCallsOutlinedIcon from '@mui/icons-material/SwapCallsOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { useProject } from '../hooks/useProject';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/changes');

export function ChangeRequestsScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  const t = useTokens();
  const navigate = useNavigate();
  const { data: project } = useProject(projectId);
  const [intakeOpen, setIntakeOpen] = useState(false);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.ChangeRequests.ROOT}
      sx={{
        minHeight: '100vh',
        display: 'flex',
        flexDirection: 'column',
        bgcolor: t.bg,
        animation: 'enterExp 240ms cubic-bezier(0.2,0.7,0.2,1)',
        '@keyframes enterExp': { from: { opacity: 0, transform: 'scale(0.985) translateY(8px)' }, to: { opacity: 1, transform: 'none' } },
      }}
    >
      {/* experience header — same chrome as the consoles */}
      <Box sx={{ flexShrink: 0, display: 'flex', alignItems: 'center', gap: 2, px: 2, py: 1.25, bgcolor: t.paper, borderBottom: `1.5px solid ${t.line}`, borderTop: `4px solid ${t.accent}` }}>
        <Tooltip title="Close — back to home base">
          <Box
            aria-label="close experience"
            data-testid={UI_IDENTIFIERS.ChangeRequests.CLOSE}
            role="button"
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: 38,
              height: 38,
              cursor: 'pointer',
              flexShrink: 0,
              bgcolor: t.accent,
              color: t.accentText,
              border: `1.5px solid ${t.hardShadow ? t.shadowColor : t.line}`,
              borderRadius: t.radius / 8 + 0.5,
              boxShadow: t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
              transition: 'all 90ms ease',
              '&:hover': { boxShadow: t.hardShadow ? `1px 1px 0 ${t.shadowColor}` : 'none', transform: t.hardShadow ? 'translate(1px,1px)' : 'scale(1.05)' },
            }}
            onClick={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
          >
            <CloseIcon sx={{ fontSize: 22 }} />
          </Box>
        </Tooltip>

        <SwapCallsOutlinedIcon sx={{ fontSize: 26, color: t.accent, flexShrink: 0 }} />
        <Box sx={{ minWidth: 0 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.22em', color: t.accent, lineHeight: 1 }}>DOOR BACK INTO THE METHOD</Typography>
          <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 20, color: t.ink, lineHeight: 1.15 }}>Change Requests</Typography>
        </Box>

        {project?.name !== undefined && (
          <Chip label={project.name} size="small" sx={{ bgcolor: t.paperAlt, color: t.ink, display: { xs: 'none', md: 'flex' } }} variant="outlined" />
        )}
        <Box sx={{ flexGrow: 1 }} />
        <Chip label="0 in flight" size="small" sx={{ bgcolor: t.awaitingBg, color: t.awaitingFg, fontFamily: t.mono, fontWeight: 700 }} />
      </Box>

      {/* body */}
      <Box sx={{ flexGrow: 1, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
        <Box sx={{ maxWidth: 1080 }}>
          <Box sx={{ p: 1.25, bgcolor: t.chatArchitectBg, border: `1.5px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5, mb: 2.5 }}>
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.chatArchitectFg, lineHeight: 1.55 }}>
              ↩ A change request on a delivered system does NOT edit the original (now-closed) project — it spawns a <b>subproject</b>: a fresh,
              <b> scaled</b> mini System-Design → Project-Design → Construction → Redeploy cycle, forward-only off <b>main</b> under a <b>cr-NN</b> label.
              <b> Triage scales the System-Design depth</b>: a bug / scope change skips System Design; a new use case runs the <b>design-validation gate</b>.
            </Typography>
          </Box>

          {/* INTAKE */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5 }}>
            <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink }}>Intake</Typography>
            <Chip label="0" size="small" sx={{ height: 18, fontSize: 9.5, bgcolor: t.paperAlt, color: t.muted }} />
            <Box sx={{ flexGrow: 1 }} />
            <Button
              color="primary"
              data-testid={UI_IDENTIFIERS.ChangeRequests.INTAKE_OPEN}
              size="small"
              startIcon={<AddIcon sx={{ fontSize: 16 }} />}
              sx={{ py: 0.25, fontSize: 11 }}
              variant="contained"
              onClick={() => { setIntakeOpen((o) => !o); }}
            >
              New change request
            </Button>
          </Box>

          {intakeOpen ? <Paper sx={{ p: 2, mb: 2 }}>
              <TextField fullWidth label="Title" size="small" slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.ChangeRequests.INTAKE_TITLE } }} sx={{ mb: 1.5 }} />
              <TextField fullWidth multiline label="Describe the change" minRows={3} size="small" slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.ChangeRequests.INTAKE_BODY } }} sx={{ mb: 1.5 }} />
              <Box sx={{ display: 'flex', gap: 1 }}>
                <Button disabled color="primary" data-testid={UI_IDENTIFIERS.ChangeRequests.INTAKE_SUBMIT} size="small" sx={{ fontSize: 11 }} variant="contained">
                  Submit for triage
                </Button>
                <Button color="inherit" data-testid={UI_IDENTIFIERS.ChangeRequests.INTAKE_CANCEL} size="small" sx={{ fontSize: 11, color: t.ink, borderColor: t.line }} variant="outlined" onClick={() => { setIntakeOpen(false); }}>
                  Cancel
                </Button>
              </Box>
              <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, mt: 1 }}>
                The change-request intake backend is not yet provisioned — submit is disabled until the change-request read/write lands.
              </Typography>
            </Paper> : null}

          {/* honest empty state */}
          <Paper
            data-testid={UI_IDENTIFIERS.ChangeRequests.EMPTY_STATE}
            sx={{ p: 4, textAlign: 'center', borderStyle: 'dashed' }}
          >
            <SwapCallsOutlinedIcon sx={{ fontSize: 30, color: t.muted, opacity: 0.5 }} />
            <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, mt: 1 }}>No change requests yet</Typography>
            <Box sx={{ maxWidth: 520, mx: 'auto' }}>
              <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, mt: 0.5, lineHeight: 1.5 }}>
                A change request becomes a scaled subproject after triage. The change-request registry is not carried by the project read projection
                yet, so this surface is empty — open a subproject from here once the change-request read lands.
              </Typography>
            </Box>
          </Paper>
        </Box>
      </Box>
    </Box>
  );
}
