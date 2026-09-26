/**
 * Pure `PostedComment[]` → review-batch logic, extracted from CommentContext.tsx
 * so it is unit-testable under node:test (which cannot import .tsx files — same
 * posture as pendingCommentsStore.ts / disabledCommentContext.ts).
 *
 * A batch has three disjoint destinations, computed off the SAME accumulated
 * `comments` list:
 *   - `toWireEntries` — anchored change-requests, PLUS margin replies (a reply
 *     arms no anchor; the server locates the thread by `replyTo`, not by anchor).
 *   - `freeformNotesFrom` — free-form (unanchored, non-reply) change-request text.
 *   - questions (see CommentContext.tsx's `pendingQuestions`) — untouched here.
 *
 * A reply must land in EXACTLY ONE destination. A margin reply composed against
 * an existing thread carries `replyTo` and arms no anchor (`anchor === null`), so
 * without the `replyTo` carve-out below it would fall into `freeformNotesFrom`
 * and reach the server as anonymous prose, detached from its thread — and if it
 * were counted in BOTH functions, the same reply would double-ride the next
 * "Send back"/"Amend" as both a routed reply and a free-form note.
 */
import type { AnchoredComment } from '../../contracts/types';
import type { PendingQuestion, PostedComment } from './commentContextTypes';

/** True when a pending entry is a question (absent type ⇒ change-request). */
export function isQuestion(c: PostedComment): boolean {
  return c.commentType === 'question';
}

/** True when a pending entry answers an existing review-ledger thread. */
function hasReplyTo(c: PostedComment): boolean {
  return c.replyTo !== undefined && c.replyTo !== '';
}

/**
 * Maps the ANCHORED change-request entries, plus any REPLY entries regardless of
 * anchor, into the wire `AnchoredComment[]` shape (questions excluded). A reply
 * with no armed anchor is sent with an empty `jsonPath`/`anchorText` — the server
 * resolves it by `replyTo`.
 */
export function toWireEntries(comments: readonly PostedComment[]): AnchoredComment[] {
  const out: AnchoredComment[] = [];
  for (const c of comments) {
    if (isQuestion(c)) continue;
    if (c.anchor !== null) {
      // anchorText is the item's rendered-text snapshot; the label already carries
      // it for every arm surface, so fall back to it when no richer text was set.
      out.push({
        jsonPath: c.anchor.jsonPath,
        text: c.text,
        anchorText: c.anchor.anchorText ?? c.anchor.label,
        // Presence-required on the wire: empty ⇒ new thread, non-empty ⇒ reply.
        replyTo: c.replyTo ?? '',
      });
    } else if (hasReplyTo(c)) {
      // A margin reply arms no anchor — the thread is already identified by
      // `replyTo`, so jsonPath/anchorText carry nothing.
      out.push({
        jsonPath: '',
        text: c.text,
        anchorText: '',
        replyTo: c.replyTo ?? '',
      });
    }
  }
  return out;
}

/**
 * The FREE-FORM change-request entries joined into the reject `feedback.notes`
 * string. Excludes questions (separate "Ask" action) AND any entry with a
 * `replyTo` (routed instead through {@link toWireEntries} — excluding it here is
 * what keeps a reply from double-riding the batch as anonymous prose too).
 */
export function freeformNotesFrom(comments: readonly PostedComment[]): string {
  return comments
    .filter((c) => c.anchor === null && !isQuestion(c) && !hasReplyTo(c))
    .map((c) => c.text)
    .join('\n');
}

/**
 * FOLDS the anchored change-requests into the free-form notes, for the one rail
 * whose op carries no `comments` array of its own: `SubmitSDPDecision`, whose
 * body is `feedback.notes` and nothing else.
 *
 * Every other decision op takes `{ notes, comments }` and keeps the two apart, so
 * the anchor survives as structure. Here it cannot, and the choice is between
 * losing the comments entirely (what the M0 gate did: staged, counted on the bar,
 * then cleared by `reset()` with the approval recording none of them) and carrying
 * them as text. Text wins — the anchor path goes in front of each line so the
 * reader of the ledger can still tell what each note was pinned to.
 *
 * Order: the free-form notes first (they are the reviewer's own summary), then one
 * line per anchored comment in staging order. Empty pieces are dropped, so a batch
 * with only comments produces no leading blank line and an empty batch produces ''.
 */
/**
 * The feedback body ONE decision sends, and whether it may carry a `comments` array
 * at all.
 *
 * This is the rule, not a convenience. A decision that names an `optionId` routes to
 * `SubmitSDPDecision`, whose body is `feedback.notes` and nothing else, and whose
 * Phase-2 ledger REFUSES any batch carrying a `replyTo` (`pdCheckNoReplyTo`,
 * CONTROLLER RULING P13 — threaded Phase-2 replies are a Stage-2 deliverable, so a
 * reply arriving there could only be flattened into a fresh unanchored comment, and
 * it refuses loudly rather than detach it silently). The M0 gate DOES offer replies,
 * so sending the array turns "reply in a thread, then Approve" into a 400.
 *
 * Every other decision op takes `{ notes, comments }` and keeps the two apart, so
 * the anchor survives as structure.
 *
 * Returned WITHOUT a `comments` key when folded — not with an empty array — because
 * the wire distinguishes absent from empty and the Manager reads `feedback.Comments`.
 */
export function decisionFeedbackFor(input: {
  /** True for a decision that carries an optionId (the M0 commit). */
  fold: boolean;
  notes: string;
  comments: readonly AnchoredComment[];
}): { notes: string; comments?: AnchoredComment[] } {
  if (input.fold) {
    return { notes: foldCommentsIntoNotes(input.notes, input.comments) };
  }
  return {
    // The Manager requires non-empty reject feedback; when the reviewer only
    // anchored comments, the notes are synthesized from them so the redraft always
    // carries actionable guidance.
    notes: input.notes.length > 0 ? input.notes : input.comments.map((c) => c.text).join('\n'),
    comments: [...input.comments],
  };
}

export function foldCommentsIntoNotes(notes: string, comments: readonly AnchoredComment[]): string {
  const lines: string[] = [];
  if (notes.trim().length > 0) lines.push(notes);
  for (const c of comments) {
    if (c.text.trim().length === 0) continue;
    lines.push(c.jsonPath.length > 0 ? `${c.jsonPath} — ${c.text}` : c.text);
  }
  return lines.join('\n');
}

/**
 * The QUESTION entries, mapped into the "Ask" payload — the third destination, and
 * the one `toWireEntries`/`freeformNotesFrom` deliberately exclude.
 *
 * `replyTo` rides through: a question thread is the CONVERSATIONAL case, so a
 * follow-up staged against an answered question is an utterance IN that thread, not
 * a new ask. It travels on the same `AskQuestions` batch verb as a fresh question
 * (a reply never dispatches on its own), and the server routes it by `replyTo`. A
 * fresh question carries `''` — the presence-required wire convention for "open a
 * new thread".
 */
export function pendingQuestionsFrom(comments: readonly PostedComment[]): PendingQuestion[] {
  return comments.filter(isQuestion).map((c) => ({
    addressee: c.addressee ?? 'pm',
    jsonPath: c.anchor?.jsonPath ?? '',
    text: c.text,
    anchorText: c.anchor?.anchorText ?? c.anchor?.label ?? '',
    replyTo: c.replyTo ?? '',
  }));
}
