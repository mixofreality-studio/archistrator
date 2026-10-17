/**
 * The two route paths the Activity Experience and the Plan live at, plus the
 * pure codecs for their search params.
 *
 * WHY THIS IS IN `contracts/`. The boundary DAG (eslint.platform.config.js
 * BOUNDARY_RULES:63-71) gives `containers → [containers, components, hooks,
 * contracts, utilities]` and `components → [components, contracts,
 * utilities]`. Neither may import `routes/`. A container that navigates and a
 * component that renders a row-as-a-link both need the path literal, so the
 * literal cannot live beside the route. `contracts → contracts` permits a
 * leaf, and this file imports NOTHING — which also lets `node --test` load it
 * directly.
 *
 * The literals are the exact strings `createRoute({ path })` registers, so
 * TanStack's module augmentation (router.tsx:178) type-checks every
 * `navigate({ to: PLAN_PATH })` against the real route tree.
 */
export const PLAN_PATH = '/project/$projectId/plan' as const;
export const ACTIVITY_PATH = '/project/$projectId/activity/$activityId' as const;
/** The project's state history (use case view-the-project-state-log). */
export const STATE_HISTORY_PATH = '/project/$projectId/history' as const;

export const LENS_IDS = ['list', 'graph', 'tasks'] as const;
export type PlanLensId = (typeof LENS_IDS)[number];

export function isPlanLensId(value: unknown): value is PlanLensId {
  return typeof value === 'string' && (LENS_IDS as readonly string[]).includes(value);
}

/**
 * `lens` is ALWAYS emitted, even for the default `list`: a route's
 * validateSearch output IS the address bar, so dropping it would quietly
 * rewrite a shared deep link — the failure the old console's codec documented
 * at useLensSelection.ts:157-163 and this one inherits.
 */
export function planSearch(lens: unknown): { lens: PlanLensId } {
  return { lens: isPlanLensId(lens) ? lens : 'list' };
}

/**
 * A junk `rev` is DROPPED, never coerced: `NaN` downstream renders as
 * "Revision NaN" and silently poisons every comparison that touches it — the
 * rule `useLensSelection.attemptOf` already states for the old `?n=`.
 *
 * `view` + `step` are the Architecture review's call-chain deep link (a
 * dynamic-view key and its 1-based step — `architectureDeepLink.ts`), carried
 * by the cross-artifact StepLink. They pass through under the same rule: a
 * blank view and a non-positive-integer step are dropped, so a validated
 * search never carries a value the reader would have to guard against.
 */
export function activitySearch(
  task: unknown,
  rev: unknown,
  view?: unknown,
  step?: unknown
): { task?: string; rev?: number; view?: string; step?: number } {
  const n = positiveInt(rev);
  const s = positiveInt(step);
  return {
    ...(typeof task === 'string' && task.length > 0 ? { task } : {}),
    ...(n !== undefined ? { rev: n } : {}),
    ...(typeof view === 'string' && view.length > 0 ? { view } : {}),
    ...(s !== undefined ? { step: s } : {}),
  };
}

function positiveInt(raw: unknown): number | undefined {
  const n = typeof raw === 'string' || typeof raw === 'number' ? Number(raw) : Number.NaN;
  return Number.isInteger(n) && n >= 1 ? n : undefined;
}
