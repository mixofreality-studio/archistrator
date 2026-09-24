/**
 * The shared full-screen design-experience shell used by BOTH the System Design
 * (Phase 1) and Project Design (Phase 2) co-author screens. Owns the chrome (NOT
 * the AppShell): an accent strip, a prominent ✕ close, the phase title, the enter
 * transition, an optional SlimSpine progress rail, the active-step body, and an
 * optional collapsible CommentMargin for anchored review threads.
 *
 * It also mounts the AnchorRegistryProvider around the content row, which is the
 * ONE place that covers both halves of the margin's contract: the rows inside
 * `children` enrol their anchors, and the `margin` beside them looks those anchors
 * up. Mount it anywhere narrower and the registry hooks silently no-op — every
 * card falls into the unplaced bucket and the margin just looks wrong.
 *
 * ── One scroller, not two (Task 8b) ─────────────────────────────────────────
 * The content column and the comment margin are BOTH inside a single scroll
 * container owned here, and this shell owns the `scrollRoot` element that the
 * margin measures anchor offsets against (hence `margin` is a FACTORY, not a
 * node). Consequences, all of them deliberate:
 *
 *   • there is exactly ONE scrollbar, at the far right of the window, past the
 *     margin — not a second one wedged between the columns;
 *   • content and cards scroll TOGETHER, so a card stays level with its row with
 *     no scroll compensation anywhere (see CommentMargin's header);
 *   • the margin column carries no border and no background of its own. It is
 *     page margin that happens to hold cards, not a docked panel.
 *
 * ── EXACTLY ONE scroller, declared not inferred ─────────────────────────────
 * Every surface must end up with exactly one scroll container — never zero, never
 * two. Which one it is cannot be guessed from the props (an earlier revision
 * inferred it from `onOpenMargin` and left the MCP System Design widget, which
 * wires no margin at all, with ZERO scrollers and a body clipped at the fold), so
 * `bodyScroll` is REQUIRED and every caller states its answer:
 *
 *   'shared' — the chrome owns it (the box below). `children` must carry no
 *              `overflowY` of its own; it is a block inside the page, and the page
 *              is what scrolls. Every surface that has, or could open, a comment
 *              margin must be 'shared' — the margin only works inside that box.
 *   'self'   — `children` owns it, and this row must not scroll. For a surface
 *              that pins its own furniture above a scrolling body (Operations' tab
 *              bar) and has no margin.
 *
 * Extracted from DesignExperience.tsx so the two phase screens share one shell
 * rather than forking it.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import useMediaQuery from '@mui/material/useMediaQuery';
import CloseIcon from '@mui/icons-material/Close';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';

import { ThemeSwitcher } from '../ThemeSwitcher';
import { SelectionPopover, type SelectionCommentSurface } from '../comments/SelectionPopover';
import { AnchorRegistryProvider } from '../comments/AnchorRegistry';
import { MARGIN_DRAWER_MEDIA, MARGIN_WIDTH } from './CommentMargin';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

// DEV-only, de-duplicated: the warning below would otherwise fire on every
// render of the mis-wired surface. Same shape as AnchorRegistry's.
const warned = new Set<string>();
function warnOnce(message: string): void {
  if (warned.has(message)) return;
  warned.add(message);
  console.warn(message);
}

export function ExperienceChrome({
  phaseNum,
  phaseTitle,
  eyebrow,
  projectName,
  onClose,
  spine,
  bodyScroll,
  margin,
  marginOpen,
  onOpenMargin,
  commentSurface,
  children,
}: {
  phaseNum: number;
  phaseTitle: string;
  /**
   * Replaces the mono eyebrow above the title. Omitted ⇒ today's
   * `PHASE N · EXPERIENCE`, unchanged. The per-activity experience is not a phase,
   * so it names itself instead (`ACTIVITY 2 · ARCHITECTURE`, `R-BSA · SERVICE`).
   */
  eyebrow?: string | undefined;
  projectName?: string | undefined;
  onClose: () => void;
  spine?: ReactNode;
  /**
   * Builds the comment margin for this surface, given the SHARED scroll container
   * (see the file header) that every anchor offset is measured against. A factory,
   * not a node: the scroller is owned here, and a pre-built node could not receive
   * it. Omitted ⇒ no margin affordance at all.
   */
  margin?: ((scrollRoot: HTMLElement | null) => ReactNode) | undefined;
  /**
   * Which element scrolls — see the file header. Required on purpose: there is no
   * safe default, and the one surface an inferred default got wrong had no
   * scroller at all.
   */
  bodyScroll: 'shared' | 'self';
  /** Whether the margin is currently shown (drives the header's re-open toggle). */
  marginOpen?: boolean | undefined;
  onOpenMargin?: (() => void) | undefined;
  /**
   * Threaded into SelectionPopover. Omitted (the Phase-2 Project Design
   * experience, and any caller that hasn't migrated) → SelectionPopover falls
   * back to ambient CommentContext, today's behavior, unchanged.
   */
  commentSurface?: SelectionCommentSurface | undefined;
  children: ReactNode;
}): ReactNode {
  const t = useTokens();
  // The ONE scroll container, held as STATE (not a ref) so the margin re-renders
  // the moment it exists: every anchor offset is measured against this element,
  // and a ref mutation would not tell the margin it had arrived.
  const [scrollRoot, setScrollRoot] = useState<HTMLElement | null>(null);
  const shared = bodyScroll === 'shared';
  // A margin can only be placed inside the shared scroller (its cards are
  // positioned in that scroller's content space), so this pairing is a wiring
  // mistake, and a silent one: the cards would all read as unplaced.
  if (import.meta.env.DEV && !shared && margin !== undefined) {
    warnOnce(
      'ExperienceChrome: bodyScroll="self" with a comment margin wired — the margin ' +
        'measures anchor offsets against the shared scroller this surface does not have, ' +
        'so every card falls into the unplaced group. Use bodyScroll="shared".'
    );
  }
  const drawer = useMediaQuery(MARGIN_DRAWER_MEDIA, { noSsr: true });
  // Built once, mounted in exactly one of the two places below. The two are
  // different DOM positions (in the page vs. pinned over it), so a viewport that
  // crosses the breakpoint remounts the margin — cheap, and rare.
  const marginNode = margin?.(scrollRoot);
  const inlineMargin = margin !== undefined && !drawer;
  const drawerMargin = margin !== undefined && drawer;
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
      <SelectionPopover commentSurface={commentSurface} />

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
              outline: 'none',
              '&:hover': {
                boxShadow: t.hardShadow ? `1px 1px 0 ${t.shadowColor}` : 'none',
                transform: t.hardShadow ? 'translate(1px,1px)' : 'scale(1.05)',
              },
              // Keyboard reachability (UX-P0-1): a role=button Box is inert to the
              // keyboard without a tabIndex + key handler + a visible focus ring.
              // Pattern mirrors comments/SelectionPopover's focus-visible treatment.
              '&:focus-visible': {
                boxShadow: `0 0 0 2px ${t.bg}, 0 0 0 4px ${t.accent}`,
              },
            }}
            tabIndex={0}
            onClick={onClose}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onClose();
              }
            }}
          >
            <CloseIcon sx={{ fontSize: 22 }} />
          </Box>
        </Tooltip>

        <Box sx={{ minWidth: 0 }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: '0.22em',
              color: t.accent,
              lineHeight: 1,
            }}
          >
            {eyebrow ?? `PHASE ${String(phaseNum)} · EXPERIENCE`}
          </Typography>
          <Typography
            sx={{
              fontFamily: t.display,
              fontWeight: 700,
              fontSize: 20,
              color: t.ink,
              lineHeight: 1.15,
            }}
          >
            {phaseTitle}
          </Typography>
        </Box>

        {projectName !== undefined && (
          <Chip
            label={projectName}
            size="small"
            sx={{ bgcolor: t.paperAlt, color: t.ink, display: { xs: 'none', md: 'flex' } }}
            variant="outlined"
          />
        )}

        <Box sx={{ flexGrow: 1 }} />

        <ThemeSwitcher />
        {marginOpen === false && onOpenMargin !== undefined && (
          <Tooltip title="Open comments">
            <IconButton
              data-testid={UI_IDENTIFIERS.Chat.TOGGLE}
              size="small"
              sx={{ border: `1.5px solid ${t.line}`, borderRadius: 1, color: t.ink }}
              onClick={onOpenMargin}
            >
              <ChatBubbleOutlineIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
      </Box>

      {/* spine bar */}
      {spine !== undefined && (
        <Box
          sx={{
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            gap: 2,
            px: 2.5,
            py: 1,
            bgcolor: t.paperAlt,
            borderBottom: `1.5px solid ${t.line}`,
          }}
        >
          <Box sx={{ flexGrow: 1, minWidth: 0, overflowX: 'auto' }}>{spine}</Box>
        </Box>
      )}

      {/* content row — the single scope the anchor registry has to cover, because
          the enrolling rows (children) and the looking-up margin are both in it.
          This outer row does NOT scroll: it is the positioning frame the narrow
          -viewport drawer hangs off, which is the one thing that must stay put
          while the scroller inside it moves. */}
      <AnchorRegistryProvider>
        <Box
          component="main"
          sx={{
            flexGrow: 1,
            minHeight: 0,
            display: 'flex',
            position: 'relative',
          }}
        >
          {shared ? (
            // THE scroller — content column and margin column together, so the only
            // scrollbar on the page sits at the far right of the window, past the
            // cards, and the two columns move as one surface.
            <Box
              data-testid={UI_IDENTIFIERS.DesignExperience.DESIGN_SCROLL}
              ref={setScrollRoot}
              sx={{ flexGrow: 1, minWidth: 0, minHeight: 0, overflowY: 'auto' }}
            >
              {/* The page. Its height is the CONTENT's height (at least a
                  viewport), which is what lets the margin beside it span the whole
                  document rather than one screenful — a flex/grid sibling could
                  not: a flex line in a scroller is capped at the scrollport, so a
                  sibling column would end at the fold and anything sticky inside it
                  would come unstuck there. The margin is therefore absolutely
                  positioned into a reserved strip of this box's padding, which
                  `top: 0; bottom: 0` stretches to the full content height. */}
              <Box
                sx={{
                  position: 'relative',
                  minHeight: '100%',
                  display: 'flex',
                  pr: inlineMargin ? `${String(MARGIN_WIDTH)}px` : 0,
                }}
              >
                {children}
                {inlineMargin ? (
                  <Box
                    sx={{
                      // No border, no background: page margin, not a docked panel.
                      position: 'absolute',
                      top: 0,
                      right: 0,
                      bottom: 0,
                      width: MARGIN_WIDTH,
                    }}
                  >
                    {marginNode}
                  </Box>
                ) : null}
              </Box>
            </Box>
          ) : (
            children
          )}

          {/* Below ~1100px a 320px column eats more of the reading area than the
              content can spare, so the margin lifts OUT of the scroller entirely
              and overlays the content, pinned to this (unscrolled) row — same
              component, same toggle. CommentMargin reads the same breakpoint and
              lays its cards out as a list there, because a pinned column is not in
              content space and cannot sit level with anything. */}
          {drawerMargin ? (
            <Box
              sx={{
                position: 'absolute',
                top: 0,
                right: 0,
                bottom: 0,
                zIndex: 4,
                width: 'min(340px, 92vw)',
                bgcolor: t.bg,
                boxShadow: `-6px 0 18px ${t.shadowColor}`,
              }}
            >
              {marginNode}
            </Box>
          ) : null}
        </Box>
      </AnchorRegistryProvider>
    </Box>
  );
}
