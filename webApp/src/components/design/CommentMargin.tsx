/**
 * The Google-Docs comment margin — the review surface that replaced `ChatRail`.
 *
 * The rail it replaces wore a chat costume (left/right bubbles under a
 * `CO-AUTHOR` header) over what is really a work queue: items filed against
 * specific content, sitting staged until a batch verb consumes them. Two things
 * follow from dropping the costume, and they are the whole point of this file:
 *
 *   1. **A card sits level with the content it anchors to.** Anchor offsets come
 *      from the {@link AnchorRegistryProvider} registry (`CommentableList`
 *      enrols every row for free); {@link stackCards} slides colliding cards down
 *      so they never overlap and never reorder.
 *   2. **The anchor reference is a link, not a label.** Pressing it scrolls the
 *      anchored row back into view — founder complaint #5.
 *
 * Everything queues. Replies and new comments STAGE into `CommentContext` and
 * ride the next batch verb; only Resolve/Reopen act immediately, because they
 * cost no AI run.
 *
 * ── Why the placement layer is translated rather than scrolled ──────────────
 * `useAnchorOffsets` returns each anchor's offset within the scroll CONTENT: it
 * measures the row's live viewport top against a content origin of
 * `root.top - root.scrollTop`, which ADDS the root's scrollTop back in. The
 * numbers are therefore stable while the reader scrolls. The margin therefore renders one absolutely-positioned layer in those
 * same content coordinates and slides the whole layer by `-scrollTop`, which is
 * exactly the transform that maps content space to viewport space. A card is then
 * level with its row by construction, at any scroll position, with no per-card
 * arithmetic.
 */
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import InputBase from '@mui/material/InputBase';
import CloseIcon from '@mui/icons-material/Close';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import SendIcon from '@mui/icons-material/ArrowUpward';
import PlaceIcon from '@mui/icons-material/Place';
import FormatQuoteIcon from '@mui/icons-material/FormatQuote';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import QuestionAnswerOutlinedIcon from '@mui/icons-material/QuestionAnswerOutlined';

import { useComments } from '../comments/CommentContext';
import { useAnchorOffsets, useScrollAnchorIntoView } from '../comments/AnchorRegistry';
import { stackCards, type MarginCard } from '../comments/commentMarginLayout';
import { isQuestion } from '../comments/reviewBatch';
import type { PostedComment } from '../comments/commentContextTypes';
import { MarginThreadCard, type StagedReply } from './MarginThreadCard';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import type {
  ReviewCommentAddressee,
  ReviewCommentType,
  ReviewCommentView,
} from '../../contracts/types';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/** Minimum vertical clearance between two stacked cards, in px. */
const GAP = 8;

/** Assumed card height until the real one is measured (one frame). */
const ASSUMED_CARD_HEIGHT = 96;

/** The margin column's width, in px. */
export const MARGIN_WIDTH = 320;

/**
 * Below this viewport width the margin stops being a column beside the content
 * and becomes a toggleable overlay drawer — at narrower widths a 320px column
 * takes more of the reading area than the content can spare.
 */
export const MARGIN_DRAWER_QUERY = '@media (max-width: 1100px)';

/**
 * The empty thread, hoisted to module scope. A `thread = []` default parameter
 * mints a NEW array every render, which re-identifies `items` and `paths` and so
 * forces `useAnchorOffsets` to re-measure the DOM on every render — on the
 * Construction console, which polls every 1.5s and passes no thread at all, that
 * is a full re-measure per poll forever.
 */
const EMPTY_THREAD: readonly ReviewCommentView[] = [];

/** One placeable margin entry: a server thread, or a locally staged note. */
type MarginItem =
  | { key: string; kind: 'thread'; entry: ReviewCommentView }
  | { key: string; kind: 'staged'; index: number; comment: PostedComment };

/** The anchor JSONPath an item wants to sit level with ('' ⇒ unanchored). */
function anchorPathOf(item: MarginItem): string {
  return item.kind === 'thread' ? item.entry.anchor : (item.comment.anchor?.jsonPath ?? '');
}

