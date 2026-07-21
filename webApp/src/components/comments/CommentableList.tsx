/**
 * The shared keyboard-navigable, item-granular-commentable list primitive.
 *
 * Every itemized artifact (glossary terms, scrubbed requirements, business
 * objectives, operational-concept decisions, standard-check rows, activities,
 * solution knobs, …) renders through this instead of an ad-hoc `.map()` over
 * inert `<Box>`s. It gives each item, for free:
 *
 *   • a real ARIA list/listitem structure (screen readers announce "list, N
 *     items" and each row by its own short label — NOT a selection widget:
 *     nothing here is selectable),
 *   • roving-tabindex keyboard navigation — ↑/↓ move focus, Home/End jump, the
 *     focused row is the single tab-stop and shows a border on focus — whether
 *     that focus arrived by pointer click OR by keyboard,
 *   • a discoverable per-row "Comment on this item" button (the founder-liked
 *     UseCaseCarousel affordance), hidden at rest and revealed at full contrast on
 *     row hover / keyboard focus-within — or kept persistently visible when the row
 *     is the armed anchor or already carries a pending comment — that arms an
 *     item-granularity anchor in the CommentContext,
 *   • the same action from the keyboard: Enter or `c` on the focused row arms it.
 *
 * The button carries `tabIndex={-1}` so the list keeps a single roving tab-stop
 * (the focused row); keyboard users reach the button via Enter/`c`, mouse users
 * click it directly. The caller supplies `getAnchor` so each artifact kind maps
 * its item to the correct typed-model JSONPath (see CommentContext builders); that
 * anchor's `label` doubles as the row's short accessible name.
 *
 * NOTE: "already carries a pending comment" means an entry accumulated THIS review
 * cycle (CommentContext.comments) whose anchor jsonPath matches the row. Committed
 * thread comments are not plumbed to this layer, so a previously-sent-and-persisted
 * comment does not (yet) light the row indicator.
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
  const { setAnchor, enabled, anchor: armedAnchor, comments } = useComments();
  const [focused, setFocused] = useState(0);
  const rowRefs = useRef<(HTMLDivElement | null)[]>([]);

  const moveTo = useCallback(
    (index: number): void => {
      // Clamp against the live item count, not `rowRefs.current.length`: the ref
      // array is nulled-but-not-truncated when rows unmount, so its length is stale.
      const clamped = Math.max(0, Math.min(index, items.length - 1));
      setFocused(clamped);
      rowRefs.current[clamped]?.focus();
    },
    [items.length]
  );

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
  // plain, inert column — NO list/listitem roles, NO roving tabindex or tab stops,
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

  // The effective (in-range) roving-focus index for THIS render. `focused` state can
  // dangle past the end when `items` shrinks while the component stays mounted (e.g.
  // a live filter narrows a still-mounted section) — an unclamped index would then
  // match NO row, dropping every row to tabIndex=-1 and leaving the section with no
  // keyboard tab-stop (unreachable by Tab). Clamping here guarantees exactly one row
  // keeps the tabIndex=0 stop every frame (and none when the list is empty). The
  // Arrow/Home/End/onFocus/onClick handlers key off each row's live `index` (and
  // `moveTo` clamps to `items.length`), so the persisted `focused` self-corrects on
  // the next interaction; `safeFocused` is its only render-time reader.
  const safeFocused = items.length === 0 ? -1 : Math.min(focused, items.length - 1);

  return (
    <Box
      aria-label={ariaLabel}
      data-testid={UI_IDENTIFIERS.Comments.LIST}
      role="list"
      sx={{ display: 'flex', flexDirection: 'column', gap, outline: 'none' }}
    >
      {items.map((item, index) => {
        const key = getKey(item, index);
        const isFocused = index === safeFocused;
        const value = getLabel?.(item, index) ?? `item ${String(index + 1)}`;
        const kind = getLabelKind?.(item, index);
        // The anchor carries this row's stable jsonPath + its short `label`. We use
        // the label as the row's accessible name and the jsonPath to detect whether
        // this row is the armed anchor or already carries a pending (this-cycle)
        // comment — both of which keep the comment button visible at rest.
        const rowAnchor = getAnchor(item, index);
        const isArmed = armedAnchor?.jsonPath === rowAnchor.jsonPath;
        const hasComments = comments.some((c) => c.anchor?.jsonPath === rowAnchor.jsonPath);
        const revealed = isArmed || hasComments;
        // 'Comment on Other Party (term)' — noun trails the value so the phrase stays
        // grammatical regardless of the item kind (P3-14).
        const commentLabel =
          kind !== undefined && kind !== ''
            ? `Comment on ${value} (${kind})`
            : `Comment on ${value}`;
        return (
          <Box
            aria-keyshortcuts="Enter c"
            aria-label={rowAnchor.label}
            data-testid={UI_IDENTIFIERS.Comments.listItem(key)}
            key={key}
            ref={(el: HTMLDivElement | null) => {
              rowRefs.current[index] = el;
            }}
            role="listitem"
            sx={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 1,
              px: 1,
              py: 0.75,
              borderRadius: 1,
              cursor: 'default',
              // Comment button: hidden at rest (opacity 0 — but kept in layout, in the
              // tab order and in the a11y tree; NO display:none / visibility:hidden),
              // revealed at FULL contrast on row hover or keyboard focus-within. Kept
              // persistently visible (revealed) when the row is the armed anchor or
              // already carries a pending comment.
              '& .commentable-row-action': {
                opacity: revealed ? 1 : 0,
                transition: 'opacity 120ms',
              },
              '&:hover .commentable-row-action, &:focus-within .commentable-row-action': {
                opacity: 1,
              },
              // Touch / no-hover pointers can't reveal-on-hover — always show it there.
              '@media (hover: none)': {
                '& .commentable-row-action': { opacity: 1 },
              },
              '&:hover': { bgcolor: t.paperAlt },
              // Focused-row border driven by DOM :focus (so a POINTER click shows it
              // immediately, not only keyboard nav) and by the armed anchor (the active
              // row stays outlined while its comment is being composed).
              ...(isArmed
                ? { outline: `2px solid ${t.accent}`, outlineOffset: 1, bgcolor: t.paperAlt }
                : {}),
              '&:focus': {
                outline: `2px solid ${t.accent}`,
                outlineOffset: 1,
                bgcolor: t.paperAlt,
              },
            }}
            tabIndex={isFocused ? 0 : -1}
            onClick={(e) => {
              // Pointer clicks sync the roving-focus index AND move DOM focus to the
              // row, so the focused-row border shows immediately and keyboard nav
              // continues from here. Skip the focus move when the click landed on the
              // comment button — it owns focus while it arms the anchor.
              if (!(e.target as HTMLElement).closest('.commentable-row-action')) {
                moveTo(index);
              }
            }}
            onFocus={() => {
              setFocused(index);
            }}
            onKeyDown={(e) => {
              onKeyDown(e, item, index);
            }}
          >
            <Box sx={{ flexGrow: 1, minWidth: 0 }}>{renderItem(item, index)}</Box>
            <Tooltip title="Comment on this item — press Enter or C">
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
