/**
 * Top-level projects landing (route `/`): the owner's catalog as a themed grid of
 * project tiles, a dashed "new project" card, and a first-login empty hero. Its
 * own minimal top bar (theme switcher + account avatar) — the per-project AppShell
 * is separate. Create flows through the shared CreateProjectDialog which mints the
 * project server-side and navigates to its home base.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Skeleton from '@mui/material/Skeleton';
import AddIcon from '@mui/icons-material/Add';
import { useNavigate } from '@tanstack/react-router';
import { useProjects } from '../hooks/useProjects';
import { useUser } from '../auth/UserContext';
import { userLabel } from '../auth/userInfo';
import { ProjectCard } from '../components/ProjectCard';
import { CreateProjectDialog } from '../components/CreateProjectDialog';
import { ThemeSwitcher } from '../components/ThemeSwitcher';
import { ErrorAlert } from '../components/shared/ErrorAlert';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function ProjectsLanding(): ReactNode {
  const t = useTokens();
  const user = useUser();
  const navigate = useNavigate();
  const { data: projects, isLoading, error } = useProjects();
  const [dialogOpen, setDialogOpen] = useState(false);

  const open = (projectId: string): void => {
    void navigate({ to: '/project/$projectId/home', params: { projectId } });
  };

  return (
    <Box
      data-testid={UI_IDENTIFIERS.ProjectsLanding.SCREEN}
      sx={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      <LandingBar t={t} user={userLabel(user)} />
      <Box
        sx={{ flexGrow: 1, maxWidth: 1180, width: '100%', mx: 'auto', px: { xs: 2, md: 4 }, py: 5 }}
      >
        <ErrorAlert error={error} />
        {isLoading ? (
          <LoadingGrid />
        ) : projects?.length === 0 ? (
          <EmptyState
            t={t}
            user={userLabel(user)}
            onNew={() => {
              setDialogOpen(true);
            }}
          />
        ) : (
          <>
            <Box sx={{ display: 'flex', alignItems: 'flex-end', mb: 3, gap: 2, flexWrap: 'wrap' }}>
              <Box>
                <Typography sx={{ color: t.muted }} variant="overline">
                  Welcome back, {userLabel(user)}
                </Typography>
                <Typography sx={{ color: t.ink }} variant="h3">
                  Your projects
                </Typography>
              </Box>
              <Box sx={{ flexGrow: 1 }} />
              <Button
                data-testid={UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_BUTTON}
                size="large"
                startIcon={<AddIcon />}
                variant="contained"
                onClick={() => {
                  setDialogOpen(true);
                }}
              >
                New project
              </Button>
            </Box>
            <Box
              data-testid={UI_IDENTIFIERS.ProjectsLanding.GRID}
              sx={{
                display: 'grid',
                gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', lg: '1fr 1fr 1fr' },
                gap: 2.5,
              }}
            >
              {(projects ?? []).map((p) => (
                <ProjectCard
                  key={p.projectId}
                  project={p}
                  onOpen={() => {
                    open(p.projectId);
                  }}
                />
              ))}
              <NewCard
                t={t}
                onNew={() => {
                  setDialogOpen(true);
                }}
              />
            </Box>
          </>
        )}
      </Box>
      <CreateProjectDialog
        open={dialogOpen}
        onClose={() => {
          setDialogOpen(false);
        }}
      />
    </Box>
  );
}

function LandingBar({ t, user }: { t: Tokens; user: string }): ReactNode {
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        px: 3,
        py: 1.5,
        borderBottom: `1.5px solid ${t.line}`,
        bgcolor: t.paper,
      }}
    >
      <Box
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 16,
          border: `1.5px solid ${t.hardShadow ? t.shadowColor : t.line}`,
          bgcolor: t.accent,
          color: t.accentText,
          px: 0.9,
          py: 0.1,
          boxShadow: t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
        }}
      >
        archi⌁
      </Box>
      <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 16, color: t.ink }}>
        archistrator
      </Typography>
      <Box sx={{ flexGrow: 1 }} />
      <ThemeSwitcher />
      <Box
        data-testid={UI_IDENTIFIERS.Shell.USER_LABEL}
        sx={{
          width: 28,
          height: 28,
          borderRadius: '50%',
          bgcolor: t.accent2,
          color: t.accentText,
          border: `1.5px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 13,
          textTransform: 'uppercase',
        }}
        title={user}
      >
        {user.slice(0, 1)}
      </Box>
    </Box>
  );
}

function NewCard({ t, onNew }: { t: Tokens; onNew: () => void }): ReactNode {
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_CARD}
      sx={{
        p: 2.5,
        cursor: 'pointer',
        borderStyle: 'dashed',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 1,
        minHeight: 170,
        color: t.muted,
        '&:hover': { color: t.accent, borderColor: t.accent },
      }}
      onClick={onNew}
    >
      <AddIcon sx={{ fontSize: 28 }} />
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13 }}>
        New project
      </Typography>
    </Paper>
  );
}

function EmptyState({ t, user, onNew }: { t: Tokens; user: string; onNew: () => void }): ReactNode {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.ProjectsLanding.EMPTY_STATE}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        textAlign: 'center',
        py: 8,
        gap: 2,
      }}
    >
      <Typography sx={{ color: t.muted }} variant="overline">
        Welcome to archistrator, {user}
      </Typography>
      <Typography sx={{ color: t.ink, maxWidth: 620 }} variant="h3">
        Let&rsquo;s design your first system.
      </Typography>
      <Typography sx={{ color: t.muted, fontSize: 15, lineHeight: 1.6, maxWidth: 560 }}>
        A project walks The Method end-to-end — business alignment, requirements, volatilities,
        architecture, a planned SDP, then a supervised build. Name it, and the architect drafts the
        mission from there.
      </Typography>
      <Button
        data-testid={UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_BUTTON}
        size="large"
        startIcon={<AddIcon />}
        sx={{ mt: 1 }}
        variant="contained"
        onClick={onNew}
      >
        Start your first project
      </Button>
    </Box>
  );
}

function LoadingGrid(): ReactNode {
  return (
    <Box
      sx={{
        display: 'grid',
        gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', lg: '1fr 1fr 1fr' },
        gap: 2.5,
      }}
    >
      {[0, 1, 2, 3].map((i) => (
        <Skeleton height={170} key={i} sx={{ borderRadius: 1 }} variant="rectangular" />
      ))}
    </Box>
  );
}
