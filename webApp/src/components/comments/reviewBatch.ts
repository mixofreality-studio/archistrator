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
import type { PostedComment } from './commentContextTypes';

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
