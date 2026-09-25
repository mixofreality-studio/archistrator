/**
 * The old rails' `beforeLoad` redirect (stage 5, spec §7.4), split out of
 * router.tsx for the same two reasons operationsGuard.ts is: router.tsx's own
 * doc comment bars local definitions there (fast refresh), and a `beforeLoad`
 * written inline in the route tree cannot be unit-tested without building the
 * whole tree.
 *
 * `/construction`, `/design/system` and `/design/project` all became ONE
 * surface — the plan — with the per-activity work behind
 * `/project/$projectId/activity/$activityId`. The three routes stay REGISTERED
 * and redirect: an unregistered path is the router's not-found, which is a
 * worse answer to an old bookmark than landing on the screen that replaced it.
 *
 * ── WIRED (Task 13, with the console's deletion) ────────────────────────────
 * router.tsx hangs `redirectToPlan` off all three routes' `beforeLoad`. The
 * wiring waited for that commit on purpose: the construction screen was the only
 * surface in the preview build that READ anything, and the preview suite's
 * fixture-miss, blocked-request and nested-preview guards all rode it, so
 * redirecting it away before the plan screen read would have left those guards
 * with no vehicle. Task 12 moved them onto the plan's fixtures first.
 *
 * The path literals are re-exported so router.tsx has ONE import for the
 * redirect and the paths it registers; the literals themselves live in
 * contracts/routePaths.ts, because containers and components may not import
 * `routes/` (see that file's header).
 */
import { redirect } from '@tanstack/react-router';
import { ACTIVITY_PATH, PLAN_PATH, planSearch, type PlanLensId } from '../contracts/routePaths.ts';

export { ACTIVITY_PATH, PLAN_PATH };

/**
 * An old construction deep link keeps only its LENS. `a`/`p`/`k`/`n` addressed
 * a task attempt inside the DetailPane and `av`/`focus`/`sc` a view of its
 * artifact; the pane is gone, so those params address nothing. Carrying them
 * into `/plan` would put junk in the address bar that validateSearch strips on
 * the next navigation — worse than dropping them here, where it is deliberate.
 */
export function planSearchFromLegacy(search: Record<string, unknown>): { lens: PlanLensId } {
  return planSearch(search['lens']);
}

/** The design rails had no lens of their own; they land on the plan's LIST. */
export function designRedirectSearch(): { lens: PlanLensId } {
  return planSearch('list');
}

export function redirectToPlan(projectId: string, search: { lens: PlanLensId }): never {
  // redirect({ throw: true }) throws internally — @tanstack/router-core's
  // Redirect extends Response, not Error, so an explicit `throw redirect(...)`
  // trips @typescript-eslint/only-throw-error (operationsGuard.ts:88-91).
  redirect({ to: PLAN_PATH, params: { projectId }, search, throw: true });
  throw new Error('unreachable: redirect did not throw');
}
