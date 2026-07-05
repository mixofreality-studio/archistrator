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
import Button from '@mui/material/Button';
import PlaceIcon from '@mui/icons-material/Place';
import SendIcon from '@mui/icons-material/ArrowUpward';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import CloseIcon from '@mui/icons-material/Close';
import FormatQuoteIcon from '@mui/icons-material/FormatQuote';
import AccountTreeOutlinedIcon from '@mui/icons-material/AccountTreeOutlined';
import ReplyIcon from '@mui/icons-material/SubdirectoryArrowRight';
import BlockIcon from '@mui/icons-material/Block';
import ReplayIcon from '@mui/icons-material/Replay';
import QuestionAnswerOutlinedIcon from '@mui/icons-material/QuestionAnswerOutlined';
import { useComments, type Anchor, type PostedComment } from '../comments/CommentContext';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import type {
  ReviewCommentAddressee,
  ReviewCommentStatus,
  ReviewCommentType,
  ReviewCommentView,
} from '../../contracts/types';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

/**
 * Thread ordering: open first (still owed), then addressed (has an agent
 * response), then waived (dismissed); within each group by round then id, so a
 * reader walks the ledger oldest-first within a lifecycle bucket.
 */
const STATUS_ORDER: Record<ReviewCommentStatus, number> = { open: 0, addressed: 1, waived: 2 };

function sortThread(thread: readonly ReviewCommentView[]): ReviewCommentView[] {
  return [...thread].sort(
    (a, b) =>
      STATUS_ORDER[a.status] - STATUS_ORDER[b.status] ||
      a.round - b.round ||
      a.id.localeCompare(b.id)
  );
}

function statusChipColors(status: ReviewCommentStatus, t: Tokens): { bg: string; fg: string } {
  switch (status) {
    case 'addressed':
      return { bg: t.committedBg, fg: t.committedFg };
    case 'waived':
      return { bg: t.paperAlt, fg: t.muted };
    case 'open':
      return { bg: t.awaitingBg, fg: t.awaitingFg };
  }
}

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
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10,
            fontWeight: 700,
            letterSpacing: '0.04em',
            whiteSpace: 'nowrap',
          }}
        >
          {anchor.source.split(' · ')[0]}
        </Typography>
        <Typography
          sx={{
            fontSize: 11.5,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            maxWidth: 200,
          }}
        >
          {anchor.label}
        </Typography>
      </Box>
    </Tooltip>
  );
}

