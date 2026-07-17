/**
 * Type definitions for CommentContext, extracted to avoid circular imports.
 */

import type {
  AnchoredComment,
  ReviewCommentAddressee,
  ReviewCommentType,
} from '../../contracts/types';

/** A pending selection the architect may turn into an anchored comment. */
export interface Anchor {
  /** Discriminates the selection origin for the chip icon + copy. */
  kind: 'text' | 'node';
  /** Short human label shown on the chip (node name / quoted text). */
  label: string;
  /** Provenance, e.g. "Architecture · C4" or "Volatilities · axis map". */
  source: string;
  /** The JSONPath into the typed model this selection refers to. */
  jsonPath: string;
  /**
   * The anchored item's RENDERED text snapshot, sent to the review ledger as
   * `anchorText`. Optional at the arm sites: when omitted, `toWire` falls back to
   * `label` (which already carries the item's content for every arm surface).
   * SelectionPopover sets it to the full, untruncated quote.
   */
  anchorText?: string;
}

/**
 * One posted entry this gate cycle: the architect's text plus the location it
 * anchors to — or `null` anchor for FREE-FORM feedback typed without arming a
 * selection. Both ride the next "Send back": anchored entries become the wire
 * `comments`, free-form entries become the reject `feedback` notes.
 */
export interface PostedComment {
  text: string;
  anchor: Anchor | null;
  /**
   * Change-request (default) or a non-blocking question (question-comments). A
   * change-request rides the next "Send back" (reject/redraft); a question rides the
   * separate "Ask" action and never triggers a redraft. Absent ⇒ 'changeRequest'
   * (migration-safe for any persisted pending entry from before this field existed).
   */
  commentType?: ReviewCommentType;
  /** For a question, the role it is addressed to (pm/architect). */
  addressee?: ReviewCommentAddressee;
}

/** Options carried on {@link CommentCtx.post} for a question (vs a plain change-request). */
export interface PostOptions {
  commentType?: ReviewCommentType;
  addressee?: ReviewCommentAddressee;
}

/** A pending question grouped for the "Ask" action: its addressee + anchored payload. */
export interface PendingQuestion {
  addressee: ReviewCommentAddressee;
  jsonPath: string;
  text: string;
  anchorText: string;
}

export interface CommentCtx {
  /**
   * Whether commenting is active in this context. `true` inside the design /
   * construction review experiences; `false` for read-only renderings (the home
   * base). When `false`, `setAnchor` is a no-op, no test probe is emitted, and
   * every comment affordance renders nothing — so a read-only surface shows zero
   * comment UI (no icons, no hover chrome, no selection popover, no tab stops).
   */
  enabled: boolean;
  /** Entries accumulated this gate cycle (anchored + free-form), oldest first. */
  comments: PostedComment[];
  /** The currently-armed selection (drives the chat composer affordance). */
  anchor: Anchor | null;
  /** Arm/disarm a selection. Arming bumps `requestId` so the rail can open.
   *  Guarded: while an unsent composer draft is pending (see {@link setDraftPending}),
   *  re-arming to a DIFFERENT anchor is refused so the draft stays paired with the
   *  location it was written against (no silent re-target). Disarming (null) and
   *  re-arming the same anchor always pass. */
  setAnchor: (a: Anchor | null) => void;
  /**
   * Signal from the composer that it holds unsent draft text (true) or is empty
   * (false). Drives {@link setAnchor}'s re-anchor guard so a half-typed comment
   * cannot be silently retargeted onto a different node by a later arm. The
   * composer (ChatRail) is expected to call this as its draft text changes; until
   * it does, this stays false and arming behaves exactly as before.
   */
  setDraftPending: (pending: boolean) => void;
  /**
   * Commit `text` as a posted entry. With an armed anchor it becomes an anchored
   * comment (clears the anchor); with no anchor a non-empty `text` becomes a
   * free-form feedback note. `opts` carries the comment type (change-request/question)
   * and, for a question, its addressee.
   */
  post: (text: string, opts?: PostOptions) => void;
  /** Drop a previously-posted entry by index. */
  remove: (index: number) => void;
  /** Clear all accumulated entries (after a successful send-back). */
  reset: () => void;
  /** Clear only the pending QUESTION entries (after a successful "Ask"), keeping change-requests. */
  clearQuestions: () => void;
  /**
   * Bind the accumulator to a (projectId, kind) localStorage slot. Loads that
   * slot's persisted pending entries and routes subsequent post/remove/reset
   * writes to it, so unsent notes survive a reload and switching artifact steps
   * swaps to that step's own pending set. A no-op on read-only surfaces.
   *
   * `projectVersion` is the project head-state Version the surface just read
   * (GetProject) — the incarnation stamp persisted alongside the entries so
   * drafts written against a DELETED-and-RECREATED project under the same
   * ProjectID are invalidated on load instead of resurrected (F-QA2-5; see
   * pendingCommentsStore.ts). Callers bind only once the project has loaded.
   */
  setActiveKey: (key: string, projectVersion: number) => void;
  /** Maps the ANCHORED CHANGE-REQUEST entries into the wire AnchoredComment[] shape (questions excluded). */
  toWire: () => AnchoredComment[];
  /** The FREE-FORM CHANGE-REQUEST entries joined into the reject `feedback` notes string (questions excluded). */
  freeformNotes: () => string;
  /** The pending QUESTION entries (anchored or free-form) for the "Ask" action. */
  pendingQuestions: () => PendingQuestion[];
  /** Monotonic counter; bumps whenever an anchor is armed. */
  requestId: number;
}
