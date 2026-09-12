/**
 * What the LIST says when it has no rows to show (designer P1-9).
 *
 * Two situations that must never share a sentence: the project genuinely has no
 * activities, and the toolbar has filtered every one of them away. The list used
 * to answer both with "No construction activities are recorded for this project
 * yet" — so a search that matched nothing told the operator their whole project
 * was empty. A filtered-away list names the query and offers the way back.
 *
 * Pure so node:test can pin it; ActivityTreeView renders it.
 */

export interface EmptyListCopy {
  message: string;
  /** Offer "Clear filters": true only when rows exist and the toolbar hid them. */
  offerClear: boolean;
}

export const NO_ACTIVITIES_MESSAGE =
  'No construction activities are recorded for this project yet.';

/** `undefined` while any row is showing. */
export function emptyListCopyFor(
  visibleCount: number,
  totalCount: number,
  query: string
): EmptyListCopy | undefined {
  if (visibleCount > 0) return undefined;
  if (totalCount === 0) return { message: NO_ACTIVITIES_MESSAGE, offerClear: false };
  const q = query.trim();
  return {
    message:
      q.length > 0 ? `No activity matches “${q}”.` : 'No activity matches the current filters.',
    offerClear: true,
  };
}