export function CommentMargin({
  thread = EMPTY_THREAD,
  scrollRoot,
  statusPending = false,
  askPending = false,
  committed = false,
  onResolve,
  onReopen,
  onAsk,
  onCollapse,
}: {
  /** The durable server review-ledger thread for the active slot. */
  thread?: readonly ReviewCommentView[];
  /**
   * The artifact's scroll container — every anchor offset is measured against it.
   * `null` until the container mounts (or on a surface that has none), in which
   * case every card falls into the unplaced group rather than being mispositioned.
   */
  scrollRoot: HTMLElement | null;
  /** A resolve/reopen mutation is in flight — the lifecycle buttons are disabled. */
  statusPending?: boolean;
  /** An AskQuestions mutation is in flight — the Ask action is disabled. */
  askPending?: boolean;
  /**
   * The active slot is COMMITTED, so staged notes ride the next Amend rather than
   * a gate "Send back". Copy-only; it changes no routing.
   */
  committed?: boolean;
  /** Close a thread. Omitted on a surface with no review-status mutation. */
  onResolve?: ((id: string) => void) | undefined;
  /** Reopen a resolved thread. Omitted alongside {@link onResolve}. */
  onReopen?: ((id: string) => void) | undefined;
  /** Submit the staged QUESTIONS without a redraft. Omitted where asking is not possible. */
  onAsk?: (() => void) | undefined;
  /** Collapse the margin (narrow viewports open it again from the chrome header). */
  onCollapse: () => void;
}): ReactNode {
  const t = useTokens();
  const { comments, remove } = useComments();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [heights, setHeights] = useState<ReadonlyMap<string, number>>(new Map());
  const [tick, setTick] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);

  // Re-measure on scroll, on resize, and whenever the content under the scroll
  // root changes shape — at most once per animation frame, because measurement
  // reads layout and an unthrottled listener would thrash it on every event.
  // The MutationObserver is what catches an artifact that renders AFTER the
  // margin has already asked the registry for offsets: nothing else announces
  // that a row just enrolled itself (the registry's map is a ref, and mutating
  // it is deliberately silent).
  useEffect(() => {
    if (scrollRoot === null) return;
    let queued = false;
    let disposed = false;
    const bump = (): void => {
      if (queued || disposed) return;
      queued = true;
      requestAnimationFrame(() => {
        queued = false;
        if (disposed) return;
        setScrollTop(scrollRoot.scrollTop);
        setTick((n) => n + 1);
      });
    };
    bump();
    // CAPTURE phase: a `scroll` event does not bubble, but it DOES capture, and
    // some artifacts scroll in a nested container of their own (the glossary's
    // fill-mode card with its sticky search header) rather than moving the root.
    // Listening only on the root would leave every card frozen while the rows it
    // points at slid past underneath.
    scrollRoot.addEventListener('scroll', bump, { capture: true, passive: true });
    window.addEventListener('resize', bump);
    const ro = new ResizeObserver(bump);
    ro.observe(scrollRoot);
    const mo = new MutationObserver(bump);
    mo.observe(scrollRoot, { childList: true, subtree: true });
    return (): void => {
      disposed = true;
      scrollRoot.removeEventListener('scroll', bump, { capture: true });
      window.removeEventListener('resize', bump);
      ro.disconnect();
      mo.disconnect();
    };
  }, [scrollRoot]);

  // A staged REPLY belongs inside the thread it answers, not beside it as a card
  // of its own: it carries no anchor (the server routes it by `replyTo`), so it
  // would otherwise land in the unplaced group, detached from its conversation.
  const { stagedReplies, ownCards } = useMemo(() => {
    const replies = new Map<string, StagedReply[]>();
    const own: MarginItem[] = [];
    comments.forEach((c, index) => {
      const replyTo = c.replyTo ?? '';
      if (replyTo.length > 0) {
        replies.set(replyTo, [...(replies.get(replyTo) ?? []), { index, text: c.text }]);
        return;
      }
      // Keyed by the note's own minted id, NOT its position: discarding a note
      // shifts every later index, and an index-keyed `heights` entry would then
      // describe a different note for a frame — long enough for a card to paint
      // at the wrong size and shove its neighbours. `id` is absent only on notes
      // persisted before it existed, which fall back to the old behaviour.
      own.push({
        key: `staged-${c.id ?? String(index)}`,
        kind: 'staged',
        index,
        comment: c,
      });
    });
    return { stagedReplies: replies, ownCards: own };
  }, [comments]);

  const items = useMemo<MarginItem[]>(
    () => [...thread.map((e): MarginItem => ({ key: e.id, kind: 'thread', entry: e })), ...ownCards],
    [thread, ownCards]
  );

  // A stable, de-duplicated path list: `useAnchorOffsets` memoizes on this array's
  // identity, so rebuilding it every render would re-measure every render.
  const paths = useMemo(() => [...new Set(items.map(anchorPathOf))].filter((p) => p.length > 0), [
    items,
  ]);
  const offsets = useAnchorOffsets(paths, scrollRoot, tick);
  const scrollAnchorIntoView = useScrollAnchorIntoView();

  const { placed, unplaced } = useMemo(() => {
    const anchored: MarginCard[] = [];
    const orphans: MarginItem[] = [];
    for (const item of items) {
      const top = offsets.get(anchorPathOf(item));
      if (top === undefined) orphans.push(item);
      else
        anchored.push({
          id: item.key,
          desiredTop: top,
          height: heights.get(item.key) ?? ASSUMED_CARD_HEIGHT,
        });
    }
    return { placed: stackCards(anchored, GAP), unplaced: orphans };
  }, [items, offsets, heights]);

  const byKey = useMemo(() => new Map(items.map((i) => [i.key, i])), [items]);

  // A fresh ref callback per render means React detaches and re-attaches it every
  // pass, so each card re-measures on every render — which is what we want, since
  // a card's height changes when it expands, collapses or gains a staged reply.
  // It cannot loop: `setHeights` returns the SAME map when the height is
  // unchanged, so a settled layout re-renders nothing.
  const measure =
    (key: string): ((el: HTMLDivElement | null) => void) =>
    (el: HTMLDivElement | null): void => {
      if (el === null) return;
      const h = el.getBoundingClientRect().height;
      setHeights((prev) => (prev.get(key) === h ? prev : new Map(prev).set(key, h)));
    };

  const renderItem = (item: MarginItem): ReactNode =>
    item.kind === 'thread' ? (
      <MarginThreadCard
        active={activeId === item.key}
        entry={item.entry}
        stagedReplies={stagedReplies.get(item.entry.id) ?? []}
        statusPending={statusPending}
        onActivate={() => {
          setActiveId(item.key);
        }}
        onDiscardStaged={remove}
        onJumpToAnchor={() => {
          scrollAnchorIntoView(item.entry.anchor);
        }}
        onReopen={onReopen}
        onResolve={onResolve}
      />
    ) : (
      <StagedNoteCard
        comment={item.comment}
        committed={committed}
        index={item.index}
        t={t}
        onDiscard={() => {
          remove(item.index);
        }}
        onJumpToAnchor={() => {
          scrollAnchorIntoView(item.comment.anchor?.jsonPath ?? '');
        }}
      />
    );

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Margin.ROOT}
      sx={{ height: '100%', display: 'flex', flexDirection: 'column', bgcolor: t.paperAlt }}
    >
      {/* The placement viewport. It carries NO header of its own: the margin's top
          edge must line up with the scroll container's top edge, or every card
          would sit one header-height below the row it belongs to. The collapse
          control therefore floats over the top-right corner instead. */}
      <Box sx={{ flexGrow: 1, minHeight: 0, position: 'relative', overflow: 'hidden' }}>
        <IconButton
          aria-label="collapse comment margin"
          size="small"
          sx={{ position: 'absolute', top: 4, right: 4, zIndex: 3, color: t.muted }}
          onClick={onCollapse}
        >
          <ChevronRightIcon fontSize="small" />
        </IconButton>

        {/* Unanchored threads and free-form notes have no row to sit beside, so
            they pin to the top of the margin under their own heading. */}
        {unplaced.length > 0 ? (
          <Box
            data-testid={UI_IDENTIFIERS.Margin.UNPLACED}
            sx={{
              position: 'absolute',
              top: 0,
              left: 0,
              right: 0,
              zIndex: 2,
              maxHeight: '55%',
              overflowY: 'auto',
              p: 1,
              pr: 4,
              display: 'flex',
              flexDirection: 'column',
              gap: 1,
              bgcolor: t.paperAlt,
              borderBottom: `1.5px solid ${t.line}`,
            }}
          >
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 9.5,
                fontWeight: 700,
                letterSpacing: '0.1em',
                color: t.muted,
              }}
            >
              UNPLACED
            </Typography>
            {unplaced.map((item) => (
              <Box key={item.key} ref={measure(item.key)}>
                {renderItem(item)}
              </Box>
            ))}
          </Box>
        ) : null}

        {/* The content-coordinate layer, slid to viewport space by -scrollTop. */}
        <Box
          sx={{
            position: 'absolute',
            top: 0,
            left: 0,
            right: 0,
            transform: `translateY(${String(-scrollTop)}px)`,
          }}
        >
          {placed.map((p) => {
            const item = byKey.get(p.id);
            if (item === undefined) return null;
            return (
              <Box
                key={p.id}
                ref={measure(p.id)}
                sx={{ position: 'absolute', top: p.top, left: 8, right: 8 }}
              >
                {renderItem(item)}
              </Box>
            );
          })}
        </Box>
      </Box>

      <MarginComposer askPending={askPending} committed={committed} t={t} onAsk={onAsk} />
    </Box>
  );
}

