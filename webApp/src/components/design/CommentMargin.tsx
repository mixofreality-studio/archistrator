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
 * ── Why there is no scroll compensation here (Task 8b) ─────────────────────
 * `useAnchorOffsets` returns each anchor's offset within the scroll CONTENT: it
 * measures the row's live viewport top against a content origin of
 * `root.top - root.scrollTop`, which ADDS the root's scrollTop back in. Those
 * numbers are stable while the reader scrolls.
 *
 * `ExperienceChrome` puts the content column and this margin inside ONE scroll
 * container, and this component renders inside it. Its root is therefore itself
 * in content space — it scrolls with the rows — so a card positioned at
 * `top: <anchor offset>` is level with its row at every scroll position, with no
 * transform and no `scrollTop` state. The margin previously owned a second
 * scroller and cancelled it with `translateY(-scrollTop)`; that compensation
 * existed only to bridge two scrollers, and went out with the second one.
 *
 * The ONE case that still needs its own scroll is the narrow-viewport DRAWER
 * (<1100px), where the chrome lifts this column out of the scroller and pins it
 * over the content. A pinned column cannot be in content space, so there cards
 * are NOT anchor-placed: they render as a plain ordered list (document order,
 * from the same stacking pass) inside the drawer's own scroll. Nothing is
 * anchored to a row the drawer is covering anyway.
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import InputBase from '@mui/material/InputBase';
import Tooltip from '@mui/material/Tooltip';
import useMediaQuery from '@mui/material/useMediaQuery';
import CloseIcon from '@mui/icons-material/Close';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import PlaceIcon from '@mui/icons-material/Place';
import FormatQuoteIcon from '@mui/icons-material/FormatQuote';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import AddCommentOutlinedIcon from '@mui/icons-material/AddCommentOutlined';

import { useComments } from '../comments/CommentContext';
import { useAnchorOffsets, useScrollAnchorIntoView } from '../comments/AnchorRegistry';
import { stackCards, type MarginCard } from '../comments/commentMarginLayout';
import { isQuestion } from '../comments/reviewBatch';
import type { Anchor, PostedComment } from '../comments/commentContextTypes';
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
 * takes more of the reading area than the content can spare. Exported as raw
 * media text, not an `@media` block, because both sides ASK rather than style:
 * the chrome mounts the margin in a different place, and this component lays its
 * cards out as a list instead of placing them — a pinned drawer is not in content
 * space. (There was an `@media` sibling export; nothing styles on it any more.)
 */
export const MARGIN_DRAWER_MEDIA = '(max-width: 1100px)';

/**
 * The empty thread, hoisted to module scope. A `thread = []` default parameter
 * mints a NEW array every render, which re-identifies `items` and `paths` and so
 * forces `useAnchorOffsets` to re-measure the DOM on every render — on the
 * Construction console, which polls every 1.5s and passes no thread at all, that
 * is a full re-measure per poll forever.
 */
const EMPTY_THREAD: readonly ReviewCommentView[] = [];

/** The single in-place draft card's key — there is at most one open at a time. */
const DRAFT_KEY = 'draft';

/**
 * One placeable margin entry: a server thread, a locally staged note, or the
 * in-place DRAFT the reviewer is composing. The draft is a first-class item on
 * purpose (Task 8b): it goes through the same {@link stackCards} pass as the
 * rest, so it opens exactly where its finished comment will sit and pushes its
 * neighbours down exactly as that comment will.
 */
type MarginItem =
  | { key: string; kind: 'thread'; entry: ReviewCommentView }
  | { key: string; kind: 'staged'; index: number; comment: PostedComment }
  | { key: typeof DRAFT_KEY; kind: 'draft'; anchor: Anchor | null };

/** The anchor JSONPath an item wants to sit level with ('' ⇒ unanchored). */
function anchorPathOf(item: MarginItem): string {
  if (item.kind === 'thread') return item.entry.anchor;
  if (item.kind === 'draft') return item.anchor?.jsonPath ?? '';
  return item.comment.anchor?.jsonPath ?? '';
}

