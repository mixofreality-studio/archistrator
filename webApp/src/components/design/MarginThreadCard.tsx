/**
 * One review thread, as a Google-Docs margin card.
 *
 * The card is a work item, not a chat bubble: an anchor reference back to the
 * content it was filed against, what kind of item it is, the utterances in order,
 * a reply box that STAGES (never dispatches), and the reviewer's Resolve/Reopen.
 *
 * The anchor reference is a real BUTTON, not a tooltip — pressing it scrolls the
 * anchored row back into view. That is founder complaint #5 ("it shows the name of
 * the content but doesn't link back"), and it is the one thing in this component
 * that must not be got wrong.
 *
 * Placement is NOT this component's business: {@link CommentMargin} measures the
 * anchor, stacks the cards and positions them. This renders one card at whatever
 * width it is given.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import InputBase from '@mui/material/InputBase';
import PlaceIcon from '@mui/icons-material/Place';
import SendIcon from '@mui/icons-material/ArrowUpward';
import CloseIcon from '@mui/icons-material/Close';

import { useComments } from '../comments/CommentContext';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { ReviewCommentView } from '../../contracts/types';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/**
 * A reply the reviewer has staged against this thread but not yet sent. It is
 * shown INSIDE the thread rather than as a card of its own: a reply carries no
 * anchor (the server routes it by `replyTo`), so anywhere else it would be a
 * detached note that reads as unrelated free-form feedback.
 */
export interface StagedReply {
  /** Its position in the CommentContext accumulator — the handle for discarding it. */
  index: number;
  text: string;
}

/** The chip copy for a thread's kind — a change request, or a question and its addressee. */
function typeLabel(entry: ReviewCommentView): string {
  if (entry.type !== 'question') return 'change request';
  return entry.addressee.length > 0 ? `question → ${entry.addressee}` : 'question';
}

