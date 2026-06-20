/**
 * One catalog tile on the projects landing grid: name, phase chip, committed
 * progress, and last-updated. Clicking opens the project's home base. Self
 * contained — the parent passes the summary + an open handler, never visuals.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import LinearProgress from '@mui/material/LinearProgress';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';
import GitHubIcon from '@mui/icons-material/GitHub';
import type { ProjectSummary } from '../api/types';
import { PHASE_LABELS, formatUpdatedAt } from './projectFormat';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function ProjectCard({
  project,
  onOpen,
}: {
  project: ProjectSummary;
  onOpen: () => void;
}): ReactNode {
  const t = useTokens();
  const pct = project.totalCount > 0 ? (project.committedCount / project.totalCount) * 100 : 0;
  const complete = project.totalCount > 0 && project.committedCount >= project.totalCount;
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.ProjectsLanding.projectCard(project.projectId)}
      sx={{
        p: 2.5,
        cursor: 'pointer',
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        minHeight: 170,
        transition: 'all 100ms ease',
        '&:hover': {
          boxShadow: t.hardShadow ? `4px 4px 0 ${t.shadowColor}` : `0 10px 28px ${t.shadowColor}`,
          transform: 'translateY(-2px)',
        },
      }}
      onClick={onOpen}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Chip
          label={PHASE_LABELS[project.phase]}
          size="small"
          sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
        />
        <Box sx={{ flexGrow: 1 }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          {formatUpdatedAt(project.updatedAt)}
        </Typography>
      </Box>
      {/* Name-as-identity: the project name IS the adopted GitHub repo name. */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, minWidth: 0 }}>
        <GitHubIcon sx={{ fontSize: 15, color: t.muted, flexShrink: 0 }} />
        <Typography
          noWrap
          sx={{ color: t.ink, lineHeight: 1.2, fontFamily: t.mono, fontSize: 15 }}
          title={project.name}
          variant="h6"
        >
          {project.name}
        </Typography>
      </Box>
      <Box sx={{ flexGrow: 1 }} />
      <LinearProgress
        sx={{
          height: 7,
          borderRadius: 0,
          border: `1.5px solid ${t.line}`,
          bgcolor: t.paperAlt,
          '& .MuiLinearProgress-bar': { bgcolor: complete ? t.committedDot : t.accent },
        }}
        value={pct}
        variant="determinate"
      />
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.ink }}>
          {project.committedCount}/{project.totalCount} committed
        </Typography>
        <ArrowForwardIcon sx={{ fontSize: 16, color: t.accent }} />
      </Box>
    </Paper>
  );
}