export function CommentMargin({
  thread = EMPTY_THREAD,
  scrollRoot,
  statusPending = false,
  committed = false,
  onResolve,
  onReopen,
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
  /**
   * The active slot is COMMITTED, so staged notes ride the next Amend rather than
   * a gate "Send back". Copy-only; it changes no routing.
   */
  committed?: boolean;
  /** Close a thread. Omitted on a surface with no review-status mutation. */
  onResolve?: ((id: string) => void) | undefined;
  /** Reopen a resolved thread. Omitted alongside {@link onResolve}. */
  onReopen?: ((id: string) => void) | undefined;
  /** Collapse the margin (narrow viewports open it again from the chrome header). */
  onCollapse: () => void;
}): ReactNode {
  const t = useTokens();
  const { comments, remove, anchor, setAnchor, enabled } = useComments();
  // The drawer is PINNED over the content rather than living in content space,
  // so it cannot place cards by anchor offset — see the file header.
  const drawer = useMediaQuery(MARGIN_DRAWER_MEDIA, { noSsr: true });
  const [activeId, setActiveId] = useState<string | null>(null);
  const [heights, setHeights] = useState<ReadonlyMap<string, number>>(new Map());
  const [tick, setTick] = useState(0);
  // A free-form (unanchored) draft is open. An ANCHORED draft needs no flag of
  // its own: an armed anchor IS the open draft (arming a row is what opens it),
  // and an armed anchor OUTRANKS this flag — arming a row while a free-form draft
  // is open simply re-files the same card (and its half-typed text) against that
  // row, which is what the reviewer meant. The flag is cleared wherever a draft
  // ends (cancel, stage), so it never outlives the card.
  const [freeform, setFreeform] = useState(false);

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
        setTick((n) => n + 1);
      });
    };
    bump();
    // CAPTURE phase: a `scroll` event does not bubble, but it DOES capture, and
    // some artifacts scroll in a NESTED container of their own rather than moving
    // the root. A nested scroll really does move a row within content space, so
    // those offsets must be re-taken.
    //
    // The ROOT's own scroll is filtered out. Offsets are in CONTENT space and the
    // cards ride along with the content, so scrolling the page changes not one of
    // them — before this guard, every frame of ordinary scrolling re-measured every
    // anchor in the DOM to arrive at the numbers it already had.
    const onScroll = (e: Event): void => {
      if (e.target === scrollRoot) return;
      bump();
    };
    scrollRoot.addEventListener('scroll', onScroll, { capture: true, passive: true });
    window.addEventListener('resize', bump);
    const ro = new ResizeObserver(bump);
    ro.observe(scrollRoot);
    const mo = new MutationObserver(bump);
    mo.observe(scrollRoot, { childList: true, subtree: true });
    return (): void => {
      disposed = true;
      scrollRoot.removeEventListener('scroll', onScroll, { capture: true });
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

  // Arming a row's comment button opens the draft card IN PLACE — the composer
  // that used to sit at the foot of the margin is gone (Task 8b).
  const draftOpen = enabled && (anchor !== null || freeform);

  const items = useMemo<MarginItem[]>(() => {
    const list: MarginItem[] = [
      ...thread.map((e): MarginItem => ({ key: e.id, kind: 'thread', entry: e })),
      ...ownCards,
    ];
    if (draftOpen) list.push({ key: DRAFT_KEY, kind: 'draft', anchor });
    return list;
  }, [thread, ownCards, draftOpen, anchor]);

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

  const closeDraft = (): void => {
    setAnchor(null);
    setFreeform(false);
  };

  const renderItem = (item: MarginItem): ReactNode =>
    item.kind === 'draft' ? (
      <MarginDraftCard
        anchor={item.anchor}
        committed={committed}
        t={t}
        onCancel={closeDraft}
        onStaged={() => {
          setFreeform(false);
        }}
      />
    ) : item.kind === 'thread' ? (
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
      sx={
        drawer
          ? {
              // Pinned overlay: its own scroll, because it is NOT in content space.
              height: '100%',
              overflowY: 'auto',
              display: 'flex',
              flexDirection: 'column',
              gap: 1,
              p: 1,
              bgcolor: t.bg,
            }
          : {
              // In content space, inside the chrome's shared scroller. No background
              // and no border: the page shows through and the cards read as notes in
              // the margin, not as a docked panel. `relative` makes this box the
              // containing block for the placed cards, whose `top` IS the anchor's
              // offset within the shared scroll content — no transform needed.
              // `height: 100%` spans the whole PAGE (the chrome stretches the column
              // to the document's height, not the viewport's), which is what gives
              // the sticky controls above something to stick within all the way down.
              position: 'relative',
              height: '100%',
            }
      }
    >
      {/* A zero-height sticky strip: the margin's own two controls float over the
          top-right corner and stay reachable at any scroll depth without occupying
          a band of margin that a card wants to sit in. */}
      <Box
        sx={{
          position: 'sticky',
          top: 0,
          height: 0,
          zIndex: 5,
          display: 'flex',
          justifyContent: 'flex-end',
          alignItems: 'flex-start',
          gap: 0.25,
          pt: 0.5,
          pr: 0.5,
        }}
      >
        {enabled ? (
          <Tooltip title="Add a note — not tied to a row">
            <IconButton
              aria-label="add an unanchored note"
              data-testid={UI_IDENTIFIERS.Margin.ADD_NOTE}
              size="small"
              sx={{ color: t.muted }}
              onClick={() => {
                // An armed anchor would be consumed by `post`, quietly turning this
                // free-form note into an anchored one. Disarm first.
                setAnchor(null);
                setFreeform(true);
              }}
            >
              <AddCommentOutlinedIcon sx={{ fontSize: 16 }} />
            </IconButton>
          </Tooltip>
        ) : null}
        <IconButton
          aria-label="collapse comment margin"
          size="small"
          sx={{ color: t.muted }}
          onClick={onCollapse}
        >
          <ChevronRightIcon fontSize="small" />
        </IconButton>
      </Box>

      {drawer ? (
        <>
          {unplaced.length > 0 ? (
            <Box
              data-testid={UI_IDENTIFIERS.Margin.UNPLACED}
              sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}
            >
              <UnplacedHeading t={t} />
              {unplaced.map((item) => (
                <Box key={item.key} ref={measure(item.key)}>
                  {renderItem(item)}
                </Box>
              ))}
            </Box>
          ) : null}
          {/* Document order: `placed` is already sorted by the stacking pass. The
              unanchored group above leads it — those items belong to no row. */}
          {placed.map((p) => {
            const item = byKey.get(p.id);
            if (item === undefined) return null;
            return (
              <Box key={p.id} ref={measure(p.id)}>
                {renderItem(item)}
              </Box>
            );
          })}
        </>
      ) : (
        <>
          {/* Unanchored threads, free-form notes and a free-form draft have no row
              to sit beside, so they lead the margin under their own heading.
              STICKY, unlike every placed card: an item with no anchor has no
              position in the document to scroll away with, and a free-form draft
              opened from ＋ while the reader is halfway down the page would
              otherwise open above the fold, out of sight. */}
          {unplaced.length > 0 ? (
            <Box
              data-testid={UI_IDENTIFIERS.Margin.UNPLACED}
              sx={{
                position: 'sticky',
                top: 0,
                // Above the placed layer: an item anchored to the very first row
                // wants the same band of margin, and this group was here first.
                zIndex: 2,
                // A sticky block taller than the scrollport can never have its
                // bottom scrolled into view, and its opaque background would mask
                // every placed card behind it for good. Construction — where every
                // note is unanchored — is exactly that case. Cap it below the
                // scrollport (the chrome above is nowhere near 50vh) and let the
                // overflow scroll inside the group.
                maxHeight: '50vh',
                overflowY: 'auto',
                p: 1,
                pr: 4,
                display: 'flex',
                flexDirection: 'column',
                gap: 1,
                // Matches the page, and masks whatever stacks underneath.
                bgcolor: t.bg,
              }}
            >
              <UnplacedHeading t={t} />
              {unplaced.map((item) => (
                <Box key={item.key} ref={measure(item.key)}>
                  {renderItem(item)}
                </Box>
              ))}
            </Box>
          ) : null}

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
        </>
      )}
    </Box>
  );
}

