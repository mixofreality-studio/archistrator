/**
 * TanStack Router setup (code-based route tree). The MUI theme root (AppTheme)
 * and session gate (UserProvider) wrap the RouterProvider in App.tsx, so the
 * root route is a bare <Outlet/>. Routes are project-scoped to mirror the typed
 * server contract:
 *
 *   /                                  → ProjectsLanding (catalog / create)
 *   /project/$projectId/home           → HomeBase (wraps itself in the AppShell)
 *   /project/$projectId/design/system  → SystemDesignScreen (phase 1, full-screen)
 *   /project/$projectId/design/project → ProjectDesignScreen (phase 2, full-screen)
 *   /project/$projectId/construction   → ConstructionConsoleScreen (phase 3, full-screen)
 *
 * Each route component is a self-contained screen export (no local component
 * definitions here) so fast-refresh stays happy alongside the `router` export.
 */
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import { ProjectsLanding } from '../screens/ProjectsLanding';
import { HomeBase } from '../screens/HomeBase';
import { SystemDesignScreen, ProjectDesignScreen } from '../screens/DesignExperience';
import { ConstructionConsoleScreen } from '../screens/ConstructionConsole';
import { OperationsConsoleScreen } from '../screens/OperationsConsole';
import { ChangeRequestsScreen } from '../screens/ChangeRequests';
import { SubprojectFlowScreen } from '../screens/SubprojectFlow';
import { BillingScreen } from '../screens/Billing';
import { TeamScreen } from '../screens/TeamView';

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
  path: '/project/$projectId/design/system',
  component: SystemDesignScreen,
});

const projectDesignRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/project/$projectId/design/project',
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
