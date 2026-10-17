/**
 * One catalog tile on the projects landing grid: name, phase chip, approved-
 * artifact progress, and last-updated. The tile is one button that opens the
 * project's home base. Self
 * contained — the parent passes the summary + an open handler, never visuals.
 */
import type { MouseEvent, ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import LinearProgress from '@mui/material/LinearProgress';
import ButtonBase from '@mui/material/ButtonBase';
import ArrowForwardIcon from '@mui/icons-material/ArrowForward';
import GitHubIcon from '@mui/icons-material/GitHub';
import type { ProjectSummary } from '../contracts/types';
import { phaseLabel, formatUpdatedAt } from './projectFormat';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

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
  const phase = phaseLabel(project.phase, project.constructionComplete);
  const updated = formatUpdatedAt(project.updatedAt);
  const progress = `${String(project.committedCount)} of ${String(project.totalCount)} design artifacts committed`;
  return (
    // The whole tile is ONE real link (keyboard + screen reader reachable, and
    // middle-click opens a tab), its accessible name the sentence a sighted user
    // reads off the tile.
    <ButtonBase
      aria-label={`Open ${project.name} — ${phase}, ${progress}${updated !== '—' ? `, updated ${updated}` : ''}`}
      data-testid={UI_IDENTIFIERS.ProjectsLanding.projectCard(project.projectId)}
      href={`/project/${encodeURIComponent(project.projectId)}/home`}
      sx={{
        display: 'block',
        width: '100%',
        textAlign: 'left',
        borderRadius: `${String(t.radius)}px`,
        '&:hover > .MuiPaper-root': {
          boxShadow: t.hardShadow ? `4px 4px 0 ${t.shadowColor}` : `0 10px 28px ${t.shadowColor}`,
          transform: 'translateY(-2px)',
        },
        '&.Mui-focusVisible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
      onClick={(e: MouseEvent) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey) return;
        e.preventDefault();
        onOpen();
      }}
    >
      <Paper
        sx={{
          p: 2.5,
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          gap: 1,
          minHeight: 170,
          transition: 'all 100ms ease',
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Chip
            label={phase}
            size="small"
            sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
          />
          <Box sx={{ flexGrow: 1 }} />
          {updated !== '—' ? (
            <Typography sx={{ fontSize: 12, color: t.muted }}>Updated {updated}</Typography>
          ) : null}
        </Box>
        {/* Name-as-identity: the project name IS the adopted GitHub repo name. */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, minWidth: 0 }}>
          <GitHubIcon aria-hidden sx={{ fontSize: 15, color: t.muted, flexShrink: 0 }} />
          <Typography
            noWrap
            component="h2"
            sx={{ color: t.ink, lineHeight: 1.2, fontFamily: t.mono, fontSize: 15 }}
            title={project.name}
            variant="h6"
          >
            {project.name}
          </Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        <LinearProgress
          aria-label={progress}
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
          <Typography sx={{ fontSize: 12.5, color: t.ink }}>
            {project.committedCount}/{project.totalCount} design artifacts committed
          </Typography>
          <ArrowForwardIcon aria-hidden sx={{ fontSize: 16, color: t.accent }} />
        </Box>
      </Paper>
    </ButtonBase>
  );
}
