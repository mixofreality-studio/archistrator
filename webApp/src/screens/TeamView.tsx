/**
 * Team view (`/project/$projectId/team`) — the canonical, book-named Method roles
 * archistrator supervises. Static roster content (the fixed 10 Method roles), no
 * backend wiring. Each card is a Worker playing a role; click opens a charter
 * (Owns / Does-NOT / Reviewed-by / chapter) with a secondary "View full prompt"
 * disclosure. Wrapped in the project-scoped AppShell, like Billing / Changes.
 *
 * Ported from the approved UX mock (methodpoc/designs/aiarch/ux-mock).
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Drawer from '@mui/material/Drawer';
import IconButton from '@mui/material/IconButton';
import Collapse from '@mui/material/Collapse';
import Button from '@mui/material/Button';
import CloseIcon from '@mui/icons-material/Close';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutline';
import BlockIcon from '@mui/icons-material/Block';
import RuleIcon from '@mui/icons-material/Rule';
import MenuBookOutlinedIcon from '@mui/icons-material/MenuBookOutlined';
import TerminalIcon from '@mui/icons-material/Terminal';
import { getRouteApi } from '@tanstack/react-router';
import { AppShell } from '../components/AppShell';
import { RoleAvatar } from '../components/RoleAvatar';
import { TEAM_SECTIONS, roleById, type TeamRole } from '../data/team';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/team');

export function TeamScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  return (
    <AppShell projectId={projectId}>
      <TeamBody />
    </AppShell>
  );
}

function TeamBody(): ReactNode {
  const t = useTokens();
  const [openId, setOpenId] = useState<string | null>(null);
  const role = openId !== null ? roleById(openId) : undefined;

  return (
    <>
      <Box
        data-testid={UI_IDENTIFIERS.Team.ROOT}
        sx={{ maxWidth: 1240, mx: 'auto', px: { xs: 2, md: 4 }, py: 4 }}
      >
        <Box sx={{ mb: 0.5 }}>
          <Typography sx={{ color: t.muted }} variant="overline">
            The Method · your team
          </Typography>
          <Typography sx={{ color: t.ink }} variant="h3">
            Roles on the project
          </Typography>
        </Box>
        <Typography sx={{ color: t.muted, fontSize: 14.5, lineHeight: 1.6, maxWidth: 760, mb: 3.5 }}>
          Every name below is a canonical role from Löwy&rsquo;s <em>The Method</em> — the book&rsquo;s team,
          not invented seats. Each is a <strong>Worker playing a role</strong> (the agent is just the
          implementation detail). Click a card to read its charter; the raw agent prompt is one disclosure
          deeper.
        </Typography>

        {TEAM_SECTIONS.map((section) => (
          <Box key={section.group} sx={{ mb: 5 }}>
            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1.5, mb: 0.5, flexWrap: 'wrap' }}>
              <Typography sx={{ color: t.ink }} variant="h5">
                {section.title}
              </Typography>
              <Chip
                label={section.phase}
                size="small"
                sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
              />
            </Box>
            <Typography sx={{ color: t.muted, fontSize: 13, lineHeight: 1.55, maxWidth: 800, mb: 2 }}>
              {section.blurb}
            </Typography>

            {section.subgroups.map((sg) => (
              <Box key={sg.key ?? 'all'} sx={{ mb: 2 }}>
                {sg.label !== undefined && (
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      letterSpacing: '0.16em',
                      textTransform: 'uppercase',
                      color: t.muted,
                      mb: 1,
                      pl: 0.25,
                    }}
                  >
                    {sg.label}
                  </Typography>
                )}
                <Box
                  sx={{
                    display: 'grid',
                    gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr', lg: '1fr 1fr 1fr' },
                    gap: 2,
                  }}
                >
                  {sg.roleIds.map((id) => {
                    const r = roleById(id);
                    if (r === undefined) return null;
                    return (
                      <RoleCard
                        key={id}
                        role={r}
                        t={t}
                        onOpen={() => {
                          setOpenId(id);
                        }}
                      />
                    );
                  })}
                </Box>
              </Box>
            ))}
          </Box>
        ))}
      </Box>

      <Drawer
        anchor="right"
        open={role !== undefined}
        slotProps={{
          paper: {
            sx: { width: { xs: '100%', sm: 480, md: 540 }, bgcolor: t.paper, backgroundImage: 'none' },
          },
        }}
        onClose={() => {
          setOpenId(null);
        }}
      >
        {role !== undefined && (
          <RoleCharter
            role={role}
            t={t}
            onClose={() => {
              setOpenId(null);
            }}
          />
        )}
      </Drawer>
    </>
  );
}

function RoleCard({ role, t, onOpen }: { role: TeamRole; t: Tokens; onOpen: () => void }): ReactNode {
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Team.roleCard(role.id)}
      sx={{
        p: 2,
        cursor: 'pointer',
        display: 'flex',
        gap: 1.75,
        alignItems: 'flex-start',
        transition: 'all 110ms ease',
        '&:hover': {
          boxShadow: t.hardShadow ? `4px 4px 0 ${t.shadowColor}` : `0 10px 26px ${t.shadowColor}`,
          transform: 'translateY(-2px)',
        },
      }}
      onClick={onOpen}
    >
      <RoleAvatar seed={role.id} size={72} />
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
          <Typography sx={{ color: t.ink, lineHeight: 1.15 }} variant="h6">
            {role.name}
          </Typography>
          <Chip
            label={role.chapterRef}
            size="small"
            sx={{ height: 18, fontSize: 9, bgcolor: t.paperAlt, color: t.muted, '& .MuiChip-label': { px: 0.75 } }}
          />
        </Box>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, letterSpacing: '0.04em', mb: 0.75 }}>
          {role.id} · Worker
        </Typography>
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, lineHeight: 1.45, color: t.ink }}>
          {role.oneLiner}
        </Typography>
      </Box>
    </Paper>
  );
}

function RoleCharter({ role, t, onClose }: { role: TeamRole; t: Tokens; onClose: () => void }): ReactNode {
  const [showPrompt, setShowPrompt] = useState(false);
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Team.CHARTER_DRAWER}
      sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}
    >
      {/* header */}
      <Box
        sx={{
          display: 'flex',
          alignItems: 'flex-start',
          gap: 2,
          p: 2.5,
          borderBottom: `1.5px solid ${t.line}`,
          bgcolor: t.paperAlt,
        }}
      >
        <RoleAvatar selected seed={role.id} size={84} />
        <Box sx={{ minWidth: 0, flexGrow: 1 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Typography sx={{ color: t.ink, lineHeight: 1.1 }} variant="h4">
              {role.name}
            </Typography>
            <Chip
              icon={<MenuBookOutlinedIcon sx={{ fontSize: 13 }} />}
              label={role.chapterRef}
              size="small"
              sx={{ bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            />
          </Box>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, letterSpacing: '0.04em', mt: 0.5 }}>
            {role.id} · Worker playing a Method role
          </Typography>
        </Box>
        <IconButton
          data-testid={UI_IDENTIFIERS.Team.CHARTER_CLOSE}
          size="small"
          sx={{ flexShrink: 0 }}
          onClick={onClose}
        >
          <CloseIcon fontSize="small" />
        </IconButton>
      </Box>

      {/* scroll body */}
      <Box sx={{ flexGrow: 1, overflowY: 'auto', p: 2.5 }}>
        <Box sx={{ borderLeft: `3px solid ${t.accent}`, pl: 1.5, py: 0.25, mb: 2.5 }}>
          <Typography sx={{ fontFamily: t.display, fontStyle: 'italic', fontSize: 14, color: t.ink, lineHeight: 1.5 }}>
            {role.pullQuote}
          </Typography>
        </Box>

        <Typography sx={{ fontSize: 13.5, lineHeight: 1.55, color: t.ink, mb: 2.5 }}>{role.oneLiner}</Typography>

        <CharterBlock
          icon={<CheckCircleOutlineIcon sx={{ fontSize: 16, color: t.committedDot }} />}
          items={role.charter.owns}
          label="Owns / drives"
          t={t}
        />
        <CharterBlock
          icon={<BlockIcon sx={{ fontSize: 16, color: t.dangerFg }} />}
          items={role.charter.doesNotDo}
          label="Does NOT do"
          t={t}
        />

        <Box
          sx={{
            mt: 2.5,
            p: 1.5,
            bgcolor: t.paperAlt,
            border: `1.5px solid ${t.line}`,
            borderRadius: `${String(t.radius)}px`,
          }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 0.5 }}>
            <RuleIcon sx={{ fontSize: 16, color: t.accent2 }} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.12em', textTransform: 'uppercase', color: t.muted }}>
              Reviewed by
            </Typography>
          </Box>
          <Typography sx={{ fontSize: 12.5, lineHeight: 1.5, color: t.ink }}>{role.charter.reviewedBy}</Typography>
        </Box>

        {/* SECONDARY: raw prompt disclosure */}
        <Box sx={{ mt: 3 }}>
          <Button
            fullWidth
            data-testid={UI_IDENTIFIERS.Team.TOGGLE_PROMPT}
            endIcon={
              <ExpandMoreIcon
                sx={{ fontSize: 18, transition: 'transform 150ms ease', transform: showPrompt ? 'rotate(180deg)' : 'none' }}
              />
            }
            startIcon={<TerminalIcon sx={{ fontSize: 16 }} />}
            sx={{ justifyContent: 'space-between', color: t.ink, borderColor: t.line }}
            variant="outlined"
            onClick={() => {
              setShowPrompt((s) => !s);
            }}
          >
            {showPrompt ? 'Hide full prompt' : 'View full prompt'}
          </Button>
          <Collapse unmountOnExit in={showPrompt} timeout="auto">
            <Box
              sx={{
                mt: 1.5,
                p: 2,
                bgcolor: t.bg,
                border: `1.5px solid ${t.line}`,
                borderRadius: `${String(t.radius)}px`,
                maxHeight: 360,
                overflowY: 'auto',
              }}
            >
              <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.1em', color: t.muted, mb: 1 }}>
                .claude/agents/{role.agentFile} · read-only
              </Typography>
              <Box
                component="pre"
                sx={{
                  fontFamily: t.mono,
                  fontSize: 11,
                  lineHeight: 1.55,
                  color: t.ink,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  m: 0,
                }}
              >
                {role.prompt}
              </Box>
            </Box>
          </Collapse>
        </Box>
      </Box>
    </Box>
  );
}

function CharterBlock({
  t,
  icon,
  label,
  items,
}: {
  t: Tokens;
  icon: ReactNode;
  label: string;
  items: string[];
}): ReactNode {
  return (
    <Box sx={{ mb: 2.5 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 1 }}>
        {icon}
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.12em', textTransform: 'uppercase', color: t.muted }}>
          {label}
        </Typography>
      </Box>
      <Box component="ul" sx={{ listStyle: 'none', m: 0, pl: 0, display: 'flex', flexDirection: 'column', gap: 0.85 }}>
        {items.map((it, i) => (
          <Box
            component="li"
            key={i}
            sx={{ display: 'flex', gap: 1, fontSize: 12.5, lineHeight: 1.5, color: t.ink }}
          >
            <Box sx={{ color: t.accent, flexShrink: 0, fontFamily: t.mono, fontWeight: 700 }}>▸</Box>
            <span>{it}</span>
          </Box>
        ))}
      </Box>
    </Box>
  );
}
