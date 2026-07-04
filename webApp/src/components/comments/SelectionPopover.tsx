/**
 * Figma / Google-Docs style selection affordance: watches text selection inside
 * any [data-commentable] region and floats a "Comment" button by the selection.
 * Clicking (or keyboard-committing) arms a prose anchor in the CommentContext (a
 * section/quote JSONPath), which the ChatRail then turns into an AnchoredComment.
 *
 * The commentable host carries `data-commentable` (a human source label) and may
 * carry `data-artifact-kind` (the typed model kind) so the anchor roots into the
 * correct model. Falls back to a generic prose anchor when absent.
 *
 * ── Input-modality independence ─────────────────────────────────────────────
 * The popover appears on ANY non-collapsed selection inside a commentable region,
 * regardless of how it was made:
 *   • mouse    — `mouseup` positions it immediately (snappy drag-to-select).
 *   • keyboard — a debounced `selectionchange` path catches shift+arrow / ⌘A
 *     selections that never fire `mouseup`.
 * The trigger is a real, Tab-reachable button (role/aria-label/focus-visible ring)
 * that commits on Enter/Space as well as click; Escape dismisses it. Focus is NOT
 * auto-moved to it, so keyboard users can keep extending the selection first, then
 * Tab to the button to commit.
 */
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useComments, proseAnchor } from './CommentContext';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

interface Pending {
  text: string;
  source: string;
  kind: string;
}

export function SelectionPopover(): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clear = useCallback((): void => {
    setPos(null);
    setPending(null);
  }, []);

  // Reads the current selection and, when it is a non-collapsed range inside a
  // SINGLE [data-commentable] host, positions the popover + captures the pending
  // anchor. A range whose two endpoints fall in different commentable blocks (or
  // partly outside one) is rejected — this is the clamp that stops a drag across
  // several items from producing one meaningless cross-item anchor.
  const evaluate = useCallback((): void => {
    const sel = window.getSelection();
    const text = sel?.toString().trim() ?? '';
    if (sel === null || sel.rangeCount === 0 || sel.isCollapsed || text.length === 0) {
      clear();
      return;
    }
    const hostOf = (node: Node | null): Element | null => {
      const el = node?.nodeType === 1 ? (node as Element) : (node?.parentElement ?? null);
      return el?.closest('[data-commentable]') ?? null;
    };
    const host = hostOf(sel.anchorNode);
    const focusHost = hostOf(sel.focusNode);
    // Both endpoints must resolve to the SAME commentable block.
    if (host === null || focusHost !== host) {
      clear();
      return;
    }
    const rect = sel.getRangeAt(0).getBoundingClientRect();
    setPos({ x: rect.left + rect.width / 2, y: rect.top - 8 });
    setPending({
      text,
      source: host.getAttribute('data-commentable') ?? 'document',
      kind: host.getAttribute('data-artifact-kind') ?? 'prose',
    });
  }, [clear]);

  useEffect(() => {
    // Mouse: settle immediately on release for a snappy drag-select.
    const onUp = (): void => {
      evaluate();
    };
    // Keyboard (and any programmatic selection): debounced so shift+arrow runs
    // don't thrash the popover while the range is still growing.
    const onSelectionChange = (): void => {
      if (debounce.current !== null) clearTimeout(debounce.current);
      debounce.current = setTimeout(() => {
        evaluate();
        debounce.current = null;
      }, 250);
    };
    const onKeyDown = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') clear();
    };
    document.addEventListener('mouseup', onUp);
    document.addEventListener('selectionchange', onSelectionChange);
    document.addEventListener('keydown', onKeyDown);
    return (): void => {
      document.removeEventListener('mouseup', onUp);
      document.removeEventListener('selectionchange', onSelectionChange);
      document.removeEventListener('keydown', onKeyDown);
      if (debounce.current !== null) clearTimeout(debounce.current);
    };
  }, [evaluate, clear]);

  if (pos === null || pending === null) return null;

  const label = pending.text.length > 60 ? `${pending.text.slice(0, 60)}…` : pending.text;

  const commit = (): void => {
    setAnchor({
      kind: 'text',
      label,
      source: pending.source,
      jsonPath: proseAnchor(pending.kind, pending.source),
    });
    clear();
    window.getSelection()?.removeAllRanges();
  };

  return (
    <Box
      aria-label={`Comment on selected text in ${pending.source}`}
      data-testid={UI_IDENTIFIERS.Comments.SELECTION_POPOVER}
      role="button"
      sx={{
        position: 'fixed',
        left: pos.x,
        top: pos.y,
        transform: 'translate(-50%, -100%)',
        zIndex: 1400,
        display: 'flex',
        alignItems: 'center',
        gap: 0.75,
        px: 1.25,
        py: 0.6,
        cursor: 'pointer',
        bgcolor: t.accent,
        color: t.accentText,
        border: `1.5px solid ${t.hardShadow ? t.shadowColor : t.line}`,
        borderRadius: t.radius / 8 + 1,
        boxShadow: t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : '0 6px 18px rgba(0,0,0,0.4)',
        fontFamily: t.mono,
        fontSize: 12,
        fontWeight: 700,
        whiteSpace: 'nowrap',
        outline: 'none',
        '&:focus-visible': {
          boxShadow: `0 0 0 2px ${t.bg}, 0 0 0 4px ${t.accent}`,
        },
        '&::after': {
          content: '""',
          position: 'absolute',
          bottom: -6,
          left: '50%',
          transform: 'translateX(-50%)',
          borderLeft: '6px solid transparent',
          borderRight: '6px solid transparent',
          borderTop: `6px solid ${t.accent}`,
        },
      }}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          commit();
        }
      }}
      onMouseDown={(e) => {
        // Keep the selection alive through the click (mousedown would otherwise
        // collapse it before onClick fires).
        e.preventDefault();
        commit();
      }}
    >
      <ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />
      Comment
    </Box>
  );
}
