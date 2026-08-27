import { Outlet, createRootRouteWithContext, createRoute, createRouter, createBrowserHistory, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

import { AppBuildsPage, BuildDetailPage } from "../features/apps/AppBuildPages";
import { AppOverviewPage } from "../features/apps/AppOverviewPage";
import { AppReleasesPage } from "../features/apps/AppReleasesPage";
import { AppSourcePage } from "../features/apps/AppSourcePage";
import { LoginPage } from "../features/auth/LoginPage";
import { sessionQueryOptions } from "../features/auth/model";
import { DeploymentDetailPage } from "../features/deployments/DeploymentDetailPage";
import { DeploymentListPage } from "../features/deployments/DeploymentListPage";
import { EditDeploymentPage, NewDeploymentPage } from "../features/deployments/DeploymentRoutes";
import { OverviewPage } from "../features/overview/OverviewPage";
import { ProjectAppsPage, ProjectEnvironmentsPage, ProjectOverviewPage, ProjectsPage } from "../features/projects/ProjectPages";
import { GitHubSettingsPage, NewWorkspacePage, WorkspaceSettingsPage } from "../features/settings/SettingsPages";
import { LegacyDeploymentsEntryPage, LegacySettingsEntryPage, WorkspaceEntryPage } from "../features/workspace/WorkspaceEntryPage";
import { AppShell } from "./AppShell";

export type RouterContext = { queryClient: QueryClient };

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: () => <Outlet /> });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (session) throw redirect({ to: "/" });
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

async function requireOwner(context: RouterContext, workspaceId?: string) {
  const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
  if (!session) throw redirect({ to: "/login" });
  if (session.actor.role !== "owner") {
    if (workspaceId) throw redirect({ to: "/workspaces/$workspaceId/deployments", params: { workspaceId } });
    throw redirect({ to: "/" });
  }
}

const workspaceEntryRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/", component: WorkspaceEntryPage });
const overviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/overview", component: OverviewPage });
const projectsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects", component: ProjectsPage });
const projectOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId", component: ProjectOverviewPage });
const projectAppsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps", component: ProjectAppsPage });
const projectEnvironmentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments", component: ProjectEnvironmentsPage });
const appOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", component: AppOverviewPage });
const appSourceRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/source", component: AppSourcePage });
const appBuildsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds", component: AppBuildsPage });
const buildDetailRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds/$buildId", component: BuildDetailPage });
const appReleasesRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases", component: AppReleasesPage });
const deploymentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/deployments", component: DeploymentListPage });
const newDeploymentRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/deployments/new", beforeLoad: ({ context, params }) => requireOwner(context, params.workspaceId), component: NewDeploymentPage });
const detailDeploymentRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/deployments/$deploymentId", component: DeploymentDetailPage });
const editDeploymentRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/deployments/$deploymentId/edit", beforeLoad: ({ context, params }) => requireOwner(context, params.workspaceId), component: EditDeploymentPage });
const settingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings", component: WorkspaceSettingsPage });
const githubSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/github", component: GitHubSettingsPage });
const newWorkspaceRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/new", beforeLoad: ({ context }) => requireOwner(context), component: NewWorkspacePage });
const legacyDeploymentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/deployments", component: LegacyDeploymentsEntryPage });
const legacyAdminRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/admin", component: LegacySettingsEntryPage });

const routeTree = rootRoute.addChildren([
  loginRoute,
  protectedRoute.addChildren([workspaceEntryRoute, overviewRoute, projectsRoute, projectOverviewRoute, projectAppsRoute, projectEnvironmentsRoute, appOverviewRoute, appSourceRoute, appBuildsRoute, buildDetailRoute, appReleasesRoute, deploymentsRoute, newDeploymentRoute, detailDeploymentRoute, editDeploymentRoute, settingsRoute, githubSettingsRoute, newWorkspaceRoute, legacyDeploymentsRoute, legacyAdminRoute]),
]);

export function createAppRouter(queryClient: QueryClient, history: ReturnType<typeof createBrowserHistory> = createBrowserHistory()) {
  return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: "intent" });
}

export { routeTree };
