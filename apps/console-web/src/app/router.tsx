import { Outlet, createRootRouteWithContext, createRoute, createRouter, createBrowserHistory, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

import { AppOverviewPage } from "../features/apps/AppOverviewPage";
import { AppSourcePage } from "../features/apps/AppSourcePage";
import { LoginPage } from "../features/auth/LoginPage";
import { EnvironmentAppBuildsPage, EnvironmentAppDeploymentsPage, EnvironmentAppOverviewPage, EnvironmentAppSettingsPage, EnvironmentAppsPage, EnvironmentBuildDetailPage } from "../features/environments/EnvironmentPages";
import { ProjectEntryPage } from "../features/environments/ProjectEntryPage";
import { sessionQueryOptions } from "../features/auth/model";
import { OverviewPage } from "../features/overview/OverviewPage";
import { ParametersPage } from "../features/parameters/ParametersPage";
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
    if (workspaceId) throw redirect({ to: "/workspaces/$workspaceId/overview", params: { workspaceId } });
    throw redirect({ to: "/" });
  }
}

const workspaceEntryRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/", component: WorkspaceEntryPage });
const overviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/overview", component: OverviewPage });
const projectsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects", component: ProjectsPage });
const parametersRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/parameters", component: ParametersPage });
const projectEntryRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId", component: ProjectEntryPage });
const projectOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/settings", component: ProjectOverviewPage });
const projectAppsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/settings/apps", component: ProjectAppsPage });
const projectEnvironmentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/settings/environments", component: ProjectEnvironmentsPage });
const environmentAppsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", component: EnvironmentAppsPage });
const environmentAppOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId", component: EnvironmentAppOverviewPage });
const environmentAppBuildsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds", component: EnvironmentAppBuildsPage });
const environmentBuildDetailRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds/$buildId", component: EnvironmentBuildDetailPage });
const environmentAppDeploymentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/deployments", component: EnvironmentAppDeploymentsPage });
const environmentAppSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings", component: EnvironmentAppSettingsPage });
const appOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", component: AppOverviewPage });
const appSourceRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/source", component: AppSourcePage });
const retiredDeploymentsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "/workspaces/$workspaceId/deployments",
  beforeLoad: ({ params }) => { throw redirect({ to: "/workspaces/$workspaceId/overview", params: { workspaceId: params.workspaceId }, replace: true }); },
});
const settingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings", component: WorkspaceSettingsPage });
const githubSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/github", component: GitHubSettingsPage });
const newWorkspaceRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/new", beforeLoad: ({ context }) => requireOwner(context), component: NewWorkspacePage });
const legacyDeploymentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/deployments", component: LegacyDeploymentsEntryPage });
const legacyAdminRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/admin", component: LegacySettingsEntryPage });

const routeTree = rootRoute.addChildren([
  loginRoute,
  protectedRoute.addChildren([workspaceEntryRoute, overviewRoute, projectsRoute, parametersRoute, projectEntryRoute, projectOverviewRoute, projectAppsRoute, projectEnvironmentsRoute, environmentAppsRoute, environmentAppOverviewRoute, environmentAppBuildsRoute, environmentBuildDetailRoute, environmentAppDeploymentsRoute, environmentAppSettingsRoute, appOverviewRoute, appSourceRoute, retiredDeploymentsRoute, settingsRoute, githubSettingsRoute, newWorkspaceRoute, legacyDeploymentsRoute, legacyAdminRoute]),
]);

export function createAppRouter(queryClient: QueryClient, history: ReturnType<typeof createBrowserHistory> = createBrowserHistory()) {
  return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: "intent" });
}

export { routeTree };