export function MarginThreadCard({
  entry,
  active,
  onActivate,
  onJumpToAnchor,
  onResolve,
  onReopen,
  statusPending,
  stagedReplies,
  onDiscardStaged,
  expandResolved,
}: {
  entry: ReviewCommentView;
  /** The margin's single expanded card. Inactive cards render compact. */
  active: boolean;
  onActivate: () => void;
  /** Scrolls this thread's anchored content back into view. */
  onJumpToAnchor: () => void;
  /**
   * Resolve an open/answered thread, and reopen a resolved one. Omitted on a
   * surface with no review-status mutation of its own (the Construction console),
   * where the lifecycle button simply does not render rather than lying.
   */
  onResolve?: ((id: string) => void) | undefined;
  onReopen?: ((id: string) => void) | undefined;
  /** A resolve/reopen mutation is in flight — the lifecycle button is disabled. */
  statusPending: boolean;
  /** Replies staged against THIS thread, not yet sent. */
  stagedReplies?: readonly StagedReply[];
  /** Drop a staged reply by its accumulator index. */
  onDiscardStaged?: ((index: number) => void) | undefined;
  /**
   * Show resolved threads as full cards instead of the collapsed one-liner below.
   * Default `false` (today's collapse) fits the live review rail, where a decided
   * thread is settled business and should cost one line of margin. A read-only
   * history (spec §7.2) sets this `true`: a decided thread there is the POINT of
   * the history, not noise in it, so it stays expanded.
   */
  expandResolved?: boolean | undefined;
}): ReactNode {
  const t = useTokens();
  const { post, enabled } = useComments();
  const [reply, setReply] = useState('');
  const resolved = entry.status === 'resolved';

  // A resolved thread that is not the active card collapses to a single muted
  // line: the decision is made, so it should cost one line of margin, not a card —
  // unless the caller asked resolved threads to stay expanded (expandResolved).
  if (resolved && !active && expandResolved !== true) {
    return (
      <Box
        data-testid={UI_IDENTIFIERS.Margin.card(entry.id)}
        role="button"
        sx={{
          px: 1,
          py: 0.5,
          opacity: 0.6,
          fontSize: 12,
          cursor: 'pointer',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          border: `1.5px solid ${t.line}`,
          borderRadius: 1,
          bgcolor: t.paperAlt,
          color: t.ink,
        }}
        tabIndex={0}
        onClick={onActivate}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onActivate();
          }
        }}
      >
        {entry.text}
      </Box>
    );
  }

  const lifecycle = resolved ? onReopen : onResolve;

  const sendReply = (): void => {
    const text = reply.trim();
    if (text.length === 0) return;
    // A reply is text + replyTo ONLY — it arms no anchor, because the server
    // locates the thread by `replyTo`. The thread's own `addressee` rides along:
    // without it the pending-questions mapper defaults to 'pm', and a follow-up
    // on an ARCHITECT-addressed question would be answered by the wrong role.
    post(text, {
      commentType: entry.type,
      replyTo: entry.id,
      ...(entry.addressee.length > 0 ? { addressee: entry.addressee } : {}),
    });
    setReply('');
  };

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Margin.card(entry.id)}
      sx={{
        p: 1.25,
        cursor: active ? 'default' : 'pointer',
        border: `1.5px solid ${active ? t.accent : t.line}`,
        bgcolor: t.paper,
        boxShadow: active && t.hardShadow ? `2px 2px 0 ${t.shadowColor}` : 'none',
      }}
      onClick={onActivate}
    >
      {/* The anchor reference — a real button back to the content (complaint #5). */}
      {entry.anchorText.length > 0 ? (
        <Button
          size="small"
          startIcon={<PlaceIcon sx={{ fontSize: 13 }} />}
          sx={{
            textTransform: 'none',
            fontSize: 11.5,
            color: t.accent,
            minWidth: 0,
            p: 0.25,
            maxWidth: '100%',
            justifyContent: 'flex-start',
            '& .MuiButton-startIcon': { mr: 0.5 },
          }}
          onClick={(e) => {
            e.stopPropagation();
            onJumpToAnchor();
          }}
        >
          <Box
            component="span"
            sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
          >
            {entry.anchorText}
          </Box>
        </Button>
      ) : null}

      <Box sx={{ display: 'flex', gap: 0.5, my: 0.5, flexWrap: 'wrap' }}>
        <Chip
          label={typeLabel(entry)}
          size="small"
          sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono }}
          variant="outlined"
        />
        <Chip
          label={entry.status}
          size="small"
          sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono }}
          variant="outlined"
        />
        {entry.reopened ? (
          <Chip
            label="reopened"
            size="small"
            sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono, color: t.awaitingFg }}
            variant="outlined"
          />
        ) : null}
      </Box>

      <Typography sx={{ fontSize: 13, lineHeight: 1.45, color: t.ink }}>{entry.text}</Typography>

      {entry.replies.map((r) => (
        <Box key={r.id} sx={{ mt: 0.75, pl: 1, borderLeft: `2px solid ${t.line}` }}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
            {r.authorRole}
          </Typography>
          <Typography sx={{ fontSize: 12.5, lineHeight: 1.45, color: t.ink }}>{r.text}</Typography>
        </Box>
      ))}

      {(stagedReplies ?? []).map((r) => (
        <Box
          key={r.index}
          sx={{
            mt: 0.75,
            pl: 1,
            borderLeft: `2px dashed ${t.accent}`,
            display: 'flex',
            alignItems: 'flex-start',
            gap: 0.5,
          }}
        >
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 10, color: t.accent, letterSpacing: '0.06em' }}
            >
              STAGED · NOT SENT
            </Typography>
            <Typography sx={{ fontSize: 12.5, lineHeight: 1.45, color: t.ink }}>
              {r.text}
            </Typography>
          </Box>
          {onDiscardStaged !== undefined ? (
            <IconButton
              aria-label="discard staged reply"
              size="small"
              sx={{ color: t.muted, p: 0.25 }}
              onClick={(e) => {
                e.stopPropagation();
                onDiscardStaged(r.index);
              }}
            >
              <CloseIcon sx={{ fontSize: 13 }} />
            </IconButton>
          ) : null}
        </Box>
      ))}

      {/* The reply box STAGES an utterance; nothing dispatches until the batch verb. */}
      {active && enabled ? (
        <Box sx={{ mt: 1, display: 'flex', gap: 0.5, alignItems: 'flex-end' }}>
          <InputBase
            multiline
            data-testid={UI_IDENTIFIERS.Margin.reply(entry.id)}
            maxRows={4}
            placeholder="Reply…"
            sx={{
              flexGrow: 1,
              fontSize: 12.5,
              color: t.ink,
              border: `1.5px solid ${t.line}`,
              borderRadius: 1,
              px: 1,
            }}
            value={reply}
            onChange={(e) => {
              setReply(e.target.value);
            }}
            onClick={(e) => {
              e.stopPropagation();
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                sendReply();
              }
            }}
          />
          <IconButton
            aria-label="post comment"
            disabled={reply.trim().length === 0}
            size="small"
            sx={{
              bgcolor: t.accent,
              color: t.accentText,
              '&:hover': { bgcolor: t.accent2 },
              '&.Mui-disabled': { bgcolor: t.line },
            }}
            onClick={(e) => {
              e.stopPropagation();
              sendReply();
            }}
          >
            <SendIcon sx={{ fontSize: 15 }} />
          </IconButton>
        </Box>
      ) : null}

      {/* Resolve / Reopen are immediate (they cost no AI run) — design §2.5. */}
      {lifecycle !== undefined ? (
        <Button
          data-testid={
            resolved
              ? UI_IDENTIFIERS.Margin.reopen(entry.id)
              : UI_IDENTIFIERS.Margin.resolve(entry.id)
          }
          disabled={statusPending}
          size="small"
          sx={{ mt: 0.5, fontSize: 11, textTransform: 'none', color: t.muted, minWidth: 0 }}
          onClick={(e) => {
            e.stopPropagation();
            lifecycle(entry.id);
          }}
        >
          {resolved ? 'Reopen' : 'Resolve'}
        </Button>
      ) : null}
    </Paper>
  );
}
