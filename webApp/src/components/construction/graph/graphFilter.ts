/**
 * The GRAPH lens's filter status (designer P1-4): filters used to dim lanes
 * silently, so "Critical path" or a search looked like nothing had happened.
 *
 * While a filter is active the lens says so above the canvas — "N of 29 match
 * · Clear filters" — and, when nothing matches, uses the list's own no-match
 * sentence (listEmptyState.emptyListCopyFor), so the two lenses never word the
 * same situation two ways.
 *
 * WHAT COUNTS AS A FILTER: search, scope, kind and layer — exactly what the
 * list's "Clear filters" resets. Sort is an ordering, not a filter; "Observed
 * only" rewrites evidence and never hides an activity (observedOnly.ts).
 *
 * Pure — no React — pinned by graphFilter.test.ts.
 */
import { emptyListCopyFor } from '../list/listEmptyState.ts';
import type { ToolbarState } from '../lens/useLensSelection.ts';

export function filtersActive(
  toolbar: Pick<ToolbarState, 'search' | 'scope' | 'kind' | 'layer'>
): boolean {
  return (
    toolbar.search.trim() !== '' ||
    toolbar.scope !== 'all' ||
    toolbar.kind !== 'all' ||
    toolbar.layer !== 'all'
  );
}

export interface GraphFilterStatus {
  /** "7 of 29 match", or the list's no-match sentence at zero. */
  message: string;
  /** Always offered while a filter is active — the way back. */
  offerClear: true;
  matched: number;
  total: number;
}

/** `undefined` when no filter is active — the canvas then carries no line. */
export function graphFilterStatusFor(
  matched: number,
  total: number,
  query: string,
  active: boolean
): GraphFilterStatus | undefined {
  if (!active) return undefined;
  const none = emptyListCopyFor(matched, total, query);
  return {
    message: none?.message ?? `${String(matched)} of ${String(total)} match`,
    offerClear: true,
    matched,
    total,
  };
}
