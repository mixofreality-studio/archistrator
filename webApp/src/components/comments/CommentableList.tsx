/**
 * The shared keyboard-navigable, item-granular-commentable list primitive.
 *
 * Every itemized artifact (glossary terms, scrubbed requirements, business
 * objectives, operational-concept decisions, standard-check rows, activities,
 * solution knobs, …) renders through this instead of an ad-hoc `.map()` over
 * inert `<Box>`s. It gives each item, for free:
 *
 *   • a real ARIA listbox/option structure (screen readers announce "list, N
 *     items" and each row as a selectable option),
 *   • roving-tabindex keyboard navigation — ↑/↓ move focus, Home/End jump, the
 *     focused row is `aria-selected` and is the single tab-stop,
 *   • a discoverable per-row "Comment on this item" button (the founder-liked
 *     UseCaseCarousel affordance), visible on row hover/focus, that arms an
 *     item-granularity anchor in the CommentContext,
 *   • the same action from the keyboard: Enter or `c` on the focused row arms it.
 *
 * The button carries `tabIndex={-1}` so the listbox keeps a single tab-stop
 * (WAI-ARIA listbox pattern); keyboard users reach it via Enter/`c`, mouse users
 * click it directly. The caller supplies `getAnchor` so each artifact kind maps
 * its item to the correct typed-model JSONPath (see CommentContext builders).
 */
import { useCallback, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useComments, type Anchor } from './CommentContext';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export function CommentableList<T>({
  items,
  getKey,
  getAnchor,
  renderItem,
  ariaLabel,
  getLabel,
  getLabelKind,
  gap = 0,
}: {
  items: readonly T[];
  /** Stable per-item key (also feeds the row + button test ids). */
  getKey: (item: T, index: number) => string;
  /** Maps an item to the anchor armed when its comment button fires. */
  getAnchor: (item: T, index: number) => Anchor;
  /** Renders the row body (the caller owns typography/layout of the content). */
  renderItem: (item: T, index: number) => ReactNode;
  /** Accessible name for the whole list (e.g. "Business objectives"). */
  ariaLabel: string;
  /** Optional short accessible name per row (the VALUE, e.g. the term / topic);
   *  falls back to the row content. Composed with {@link getLabelKind} into the
   *  comment button's accessible name. */
  getLabel?: (item: T, index: number) => string;
  /** Optional noun classifying the item (e.g. "term", "decision"), rendered AFTER
   *  the value so the button reads 'Comment on Other Party (term)' rather than the
   *  ungrammatical 'Comment on term Other Party'. */
  getLabelKind?: (item: T, index: number) => string;
  /** Vertical gap between rows, in theme spacing units. */
  gap?: number;
}): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  const [focused, setFocused] = useState(0);
  const rowRefs = useRef<(HTMLDivElement | null)[]>([]);

  const moveTo = useCallback((index: number): void => {
    const clamped = Math.max(0, Math.min(index, rowRefs.current.length - 1));
    setFocused(clamped);
    rowRefs.current[clamped]?.focus();
  }, []);

  const arm = useCallback(
    (item: T, index: number): void => {
      setAnchor(getAnchor(item, index));
    },
    [getAnchor, setAnchor]
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent, item: T, index: number): void => {
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          moveTo(index + 1);
          break;
        case 'ArrowUp':
          e.preventDefault();
          moveTo(index - 1);
          break;
        case 'Home':
          e.preventDefault();
          moveTo(0);
          break;
        case 'End':
          e.preventDefault();
          moveTo(items.length - 1);
          break;
        case 'Enter':
        case 'c':
        case 'C':
          e.preventDefault();
          arm(item, index);
          break;
        default:
          break;
      }
    },
    [arm, items.length, moveTo]
  );

  // Read-only surface (no active commenting context): render the item bodies as a
  // plain, inert column — NO listbox/option roles, NO roving tabindex or tab stops,
  // NO per-row comment button, NO hover chrome. Zero comment affordance, zero
  // orphaned ARIA, zero focusable ghosts.
  if (!enabled) {
    return (
      <Box sx={{ display: 'flex', flexDirection: 'column', gap }}>
        {items.map((item, index) => (
          <Box key={getKey(item, index)} sx={{ px: 1, py: 0.75 }}>
            {renderItem(item, index)}
          </Box>
        ))}
      </Box>
    );
  }

  return (
    <Box
      aria-label={ariaLabel}
      data-testid={UI_IDENTIFIERS.Comments.LIST}
      role="listbox"
      sx={{ display: 'flex', flexDirection: 'column', gap, outline: 'none' }}
    >
      {items.map((item, index) => {
        const key = getKey(item, index);
        const isFocused = index === focused;
        const value = getLabel?.(item, index) ?? `item ${String(index + 1)}`;
        const kind = getLabelKind?.(item, index);
        // 'Comment on Other Party (term)' — noun trails the value so the phrase stays
        // grammatical regardless of the item kind (P3-14).
        const commentLabel =
          kind !== undefined && kind !== '' ? `Comment on ${value} (${kind})` : `Comment on ${value}`;
        return (
          <Box
            aria-selected={isFocused}
            data-testid={UI_IDENTIFIERS.Comments.listItem(key)}
            key={key}
            ref={(el: HTMLDivElement | null) => {
              rowRefs.current[index] = el;
            }}
            role="option"
            sx={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 1,
              px: 1,
              py: 0.75,
              borderRadius: 1,
              cursor: 'default',
              // The comment button carries a FAINT persistent presence (so it is
              // discoverable without hovering — no hover-only affordance), then rises
              // to full on row hover / keyboard focus.
              '& .commentable-row-action': { opacity: 0.4, transition: 'opacity 120ms' },
              '&:hover .commentable-row-action, &:focus-visible .commentable-row-action': {
                opacity: 1,
              },
              '&:focus-visible': {
                outline: `2px solid ${t.accent}`,
                outlineOffset: 1,
                bgcolor: t.paperAlt,
                '& .commentable-row-action': { opacity: 1 },
              },
              '&:hover': { bgcolor: t.paperAlt },
            }}
            tabIndex={isFocused ? 0 : -1}
            onFocus={() => {
              setFocused(index);
            }}
            onKeyDown={(e) => {
              onKeyDown(e, item, index);
            }}
          >
            <Box sx={{ flexGrow: 1, minWidth: 0 }}>{renderItem(item, index)}</Box>
            <Tooltip title="Comment on this item">
              <IconButton
                aria-label={commentLabel}
                className="commentable-row-action"
                data-testid={UI_IDENTIFIERS.Comments.listItemComment(key)}
                size="small"
                sx={{
                  flexShrink: 0,
                  color: t.accentText,
                  bgcolor: t.accent,
                  border: `1.5px solid ${t.line}`,
                  borderRadius: 1,
                  '&:hover': { bgcolor: t.accent2 },
                }}
                tabIndex={-1}
                onClick={() => {
                  arm(item, index);
                }}
              >
                <ChatBubbleOutlineIcon sx={{ fontSize: 15 }} />
              </IconButton>
            </Tooltip>
          </Box>
        );
      })}
    </Box>
  );
}
