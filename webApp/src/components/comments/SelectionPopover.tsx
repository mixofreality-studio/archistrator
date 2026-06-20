/**
 * Figma / Google-Docs style selection affordance: watches text selection inside
 * any [data-commentable] region and floats a "Comment" button by the selection.
 * Clicking arms a prose anchor in the CommentContext (a section/quote JSONPath),
 * which the ChatRail then turns into an AnchoredComment.
 *
 * The commentable host carries `data-commentable` (a human source label) and may
 * carry `data-artifact-kind` (the typed model kind) so the anchor roots into the
 * correct model. Falls back to a generic prose anchor when absent.
 */
import { useEffect, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useComments, proseAnchor } from './CommentContext';
import { useTokens } from '../../theme/ThemeContext';

export function SelectionPopover(): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const [pending, setPending] = useState<{ text: string; source: string; kind: string } | null>(
    null
  );

  useEffect(() => {
    const onUp = (): void => {
      const sel = window.getSelection();
      const text = sel?.toString().trim() ?? '';
      if (sel === null || sel.rangeCount === 0 || text.length < 3) {
        setPos(null);
        setPending(null);
        return;
      }
      const node = sel.anchorNode;
      const el = node?.nodeType === 1 ? (node as Element) : (node?.parentElement ?? null);
      const host = el?.closest('[data-commentable]') ?? null;
      if (host === null) {
        setPos(null);
        setPending(null);
        return;
      }
      const rect = sel.getRangeAt(0).getBoundingClientRect();
      setPos({ x: rect.left + rect.width / 2, y: rect.top - 8 });
      setPending({
        text,
        source: host.getAttribute('data-commentable') ?? 'document',
        kind: host.getAttribute('data-artifact-kind') ?? 'prose',
      });
    };
    document.addEventListener('mouseup', onUp);
    return (): void => {
      document.removeEventListener('mouseup', onUp);
    };
  }, []);

  if (pos === null || pending === null) return null;

  const label = pending.text.length > 60 ? `${pending.text.slice(0, 60)}…` : pending.text;

  return (
    <Box
      aria-label="comment on selection"
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
      onMouseDown={(e) => {
        e.preventDefault();
        setAnchor({
          kind: 'text',
          label,
          source: pending.source,
          jsonPath: proseAnchor(pending.kind, pending.source),
        });
        setPos(null);
        setPending(null);
        window.getSelection()?.removeAllRanges();
      }}
    >
      <ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />
      Comment
    </Box>
  );
}
