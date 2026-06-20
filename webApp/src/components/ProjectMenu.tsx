/**
 * AppShell project switcher: a button showing the current project's name, a menu
 * of the owner's projects (from useProjects) that navigates on select, and a
 * "new project" entry that opens the shared CreateProjectDialog. Bound to real
 * TanStack Router navigation — switching just routes to the chosen home base.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Menu from '@mui/material/Menu';
import MenuItem from '@mui/material/MenuItem';
import Divider from '@mui/material/Divider';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import AddIcon from '@mui/icons-material/Add';
import CheckIcon from '@mui/icons-material/Check';
import { useNavigate } from '@tanstack/react-router';
import { useProjects } from '../hooks/useProjects';
import { CreateProjectDialog } from './CreateProjectDialog';
import { PHASE_LABELS, formatUpdatedAt } from './projectFormat';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function ProjectMenu({
  projectId,
  currentName,
}: {
  projectId: string;
  currentName: string;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const { data: projects } = useProjects();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const [newOpen, setNewOpen] = useState(false);

  const goto = (id: string): void => {
    setAnchorEl(null);
    void navigate({ to: '/project/$projectId/home', params: { projectId: id } });
  };

  return (
    <>
      <Button
        data-testid={UI_IDENTIFIERS.Shell.PROJECT_MENU}
        endIcon={<ExpandMoreIcon sx={{ fontSize: 18 }} />}
        size="small"
        sx={{
          color: t.ink,
          border: `1.5px solid ${t.line}`,
          borderRadius: t.radius / 8 + 0.5,
          bgcolor: t.paperAlt,
          maxWidth: 280,
        }}
        onClick={(e) => {
          setAnchorEl(e.currentTarget);
        }}
      >
        <Box
          sx={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            fontFamily: t.body,
            fontWeight: 600,
          }}
        >
          {currentName}
        </Box>
      </Button>

      <Menu
        anchorEl={anchorEl}
        open={anchorEl !== null}
        slotProps={{ paper: { sx: { minWidth: 340 } } }}
        onClose={() => {
          setAnchorEl(null);
        }}
      >
        <Typography
          sx={{
            px: 2,
            py: 1,
            fontFamily: t.mono,
            fontSize: 11,
            letterSpacing: '0.12em',
            color: 'text.secondary',
          }}
        >
          PROJECTS
        </Typography>
        {(projects ?? []).map((p) => (
          <MenuItem
            data-testid={UI_IDENTIFIERS.Shell.projectMenuItem(p.projectId)}
            key={p.projectId}
            selected={p.projectId === projectId}
            sx={{ gap: 1, py: 1.25, alignItems: 'flex-start' }}
            onClick={() => {
              goto(p.projectId);
            }}
          >
            <Box sx={{ flexGrow: 1 }}>
              <Typography sx={{ fontWeight: 600, fontSize: 14, fontFamily: t.body }}>
                {p.name}
              </Typography>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mt: 0.25 }}>
                <Chip
                  label={PHASE_LABELS[p.phase]}
                  size="small"
                  sx={{
                    height: 18,
                    fontSize: 9.5,
                    bgcolor: t.chatArchitectBg,
                    color: t.chatArchitectFg,
                  }}
                />
                <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: 'text.secondary' }}>
                  {p.committedCount}/{p.totalCount} · {formatUpdatedAt(p.updatedAt)}
                </Typography>
              </Box>
            </Box>
            {p.projectId === projectId && <CheckIcon sx={{ fontSize: 16, mt: 0.5 }} />}
          </MenuItem>
        ))}
        <Divider />
        <MenuItem
          data-testid={UI_IDENTIFIERS.Shell.PROJECT_MENU_NEW}
          sx={{ gap: 1, py: 1.25, color: t.accent }}
          onClick={() => {
            setAnchorEl(null);
            setNewOpen(true);
          }}
        >
          <AddIcon sx={{ fontSize: 18 }} />
          <Typography sx={{ fontWeight: 700, fontFamily: t.mono, fontSize: 13 }}>
            New project
          </Typography>
        </MenuItem>
      </Menu>

      <CreateProjectDialog
        open={newOpen}
        onClose={() => {
          setNewOpen(false);
        }}
      />
    </>
  );
}
