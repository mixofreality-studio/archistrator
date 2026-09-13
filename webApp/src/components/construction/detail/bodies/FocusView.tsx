/**
 * THE FOCUS VIEW — the artifact at full viewport (designer renderers-placement §3).
 *
 * Not a route and not a wider drawer. A route would remount the console and drop
 * its viewport and selection store and split Approve / Send back from the
 * artifact; a wider drawer still cannot fit the code diagram at 820px and covers
 * the lens the operator came from. So: an in-route layer over the console, driven
 * by `&focus=1`, with the lens still MOUNTED underneath — selection, viewport and
 * the 1.5s poll all survive it.
 *
 *   left rail (320px) — the SAME invariant header and the SAME action bar the
 *                       pane renders, so Approve / Send back / Run stay in reach;
 *   right             — the artifact frame at full width, with its tabs.
 *
 * It is a Modal: it covers everything, so focus stays inside it. Escape and the
 * close button exit (the caller decides whether that is a history Back). Below
 * 600px the rail stacks above the artifact.
 */
import { useEffect, useState, type ReactElement, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Modal from '@mui/material/Modal';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import CloseFullscreenRoundedIcon from '@mui/icons-material/CloseFullscreenRounded';

import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';

export function FocusView({
  open,
  header,
  actionBar,
  children,
  onClose,
}: {
  open: boolean;
  header: ReactNode;
  actionBar: ReactNode;
  children: ReactNode;
  onClose: () => void;
}): ReactElement {
  const t = useTokens();
  // The close button, not the whole layer, takes focus on entry (the trap still
  // keeps Tab inside). A callback ref: the Modal's portal mounts its children a
  // render after `open`, so an effect would find no node on a deep link.
  const [closeButton, setCloseButton] = useState<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (open) closeButton?.focus();
  }, [open, closeButton]);
  // Escape exits even when focus is outside the layer (a deep link, a click on
  // the page before the portal mounted) — the Modal's own handler only hears keys
  // pressed inside it. A key an inner control already handled is left alone.
  useEffect(() => {
    if (!open) return undefined;
    const onKey = (e: KeyboardEvent): void => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      e.preventDefault();
      onClose();
    };
    document.addEventListener('keydown', onKey);
    return (): void => {
      document.removeEventListener('keydown', onKey);
    };
  }, [open, onClose]);
  return (
    <Modal disableAutoFocus disableEscapeKeyDown hideBackdrop open={open}>
      <Box
        aria-label="Focus view"
        aria-modal="true"
        data-testid={UI_IDENTIFIERS.Construction.FOCUS_VIEW}
        role="dialog"
        sx={{
          position: 'fixed',
          inset: 0,
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', sm: '320px minmax(0, 1fr)' },
          gridTemplateRows: { xs: 'auto minmax(0, 1fr)', sm: 'minmax(0, 1fr)' },
          bgcolor: t.bg,
          outline: 0,
        }}
        tabIndex={-1}
      >
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            minHeight: 0,
            maxHeight: { xs: '45vh', sm: 'none' },
            borderRight: { xs: 'none', sm: `1.5px solid ${t.line}` },
            borderBottom: { xs: `1.5px solid ${t.line}`, sm: 'none' },
            bgcolor: t.paper,
          }}
        >
          <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>{header}</Box>
          {actionBar}
        </Box>
        <Box sx={{ minWidth: 0, minHeight: 0, overflowY: 'auto', px: { xs: 1.5, sm: 3 }, py: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <Typography
              sx={{
                flexGrow: 1,
                fontFamily: t.mono,
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.08em',
                color: t.muted,
              }}
            >
              FOCUS VIEW · Esc to return
            </Typography>
            <Tooltip title="Back to the console (Esc)">
              <IconButton
                aria-label="Close focus view"
                data-testid={UI_IDENTIFIERS.Construction.FOCUS_CLOSE}
                ref={setCloseButton}
                size="small"
                sx={{ color: t.ink }}
                onClick={onClose}
              >
                <CloseFullscreenRoundedIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Box>
          {children}
        </Box>
      </Box>
    </Modal>
  );
}