/** The mono "UNPLACED" label above the unanchored group. */
function UnplacedHeading({ t }: { t: Tokens }): ReactNode {
  return (
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
 * The IN-PLACE draft card (Task 8b, founder correction #2).
 *
 * Arming a row's comment button used to focus a composer at the FOOT of the
 * margin — a panel affordance: you typed in one place and the comment appeared
 * in another. This card opens where the finished comment will live. It is a
 * {@link MarginItem} like any other, so the same {@link stackCards} pass places
 * it level with its row and pushes its neighbours down exactly as the staged
 * card will once you press Comment.
 *
 * It owns the one thing the other margin cards cannot: opening a NEW thread —
 * anchored (a row armed it) or free-form (the margin's ＋ affordance opened it)
 * — as either a change request or a question. Comment STAGES it into
 * `CommentContext`; nothing dispatches until the next batch verb.
 *
 * The `Ask` action that used to sit beside it moved to Task 10's submit bar
 * (Ruling P17) — every review verb converges through that one bar.
 */
function MarginDraftCard({
  anchor,
  committed,
  onCancel,
  onStaged,
  t,
}: {
  /** The armed anchor this draft is filed against, or `null` for a free-form note. */
  anchor: Anchor | null;
  committed: boolean;
  /** Discard the draft and disarm the anchor. */
  onCancel: () => void;
  /**
   * The draft was staged. `post` clears an ARMED anchor itself (which unmounts an
   * anchored draft); this closes the free-form case, which has no anchor to clear.
   */
  onStaged: () => void;
  t: Tokens;
}): ReactNode {
  const { post, setDraftPending, anchorRefusals } = useComments();
  const [draft, setDraft] = useState('');
  const [commentType, setCommentType] = useState<ReviewCommentType>('changeRequest');
  const [addressee, setAddressee] = useState<Exclude<ReviewCommentAddressee, ''>>('pm');
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  // CommentContext refuses to move an armed anchor while this card holds unsent
  // text (it would strand the half-typed comment on the wrong row). That refusal
  // is right, but it used to be SILENT — the reviewer pressed another row's
  // comment button and nothing happened. The counter resets on every accepted
  // arm/disarm, so a non-zero value here always means THIS card blocked it.
  const blockedAnotherRow = anchorRefusals > 0;

  // Google-Docs behaviour: the card opens ready to type. `preventScroll` because
  // the card and the shared scroller now live in the SAME scroll container — a
  // focus that scrolled would yank the reader off the row they just armed.
  useEffect(() => {
    inputRef.current?.focus({ preventScroll: true });
  }, []);

  // UX-P1-3: CommentContext's re-anchor guard only engages while a composer holds
  // unsent text — keep it in sync, and clear it when the card goes away (cancel,
  // stage, or the artifact changing underneath it).
  useEffect(() => {
    setDraftPending(draft.length > 0);
  }, [draft, setDraftPending]);
  useEffect(
    () => (): void => {
      setDraftPending(false);
    },
    [setDraftPending]
  );

  const canSend = anchor !== null || draft.trim().length > 0;
  const submit = (): void => {
    if (!canSend) return;
    post(draft, {
      commentType,
      ...(commentType === 'question' ? { addressee } : {}),
    });
    setDraft('');
    onStaged();
  };

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.Margin.COMPOSER}
      sx={{ p: 1.25, border: `1.5px solid ${t.accent}`, bgcolor: t.paper }}
    >
      {anchor !== null && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 1, minWidth: 0 }}>
          {anchor.kind === 'node' ? (
            <AccountTreeOutlinedIcon sx={{ fontSize: 15, color: t.accent, flexShrink: 0 }} />
          ) : (
            <FormatQuoteIcon sx={{ fontSize: 15, color: t.accent, flexShrink: 0 }} />
          )}
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
              {anchor.source}
            </Typography>
            <Typography
              sx={{
                fontSize: 12,
                color: t.ink,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {anchor.label}
            </Typography>
          </Box>
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
          border: `1.5px solid ${t.line}`,
          borderRadius: 1.5,
          px: 1.25,
          py: 0.25,
          bgcolor: t.paper,
        }}
      >
        <InputBase
          multiline
          data-testid={UI_IDENTIFIERS.Chat.INPUT}
          inputRef={inputRef}
          maxRows={8}
          placeholder={
            commentType === 'question'
              ? 'Ask a question…'
              : anchor !== null
                ? 'Add your comment…'
                : committed
                  ? 'Type feedback for an amendment…'
                  : 'Type feedback for a redraft…'
          }
          sx={{ width: '100%', fontSize: 13.5, py: 0.5, color: t.ink }}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault();
              onCancel();
              return;
            }
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
        />
      </Box>

      {blockedAnotherRow ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Margin.DRAFT_BLOCKING}
          role="status"
          sx={{ fontFamily: t.mono, fontSize: 10, color: t.accent, mt: 0.75 }}
        >
          Comment or Cancel first — this draft is holding the comment button on other
          rows, so it cannot be moved off {anchor !== null ? 'this one' : 'the margin'}.
        </Typography>
      ) : null}

      <Box
        sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mt: 1, justifyContent: 'flex-end' }}
      >
        <Button
          size="small"
          sx={{ color: t.muted, fontSize: 12, textTransform: 'none', minWidth: 0, px: 1 }}
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button
          data-testid={UI_IDENTIFIERS.Chat.SEND}
          disabled={!canSend}
          size="small"
          sx={{
            bgcolor: t.accent,
            color: t.accentText,
            fontSize: 12,
            fontWeight: 700,
            textTransform: 'none',
            minWidth: 0,
            px: 1.5,
            '&:hover': { bgcolor: t.accent2 },
            '&.Mui-disabled': { bgcolor: t.line, color: t.muted },
          }}
          onClick={submit}
        >
          Comment
        </Button>
      </Box>
    </Paper>
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
