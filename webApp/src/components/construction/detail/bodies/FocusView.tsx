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
 * THE RAIL COLLAPSES (designer check on renderers S3). With it open the code
 * diagram needs a window of about 1712px, so a 14" MacBook at 1512 never saw it;
 * collapsed, the artifact column is the window less its padding, and the diagram
 * draws from about 1392 (focusRail.ts). The choice is per viewer, remembered in
 * this browser. Collapsed, nothing that judges the artifact is lost: the action
 * bar moves under the artifact, and a review's verdict rides the header as one
 * chip (`collapsedSummary`) that opens the rail again. Below 600px the rail
 * stacks above the artifact and does not collapse.
 *
 * It is a Modal: it covers everything, so focus stays inside it. On entry focus
 * goes to the focus region's HEADING (tabIndex -1), not the close button, whose
 * tooltip otherwise showed on arrival (polish 4). Escape and the close button
 * exit (the caller decides whether that is a history Back).
 */
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Modal from '@mui/material/Modal';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import CloseFullscreenRoundedIcon from '@mui/icons-material/CloseFullscreenRounded';
import KeyboardDoubleArrowLeftRoundedIcon from '@mui/icons-material/KeyboardDoubleArrowLeftRounded';
import KeyboardDoubleArrowRightRoundedIcon from '@mui/icons-material/KeyboardDoubleArrowRightRounded';

import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { FOCUS_RAIL_WIDTH, readRailCollapsed, writeRailCollapsed } from '../../focusRail.ts';
import { FocusRailContext, type FocusRailState } from '../../FocusRailContext.ts';

const HEADING_ID = 'construction-focus-heading';
const RAIL_ID = 'construction-focus-side-panel';

export function FocusView({
  open,
  header,
  rail,
  actionBar,
  children,
  onClose,
  collapsedSummary,
}: {
  open: boolean;
  header: ReactNode;
  /** Under the header in the rail: the note, the sentence, the verdict. */
  rail?: ReactNode;
  actionBar: ReactNode;
  children: ReactNode;
  onClose: () => void;
  /** In the header while the rail is collapsed: what must stay in sight (a verdict chip). */
  collapsedSummary?: ReactNode;
}): ReactElement {
  const t = useTokens();
  const theme = useTheme();
  // The rail collapses only where it sits beside the artifact (sm and up).
  const collapsible = useMediaQuery(theme.breakpoints.up('sm'), { noSsr: true });
  const [collapsedPref, setCollapsedPref] = useState<boolean>(() => readRailCollapsed());
  const collapsed = collapsible && collapsedPref;
  const setCollapsed = useCallback((next: boolean): void => {
    setCollapsedPref(next);
    writeRailCollapsed(next);
  }, []);
  const railState = useMemo(
    (): FocusRailState => ({ collapsed, collapsible, setCollapsed }),
    [collapsed, collapsible, setCollapsed]
  );
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
    <FocusRailContext.Provider value={railState}>
      <Modal disableAutoFocus disableEscapeKeyDown hideBackdrop open={open}>
        <Box
          aria-labelledby={HEADING_ID}
          aria-modal="true"
          data-rail={collapsed ? 'collapsed' : 'open'}
          data-testid={UI_IDENTIFIERS.Construction.FOCUS_VIEW}
          role="dialog"
          sx={{
            position: 'fixed',
            inset: 0,
            display: 'grid',
            gridTemplateColumns: collapsed
              ? 'minmax(0, 1fr)'
              : { xs: '1fr', sm: `${String(FOCUS_RAIL_WIDTH)}px minmax(0, 1fr)` },
            gridTemplateRows: collapsed
              ? 'minmax(0, 1fr)'
              : { xs: 'auto minmax(0, 1fr)', sm: 'minmax(0, 1fr)' },
            bgcolor: t.bg,
            outline: 0,
          }}
          tabIndex={-1}
        >
          <Box
            id={RAIL_ID}
            sx={{
              // Collapsed, the rail stays mounted (its disclosure and attempt
              // pick survive a toggle) but takes no room at all.
              display: collapsed ? 'none' : 'flex',
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
            {collapsed ? null : actionBar}
          </Box>
          <Box sx={{ minWidth: 0, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
            <Box
              sx={{
                flexGrow: 1,
                minWidth: 0,
                minHeight: 0,
                overflowY: 'auto',
                px: { xs: 1.5, sm: 3 },
                py: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                {collapsible ? (
                  <Tooltip title={collapsed ? 'Show the side panel' : 'Collapse the side panel'}>
                    <IconButton
                      aria-controls={RAIL_ID}
                      aria-expanded={!collapsed}
                      aria-label={collapsed ? 'Show the side panel' : 'Collapse the side panel'}
                      data-testid={UI_IDENTIFIERS.Construction.FOCUS_RAIL_TOGGLE}
                      size="small"
                      sx={{ color: t.ink, ml: -0.75 }}
                      onClick={() => {
                        setCollapsed(!collapsed);
                      }}
                    >
                      {collapsed ? (
                        <KeyboardDoubleArrowRightRoundedIcon fontSize="small" />
                      ) : (
                        <KeyboardDoubleArrowLeftRoundedIcon fontSize="small" />
                      )}
                    </IconButton>
                  </Tooltip>
                ) : null}
                <Typography
                  component="h2"
                  data-testid={UI_IDENTIFIERS.Construction.FOCUS_HEADING}
                  id={HEADING_ID}
                  ref={setHeading}
                  sx={{
                    flexGrow: collapsed && collapsedSummary !== undefined ? 0 : 1,
                    m: 0,
                    fontFamily: t.mono,
                    fontSize: 10,
                    fontWeight: 700,
                    letterSpacing: '0.08em',
                    color: t.muted,
                    whiteSpace: 'nowrap',
                    // A programmatic landing spot (tabIndex -1), not a control: no ring.
                    outline: 0,
                    '&:focus, &:focus-visible': { outline: 'none', boxShadow: 'none' },
                  }}
                  tabIndex={-1}
                >
                  FOCUS VIEW · Esc to return
                </Typography>
                {collapsed && collapsedSummary !== undefined ? (
                  <Box sx={{ flexGrow: 1, minWidth: 0, display: 'flex' }}>{collapsedSummary}</Box>
                ) : null}
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
            {/* Collapsed, the decision stays in reach: the action bar under the artifact. */}
            {collapsed ? actionBar : null}
          </Box>
        </Box>
      </Modal>
    </FocusRailContext.Provider>
  );
}
