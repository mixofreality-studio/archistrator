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
 * ── Written and tested here; WIRED with the teardown ────────────────────────
 * router.tsx does not yet hang these off the three routes' `beforeLoad`. The
 * construction screen is still the only surface in the preview build that READS
 * anything, and the preview suite's fixture-miss, blocked-request and
 * nested-preview guards all ride it; redirecting it away before the plan screen
 * reads would leave those guards with no vehicle for the rest of the stage. The
 * wiring lands in the teardown task, together with the console's deletion and
 * the retargeted preview spec. The redirect itself is complete and covered by
 * activityRedirect.test.ts, so that task adds three `beforeLoad` lines and
 * nothing else.
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