export function ChatRail({
  onCollapse,
  thread = [],
  statusPending = false,
  askPending = false,
  onWaive,
  onReopen,
  onAsk,
}: {
  onCollapse: () => void;
  /** The durable server review-ledger thread for the active slot. */
  thread?: readonly ReviewCommentView[];
  /** A waive/reopen mutation is in flight — disable the per-entry actions. */
  statusPending?: boolean;
  /** An AskQuestions mutation is in flight — disable the Ask action. */
  askPending?: boolean;
  /** Dismiss an open entry. Omitted on read-only surfaces (no affordance). */
  onWaive?: (id: string) => void;
  /** Reopen an addressed entry. Omitted on read-only surfaces. */
  onReopen?: (id: string) => void;
  /**
   * Submit the pending QUESTIONS (question-comments) — a separate send from the gate's
   * "Send back", so asking never triggers a redraft. Omitted on surfaces that cannot ask
   * (the type picker still lets a comment be marked a question, but Ask is hidden).
   */
  onAsk?: () => void;
}): ReactNode {
  const t = useTokens();
  const { comments, anchor, setAnchor, post, remove, pendingQuestions } = useComments();
  const [draft, setDraft] = useState('');
  // Composer type + addressee (question-comments): a comment is a change-request by default;
  // marking it a question routes it to an addressee and off the redraft path.
  const [commentType, setCommentType] = useState<ReviewCommentType>('changeRequest');
  const [addressee, setAddressee] = useState<Exclude<ReviewCommentAddressee, ''>>('pm');
  const sortedThread = sortThread(thread);
  const pendingQuestionCount = pendingQuestions().length;

  // With an anchor armed, post the anchored comment (empty text gets a fallback);
  // with no anchor, post free-form feedback only when something was typed.
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
    <Paper
      data-testid={UI_IDENTIFIERS.Chat.RAIL}
      sx={{ height: '100%', display: 'flex', flexDirection: 'column', bgcolor: t.paperAlt }}
    >
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 2,
          py: 1.25,
          borderBottom: `1.5px solid ${t.line}`,
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.1em', fontSize: 12 }}
        >
          CO-AUTHOR
        </Typography>
        <Chip
          label="architect"
          size="small"
          sx={{ height: 20, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
          variant="outlined"
        />
        <Box sx={{ flexGrow: 1 }} />
        <IconButton
          aria-label="collapse chat"
          size="small"
          sx={{ color: t.ink }}
          onClick={onCollapse}
        >
          <ChevronRightIcon fontSize="small" />
        </IconButton>
      </Box>

      <Box
        sx={{
          flexGrow: 1,
          overflowY: 'auto',
          p: 2,
          display: 'flex',
          flexDirection: 'column',
          gap: 1.5,
        }}
      >
        {/* Durable review-ledger thread (server truth): every posted entry, its
            agent response, lifecycle status, and waive/reopen actions. */}
        {sortedThread.length > 0 ? (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, letterSpacing: '0.1em', color: t.muted }}
            >
              REVIEW THREAD
            </Typography>
            {sortedThread.map((entry) => (
              <ThreadEntry
                entry={entry}
                key={entry.id}
                statusPending={statusPending}
                t={t}
                onReopen={onReopen}
                onWaive={onWaive}
              />
            ))}
          </Box>
        ) : null}

        {/* Pending (client-side, unsent) notes: persist to localStorage, ride the
            next Send back, then the server thread becomes the source of truth. */}
        {comments.length === 0 ? (
          sortedThread.length === 0 ? (
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 11.5,
                color: t.muted,
                textAlign: 'center',
                my: 2,
                lineHeight: 1.6,
              }}
            >
              Type feedback to send back for a redraft — or click the comment button on any item (or
              a diagram node) to anchor it to that spot first. Everything here rides the next “Send
              back”.
            </Typography>
          ) : null
        ) : (
          <>
            {/* Always label the pending group so a staged (not-yet-sent) note is
                never mistaken for a sent thread entry — even on a surface with no
                prior server thread (this composer is reused across the design,
                project-design, and construction rails). */}
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.1em',
                color: t.muted,
                mt: sortedThread.length > 0 ? 1 : 0,
              }}
            >
              PENDING · NOT SENT
            </Typography>
            {comments.map((c, i) => (
              <CommentBubble
                c={c}
                index={i}
                key={i}
                t={t}
                onRemove={() => {
                  remove(i);
                }}
              />
            ))}
          </>
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
        {/* Composer type picker (question-comments): change-request (default, rides the
            next Send back) vs question (a non-blocking ask routed to an addressee). */}
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
              <Box sx={{ width: 8 }} />
              <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>to</Typography>
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
        {/* Ask — submit ONLY the pending questions, no redraft. Shown when this surface can
            ask and at least one question is staged. */}
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
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, mt: 0.75 }}>
          Embedded mode · agent conversation mirroring is stubbed in this build
        </Typography>
      </Box>
    </Paper>
  );
}

/** A compact pill toggle for the composer type/addressee pickers. */
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

