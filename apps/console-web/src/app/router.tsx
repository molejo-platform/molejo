import { Outlet, createRootRouteWithContext, createRoute, createRouter, createBrowserHistory, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

import { LoginPage } from "../features/auth/LoginPage";
import { sessionQueryOptions } from "../features/auth/model";
import { AppShell } from "./AppShell";
import { DeploymentDetailPage } from "../features/deployments/DeploymentDetailPage";
import { DeploymentListPage } from "../features/deployments/DeploymentListPage";
import { EditDeploymentPage, NewDeploymentPage } from "../features/deployments/DeploymentRoutes";
import { AdminPage } from "../features/admin/AdminPage";

export type RouterContext = { queryClient: QueryClient };

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: () => <Outlet /> });

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => { throw redirect({ to: "/deployments" }); },
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (session) throw redirect({ to: "/deployments" });
  },
  component: LoginPage,
});

const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "protected",
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (!session) throw redirect({ to: "/login" });
  },
  component: AppShell,
});

async function requireOwner(context: RouterContext) {
  const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
  if (!session) throw redirect({ to: "/login" });
  if (session.actor.role !== "owner") throw redirect({ to: "/deployments" });
}

const deploymentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/deployments", component: DeploymentListPage });
const newDeploymentRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/deployments/new", beforeLoad: ({ context }) => requireOwner(context), component: NewDeploymentPage });
const detailDeploymentRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/deployments/$deploymentId", component: DeploymentDetailPage });
const editDeploymentRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/deployments/$deploymentId/edit", beforeLoad: ({ context }) => requireOwner(context), component: EditDeploymentPage });
const adminRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/admin", component: AdminPage });

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  protectedRoute.addChildren([deploymentsRoute, newDeploymentRoute, detailDeploymentRoute, editDeploymentRoute, adminRoute]),
]);

export function createAppRouter(queryClient: QueryClient, history: ReturnType<typeof createBrowserHistory> = createBrowserHistory()) {
  return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: "intent" });
}

export { routeTree };
