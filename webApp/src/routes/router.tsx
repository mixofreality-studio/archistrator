/**
 * TanStack Router setup (code-based route tree). The MUI theme root (AppTheme)
 * and session gate (UserProvider) wrap the RouterProvider in App.tsx, so the
 * root route is a bare <Outlet/>. Routes are project-scoped to mirror the typed
 * server contract:
 *
 *   /                                  → ProjectsLanding (catalog / create)
 *   /project/$projectId/home                      → HomeBase (wraps itself in the AppShell)
 *   /project/$projectId/design/system/{-$stepSlug}  → SystemDesignScreen (phase 1, full-screen)
 *   /project/$projectId/design/project/{-$stepSlug} → ProjectDesignScreen (phase 2, full-screen)
 *   /project/$projectId/construction              → ConstructionConsoleScreen (phase 3, full-screen)
 *
 * The design experiences carry an OPTIONAL step slug as the last path segment
 * ({-$stepSlug}, kebab-case of the step title — see slugForKind) so a step is
 * deep-linkable and survives reload; absent, the experience normalizes the URL
 * to its derived default step.
 *
 * Each route component is a self-contained screen export (no local component
 * definitions here) so fast-refresh stays happy alongside the `router` export.
 */
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import { ProjectsLanding } from './ProjectsLanding';
import { HomeBase } from './HomeBase';
import { SystemDesignScreen, ProjectDesignScreen } from './DesignExperience';
import { ConstructionConsoleScreen } from './ConstructionConsole';
import { OperationsConsoleScreen } from './OperationsConsole';
import { ChangeRequestsScreen } from './ChangeRequests';
import { SubprojectFlowScreen } from './SubprojectFlow';
import { BillingScreen } from './Billing';
import { TeamScreen } from './TeamView';
import { operationsBeforeLoad } from './operationsGuard';
import { fetchCapabilities } from '../hooks/useCapabilities';

const rootRoute = createRootRoute({ component: Outlet });

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
  beforeLoad: () => operationsBeforeLoad(fetchCapabilities),
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
  systemDesignRoute,
  projectDesignRoute,
  constructionRoute,
  operationsRoute,
  changeRequestsRoute,
  subprojectRoute,
  billingRoute,
  teamRoute,
]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
