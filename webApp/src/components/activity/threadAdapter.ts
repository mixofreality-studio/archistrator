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

type ThreadWire = components['schemas']['DeliveryReviewThreadComment'];
type ReplyWire = components['schemas']['DeliveryReviewThreadReply'];

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

/**
 * The threads that BLOCK APPROVE (`submitVerb.openThreads`) — the client half of
 * `projectstate.ReviewCommentBlocksApprove`, which is the rule the server enforces:
 * `status == open && !question`.
 *
 * TWO facts, and both are the server's, not this module's opinion:
 *
 *   - a QUESTION never blocks. Doctrine makes an open question "a soft warning at
 *     the approve gate, never a hard block", and `OpenReviewCommentIDs` excludes
 *     them, so counting one here would grey out an Approve the server accepts.
 *   - an ANSWERED change request does not block either. It used to (`!== 'resolved'`),
 *     which was the mirror of the defect stage 4b1 is fixing: the reviewer saw
 *     "Resolve 2 threads to approve" on a gate `deliveryManager` would have taken,
 *     with no way to clear it — the answered thread is the AGENT's reply, and only
 *     the reviewer's own resolve moves it, so the bar demanded work of itself. The
 *     server's precondition (`refuseApproveOverOpenComments`) is what this mirrors,
 *     to the field.
 */
export function openThreadCount(thread: readonly ReviewCommentView[]): number {
  return thread.filter((c) => c.type === 'changeRequest' && c.status === 'open').length;
}