/** A single durable review-ledger entry (co-author side: left-aligned). */
function ThreadEntry({
  entry,
  t,
  statusPending,
  onWaive,
  onReopen,
}: {
  entry: ReviewCommentView;
  t: Tokens;
  statusPending: boolean;
  onWaive?: ((id: string) => void) | undefined;
  onReopen?: ((id: string) => void) | undefined;
}): ReactNode {
  const chip = statusChipColors(entry.status, t);
  const hasAnchor = entry.anchor.length > 0 || entry.anchorText.length > 0;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Chat.threadEntry(entry.id)}
      sx={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', maxWidth: '96%' }}
    >
      {/* header: author role, round badge, status chip */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 0.25, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, color: t.ink }}>
          {entry.authorRole.length > 0 ? entry.authorRole : 'reviewer'}
        </Typography>
        <Chip
          label={`round ${String(entry.round)}`}
          size="small"
          sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono, bgcolor: t.paperAlt, color: t.muted }}
          variant="outlined"
        />
        <Chip
          label={entry.status}
          size="small"
          sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono, bgcolor: chip.bg, color: chip.fg }}
          variant="outlined"
        />
        {entry.type === 'question' ? (
          <Chip
            icon={<QuestionAnswerOutlinedIcon sx={{ fontSize: 11 }} />}
            label={entry.addressee.length > 0 ? `question → ${entry.addressee}` : 'question'}
            size="small"
            sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            variant="outlined"
          />
        ) : null}
      </Box>
      {/* anchor pill (reuse the pill styling; show the anchorText snippet) */}
      {hasAnchor ? (
        <Tooltip title={entry.anchor.length > 0 ? entry.anchor : 'anchored comment'}>
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
            <Typography
              sx={{
                fontSize: 11.5,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                maxWidth: 220,
              }}
            >
              {entry.anchorText.length > 0 ? entry.anchorText : entry.anchor}
            </Typography>
          </Box>
        </Tooltip>
      ) : null}
      {/* the reviewer's comment text */}
      <Box
        sx={{
          bgcolor: t.paperAlt,
          color: t.ink,
          border: `1.5px solid ${t.line}`,
          borderRadius: 1.5,
          px: 1.5,
          py: 1,
          fontSize: 13.5,
          lineHeight: 1.5,
          opacity: entry.status === 'waived' ? 0.65 : 1,
        }}
      >
        {entry.text}
      </Box>
      {/* the agent's response, visually distinct (indented reply) */}
      {entry.response.length > 0 ? (
        <Box sx={{ display: 'flex', gap: 0.5, mt: 0.5, ml: 1.5, maxWidth: '100%' }}>
          <ReplyIcon sx={{ fontSize: 15, color: t.chatArchitectFg, mt: 0.25 }} />
          <Box
            sx={{
              bgcolor: t.chatArchitectBg,
              color: t.chatArchitectFg,
              border: `1.5px solid ${t.accent}`,
              borderRadius: 1.5,
              px: 1.25,
              py: 0.75,
              fontSize: 12.5,
              lineHeight: 1.45,
            }}
          >
            {entry.response}
          </Box>
        </Box>
      ) : null}
      {/* per-entry lifecycle actions: open → Waive; addressed → Reopen */}
      <Box sx={{ display: 'flex', gap: 0.5, mt: 0.5 }}>
        {entry.status === 'open' && onWaive !== undefined ? (
          <Button
            data-testid={UI_IDENTIFIERS.Chat.threadWaive(entry.id)}
            disabled={statusPending}
            size="small"
            startIcon={<BlockIcon sx={{ fontSize: 14 }} />}
            sx={{ color: t.muted, fontSize: 11, minWidth: 0, textTransform: 'none' }}
            onClick={() => {
              onWaive(entry.id);
            }}
          >
            Waive
          </Button>
        ) : null}
        {entry.status === 'addressed' && onReopen !== undefined ? (
          <Button
            data-testid={UI_IDENTIFIERS.Chat.threadReopen(entry.id)}
            disabled={statusPending}
            size="small"
            startIcon={<ReplayIcon sx={{ fontSize: 14 }} />}
            sx={{ color: t.awaitingFg, fontSize: 11, minWidth: 0, textTransform: 'none' }}
            onClick={() => {
              onReopen(entry.id);
            }}
          >
            Reopen
          </Button>
        ) : null}
        {/* Amendment of a committed artifact is not a per-entry action — it lives on
            the committed panel's Amend affordance (CommittedArtifactPanel), which
            folds any pending rail comments into the amend rationale. */}
      </Box>
    </Box>
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
  // Two-step inline discard, matching the approve-gate's discard-confirm idiom:
  // the ✕ arms a confirm strip rather than dropping the staged note instantly.
  const [confirming, setConfirming] = useState(false);
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Chat.commentAnchor(index)}
      sx={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end' }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 0.25 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, color: t.ink }}>
          You
        </Typography>
        {c.commentType === 'question' ? (
          <Chip
            icon={<QuestionAnswerOutlinedIcon sx={{ fontSize: 11 }} />}
            label={`question → ${c.addressee ?? 'pm'}`}
            size="small"
            sx={{ height: 17, fontSize: 9.5, fontFamily: t.mono, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            variant="outlined"
          />
        ) : null}
        {confirming ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            <Typography sx={{ fontSize: 11, color: t.muted }}>Discard this note?</Typography>
            <Button
              aria-label="confirm discard comment"
              size="small"
              sx={{ color: t.dangerFg, fontSize: 11, minWidth: 0, textTransform: 'none', py: 0 }}
              variant="text"
              onClick={onRemove}
            >
              Discard
            </Button>
            <Button
              aria-label="cancel discard comment"
              size="small"
              sx={{ color: t.muted, fontSize: 11, minWidth: 0, textTransform: 'none', py: 0 }}
              variant="text"
              onClick={() => {
                setConfirming(false);
              }}
            >
              Cancel
            </Button>
          </Box>
        ) : (
          <IconButton
            aria-label="remove comment"
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
      <Box
        sx={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', maxWidth: '92%' }}
      >
        {c.anchor !== null ? <LocationPill anchor={c.anchor} t={t} /> : null}
        <Box
          sx={{
            bgcolor: t.accent,
            color: t.accentText,
            border: `1.5px solid ${t.line}`,
            borderRadius: 1.5,
            px: 1.5,
            py: 1,
            fontSize: 13.5,
            lineHeight: 1.5,
          }}
        >
          {c.text}
        </Box>
      </Box>
    </Box>
  );
}
