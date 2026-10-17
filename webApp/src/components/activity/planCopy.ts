/** Every sentence the Plan screen says. Pure, tested, no i18n (house convention). */

export const LENS_LABEL: Readonly<Record<'list' | 'graph' | 'tasks', string>> = {
  list: 'List',
  graph: 'Graph',
  tasks: 'Tasks',
};

/** The divider between the three design activities and the build stack. */
export const M0_DIVIDER = 'Milestone M0 · delivery plan (SDP) approved — construction starts here';

/** The milestone node's own two lines on the GRAPH. */
export const M0_LABEL = 'M0';
export const M0_CAPTION = 'plan approved — build starts';

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
    ? 'No activity list is committed yet. Once the Project Design activity is approved (milestone M0), every activity of the plan appears here.'
    : 'The committed activity list is empty.';
}

/** The always-visible word on a critical-path tile — colour is never alone. */
export const CRITICAL_MARK = 'CP';

/** What CRITICAL_MARK abbreviates, for the legend and the marks' own titles. */
export const CRITICAL_MEANING =
  'Critical path — any delay to these activities delays the whole project';

/** The plan's legend, one entry per mark a row or tile can carry. */
export const LEGEND = {
  steps:
    'Dots — the activity’s steps in order (requirements, design, test plan, build, reviews, integration); filled dots are done, “n/m” counts them',
  awaitingYou: 'Awaiting you — a step needs your review; the Tasks view lists them',
  inReview: 'In review — at a review step that is not waiting on you',
  milestone:
    'M0 — the delivery plan (Software Development Plan, SDP) is approved; no construction starts before it',
} as const;

/** Each lens's one-line description, under the Plan heading. */
export const LENS_SUBTITLE: Readonly<Record<'list' | 'graph' | 'tasks', string>> = {
  list: 'Every activity of the committed plan, in build order — open one to see its steps',
  graph:
    'The plan as a dependency network, one row per architecture layer — every build activity waits on milestone M0',
  tasks: 'Only the tasks that need a decision from you',
};

/** The always-visible numeral beside the critical-path rail (WCAG 1.4.1 — never colour alone). */
export function criticalPathNote(count: number): string {
  return count === 1
    ? '1 activity on the critical path'
    : `${String(count)} activities on the critical path`;
}
