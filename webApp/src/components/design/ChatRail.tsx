/**
 * The collapsible co-author rail. It accumulates the architect's ANCHORED comments
 * for the current gate cycle (from CommentContext) and renders them as chat
 * entries, each with a location pill referencing the typed-model JSONPath anchor.
 * When a selection is armed (a diagram node, scatter point, or prose quote), the
 * composer attaches to it; pressing send posts the comment into the accumulator.
 * The architect later submits the whole set via the gate's "Send back".
 *
 * STUB: "reflect the CLI / agent conversation" is intentionally not wired — there
 * is no conversation backend in this build. The rail shows only the user's own
 * anchored comments; the footer documents the embedded-mode stub.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import InputBase from '@mui/material/InputBase';
import Tooltip from '@mui/material/Tooltip';
import PlaceIcon from '@mui/icons-material/Place';
import SendIcon from '@mui/icons-material/ArrowUpward';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import CloseIcon from '@mui/icons-material/Close';
import FormatQuoteIcon from '@mui/icons-material/FormatQuote';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import { useComments, type Anchor, type PostedComment } from '../comments/CommentContext';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

function LocationPill({ anchor, t }: { anchor: Anchor; t: Tokens }): ReactNode {
  return (
    <Tooltip title={`${anchor.source} → ${anchor.jsonPath}`}>
      <Box
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.5,
          mb: 0.5,
          px: 1,
          py: 0.4,
          maxWidth: '100%',
          borderRadius: 99,
          border: `1.5px solid ${t.accent}`,
          bgcolor: t.chatArchitectBg,
          color: t.ink,
        }}
      >
        <PlaceIcon sx={{ fontSize: 13 }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, letterSpacing: '0.04em', whiteSpace: 'nowrap' }}>
          {anchor.source.split(' · ')[0]}
        </Typography>
        <Typography sx={{ fontSize: 11.5, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 200 }}>
          {anchor.label}
        </Typography>
      </Box>
    </Tooltip>
  );
}

export function ChatRail({ onCollapse }: { onCollapse: () => void }): ReactNode {
  const t = useTokens();
  const { comments, anchor, setAnchor, post, remove } = useComments();
  const [draft, setDraft] = useState('');

  // With an anchor armed, post the anchored comment (empty text gets a fallback);
  // with no anchor, post free-form feedback only when something was typed.
  const canSend = anchor !== null || draft.trim().length > 0;
  const submit = (): void => {
    if (!canSend) return;
    post(draft);
    setDraft('');
  };

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Chat.RAIL}
      sx={{ height: '100%', display: 'flex', flexDirection: 'column', bgcolor: t.paperAlt }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 2, py: 1.25, borderBottom: `1.5px solid ${t.line}` }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}>CO-AUTHOR</Typography>
        <Chip label="architect" size="small" sx={{ height: 20, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }} variant="outlined" />
        <Box sx={{ flexGrow: 1 }} />
        <IconButton aria-label="collapse chat" size="small" sx={{ color: t.ink }} onClick={onCollapse}>
          <ChevronRightIcon fontSize="small" />
        </IconButton>
      </Box>

      <Box sx={{ flexGrow: 1, overflowY: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        {comments.length === 0 ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, textAlign: 'center', my: 2, lineHeight: 1.6 }}>
            Type feedback to send back for a redraft — or highlight prose / select a diagram node or
            scatter point first to anchor it to a spot. Everything here rides the next “Send back”.
          </Typography>
        ) : (
          comments.map((c, i) => <CommentBubble c={c} index={i} key={i} t={t} onRemove={() => { remove(i); }} />)
        )}
      </Box>

      <Box sx={{ p: 1.5, borderTop: `1.5px solid ${t.line}` }}>
        {anchor !== null && (
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              mb: 1,
              px: 1.25,
              py: 0.75,
              border: `1.5px solid ${t.accent}`,
              borderRadius: 1.5,
              bgcolor: t.chatArchitectBg,
            }}
          >
            {anchor.kind === 'node' ? (
              <AccountTreeOutlinedIcon sx={{ fontSize: 16, color: t.accent }} />
            ) : (
              <FormatQuoteIcon sx={{ fontSize: 16, color: t.accent }} />
            )}
            <Box sx={{ flexGrow: 1, minWidth: 0 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>{anchor.source}</Typography>
              <Typography sx={{ fontSize: 12.5, color: t.ink, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {anchor.label}
              </Typography>
            </Box>
            <IconButton size="small" sx={{ color: t.muted }} onClick={() => { setAnchor(null); }}>
              <CloseIcon sx={{ fontSize: 14 }} />
            </IconButton>
          </Box>
        )}
        <Box sx={{ display: 'flex', alignItems: 'center', border: `1.5px solid ${t.line}`, borderRadius: 1.5, px: 1.5, bgcolor: t.paper }}>
          <InputBase
            multiline
            data-testid={UI_IDENTIFIERS.Chat.INPUT}
            maxRows={4}
            placeholder={anchor !== null ? 'Add your comment…' : 'Type feedback for a redraft…'}
            sx={{ flexGrow: 1, fontSize: 13.5, py: 1, color: t.ink }}
            value={draft}
            onChange={(e) => { setDraft(e.target.value); }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <IconButton
            aria-label="post comment"
            data-testid={UI_IDENTIFIERS.Chat.SEND}
            disabled={!canSend}
            size="small"
            sx={{ bgcolor: t.accent, color: t.accentText, ml: 1, '&:hover': { bgcolor: t.accent2 }, '&.Mui-disabled': { bgcolor: t.line } }}
            onClick={submit}
          >
            <SendIcon sx={{ fontSize: 16 }} />
          </IconButton>
        </Box>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, mt: 0.75 }}>
          Embedded mode · agent conversation mirroring is stubbed in this build
        </Typography>
      </Box>
    </Paper>
  );
}

function CommentBubble({
  index,
  c,
  t,
  onRemove,
}: {
  index: number;
  c: PostedComment;
  t: Tokens;
  onRemove: () => void;
}): ReactNode {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Chat.commentAnchor(index)}
      sx={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end' }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 0.25 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, color: t.ink }}>You</Typography>
        <IconButton aria-label="remove comment" size="small" sx={{ color: t.muted, p: 0.25 }} onClick={onRemove}>
          <CloseIcon sx={{ fontSize: 13 }} />
        </IconButton>
      </Box>
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', maxWidth: '92%' }}>
        {c.anchor !== null ? <LocationPill anchor={c.anchor} t={t} /> : null}
        <Box sx={{ bgcolor: t.accent, color: t.accentText, border: `1.5px solid ${t.line}`, borderRadius: 1.5, px: 1.5, py: 1, fontSize: 13.5, lineHeight: 1.5 }}>
          {c.text}
        </Box>
      </Box>
    </Box>
  );
}
