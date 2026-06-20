/**
 * Per-project app shell: a sticky top bar with the brand (→ projects landing), a
 * ProjectMenu switcher, a current-project name/phase indicator, the theme
 * switcher, an account menu, and the dev-auth / sign-out affordances (GTD parity).
 * Wraps a project-scoped screen passed as children. The current project is read
 * from useProject(projectId) so the indicator + menu reflect live head-state.
 */
import { useState, type ReactNode } from 'react';
import AppBar from '@mui/material/AppBar';
import Toolbar from '@mui/material/Toolbar';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Menu from '@mui/material/Menu';
import MenuItem from '@mui/material/MenuItem';
import Divider from '@mui/material/Divider';
import IconButton from '@mui/material/IconButton';
import ListItemIcon from '@mui/material/ListItemIcon';
import HomeOutlinedIcon from '@mui/icons-material/HomeOutlined';
import SwapHorizIcon from '@mui/icons-material/SwapHoriz';
import SwapCallsOutlinedIcon from '@mui/icons-material/SwapCallsOutlined';
import ReceiptLongOutlinedIcon from '@mui/icons-material/ReceiptLongOutlined';
import GroupsOutlinedIcon from '@mui/icons-material/GroupsOutlined';
import LogoutIcon from '@mui/icons-material/Logout';
import { useNavigate } from '@tanstack/react-router';
import { useUser } from '../auth/UserContext';
import { userLabel } from '../auth/userInfo';
import { useProject } from '../hooks/useProject';
import { config } from '../config';
import { PHASE_LABELS } from './projectFormat';
import { ProjectMenu } from './ProjectMenu';
import { ThemeSwitcher } from './ThemeSwitcher';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function AppShell({
  projectId,
  children,
}: {
  projectId: string;
  children: ReactNode;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const user = useUser();
  const { data: project } = useProject(projectId);
  const [acct, setAcct] = useState<HTMLElement | null>(null);

  const name = project?.name ?? '…';
  const label = userLabel(user);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Shell.APP_SHELL}
      sx={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      <AppBar data-testid={UI_IDENTIFIERS.Shell.APP_BAR} position="sticky">
        <Toolbar sx={{ gap: 1.5, minHeight: 56 }}>
          <Box
            component="button"
            data-testid={UI_IDENTIFIERS.Shell.BRAND}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              mr: 0.5,
              border: 0,
              bgcolor: 'transparent',
              cursor: 'pointer',
              p: 0,
            }}
            onClick={() => {
              void navigate({ to: '/' });
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
            <Typography
              sx={{
                fontFamily: t.display,
                fontWeight: 700,
                fontSize: 16,
                color: t.ink,
                letterSpacing: '-0.01em',
                display: { xs: 'none', sm: 'block' },
              }}
            >
              archistrator
            </Typography>
          </Box>

          <Box sx={{ width: '1px', height: 22, bgcolor: t.line, flexShrink: 0 }} />
          <ProjectMenu currentName={name} projectId={projectId} />
          {project !== undefined && (
            <Chip
              label={PHASE_LABELS[project.phase]}
              size="small"
              sx={{
                display: { xs: 'none', md: 'flex' },
                bgcolor: t.chatArchitectBg,
                color: t.chatArchitectFg,
              }}
            />
          )}

          <Box
            component="button"
            data-testid={UI_IDENTIFIERS.Shell.TEAM_NAV}
            sx={{
              display: { xs: 'none', sm: 'flex' },
              alignItems: 'center',
              gap: 0.5,
              ml: 0.5,
              px: 1,
              py: 0.4,
              border: 0,
              cursor: 'pointer',
              borderRadius: 1,
              bgcolor: 'transparent',
              color: t.ink,
              fontFamily: t.mono,
              fontSize: 12,
              fontWeight: 700,
              letterSpacing: '0.02em',
              '&:hover': { bgcolor: t.paperAlt },
            }}
            onClick={() => {
              void navigate({ to: '/project/$projectId/team', params: { projectId } });
            }}
          >
            <GroupsOutlinedIcon sx={{ fontSize: 16 }} />
            Team
          </Box>

          <Box sx={{ flexGrow: 1 }} />

          <ThemeSwitcher />
          {config.authMode === 'dev' && (
            <Chip
              color="warning"
              data-testid={UI_IDENTIFIERS.Shell.DEV_MODE_BADGE}
              label="DEV AUTH"
              size="small"
            />
          )}

          <IconButton
            data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_BUTTON}
            size="small"
            sx={{ p: 0.25 }}
            onClick={(e) => {
              setAcct(e.currentTarget);
            }}
          >
            <Box
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
            >
              {label.slice(0, 1)}
            </Box>
          </IconButton>
          <Menu
            anchorEl={acct}
            open={acct !== null}
            slotProps={{ paper: { sx: { minWidth: 200 } } }}
            onClose={() => {
              setAcct(null);
            }}
          >
            <Box sx={{ px: 2, py: 1 }}>
              <Typography
                data-testid={UI_IDENTIFIERS.Shell.USER_LABEL}
                sx={{ fontWeight: 700, fontFamily: t.body }}
              >
                {label}
              </Typography>
            </Box>
            <Divider />
            <MenuItem
              data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_HOME}
              onClick={() => {
                setAcct(null);
                void navigate({ to: '/project/$projectId/home', params: { projectId } });
              }}
            >
              <ListItemIcon>
                <HomeOutlinedIcon fontSize="small" />
              </ListItemIcon>
              Home base
            </MenuItem>
            <MenuItem
              data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_ALL_PROJECTS}
              onClick={() => {
                setAcct(null);
                void navigate({ to: '/' });
              }}
            >
              <ListItemIcon>
                <SwapHorizIcon fontSize="small" />
              </ListItemIcon>
              All projects
            </MenuItem>
            <MenuItem
              data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_TEAM}
              onClick={() => {
                setAcct(null);
                void navigate({ to: '/project/$projectId/team', params: { projectId } });
              }}
            >
              <ListItemIcon>
                <GroupsOutlinedIcon fontSize="small" />
              </ListItemIcon>
              Team
            </MenuItem>
            <Divider />
            <MenuItem
              data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_CHANGES}
              onClick={() => {
                setAcct(null);
                void navigate({ to: '/project/$projectId/changes', params: { projectId } });
              }}
            >
              <ListItemIcon>
                <SwapCallsOutlinedIcon fontSize="small" />
              </ListItemIcon>
              Change requests
            </MenuItem>
            <MenuItem
              data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_BILLING}
              onClick={() => {
                setAcct(null);
                void navigate({ to: '/project/$projectId/billing', params: { projectId } });
              }}
            >
              <ListItemIcon>
                <ReceiptLongOutlinedIcon fontSize="small" />
              </ListItemIcon>
              Billing
            </MenuItem>
            {config.authMode === 'keycloak' && (
              <>
                <Divider />
                <MenuItem
                  data-testid={UI_IDENTIFIERS.Shell.LOGOUT_BUTTON}
                  onClick={() => {
                    window.location.href = '/logout';
                  }}
                >
                  <ListItemIcon>
                    <LogoutIcon fontSize="small" />
                  </ListItemIcon>
                  Sign out
                </MenuItem>
              </>
            )}
          </Menu>
        </Toolbar>
      </AppBar>
      <Box component="main" sx={{ flexGrow: 1 }}>
        {children}
      </Box>
    </Box>
  );
}
