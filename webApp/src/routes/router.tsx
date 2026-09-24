/**
 * TanStack Router setup (code-based route tree). The MUI theme root (AppTheme)
 * and session gate (UserProvider) wrap the RouterProvider in App.tsx, so the
 * root route is a bare <Outlet/>. Routes are project-scoped to mirror the typed
 * server contract:
 *
 *   /                                  → ProjectsLanding (catalog / create)
 *   /project/$projectId/home                      → HomeBase (wraps itself in the AppShell)
 *   /project/$projectId/plan                      → PlanScreen (the ONE plan surface, full-screen)
 *   /project/$projectId/activity/$activityId      → ActivityExperienceScreen (one activity, full-screen)
 *   /project/$projectId/design/system/{-$stepSlug}  → SystemDesignScreen (phase 1, full-screen)
 *   /project/$projectId/design/project/{-$stepSlug} → ProjectDesignScreen (phase 2, full-screen)
 *   /project/$projectId/construction              → ConstructionConsoleScreen (phase 3, full-screen)
 *
 * The last three are the OLD rails, which the plan and the Activity Experience
 * replace. They stay REGISTERED and, in the stage's teardown task, swap their
 * component for a `beforeLoad` redirect into `/plan` — an unregistered path is
 * the router's not-found, which is a worse answer to an old bookmark than the
 * screen that replaced it. That redirect is already written and tested in
 * `activityRedirect.ts` (split out for the same reason operationsGuard.ts is: a
 * beforeLoad written inline here cannot be unit-tested); it is wired at teardown,
 * WITH the console's deletion and the retargeted preview spec, because the
 * preview suite's fixture-miss / blocked-request / nested-preview guards ride the
 * construction screen and have no other vehicle until the plan screen reads.
 *
 * The design experiences carry an OPTIONAL step slug as the last path segment
 * ({-$stepSlug}, kebab-case of the step title — see slugForKind) so a step is
 * deep-linkable and survives reload; absent, the experience normalizes the URL
 * to its derived default step.
 *
 * Each route component is a self-contained screen export (no local component
 * definitions here) so fast-refresh stays happy alongside the router factory.
 */
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  type RouterHistory,
} from '@tanstack/react-router';
import { ProjectsLanding } from './ProjectsLanding';
import { HomeBase } from './HomeBase';
import { SystemDesignScreen, ProjectDesignScreen } from './DesignExperience';
import { ConstructionConsoleScreen } from './ConstructionConsole';
import { OperationsConsoleScreen } from './OperationsConsole';
import { ChangeRequestsScreen } from './ChangeRequests';
import { SubprojectFlowScreen } from './SubprojectFlow';
import { BillingScreen } from './Billing';
import { TeamScreen } from './TeamView';
import { PlanScreen } from './Plan';
import { ActivityExperienceScreen } from './ActivityExperience';
import { operationsBeforeLoad } from './operationsGuard';
// One import for both new paths: activityRedirect owns the redirect the old rails
// take at teardown and re-exports the literals it redirects between, so the path
// a route registers and the path the redirect names cannot drift.
import { ACTIVITY_PATH, PLAN_PATH } from './activityRedirect';
import { activitySearch } from '../contracts/routePaths';
import {
  parseLensSearch,
  serializeLensSearch,
  validateLensSearch,
  type LegacyLensSearchParams,
} from '../components/construction/lens/useLensSelection';
import type { Capabilities } from '../utilities/capabilities';

/**
 * What the shell that builds the router hands every route. The operations guard
 * runs in `beforeLoad`, outside React, so it cannot reach the OpsClient context;
 * the shell passes the capabilities read bound to ITS transport instead (main.tsx
 * the REST client, previewShell the fixture client).
 */
export interface AppRouterContext {
  readonly fetchCapabilities: () => Promise<Capabilities>;
}

const rootRoute = createRootRouteWithContext<AppRouterContext>()({ component: Outlet });

const landingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: ProjectsLanding,
});

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/home',
  component: HomeBase,
});

const planRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: PLAN_PATH,
  component: PlanScreen,
  // The plan's LENS is the only selection left in the URL — the DetailPane's
  // a/p/k/n/av/focus/sc died with it (spec §7.4). An unknown lens falls back
  // to `list` instead of throwing, and `lens` is ALWAYS emitted, even for the
  // default, because validateSearch's output IS the address bar: dropping it
  // would quietly rewrite a shared deep link.
  validateSearch: validateLensSearch,
});

const activityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ACTIVITY_PATH,
  component: ActivityExperienceScreen,
  // ?task=<lifecycle task id>&rev=<1-based revision>. Both optional: absent,
  // the experience opens the default task (spec §7.2 — awaiting-human →
  // failed → running → last passed → first) at its latest revision. The codec
  // is `contracts/routePaths.activitySearch`, not an inline lambda, because
  // the containers construct the same object when they navigate and a second
  // copy of the rule is how two callers end up disagreeing about `?rev=0`.
  validateSearch: (search: Record<string, unknown>): { task?: string; rev?: number } =>
    activitySearch(search['task'], search['rev']),
});

const systemDesignRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/design/system/{-$stepSlug}',
  component: SystemDesignScreen,
  // Optional ?view=<dynamic-view-key>&step=<1-based-seq> deep link: the
  // Architecture step's viewer preselects the Dynamic lens on that view (the
  // use-case → call-chain jump), landing on a specific step of the chain when
  // `step` also parses as a positive integer. A dangling key / bad step is
  // harmless — the viewer falls back to its defaults.
  validateSearch: (search: Record<string, unknown>): { view?: string; step?: number } => {
    const view = search['view'];
    const step = search['step'];
    const stepValid =
      (typeof step === 'string' || typeof step === 'number') &&
      Number.isInteger(Number(step)) &&
      Number(step) > 0;
    return {
      ...(typeof view === 'string' && view.length > 0 ? { view } : {}),
      ...(stepValid ? { step: Number(step) } : {}),
    };
  },
});

const projectDesignRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/design/project/{-$stepSlug}',
  component: ProjectDesignScreen,
});

const constructionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/construction',
  component: ConstructionConsoleScreen,
  // The construction console's lens + selection live in the search params —
  // ?lens=list&a=<activityId>&p=<lifecyclePhase>&k=<task>&n=<attempt> — so the
  // shared detail pane never owns selection, the 1.5s cascade poll's remount
  // cannot wipe it, and a link addresses exactly one task attempt. Registering
  // the schema here is what makes a deep link VALIDATE (an unknown lens falls
  // back to `list`, a junk attempt is dropped) instead of throwing or rendering
  // a blank surface. See lens/useLensSelection.ts.
  //
  // This is the LEGACY codec, spelled out here rather than reusing
  // `validateLensSearch`: as of stage 5 that function is the PLAN's rule (lens
  // and nothing else), and this console's pane still addresses a task attempt
  // through a/p/k/n and its artifact through av/focus/sc. Handing it the plan's
  // rule would strip those from every deep link into a screen that still renders
  // them. It goes when the screen goes, and the path then redirects through
  // activityRedirect.planSearchFromLegacy, which drops those params deliberately
  // because the pane they address will no longer exist.
  validateSearch: (search: Record<string, unknown>): LegacyLensSearchParams =>
    serializeLensSearch(parseLensSearch(search)),
});

const operationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/operations/$operatedAppId',
  component: OperationsConsoleScreen,
  // D9 (operations-argocd-deployment Task 11): the local profile holds no
  // deployment credential and must not surface operations at all — not a
  // disabled console, not a simulated one. Confirmed fresh from the server on
  // every navigation (GET /api/v1/capabilities, never trusted from a stale
  // client cache) BEFORE the console mounts: {operations:false} redirects
  // home (local always answers successfully, so this IS the D9 case); a
  // genuinely unreachable capabilities check does NOT redirect — it hands
  // OperationsConsoleScreen an explicit error state via context instead, so a
  // cloud operator mid-incident sees why, not a silent bounce to the catalog.
  // See operationsGuard.ts.
  beforeLoad: ({ context }) => operationsBeforeLoad(context.fetchCapabilities),
});

const changeRequestsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/changes',
  component: ChangeRequestsScreen,
});

const subprojectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/changes/$subprojectId',
  component: SubprojectFlowScreen,
});

const billingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/billing',
  component: BillingScreen,
});

const teamRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/team',
  component: TeamScreen,
});

const routeTree = rootRoute.addChildren([
  landingRoute,
  homeRoute,
  planRoute,
  activityRoute,
  systemDesignRoute,
  projectDesignRoute,
  constructionRoute,
  operationsRoute,
  changeRequestsRoute,
  subprojectRoute,
  billingRoute,
  teamRoute,
]);

/**
 * The router FACTORY (design-renderer-data.md §2′.5 item 2). It used to be a
 * module singleton on browser history. main.tsx now passes createBrowserHistory(),
 * which is exactly what createRouter defaulted to, so the browser SPA is
 * unchanged; the preview shell passes a memory history seeded at the fixture's
 * route, so a preview never reads or writes the page URL.
 */
/** The router createRouter builds over this route tree (its defaults, any history). */
export type AppRouter = ReturnType<
  typeof createRouter<typeof routeTree, 'never', false, RouterHistory>
>;

export function createAppRouter(history: RouterHistory, context: AppRouterContext): AppRouter {
  return createRouter({ routeTree, history, context });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: AppRouter;
  }
}
