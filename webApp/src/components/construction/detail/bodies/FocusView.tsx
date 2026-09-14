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
 *                       pane renders, so Approve / Send back / Run stay in reach,
 *                       and between them what judges the artifact: the attempt's
 *                       provenance note, the "nothing links it" sentence and a
 *                       review's verdict (designer check on renderers S1, polish 1);
 *   right             — the artifact frame at full width, with its tabs, and
 *                       nothing above it.
 *
 * It is a Modal: it covers everything, so focus stays inside it. On entry focus
 * goes to the focus region's HEADING (tabIndex -1), not the close button, whose
 * tooltip otherwise showed on arrival (polish 4). Escape and the close button
 * exit (the caller decides whether that is a history Back). Below 600px the rail
 * stacks above the artifact.
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

const HEADING_ID = 'construction-focus-heading';

export function FocusView({
  open,
  header,
  rail,
  actionBar,
  children,
  onClose,
}: {
  open: boolean;
  header: ReactNode;
  /** Under the header in the rail: the note, the sentence, the verdict. */
  rail?: ReactNode;
  actionBar: ReactNode;
  children: ReactNode;
  onClose: () => void;
}): ReactElement {
  const t = useTokens();
  // The region's heading, not the whole layer and not the close button, takes
  // focus on entry (the trap still keeps Tab inside). A callback ref: the Modal's
  // portal mounts its children a render after `open`, so an effect would find no
  // node on a deep link.
  const [heading, setHeading] = useState<HTMLElement | null>(null);
  useEffect(() => {
    if (open) heading?.focus();
  }, [open, heading]);
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
        aria-labelledby={HEADING_ID}
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
          <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
            {header}
            {rail !== undefined && rail !== null ? (
              <Box
                data-testid={UI_IDENTIFIERS.Construction.FOCUS_RAIL}
                sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, px: 2, py: 1.5 }}
              >
                {rail}
              </Box>
            ) : null}
          </Box>
          {actionBar}
        </Box>
        <Box sx={{ minWidth: 0, minHeight: 0, overflowY: 'auto', px: { xs: 1.5, sm: 3 }, py: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
            <Typography
              component="h2"
              data-testid={UI_IDENTIFIERS.Construction.FOCUS_HEADING}
              id={HEADING_ID}
              ref={setHeading}
              sx={{
                flexGrow: 1,
                m: 0,
                fontFamily: t.mono,
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.08em',
                color: t.muted,
                // A programmatic landing spot (tabIndex -1), not a control: no ring.
                outline: 0,
                '&:focus, &:focus-visible': { outline: 'none', boxShadow: 'none' },
              }}
              tabIndex={-1}
            >
              FOCUS VIEW · Esc to return
            </Typography>
            <Tooltip title="Back to the console (Esc)">
              <IconButton
                aria-label="Close focus view"
                data-testid={UI_IDENTIFIERS.Construction.FOCUS_CLOSE}
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
