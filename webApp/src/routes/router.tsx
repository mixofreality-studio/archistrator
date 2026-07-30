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
  // Optional ?view=<dynamic-view-key> deep link: the Architecture step's viewer
  // preselects the Dynamic lens on that view (the use-case → call-chain jump).
  // A dangling key is harmless — the viewer falls back to its defaults.
  validateSearch: (search: Record<string, unknown>): { view?: string } => {
    const view = search['view'];
    return typeof view === 'string' && view.length > 0 ? { view } : {};
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