/**
 * A note the reviewer staged this cycle but has not sent. It is deliberately NOT
 * styled like a thread card: it has no status, no reply box and no Resolve —
 * there is nothing to converse with yet. It carries its anchor reference (so it
 * still links back) and a discard.
 */
function StagedNoteCard({
  comment,
  committed,
  index,
  onDiscard,
  onJumpToAnchor,
  t,
}: {
  comment: PostedComment;
  committed: boolean;
  /** Its position in the accumulator — the stable handle for its test ids. */
  index: number;
  onDiscard: () => void;
  onJumpToAnchor: () => void;
  t: Tokens;
}): ReactNode {
  const [confirming, setConfirming] = useState(false);
  const question = isQuestion(comment);
  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Margin.staged(index)}
      sx={{ p: 1.25, border: `1.5px dashed ${t.accent}`, bgcolor: t.paper }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 9.5,
            fontWeight: 700,
            letterSpacing: '0.08em',
            color: t.accent,
            flexGrow: 1,
          }}
        >
          {question ? `STAGED QUESTION → ${comment.addressee ?? 'pm'}` : 'STAGED · NOT SENT'}
        </Typography>
        {confirming ? (
          <>
            <Button
              data-testid={UI_IDENTIFIERS.Margin.stagedDiscard(index)}
              size="small"
              sx={{ color: t.dangerFg, fontSize: 10.5, minWidth: 0, textTransform: 'none', py: 0 }}
              onClick={onDiscard}
            >
              Discard
            </Button>
            <Button
              size="small"
              sx={{ color: t.muted, fontSize: 10.5, minWidth: 0, textTransform: 'none', py: 0 }}
              onClick={() => {
                setConfirming(false);
              }}
            >
              Keep
            </Button>
          </>
        ) : (
          <IconButton
            aria-label="discard staged note"
            size="small"
            sx={{ color: t.muted, p: 0.25 }}
            onClick={() => {
              setConfirming(true);
            }}
          >
            <CloseIcon sx={{ fontSize: 13 }} />
          </IconButton>
        )}
      </Box>

      {comment.anchor !== null ? (
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
          onClick={onJumpToAnchor}
        >
          <Box
            component="span"
            sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
          >
            {comment.anchor.label}
          </Box>
        </Button>
      ) : null}

      <Typography sx={{ fontSize: 13, lineHeight: 1.45, color: t.ink }}>{comment.text}</Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, mt: 0.5 }}>
        {question
          ? 'rides the next Ask (no redraft)'
          : committed
            ? 'rides your next Amend'
            : 'rides the next Send back'}
      </Typography>
    </Paper>
  );
}

