/**
 * The PM coverage table's cell text and finding rule (FPC Task 2.14), pure and React-free so
 * `node --test` loads it directly and the panel file exports only its component.
 */
import type { PathCoverageView } from '../../contracts/types.ts';

/** The cell text for a path's door: the Client and its surface, or NONE. */
export function doorLabel(p: PathCoverageView): string {
  if (p.client === '') return 'NONE';
  return p.door === '' ? p.client : `${p.client} (${p.door})`;
}

/** The cell text for a list column: its items joined, or the stated empty word. */
export function listLabel(items: readonly string[], none: string): string {
  return items.length === 0 ? none : items.join('; ');
}

/** A path is a finding when it has no door, no outcome, or a derivation defect. */
export function pathIsFinding(p: PathCoverageView): boolean {
  return p.client === '' || p.outcomeNodes.length === 0 || p.problems.length > 0;
}

/**
 * Said beside the table rather than left to be found by being refused: the PM's send-back is a
 * floor under every preset, and approving over it is an override recorded with a reason.
 */
export const COVERAGE_OVERRIDE_NOTE =
  "The product manager's send-back holds this gate for a human under every review preset. " +
  'Approving over it is an override: resolve its thread, and give the reason in the approve notes.';
