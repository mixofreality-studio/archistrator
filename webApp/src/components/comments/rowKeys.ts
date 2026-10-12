/**
 * Whether a key pressed inside a CommentableList row belongs to a NESTED
 * interactive element rather than to the row — pure, so `node --test` runs it
 * over plain objects.
 *
 * The row's own keys (Enter / `c` arm a comment) must not swallow a key meant
 * for a button, link or field inside the row: the row's `preventDefault()` on
 * Enter cancelled a native button's implicit click, so Enter on an expandable
 * check or scenario row armed a comment instead of expanding it (C3 review I4,
 * ruling R48). Space is listed too: a row never acts on it, but a nested
 * control does, and the row must leave it alone.
 */

/** The slice of a DOM element this reads (structural; an HTMLElement satisfies it). */
export interface KeyTargetNode {
  readonly tagName?: string;
  readonly parentElement?: KeyTargetNode | null;
  getAttribute?(name: string): string | null;
}

const ROW_KEYS = new Set(['Enter', ' ', 'c', 'C']);
const INTERACTIVE_TAGS = new Set(['BUTTON', 'A', 'INPUT', 'TEXTAREA', 'SELECT']);

function isInteractive(n: KeyTargetNode): boolean {
  if (n.tagName !== undefined && INTERACTIVE_TAGS.has(n.tagName.toUpperCase())) return true;
  return n.getAttribute?.('role') === 'button';
}

/**
 * True when `key` is one the row would act on and `target` is an interactive
 * element strictly inside `row` (the row itself, or plain content inside it,
 * is not). The walk stops at the row.
 */
export function keyBelongsToNestedControl(
  key: string,
  target: KeyTargetNode | null,
  row: KeyTargetNode | null
): boolean {
  if (!ROW_KEYS.has(key)) return false;
  for (let n = target; n !== null && n !== row; n = n.parentElement ?? null) {
    if (isInteractive(n)) return true;
  }
  return false;
}
