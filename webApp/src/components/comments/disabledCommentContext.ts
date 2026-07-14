/**
 * Extracted disabled CommentCtx for use in both CommentContext.tsx (React)
 * and commentContext.test.ts (node:test, which cannot import .tsx files).
 *
 * A fully-disabled default: every mutator is a no-op, every accessor reads
 * empty/zero. Returned by {@link useComments} when no {@link CommentProvider}
 * ancestor exists, so a consumer mounted outside any provider (e.g. the pure
 * `SystemDesignView` composed by an MCP host with no CommentProvider at all —
 * see Task 8) degrades to "no comment affordances" instead of throwing. Every
 * SPA call site keeps rendering inside a real CommentProvider, so this is
 * purely a safety net — behavior there is unchanged.
 */

import type { CommentCtx } from './commentContextTypes';

/* eslint-disable @typescript-eslint/no-empty-function -- deliberate no-op mutators */
export const DISABLED_COMMENT_CTX: CommentCtx = {
  enabled: false,
  comments: [],
  anchor: null,
  setAnchor: () => {},
  setDraftPending: () => {},
  post: () => {},
  remove: () => {},
  reset: () => {},
  clearQuestions: () => {},
  setActiveKey: () => {},
  toWire: () => [],
  freeformNotes: () => '',
  pendingQuestions: () => [],
  requestId: 0,
};
/* eslint-enable @typescript-eslint/no-empty-function */