/**
 * The composer, moved wholesale out of the deleted rail into the foot of the
 * margin. It still owns the one thing the margin cards cannot: opening a NEW
 * thread, anchored (when a selection is armed) or free-form.
 *
 * The `Ask` action stays here for now — Task 10's submit bar is where every
 * review verb finally converges, and it takes this button with it.
 */
function MarginComposer({
  askPending,
  committed,
  onAsk,
  t,
}: {
  askPending: boolean;
  committed: boolean;
  onAsk?: (() => void) | undefined;
  t: Tokens;
}): ReactNode {
  const { anchor, setAnchor, post, pendingQuestions, setDraftPending, enabled } = useComments();
  const [draft, setDraft] = useState('');
  const [commentType, setCommentType] = useState<ReviewCommentType>('changeRequest');
  const [addressee, setAddressee] = useState<Exclude<ReviewCommentAddressee, ''>>('pm');

  // UX-P1-3: CommentContext's re-anchor guard only engages while the composer
  // holds unsent text — keep it in sync with the draft field.
  useEffect(() => {
    setDraftPending(draft.length > 0);
  }, [draft, setDraftPending]);

  if (!enabled) return null;

  const pendingQuestionCount = pendingQuestions().length;
  const canSend = anchor !== null || draft.trim().length > 0;
  const submit = (): void => {
    if (!canSend) return;
    post(draft, {
      commentType,
      ...(commentType === 'question' ? { addressee } : {}),
    });
    setDraft('');
  };

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Margin.COMPOSER}
      sx={{ flexShrink: 0, p: 1.25, borderTop: `1.5px solid ${t.line}`, bgcolor: t.paperAlt }}
    >
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
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>
              {anchor.source}
            </Typography>
            <Typography
              sx={{
                fontSize: 12.5,
                color: t.ink,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {anchor.label}
            </Typography>
          </Box>
          <IconButton
            aria-label="clear armed anchor"
            size="small"
            sx={{ color: t.muted }}
            onClick={() => {
              setAnchor(null);
            }}
          >
            <CloseIcon sx={{ fontSize: 14 }} />
          </IconButton>
        </Box>
      )}

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 1, flexWrap: 'wrap' }}>
        <ComposerToggle
          active={commentType === 'changeRequest'}
          label="Change request"
          t={t}
          testid={UI_IDENTIFIERS.Chat.TYPE_CHANGE_REQUEST}
          onClick={() => {
            setCommentType('changeRequest');
          }}
        />
        <ComposerToggle
          active={commentType === 'question'}
          label="Question"
          t={t}
          testid={UI_IDENTIFIERS.Chat.TYPE_QUESTION}
          onClick={() => {
            setCommentType('question');
          }}
        />
        {commentType === 'question' ? (
          <>
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, ml: 0.5 }}>
              to
            </Typography>
            <ComposerToggle
              active={addressee === 'pm'}
              label="PM"
              t={t}
              testid={UI_IDENTIFIERS.Chat.ADDRESSEE_PM}
              onClick={() => {
                setAddressee('pm');
              }}
            />
            <ComposerToggle
              active={addressee === 'architect'}
              label="Architect"
              t={t}
              testid={UI_IDENTIFIERS.Chat.ADDRESSEE_ARCHITECT}
              onClick={() => {
                setAddressee('architect');
              }}
            />
          </>
        ) : null}
      </Box>

      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          border: `1.5px solid ${t.line}`,
          borderRadius: 1.5,
          px: 1.5,
          bgcolor: t.paper,
        }}
      >
        <InputBase
          multiline
          data-testid={UI_IDENTIFIERS.Chat.INPUT}
          maxRows={4}
          placeholder={
            commentType === 'question'
              ? 'Ask a question…'
              : anchor !== null
                ? 'Add your comment…'
                : committed
                  ? 'Type feedback for an amendment…'
                  : 'Type feedback for a redraft…'
          }
          sx={{ flexGrow: 1, fontSize: 13.5, py: 1, color: t.ink }}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
          }}
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
          sx={{
            bgcolor: t.accent,
            color: t.accentText,
            ml: 1,
            '&:hover': { bgcolor: t.accent2 },
            '&.Mui-disabled': { bgcolor: t.line },
          }}
          onClick={submit}
        >
          <SendIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Box>

      {onAsk !== undefined && pendingQuestionCount > 0 ? (
        <Button
          fullWidth
          data-testid={UI_IDENTIFIERS.Chat.ASK}
          disabled={askPending}
          size="small"
          startIcon={<QuestionAnswerOutlinedIcon sx={{ fontSize: 15 }} />}
          sx={{
            mt: 1,
            color: t.accentText,
            bgcolor: t.accent,
            textTransform: 'none',
            fontFamily: t.mono,
            fontSize: 12,
            '&:hover': { bgcolor: t.accent2 },
          }}
          variant="contained"
          onClick={onAsk}
        >
          {`Ask ${String(pendingQuestionCount)} question${pendingQuestionCount === 1 ? '' : 's'} (no redraft)`}
        </Button>
      ) : null}
    </Box>
  );
}

/** A compact pill toggle for the composer's type/addressee pickers. */
function ComposerToggle({
  active,
  label,
  onClick,
  t,
  testid,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
  t: Tokens;
  testid: string;
}): ReactNode {
  return (
    <Box
      aria-pressed={active}
      component="button"
      data-testid={testid}
      sx={{
        cursor: 'pointer',
        px: 1,
        py: 0.3,
        borderRadius: 99,
        fontFamily: t.mono,
        fontSize: 10.5,
        fontWeight: 700,
        letterSpacing: '0.03em',
        border: `1.5px solid ${active ? t.accent : t.line}`,
        bgcolor: active ? t.accent : 'transparent',
        color: active ? t.accentText : t.muted,
      }}
      onClick={onClick}
    >
      {label}
    </Box>
  );
}
