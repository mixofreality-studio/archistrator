/**
 * Per-project app shell: a sticky top bar with the brand (→ projects landing), a
 * ProjectMenu switcher, a current-project phase indicator, the project's section
 * nav (Overview · Plan · Team · Change requests · Billing · Operations), the theme
 * switcher, an account menu (which carries the sections below lg), and the
 * dev-auth / sign-out affordances (GTD parity).
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
import ListSubheader from '@mui/material/ListSubheader';
import HomeOutlinedIcon from '@mui/icons-material/HomeOutlined';
import SwapHorizIcon from '@mui/icons-material/SwapHoriz';
import SwapCallsOutlinedIcon from '@mui/icons-material/SwapCallsOutlined';
import ReceiptLongOutlinedIcon from '@mui/icons-material/ReceiptLongOutlined';
import GroupsOutlinedIcon from '@mui/icons-material/GroupsOutlined';
import LogoutIcon from '@mui/icons-material/Logout';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import MonitorHeartOutlinedIcon from '@mui/icons-material/MonitorHeartOutlined';
import Button from '@mui/material/Button';
import Tooltip from '@mui/material/Tooltip';
import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useUser } from '../utilities/auth/UserContext';
import { userLabel } from '../utilities/auth/userInfo';
import { useProject } from '../hooks/useDeliveryQueries';
import { useOperatedAppId } from '../hooks/useOperatedAppId';
import { PLAN_PATH, planSearch } from '../contracts/routePaths';
import { config } from '../utilities/config';
import { phaseLabel } from './projectFormat';
import { ProjectMenu } from './ProjectMenu';
import { ThemeSwitcher } from './ThemeSwitcher';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/** The account menu's copy of each section keeps the ids it always had. */
const MENU_TESTID: Readonly<Record<string, string>> = {
  home: UI_IDENTIFIERS.Shell.ACCOUNT_MENU_HOME,
  team: UI_IDENTIFIERS.Shell.ACCOUNT_MENU_TEAM,
  changes: UI_IDENTIFIERS.Shell.ACCOUNT_MENU_CHANGES,
  billing: UI_IDENTIFIERS.Shell.ACCOUNT_MENU_BILLING,
};

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
  const operatedAppId = useOperatedAppId(projectId);
  const pathname = useRouterState({ select: (st) => st.location.pathname });

  // The project's sections, as ONE visible nav (they used to hide in the avatar
  // menu, where nobody looks for "Plan" or "Billing"). Below lg the same entries
  // stay in the account menu, so a phone still reaches every one.
  const sections: {
    key: string;
    label: string;
    icon: ReactNode;
    testid?: string;
    active: boolean;
    go: () => void;
  }[] = [
    {
      key: 'home',
      label: 'Overview',
      icon: <HomeOutlinedIcon sx={{ fontSize: 18 }} />,
      active: pathname.endsWith('/home'),
      go: () => void navigate({ to: '/project/$projectId/home', params: { projectId } }),
    },
    {
      key: 'plan',
      label: 'Plan',
      icon: <AccountTreeOutlinedIcon sx={{ fontSize: 18 }} />,
      active: pathname.endsWith('/plan') || pathname.includes('/activity/'),
      go: () =>
        void navigate({ to: PLAN_PATH, params: { projectId }, search: () => planSearch('list') }),
    },
    {
      key: 'team',
      label: 'Team',
      icon: <GroupsOutlinedIcon sx={{ fontSize: 18 }} />,
      testid: UI_IDENTIFIERS.Shell.TEAM_NAV,
      active: pathname.endsWith('/team'),
      go: () => void navigate({ to: '/project/$projectId/team', params: { projectId } }),
    },
    {
      key: 'changes',
      label: 'Change requests',
      icon: <SwapCallsOutlinedIcon sx={{ fontSize: 18 }} />,
      active: pathname.includes('/changes'),
      go: () => void navigate({ to: '/project/$projectId/changes', params: { projectId } }),
    },
    {
      key: 'billing',
      label: 'Billing',
      icon: <ReceiptLongOutlinedIcon sx={{ fontSize: 18 }} />,
      active: pathname.endsWith('/billing'),
      go: () => void navigate({ to: '/project/$projectId/billing', params: { projectId } }),
    },
    ...(operatedAppId !== undefined
      ? [
          {
            key: 'operations',
            label: 'Operations',
            icon: <MonitorHeartOutlinedIcon sx={{ fontSize: 18 }} />,
            active: pathname.startsWith('/operations/'),
            go: () =>
              void navigate({ to: '/operations/$operatedAppId', params: { operatedAppId } }),
          },
        ]
      : []),
  ];

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Shell.APP_SHELL}
      sx={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}
    >
      <AppBar data-testid={UI_IDENTIFIERS.Shell.APP_BAR} position="sticky">
        <Toolbar sx={{ gap: 1.5, minHeight: 56 }}>
          <Box
            aria-label="archistrator — all projects"
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
              minHeight: 48,
              borderRadius: 1,
              '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
            }}
            onClick={() => {
              void navigate({ to: '/' });
            }}
          >
            <Box
              aria-hidden
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
              aria-hidden
              sx={{
                fontFamily: t.display,
                fontWeight: 700,
                fontSize: 16,
                color: t.ink,
                letterSpacing: '-0.01em',
                display: { xs: 'none', xl: 'block' },
              }}
            >
              archistrator
            </Typography>
          </Box>

          <Box sx={{ width: '1px', height: 22, bgcolor: t.line, flexShrink: 0 }} />
          <ProjectMenu currentName={name} projectId={projectId} />
          {project !== undefined && (
            <Tooltip title="The phase of The Method this project is in now">
              <Chip
                aria-label={`Current phase: ${phaseLabel(project.phase, project.operating)}`}
                label={phaseLabel(project.phase, project.operating)}
                size="small"
                sx={{
                  display: { xs: 'none', md: 'flex' },
                  bgcolor: t.chatArchitectBg,
                  color: t.chatArchitectFg,
                }}
                tabIndex={0}
              />
            </Tooltip>
          )}

          <Box
            aria-label="Project sections"
            component="nav"
            sx={{ display: { xs: 'none', lg: 'flex' }, alignItems: 'center', gap: 0.25, ml: 0.5 }}
          >
            {sections.map((sec) => (
              <Button
                aria-current={sec.active ? 'page' : undefined}
                data-testid={sec.testid}
                key={sec.key}
                size="small"
                startIcon={sec.icon}
                sx={{
                  minHeight: 40,
                  px: 1.25,
                  color: sec.active ? t.ink : t.muted,
                  fontWeight: sec.active ? 700 : 500,
                  textTransform: 'none',
                  whiteSpace: 'nowrap',
                  borderRadius: 1,
                  bgcolor: sec.active ? t.paperAlt : 'transparent',
                  boxShadow: sec.active ? `inset 0 -2px 0 ${t.accent}` : 'none',
                  '&:hover': { bgcolor: t.paperAlt, color: t.ink },
                }}
                variant="text"
                onClick={sec.go}
              >
                {sec.label}
              </Button>
            ))}
          </Box>

          <Box sx={{ flexGrow: 1 }} />

          <ThemeSwitcher />
          {config.authMode === 'dev' && (
            <Tooltip title="Signed in with local development credentials — not a real account. Nothing here is billed.">
              <Chip
                data-testid={UI_IDENTIFIERS.Shell.DEV_MODE_BADGE}
                label="Dev sign-in"
                size="small"
                sx={{
                  display: { xs: 'none', sm: 'flex' },
                  bgcolor: t.awaitingBg,
                  color: t.awaitingFg,
                  border: `1px solid ${t.awaitingFg}`,
                  fontWeight: 700,
                }}
                tabIndex={0}
              />
            </Tooltip>
          )}

          <IconButton
            aria-controls={acct !== null ? 'account-menu' : undefined}
            aria-expanded={acct !== null ? 'true' : undefined}
            aria-haspopup="menu"
            aria-label={`Account and navigation menu — signed in as ${label}`}
            data-testid={UI_IDENTIFIERS.Shell.ACCOUNT_MENU_BUTTON}
            sx={{ p: 0.75 }}
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
            id="account-menu"
            open={acct !== null}
            slotProps={{ paper: { sx: { minWidth: 200 } } }}
            onClose={() => {
              setAcct(null);
            }}
          >
            <ListSubheader
              data-testid={UI_IDENTIFIERS.Shell.USER_LABEL}
              role="presentation"
              sx={{ fontWeight: 700, fontFamily: t.body, color: t.ink, lineHeight: 2.5 }}
            >
              Signed in as {label}
            </ListSubheader>
            <Divider />
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
            {/* Below lg the top-bar nav is hidden, so its sections live here. */}
            <Divider sx={{ display: { lg: 'none' } }} />
            {sections.map((sec) => (
              <MenuItem
                data-testid={MENU_TESTID[sec.key]}
                key={sec.key}
                selected={sec.active}
                sx={{ display: { lg: 'none' } }}
                onClick={() => {
                  setAcct(null);
                  sec.go();
                }}
              >
                <ListItemIcon>{sec.icon}</ListItemIcon>
                {sec.label}
              </MenuItem>
            ))}
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
