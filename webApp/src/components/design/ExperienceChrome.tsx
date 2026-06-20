/**
 * The shared full-screen design-experience shell used by BOTH the System Design
 * (Phase 1) and Project Design (Phase 2) co-author screens. Owns the chrome (NOT
 * the AppShell): an accent strip, a prominent ✕ close, the phase title, the enter
 * transition, an optional SlimSpine progress rail, the active-step body, and an
 * optional collapsible ChatRail for anchored comments.
 *
 * Extracted from DesignExperience.tsx so the two phase screens share one shell
 * rather than forking it.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import CloseIcon from '@mui/icons-material/Close';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';

import { ThemeSwitcher } from '../ThemeSwitcher';
import { SelectionPopover } from '../comments/SelectionPopover';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

export function ExperienceChrome({
  phaseNum,
  phaseTitle,
  projectName,
  onClose,
  spine,
  chat,
  chatOpen,
  onOpenChat,
  children,
}: {
  phaseNum: number;
  phaseTitle: string;
  projectName?: string | undefined;
  onClose: () => void;
  spine?: ReactNode;
  chat?: ReactNode;
  chatOpen?: boolean | undefined;
  onOpenChat?: (() => void) | undefined;
  children: ReactNode;
}): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.DesignExperience.ROOT}
      sx={{
        height: '100vh',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        bgcolor: t.bg,
        transformOrigin: 'center top',
        animation: 'enterExp 240ms cubic-bezier(0.2,0.7,0.2,1)',
        '@keyframes enterExp': {
          from: { opacity: 0, transform: 'scale(0.985) translateY(8px)' },
          to: { opacity: 1, transform: 'none' },
        },
      }}
    >
      <SelectionPopover />

      {/* experience header */}
      <Box
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
        <Tooltip title="Close — back to home base">
          <Box
            aria-label="close experience"
            data-testid={UI_IDENTIFIERS.DesignExperience.CLOSE}
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
              '&:hover': {
                boxShadow: t.hardShadow ? `1px 1px 0 ${t.shadowColor}` : 'none',
                transform: t.hardShadow ? 'translate(1px,1px)' : 'scale(1.05)',
              },
            }}
            onClick={onClose}
          >
            <CloseIcon sx={{ fontSize: 22 }} />
          </Box>
        </Tooltip>

        <Box sx={{ minWidth: 0 }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.22em', color: t.accent, lineHeight: 1 }}>
            {`PHASE ${String(phaseNum)} · EXPERIENCE`}
          </Typography>
          <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 20, color: t.ink, lineHeight: 1.15 }}>
            {phaseTitle}
          </Typography>
        </Box>

        {projectName !== undefined && (
          <Chip label={projectName} size="small" sx={{ bgcolor: t.paperAlt, color: t.ink, display: { xs: 'none', md: 'flex' } }} variant="outlined" />
        )}

        <Box sx={{ flexGrow: 1 }} />

        <ThemeSwitcher />
        {chatOpen === false && onOpenChat !== undefined && (
          <Tooltip title="Open co-author chat">
            <IconButton
              data-testid={UI_IDENTIFIERS.Chat.TOGGLE}
              size="small"
              sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }}
              onClick={onOpenChat}
            >
              <ChatBubbleOutlineIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
      </Box>

      {/* spine bar */}
      {spine !== undefined && (
        <Box sx={{ flexShrink: 0, display: 'flex', alignItems: 'center', gap: 2, px: 2.5, py: 1, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}` }}>
          <Box sx={{ flexGrow: 1, minWidth: 0, overflowX: 'auto' }}>{spine}</Box>
        </Box>
      )}

      {/* content row */}
      <Box sx={{ flexGrow: 1, minHeight: 0, display: 'flex', alignItems: 'stretch' }}>
        {children}
        {chat !== undefined && (
          <Box sx={{ width: 380, flexShrink: 0, height: '100%', borderLeft: `1.5px solid ${t.line}` }}>
            {chat}
          </Box>
        )}
      </Box>
    </Box>
  );
}
