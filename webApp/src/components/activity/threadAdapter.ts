/**
 * The stage-3 review round's thread, in the shape the comment margin already
 * speaks. `CommentMargin` / `MarginThreadCard` were built for the design
 * rail's `ReviewCommentView`; the unified rail serves
 * `ConstructionReviewThreadComment`, whose members are parallel but not
 * identical (`anchorText` and `addressee` are optional on the wire and
 * required in the view; the view carries a deprecated `response` nothing
 * reads). Adapting is a dozen lines; rewriting the margin is not.
 */
import type { components } from '../../contracts/schema.ts';
import type { ReviewCommentView, ReviewCommentReply } from '../../contracts/types.ts';

type ThreadWire = components['schemas']['ConstructionReviewThreadComment'];
type ReplyWire = components['schemas']['ConstructionReviewThreadReply'];

function replyOf(r: ReplyWire): ReviewCommentReply {
  return { id: r.id, authorRole: r.authorRole, text: r.text, at: r.at };
}

/**
 * `staleAck` has no counterpart in the view's two-value `type` — it is an
 * audit entry, not a change request and not a question. It maps to
 * `changeRequest` (the wire's own migration-safe default) and the margin
 * renders it as an ordinary thread: dropping it would hide a recorded
 * acknowledgement from the history it belongs to.
 */
export function toReviewCommentView(c: ThreadWire): ReviewCommentView {
  return {
    id: c.id,
    anchor: c.anchor,
    anchorText: c.anchorText ?? '',
    text: c.text,
    authorRole: c.authorRole,
    round: c.round,
    status: c.status,
    replies: c.replies.map(replyOf),
    reopened: c.reopened,
    type: c.type === 'question' ? 'question' : 'changeRequest',
    addressee: c.addressee === 'pm' || c.addressee === 'architect' ? c.addressee : '',
  };
}

export function toReviewThread(thread: readonly ThreadWire[] | undefined): ReviewCommentView[] {
  return (thread ?? []).map(toReviewCommentView);
}

/** Threads still owed an answer — what blocks approve (`submitVerb.openThreads`). */
export function openThreadCount(thread: readonly ReviewCommentView[]): number {
  return thread.filter((c) => c.type === 'changeRequest' && c.status !== 'resolved').length;
}
