/**
 * The PM coverage table's cell text and finding rule (FPC Task 2.14), pure and React-free so
 * `node --test` loads it directly and the panel file exports only its component.
 */
import type { PathCoverageView } from '../../contracts/types.ts';
import { assertNever } from '../../contracts/exhaustive.ts';

/**
 * The cell text for a path's door: the Client and its surface; NONE when no Client is reached
 * (the finding); or the not-yet-judged state a door is in while the architecture is not
 * recorded, which must never read as NONE.
 */
export function doorLabel(p: PathCoverageView): string {
  const d = p.door;
  switch (d.kind) {
    case 'client':
      return d.surface === '' ? d.client : `${d.client} (${d.surface})`;
    case 'none':
      return 'NONE';
    case 'pendingArchitecture':
      return 'not yet judged (architecture pending)';
    default:
      return assertNever(d);
  }
}

/** The cell text for a list column: its items joined, or the stated empty word. */
export function listLabel(items: readonly string[], none: string): string {
  return items.length === 0 ? none : items.join('; ');
}

/**
 * A path is a finding when it has no door, no outcome, or a derivation defect. A door not yet
 * judged is not a finding: the architecture that would judge it is what is pending.
 */
export function pathIsFinding(p: PathCoverageView): boolean {
  const d = p.door;
  let doorMissing: boolean;
  switch (d.kind) {
    case 'client':
    case 'pendingArchitecture':
      doorMissing = false;
      break;
    case 'none':
      doorMissing = true;
      break;
    default:
      return assertNever(d);
  }
  return doorMissing || p.outcomeNodes.length === 0 || p.problems.length > 0;
}

/**
 * Said beside the table rather than left to be found by being refused: the PM's send-back is a
 * floor under every preset, and approving over it is an override recorded with a reason.
 */
export const COVERAGE_OVERRIDE_NOTE =
  "The product manager's send-back holds this gate for a human under every review preset. " +
  'Approving over it is an override: resolve its thread, and give the reason in the approve notes.';
