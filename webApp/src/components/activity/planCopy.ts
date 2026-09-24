/** Every sentence the Plan screen says. Pure, tested, no i18n (house convention). */

export const LENS_LABEL: Readonly<Record<'list' | 'graph' | 'tasks', string>> = {
  list: 'List',
  graph: 'Graph',
  tasks: 'Tasks',
};

/** The divider between the three design activities and the build stack. */
export const M0_DIVIDER = 'M0 · SDP Review approved — construction begins';

/** The milestone node's own two lines on the GRAPH. */
export const M0_LABEL = 'M0';
export const M0_CAPTION = 'gates all construction';

/**
 * Why an activity is in the list but not on the canvas. The committed activity
 * list decides what EXISTS; the model decides where a tile can be DRAWN. When
 * the two disagree the screen says so rather than dropping a row on the floor.
 */
export function unplacedTilesNote(ids: readonly string[]): string {
  const names = [...ids].sort((a, b) => a.localeCompare(b)).join(', ');
  return `Not drawn: ${names}. These activities build no layered component, so the plan has no row for them — they are listed above.`;
}

/** The list/graph when the project has no committed plan yet. */
export function planEmptyState(reason: 'noPlan' | 'noRows'): string {
  return reason === 'noPlan'
    ? 'No activity list is committed yet. Approve the Project Design activity (M0) and the plan appears here.'
    : 'The committed activity list is empty.';
}

/** The always-visible word on a critical-path tile — colour is never alone. */
export const CRITICAL_MARK = 'CP';

/** The always-visible numeral beside the critical-path rail (WCAG 1.4.1 — never colour alone). */
export function criticalPathNote(count: number): string {
  return count === 1
    ? '1 activity on the critical path'
    : `${String(count)} activities on the critical path`;
}
